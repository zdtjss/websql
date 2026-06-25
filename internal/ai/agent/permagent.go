package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func init() {
	schema.RegisterName[*PermDecisionOutput]("permission.PermDecisionOutput")
}

func NewPermissionAgent(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, userID string, schemaNames []string) (*adk.ChatModelAgent, error) {
	cm, err := BuildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create permission agent model failed: %w", err)
	}

	tools, err := buildPermissionAgentTools(ctx, connID, userID, dbSchema, schemaNames)
	if err != nil {
		return nil, fmt.Errorf("create permission agent tools failed: %w", err)
	}

	sysPrompt := buildPermissionAgentPrompt(dbType, dbSchema, userID, connID)

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "PermissionAgent",
		Description: "数据权限审核专家，判断SQL操作是否在用户授权范围内",
		Instruction: sysPrompt,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			// PermToolCallLoggingMiddleware 记录权限 Agent 内部工具调用日志
			// （get_table_structure / get_user_permissions），便于排查权限判断过程
			&PermToolCallLoggingMiddleware{},
		},
		MaxIterations: 8,
	})
	if err != nil {
		return nil, fmt.Errorf("create PermissionAgent failed: %w", err)
	}

	return agent, nil
}

func buildPermissionAgentTools(_ context.Context, connID, userID, dbSchema string, schemaNames []string) ([]tool.BaseTool, error) {
	tableStructureTool, _ := utils.InferTool("get_table_structure",
		"获取指定表的结构信息（列名、类型、是否可空等）。用于了解SQL中涉及的表有哪些",
		newGetTableStructureFunc(connID, dbSchema, userID))

	userPermsTool, _ := utils.InferTool("get_user_permissions",
		"获取当前用户在此数据库连接上的数据权限配置。返回连接级、表级、字段级的授权范围",
		newGetUserPermissionsFunc(userID, connID, schemaNames))

	allTools := []tool.BaseTool{tableStructureTool, userPermsTool}
	var validTools []tool.BaseTool
	for _, t := range allTools {
		if t != nil {
			validTools = append(validTools, t)
		}
	}
	return validTools, nil
}

