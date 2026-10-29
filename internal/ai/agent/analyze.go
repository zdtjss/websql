// analyze.go — analyze_data 子 agent。
//
// 采用 adk.NewAgentTool 的增量方式：子 agent 在自己的上下文里完成多步取数/检索，
// 只把结论与证据返回主 agent。它复用 PermissionMiddleware、ToolErrorRecovery、
// reduction 与 dataset 中间件，避免成为绕过权限的旁路。
package agent

import (
	"context"
	"errors"
	"os"
	"time"

	"websql/internal/ai/agent/export"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const dataAnalystInstruction = `你是数据分析子 agent，负责在隔离上下文中完成多步数据探索。
规则：
1. 只返回结论、证据（SQL/行号/统计）与局限，不要返回原始明细。
2. 优先使用 dataset_query 在数据集上聚合/过滤/采样；没有 datasetId 时用 query_data，并检查 truncated/notice。
3. 上传文件先用 read_file_data 的 outline/search/profile，再按 unit/offset 精读；不要假设一次读取拿到全文。
4. 被截断或覆盖不足时继续取数、改用聚合，或在结论中明确说明范围与局限。
5. 不执行写操作，不访问未授权表；无法确认时直接说明。`

// buildAnalyzeDataTool 构造 analyze_data 子 agent 工具。
// 仅在能成功创建子 agent 时返回；失败由调用方降级为不注册。
func buildAnalyzeDataTool(
	ctx context.Context,
	cm model.ToolCallingChatModel,
	connID, dbType, dbSchema string,
	schemas []SchemaRef,
	auditCtx *ExecAuditCtx,
	scope *PermissionScope,
	permAgentTool tool.BaseTool,
	reductionMW, datasetMW adk.ChatModelAgentMiddleware,
) (tool.BaseTool, error) {
	if cm == nil || scope == nil {
		return nil, errors.New("子 agent 缺少模型或权限上下文")
	}

	queryTool, qErr := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询；可用 maxRows 请求更大结果集，服务端有硬上限。返回 rowCount/truncated/notice。", NewQueryFunc(connID, schemas, auditCtx, scope.UserID))
	listTool, lErr := utils.InferTool("list_tables", "获取当前数据库的表名及注释；支持 offset/limit 分页。", NewListTablesFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	schemaTool, sErr := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息。", NewSchemaFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	readFileTool, rfErr := utils.InferTool("read_file_data", "读取上传文件；mode=read|outline|search|profile，返回 sections/pages/sheets/matches/profile。", NewReadFileDataFunc(scope.UserID))
	readOffloadTool, roErr := utils.InferTool("read_offload", "按行读取被截断落盘的 offload 文件；仅当前用户/会话。", NewReadOffloadFunc())
	datasetTool, dErr := utils.InferTool("dataset_query", "在 datasetId 对应的数据集上执行只读 SQL，表名为 result；用于聚合/过滤/采样。", NewDatasetQueryFunc(auditCtx))
	crossJoinTool, cjErr := utils.InferTool("cross_db_join", "跨库关联分析：服务端 Hash Join，只返回统计与少量样本。", NewCrossDBJoinFunc(connID, schemas, auditCtx, scope.UserID))
	for _, err := range []error{qErr, lErr, sErr, rfErr, roErr, dErr, cjErr} {
		if err != nil {
			return nil, err
		}
	}

	subTools := []tool.BaseTool{queryTool, listTool, schemaTool, readFileTool, readOffloadTool, crossJoinTool}
	if os.Getenv("AGENT_DATASET") != "0" {
		subTools = append(subTools, datasetTool)
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 4)
	handlers = append(handlers, &ToolErrorRecoveryMiddleware{})
	if reductionMW != nil {
		handlers = append(handlers, reductionMW)
	}
	if datasetMW != nil {
		handlers = append(handlers, datasetMW)
	}
	handlers = append(handlers, &PermissionMiddleware{Scope: scope, PermAgent: permAgentTool, ConnID: connID, Schemas: schemas})

	// 子 agent 复用上传虚拟 FS 的 read_file/grep/glob，但禁用写工具，保持只读。
	if os.Getenv("AGENT_UPLOAD_FS") != "0" {
		if skillEnv := export.GetSkillEnv(); skillEnv != nil {
			// 与主 agent 一致：文件沙箱按用户隔离。
			userBackend := skillEnv.FilesystemBackend().CloneForUser(scope.UserID)
			fsMW, fsErr := filesystem.New(ctx, &filesystem.MiddlewareConfig{
				Backend:             newUploadFSBackend(userBackend, scope.UserID),
				WriteFileToolConfig: &filesystem.ToolConfig{Disable: true},
				EditFileToolConfig:  &filesystem.ToolConfig{Disable: true},
			})
			if fsErr == nil {
				handlers = append(handlers, fsMW)
			}
		}
	}

	subAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "analyze_data",
		Description:   "大数据集/多步分析子 agent：在自己的上下文里检索、聚合、核对，只返回结论与证据。适合需要大量中间数据、且主流程只关心结论的任务；简单查询不要委托。",
		Instruction:   dataAnalystInstruction,
		Model:         cm,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: subTools}},
		Handlers:      handlers,
		MaxIterations: 12,
	})
	if err != nil {
		return nil, err
	}

	inputSchema := schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"request": {
			Desc:     "要子 agent 完成的分析任务：目标、涉及的数据/文件、期望输出的证据形式（不要粘贴原始明细）",
			Required: true,
			Type:     schema.String,
		},
	})
	base := adk.NewAgentTool(ctx, subAgent, adk.WithAgentInputSchema(inputSchema))
	if invokable, ok := base.(tool.InvokableTool); ok {
		return &timeoutInvokableTool{inner: invokable, timeout: 5 * time.Minute}, nil
	}
	return base, nil
}

// timeoutInvokableTool 给子 agent 增加总时长上限，避免长时间占用主流程。
type timeoutInvokableTool struct {
	inner   tool.InvokableTool
	timeout time.Duration
}

func (t *timeoutInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *timeoutInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	return t.inner.InvokableRun(runCtx, argumentsInJSON, opts...)
}