func buildPermissionAgentPrompt(dbType, dbSchema, userID, connID string) string {
	var sb strings.Builder

	sb.WriteString(`你是数据权限审核专家，职责是判断用户提交的 SQL 是否在其授权的数据范围内。

## 输入格式
你收到的请求格式固定如下：
  检查以下SQL的权限
  工具名称：<query_data | exec_sql | export>
  SQL语句：
  <SQL>

你需要调用工具获取表结构和用户权限信息后，给出是否允许的判断。

## 权限层级（由高到低，高级别可覆盖低级别）
- 级别1 - 连接级(conn)：拥有该连接下所有库、表、字段的完整访问权限（hasFullConnAccess=true）
- 级别2 - 库级(schema)：拥有该库下所有表、字段的完整访问权限（hasFullSchemaAccess=true）
- 级别3 - 表级(table)：拥有该表所有字段的完整访问权限（accessLevel=full）
- 级别4 - 字段级(column)：仅拥有该表指定字段的访问权限（accessLevel=column, allowedColumns非空）

## 最具体优先原则（核心规则）
当同一连接下同时存在高级别与低级别权限时，以最具体的权限配置为准：
- 若存在表级或字段级权限，则连接级/库级权限自动降级，不得越权放行
- 例如：用户对 orders 表有 column 级权限 [id,name]，即使同时有 schema 级权限，也只能访问 orders 表的 id 和 name 两列

## 工作流程（严格按顺序执行）

### 步骤1：解析 SQL
从 SQL 中提取所有涉及的表和字段：
- SELECT：提取 FROM / JOIN / 子查询中的表，以及 SELECT 子句中的字段
- INSERT：提取目标表名和 VALUES / SELECT 中的字段
- UPDATE：提取目标表名和 SET 子句中的字段
- DELETE：提取目标表名
- DDL（CREATE/ALTER/DROP/TRUNCATE）：提取目标表名
- 注意：WITH 定义的 CTE 名称不属于需检查的表；子查询中的表必须逐层检查
- 注意：若 SQL 中包含 SELECT * 或 table.*，识别为"所有字段"，单独标记
- 注意：若表名有 schema 前缀（如 schema.table），提取纯表名用于匹配

### 步骤2：查询表结构
调用 get_table_structure，传入所有提取到的表名列表（支持一次传入多个表名）。
- 若表不存在（exists=false），该表无需权限检查，直接跳过

### 步骤3：查询用户权限
调用 get_user_permissions 获取用户的权限配置。
- 若 tablePermissions 为空且 hasFullConnAccess/hasFullSchemaAccess 均为 false，说明用户无任何权限，所有操作均应拒绝

### 步骤4：逐表逐字段比对
按以下规则判断每个表的每个字段：
- hasFullConnAccess=true：全部允许
- hasFullSchemaAccess=true：全部允许（但仅限当前 schema）
- 表级权限(accessLevel=full)：该表全部字段允许
- 字段级权限(accessLevel=column)：仅 allowedColumns 内的字段允许，其他字段拒绝；将拒绝字段写入 deniedColumns
- 不在权限列表中的表：拒绝，写入 deniedTables

## 特殊规则
- SELECT * 拦截：若用户仅有字段级权限(column)，且 SQL 中使用 SELECT * 或 SELECT table.*，必须拒绝。原因：* 会查出未授权字段。deniedColumns 填入 ["*"] 或 ["table.*"]
- 视图(View)：按表级规则处理，检查视图名是否在权限列表中
- 多表 JOIN：每个表独立检查，任一表无权限即拒绝该表
- UNION 查询：分别检查每个 SELECT 分支
- 系统表/信息模式表（如 information_schema.*、pg_catalog.*、sqlite_master）：直接放行
- 不检查 SQL 语法正确性，只检查数据访问权限

## 工具名称语义
- query_data：只读查询工具，检查 SELECT 涉及的所有表和字段
- exec_sql：写操作工具，检查 INSERT/UPDATE/DELETE/DDL 涉及的表和字段
- export：数据导出工具，与 query_data 类似，检查 SELECT 涉及的表和字段

## 性能要求
- get_table_structure 支持一次传入多个表名，请合并调用，不要逐表查询
- get_user_permissions 只需调用一次即可获取所有权限配置
- 整个权限检查应在 2-3 次工具调用内完成（1 次表结构 + 1 次权限查询）

## 输出格式（严格遵守）
只输出纯 JSON 对象，不得包含：
- 任何其他文字说明
- Markdown 代码块标记（如 ` + "```json" + `）
- JSON 以外的任何内容

当权限检查通过时：
{
  "allowed": true,
  "deniedTables": [],
  "deniedColumns": [],
  "reason": "权限检查通过：具体的通过原因（如：用户拥有表 orders 的完整权限）"
}

当权限不足时：
{
  "allowed": false,
  "deniedTables": ["无权限的表名"],
  "deniedColumns": ["表名.字段名", "表名.字段名"],
  "reason": "具体且完整的拒绝原因（列出每个被拒绝的表和字段及其原因）"
}

### reason 字段要求
- allowed=true 时：说明通过的权限级别和涉及的表
- allowed=false 时：必须逐个列出每个被拒绝的表名和字段名，说明原因
- 若用户有字段级权限(column)，reason 中必须明确区分"授权字段"和"拒绝字段"

## 判断速查表
| 条件 | 判定 |
|------|------|
| hasFullConnAccess=true | 全部允许 |
| hasFullSchemaAccess=true | 全部允许 |
| 表在 AllowedTables 中 | 该表全部字段允许 |
| 表在 AllowedColumns 中 | 仅 allowedColumns 内的字段允许，其余拒绝 |
| 表不在任何权限列表中 | 拒绝 |
| column权限 + SELECT * | 必须拒绝 |

## 判断示例
示例1：用户有 orders 表的 column 级权限 [id, name, amount]，SQL 为 SELECT id, name, amount FROM orders
→ allowed=true，reason="用户拥有 orders 表 id/name/amount 字段的访问权限"

示例2：用户有 orders 表的 column 级权限 [id, name]，SQL 为 SELECT * FROM orders
→ allowed=false，deniedColumns=["*"]，reason="用户仅有 orders 表的 [id,name] 字段权限，SELECT * 会暴露未授权字段"

示例3：用户有 schema 级权限，SQL 为 SELECT id FROM orders
→ allowed=true，reason="用户拥有 schema 级完整权限，可访问 orders 表"

示例4：用户有 orders 表 full 权限和 products 表无权限，SQL 为 SELECT o.id, p.name FROM orders o JOIN products p
→ allowed=false，deniedTables=["products"]，reason="orders 表有完整权限；products 表不在权限列表中"

`)
	fmt.Fprintf(&sb, "## 上下文信息\n")
	fmt.Fprintf(&sb, "- 数据库类型：%s\n", dbType)
	fmt.Fprintf(&sb, "- 默认 Schema：%s\n", dbSchema)
	fmt.Fprintf(&sb, "- 用户 ID：%s\n", userID)
	fmt.Fprintf(&sb, "- 连接 ID：%s\n", connID)

	return sb.String()
}

func callPermissionAgent(ctx context.Context, permAgent tool.BaseTool, sql, toolName string) (*PermDecisionOutput, error) {
	invokable, ok := permAgent.(tool.InvokableTool)
	if !ok {
		return nil, errors.New("permission agent does not support invokable mode")
	}

	request := fmt.Sprintf("检查以下SQL的权限\n工具名称：%s\nSQL语句：\n%s", toolName, sql)

	type agentToolInput struct {
		Request string `json:"request"`
	}
	input := agentToolInput{Request: request}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal permission input failed: %w", err)
	}

	startTime := time.Now()
	sqlPreview := sql
	if len(sqlPreview) > 200 {
		sqlPreview = sqlPreview[:200] + "...(truncated)"
	}
	sqlPreview = strings.ReplaceAll(sqlPreview, "\n", " ")
	log.Printf("[PermAgent] 开始权限检查 - tool=%s, sqlLen=%d, sql=%s\n", toolName, len(sql), sqlPreview)

	result, err := invokable.InvokableRun(ctx, string(inputJSON))
	elapsed := time.Since(startTime)
	if err != nil {
		log.Printf("[PermAgent] 权限检查失败 - tool=%s, duration=%v, err=%v\n", toolName, elapsed, err)
		return nil, fmt.Errorf("permission agent run failed: %w", err)
	}

	log.Printf("[PermAgent] 权限检查完成 - tool=%s, duration=%v, resultLen=%d\n", toolName, elapsed, len(result))

	decision, err := unmarshalPermDecision(result)
	if err != nil {
		rawPreview := result
		if len(rawPreview) > 300 {
			rawPreview = rawPreview[:300] + "...(truncated)"
		}
		log.Printf("[PermAgent] 结果解析失败 - tool=%s, duration=%v, err=%v, raw=%s\n", toolName, elapsed, err, rawPreview)
		return nil, fmt.Errorf("parse permission agent result failed: %w, raw=%s", err, result)
	}

	if decision.Allowed {
		log.Printf("[PermAgent] 权限检查通过 - tool=%s, duration=%v, reason=%s\n", toolName, elapsed, decision.Reason)
	} else {
		log.Printf("[PermAgent] 权限检查拒绝 - tool=%s, duration=%v, deniedTables=%v, deniedColumns=%v, reason=%s\n",
			toolName, elapsed, decision.DeniedTables, decision.DeniedColumns, decision.Reason)
	}

	return decision, nil
}

// PermToolCallLoggingMiddleware 是 PermissionAgent 专用的工具调用日志中间件。
// 与主 Agent 的 ToolCallLoggingMiddleware 类似，但使用 [PermAgentTool] 前缀，
// 便于在日志中区分权限 Agent 内部工具调用（get_table_structure / get_user_permissions）
// 与主 Agent 的工具调用。
type PermToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *PermToolCallLoggingMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	ctx = context.WithValue(ctx, contextKeyStartTime{}, time.Now())
	ctx = context.WithValue(ctx, contextKeyIteration{}, new(int))
	log.Printf("[PermAgentLifecycle] 权限 Agent 开始执行\n")
	return ctx, runCtx, nil
}

func (m *PermToolCallLoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	if startTime, ok := ctx.Value(contextKeyStartTime{}).(time.Time); ok && !startTime.IsZero() {
		elapsed := time.Since(startTime)
		iterCount := 0
		if ic, ok := ctx.Value(contextKeyIteration{}).(*int); ok && ic != nil {
			iterCount = *ic
		}
		msgCount := 0
		if state != nil {
			msgCount = len(state.Messages)
		}
		log.Printf("[PermAgentLifecycle] 权限 Agent 执行完毕 - 总耗时=%v, 迭代次数=%d, 消息数=%d\n",
			elapsed, iterCount, msgCount)
	}
	return ctx, nil
}

func (m *PermToolCallLoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if _, ok := ctx.Value(contextKeyStartTime{}).(time.Time); !ok {
		ctx = context.WithValue(ctx, contextKeyStartTime{}, time.Now())
		ctx = context.WithValue(ctx, contextKeyIteration{}, new(int))
	}
	if ic, ok := ctx.Value(contextKeyIteration{}).(*int); ok && ic != nil {
		*ic++
		toolCount := 0
		if state != nil && state.ToolInfos != nil {
			toolCount = len(state.ToolInfos)
		}
		msgCount := 0
		if state != nil {
			msgCount = len(state.Messages)
		}
		log.Printf("[PermAgentLifecycle] 模型调用 #%d - 消息数=%d, 可见工具数=%d\n", *ic, msgCount, toolCount)
	}
	return ctx, state, nil
}

func (m *PermToolCallLoggingMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		startTime := time.Now()
		iterNum := 0
		if ic, ok := ctx.Value(contextKeyIteration{}).(*int); ok && ic != nil {
			iterNum = *ic
		}
		argsPreview := truncateArgsForLog(argumentsInJSON)
		log.Printf("[PermAgentTool] 开始调用 - iter=%d, name=%s, callID=%s, args=%s\n",
			iterNum, tCtx.Name, tCtx.CallID, argsPreview)

		result, err := endpoint(ctx, argumentsInJSON, opts...)

		elapsed := time.Since(startTime)
		if err != nil {
			log.Printf("[PermAgentTool] 调用失败 - iter=%d, name=%s, callID=%s, duration=%v, err=%v\n",
				iterNum, tCtx.Name, tCtx.CallID, elapsed, err)
		} else {
			resultPreview := truncateResultForLog(result)
			log.Printf("[PermAgentTool] 调用完成 - iter=%d, name=%s, callID=%s, duration=%v, resultLen=%d, result=%s\n",
				iterNum, tCtx.Name, tCtx.CallID, elapsed, len(result), resultPreview)
		}

		return result, err
	}, nil
}
