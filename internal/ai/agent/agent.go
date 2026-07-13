// package agent 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"websql/internal/ai/agent/export"
	system "websql/internal/app/system"
	idgen "websql/internal/pkg/idgen"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────
// 数据结构
// ──────────────────────────────────────────────

// StreamChunk 流式输出块
type StreamChunk struct {
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
	SQL          string `json:"sql,omitempty"`          // 展示用
	InterruptID  string `json:"interruptId,omitempty"`  // Eino 中断地址 ID
	CheckPointID string `json:"checkPointId,omitempty"` // Runner CheckPoint ID
}

// ChatRequest 聊天请求
type ChatRequest struct {
	SessionID    string      `json:"sessionId"`
	UserID       string      `json:"userId"`
	ConnID       string      `json:"connId"`
	Schema       string      `json:"schema"`
	Schemas      []SchemaRef `json:"schemas,omitempty"` // 多 schema 模式
	Question     string      `json:"question"`
	TableContext []string    `json:"tableContext"`
	Confirmed    bool        `json:"confirmed,omitempty"`
	InterruptIDs []string    `json:"interruptIds,omitempty"` // 确认时回传（支持多条）
	CheckPointID string      `json:"checkPointId,omitempty"` // 确认时回传
	ExcelData    *ExcelData  `json:"excelData,omitempty"`
	ModelId      string      `json:"modelId,omitempty"` // 选中的模型 ID
}

// SchemaRef 单个 schema 引用
type SchemaRef struct {
	ConnID string `json:"connId"`
	Schema string `json:"schema"`
}

// ExcelData 前端上传的文件信息（支持 excel/csv/markdown）
type ExcelData struct {
	FileID    string   `json:"fileId"`
	Columns   []string `json:"columns"`
	TotalRows int      `json:"totalRows"`
	FileType  string   `json:"fileType,omitempty"`  // excel | csv | markdown
	CharCount int      `json:"charCount,omitempty"` // markdown 字符数
}

// SessionMeta 会话列表摘要
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionContext 会话上下文（保存当时选择的 schema 和表）
type SessionContext struct {
	Schemas []SchemaRef `json:"schemas,omitempty"`
	Tables  []string    `json:"tables,omitempty"`
}

// SessionDetail 会话详情
type SessionDetail struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	CreatedAt time.Time              `json:"createdAt"`
	Messages  []SessionDetailMessage `json:"messages"`
	Context   *SessionContext        `json:"context,omitempty"`
}

// SessionDetailMessage 会话消息
type SessionDetailMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ──────────────────────────────────────────────
// SQLAgent + Runner
// ──────────────────────────────────────────────

const (
	maxIterations = 50

	defaultContextTokens = 128000
)

// 全局 CheckPointStore（单实例共享），使用内存存储 + 15 分钟 TTL
var globalCheckPointStore = NewInMemoryCheckPointStore()

// SQLAgent 是工厂缓存中**共享**的实例，可能同时被多个 RunStream 调用。
//
// 关键设计：cancelFuncs 是 map[runID]CancelFunc，因为同一 (connID, dbSchema, userID, schemas)
// 在 factory cache 中只对应一个 SQLAgent，多 tab / 多 SSE 长连接并发时，
// 必须按 runID 区分 cancelFunc，否则后启动的 RunStream 会覆盖前一个的，
// 造成 Cancel() 错位（参见 EINO_DEEP_ANALYSIS §6.2）。
type SQLAgent struct {
	runner           *adk.Runner
	agent            *adk.ChatModelAgent
	sessions         *SessionStore
	dbType           string
	dbSchema         string
	dbVersion        string
	scope            *PermissionScope
	schemas          []SchemaRef
	maxContextTokens int
	cancelMu         sync.Mutex
	cancelFuncs      map[string]adk.AgentCancelFunc // runID -> cancel
	sessionSync      *SessionSyncMiddleware         // Eino v0.9 会话同步中间件
}

// registerCancel 注册当前 run 的 cancelFunc。
// runID 必须全局唯一（建议用 sessionID + 时间戳）。
func (a *SQLAgent) registerCancel(runID string, fn adk.AgentCancelFunc) {
	if fn == nil {
		return
	}
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if a.cancelFuncs == nil {
		a.cancelFuncs = make(map[string]adk.AgentCancelFunc)
	}
	a.cancelFuncs[runID] = fn
}

// unregisterCancel 注销 cancelFunc（runStream 退出时调用）
func (a *SQLAgent) unregisterCancel(runID string) {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if a.cancelFuncs != nil {
		delete(a.cancelFuncs, runID)
	}
}

// Cancel 按 runID 取消单个 run。
// 不带 runID 时取消所有 run（向后兼容）。
func (a *SQLAgent) Cancel(runID ...string) {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if len(runID) == 0 {
		for id, fn := range a.cancelFuncs {
			fn()
			delete(a.cancelFuncs, id)
		}
		return
	}
	for _, id := range runID {
		if fn, ok := a.cancelFuncs[id]; ok {
			fn()
			delete(a.cancelFuncs, id)
		}
	}
}

// RunStream 流式执行（首次查询）
// RunStream 执行一次对话
//
// runID: 用于 Cancel 精确指定要取消的运行（多 SSE / 多 tab 并发时使用）。
//
// 返回 sessionID 与 nil error（兼容旧签名）。实际错误已通过 flush 推给前端。
func (a *SQLAgent) RunStream(ctx context.Context, runID string, req ChatRequest, flush func(StreamChunk)) (string, error) {
	log.Printf("[Agent] 开始执行 - sessionID=%s, userID=%s, connID=%s\n", req.SessionID, req.UserID, req.ConnID)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = idgen.RandomStr()
		log.Printf("[Agent] 新建会话 - sessionID=%s\n", sessionID)
	}
	if req.UserID == "" {
		return "", errors.New("userId 不能为空")
	}

	sess, err := a.sessions.GetOrCreate(sessionID, req.UserID)
	if err != nil {
		return "", err
	}
	flush(StreamChunk{Type: "session", Content: sess.ID})

	if err := sess.Append("user", req.Question); err != nil {
		return sessionID, err
	}

	allMsgs := sess.GetMessages()
	log.Printf("[Agent] 历史消息 - total=%d, maxContextTokens=%d\n", len(allMsgs), a.maxContextTokens)

	if !a.scope.HasAnyAccess() {
		flush(StreamChunk{Type: "error", Content: "您暂时没有可访问的数据表权限，请联系管理员开通。"})
		flush(StreamChunk{Type: "done"})
		return sessionID, nil
	}

	defaultConnID := req.ConnID
	if defaultConnID == "" && len(req.Schemas) > 0 {
		defaultConnID = req.Schemas[0].ConnID
	}
	sysPrompt := buildSystemPrompt(defaultConnID, a.dbType, a.dbSchema, a.dbVersion, req.TableContext, a.scope, req.Schemas, export.GetSkillEnv() != nil)

	if detectPreviousExecution(allMsgs) {
		sysPrompt += "\n\n## 📌 上一轮有查询或写入操作。当用户追问、要求重新操作、要求导出时，" +
			"你必须基于对话历史中实际执行的 tool_calls 参数和 tool 返回结果来回答，" +
			"禁止凭记忆编造。如果历史中没有相关信息，直接告知用户，禁止猜测。\n"
		log.Printf("[Agent] 检测到历史执行记录，追加历史引导\n")
	}

	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		if req.ExcelData.FileType == "markdown" {
			sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Markdown 文档（fileId=%s）：共 %d 字符。\n",
				req.ExcelData.FileID, req.ExcelData.CharCount)
			sysPrompt += "该文件为文本文档，仅支持内容分析/解读/结合数据库分析，不支持导入数据库。" +
				"需要查看内容时调用 read_file_data 读取全文（不做数据量限制）。\n"
		} else {
			ftLabel := "数据文件"
			switch req.ExcelData.FileType {
			case "csv":
				ftLabel = "CSV 文件"
			case "excel":
				ftLabel = "Excel 文件"
			}
			sysPrompt += fmt.Sprintf("\n\n📎 用户上传了%s（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
				ftLabel, req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
			sysPrompt += "用户上传文件的目的可能是：① 仅对文件内数据进行分析/统计/可视化；② 结合数据库表进行关联分析；③ 导入数据库。" +
				"请根据用户提问判断意图：\n" +
				"- 分析/统计/可视化文件数据：调用 read_file_data 读取数据后分析，禁止执行导入。\n" +
				"- 结合数据库表分析：可同时调用 read_file_data 与 query_data 进行关联分析。\n" +
				"- 要求导入数据库：告知用户数据导入请到「表数据浏览」页面操作（在左侧树中右键目标表→浏览数据→工具栏导入按钮），支持 Excel/CSV/JSON 导入及字段映射预览。\n"
		}
	}

	// 构建 Eino 消息列表
	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
	}
	for _, msg := range allMsgs {
		switch msg.Role {
		case "user":
			messages = append(messages, &schema.Message{Role: schema.User, Content: msg.Content})
		case "assistant":
			sm := &schema.Message{Role: schema.Assistant, Content: msg.Content}
			if len(msg.ToolCalls) > 0 {
				sm.ToolCalls = sessionToolCallsToSchema(msg.ToolCalls)
			}
			messages = append(messages, sm)
		case "tool":
			messages = append(messages, &schema.Message{
				Role:       schema.Tool,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
				ToolName:   msg.ToolName,
			})
		}
	}

	log.Printf("[Agent] LLM 输入消息 - total=%d\n", len(messages))
	for i, msg := range messages {
		role := msg.Role
		contentLen := len(msg.Content)
		tcCount := len(msg.ToolCalls)
		toolCallID := msg.ToolCallID
		toolName := msg.ToolName
		if role == schema.System {
			log.Printf("[Agent]   msg[%d] - role=%s, contentLen=%d\n", i, role, contentLen)
		} else {
			log.Printf("[Agent]   msg[%d] - role=%s, contentLen=%d, toolCalls=%d, toolCallID=%s, toolName=%s\n",
				i, role, contentLen, tcCount, toolCallID, toolName)
		}
	}

	checkPointID := fmt.Sprintf("cp_%s_%d", sessionID, time.Now().UnixMilli())
	// 优先使用外部传入的 runID（用于 Cancel 精确指定），未传时回退到 sessionID+ts
	if runID == "" {
		runID = fmt.Sprintf("%s_%d", sessionID, time.Now().UnixNano())
	}

	// 使用 Eino v0.9 Agent Cancel：通过 WithCancel 获取 AgentRunOption 和 CancelFunc，
	// 支持安全点取消（等待当前工具调用完成后再取消）。
	// 按 runID 注册，多 SSE / 多 tab 并发时不会互相覆盖。
	cancelOpt, cancelFn := adk.WithCancel()
	a.registerCancel(runID, cancelFn)
	defer a.unregisterCancel(runID)

	// 绑定 SessionSyncMiddleware，使 summarization 压缩后的消息能同步到 Session
	if a.sessionSync != nil {
		a.sessionSync.SetSession(sess, len(allMsgs))
	}

	iter := a.runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID), cancelOpt)

	_, _ = a.processEvents(iter, flush, sess, checkPointID)

	// 清理 session 绑定（cancelFunc 已经在 defer unregisterCancel 中清理）
	if a.sessionSync != nil {
		a.sessionSync.ClearSession()
	}

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] 保存会话失败 - err=%v\n", err)
	}

	log.Printf("[Agent] 执行完毕 - sessionID=%s\n", sessionID)

	return sessionID, nil
}

// ResumeStream 恢复被中断的执行（用户确认/取消后）
// 当再次被中断时（如 LLM 生成了新的危险 SQL），不发送 done，让前端继续等待用户确认
func (a *SQLAgent) ResumeStream(ctx context.Context, checkPointID string, targets map[string]bool, flush func(StreamChunk), sess *Session) error {
	log.Printf("[Agent] resume 开始 - cpID=%s, targets=%v, sessionMsgs=%d\n", checkPointID, targets, len(sess.messages))

	targetsAny := make(map[string]any, len(targets))
	for id, approved := range targets {
		targetsAny[id] = SQLApprovalResult{Approved: approved}
	}

	resumeStart := time.Now()
	iter, err := a.runner.ResumeWithParams(ctx, checkPointID, &adk.ResumeParams{
		Targets: targetsAny,
	})
	if err != nil {
		return fmt.Errorf("resume failed: %w", err)
	}
	log.Printf("[Agent] resume ResumeWithParams 返回 - elapsed=%v\n", time.Since(resumeStart))

	_, _ = a.processEvents(iter, flush, sess, checkPointID)

	log.Printf("[Agent] resume processEvents 完成 - totalElapsed=%v, sessionMsgs=%d\n", time.Since(resumeStart), len(sess.messages))

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] save assistant msg failed - err=%v\n", err)
	}

	return nil
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

func detectPreviousExecution(msgs []SessionMessage) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// authTransport 实现了 http.RoundTripper 接口
type authTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clonedReq := req.Clone(req.Context())
	if clonedReq.Header == nil {
		clonedReq.Header = make(http.Header)
	}
	clonedReq.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(clonedReq)
}

func NewAuthClient(token string) *http.Client {
	return &http.Client{
		Transport: &authTransport{token: token, transport: http.DefaultTransport},
	}
}

// ──────────────────────────────────────────────
// 模型与工具构建
// ──────────────────────────────────────────────

var ollamaURLPrefix = "https://ollama.com"

func isOllamaURL(baseURL string) bool {
	if strings.HasPrefix(strings.ToLower(baseURL), ollamaURLPrefix) {
		return true
	}
	return false
}

func BuildChatModel(ctx context.Context, cfg *system.AIConfig) (model.ToolCallingChatModel, error) {
	log.Printf("[ChatModel] 初始化 - provider=%s, model=%s\n", cfg.Provider, cfg.Model)

	var cm model.ToolCallingChatModel
	var err error

	switch cfg.Provider {
	case "ollama":
		ollamaCfg := &ollama.ChatModelConfig{
			BaseURL: cfg.BaseURL, Model: cfg.Model, Timeout: 30 * time.Minute,
		}
		if cfg.EnableThinking {
			ollamaCfg.Thinking = &ollama.ThinkValue{Value: true}
		}
		if cfg.Temperature > 0 {
			ollamaCfg.Options = &ollama.Options{Temperature: cfg.Temperature}
		}
		ollamaCfg.HTTPClient = NewAuthClient(cfg.ApiKey)
		cm, err = ollama.NewChatModel(ctx, ollamaCfg)
	case "openai":
		openaiCfg := &openai.ChatModelConfig{
			BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.ApiKey,
			Timeout: 30 * time.Minute,
		}
		if cfg.Temperature > 0 {
			temp := cfg.Temperature
			openaiCfg.Temperature = &temp
		}
		cm, err = openai.NewChatModel(ctx, openaiCfg)
	default:
		return nil, fmt.Errorf("不支持的 AI 提供商：%s", cfg.Provider)
	}

	if err != nil {
		return nil, err
	}

	// 1. toolCallIndexFixerModel：在流输出层面修复 ToolCall Index 冲突
	// 2. loggingToolCallingChatModel：输出模型调用日志（输入/输出概要、耗时、chunk 统计）
	return newLoggingModel(&toolCallIndexFixerModel{ToolCallingChatModel: cm}), nil
}

func buildTools(_ context.Context, connID, dbType, dbSchema string, schemas []SchemaRef, auditCtx *ExecAuditCtx, scope *PermissionScope) (coreTools []tool.BaseTool, deferredTools []tool.BaseTool, err error) {
	conn, _ := GetConn(connID, scope.UserID)
	queryTool, qErr := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", NewQueryFunc(connID, schemas, auditCtx, scope.UserID))
	schemaTool, sErr := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息，支持一次传入多个表名", NewSchemaFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	listTablesTool, lErr := utils.InferTool("list_tables", "获取当前数据库的所有表名及表注释", NewListTablesFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	currentDateInfoTool, dErr := utils.InferTool("get_current_date_info", "获取当前日期、星期几和时间", GetCurrentDateInfo())
	// read_file_data 为只读工具，用于分析用户上传的数据文件，不依赖写权限，常驻核心工具
	readFileTool, rfErr := utils.InferTool("read_file_data", "读取已上传的数据文件内容（只读，用于数据分析，不会导入数据库）。可传 limit(默认100,最大500)/offset 分页读取", NewReadFileDataFunc())

	for _, t := range []tool.BaseTool{queryTool, schemaTool, listTablesTool, currentDateInfoTool, readFileTool} {
		if t != nil {
			coreTools = append(coreTools, t)
		}
	}
	if qErr != nil || sErr != nil || lErr != nil || dErr != nil || rfErr != nil {
		return nil, nil, fmt.Errorf("创建核心工具失败：query=%v schema=%v list=%v date=%v readFile=%v", qErr, sErr, lErr, dErr, rfErr)
	}

	// 导出工具均为 Go 原生兜底实现：当 Python Skill 不可用或失败时使用。
	// Agent 生成 Word/PPT/HTML 报告时，应优先调用 skill 工具加载对应 SKILL.md，
	// 按其指引组装数据并执行 Python 脚本生成专业产物；
	// 若 Python 不可用或脚本执行失败，再回退到这些原生工具。
	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 表格数据（Go 原生实现），须传入 sql 参数", export.NewExportExcelFunc(conn))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel（Go 原生实现），图表类型根据数据特征自动选择", export.NewExportExcelWithChartFunc(conn))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿（Go 原生兜底实现，基础版）。若需专业科技感 PPT，请优先用 skill 工具加载 export-ppt 技能。优先使用 content 模式避免重复查询", export.NewExportPPTFunc(conn))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成数据分析报告 Word（Go 原生兜底实现，基础版）。若需专业科技感 Word 报告（含封面/目录/KPI/图表），请优先用 skill 工具加载 export-word 技能。优先使用 content 模式", export.NewExportAnalysisDocxFunc(conn))
	exportHTMLTool, _ := utils.InferTool("export_html", "生成 HTML 报告（Go 原生实现，支持 Markdown、Mermaid 图表交互、代码高亮、数学公式）。优先使用 content 模式。也可先用 skill 工具加载 export-html 技能获取高级用法", export.NewExportHTMLFunc(conn))

	for _, t := range []tool.BaseTool{exportExcelTool, exportExcelChartTool, exportPPTTool, exportDocxTool, exportHTMLTool} {
		if t != nil {
			deferredTools = append(deferredTools, t)
		}
	}

	if scope.AllowModify {
		execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID, schemas, auditCtx, scope.UserID))
		if execTool != nil {
			deferredTools = append(deferredTools, execTool)
		}
	}

	return coreTools, deferredTools, nil
}

// ──────────────────────────────────────────────
// 系统提示词
// ──────────────────────────────────────────────

// estimateTokenCount 估算文本的 token 数量。
// 提供比默认 4 chars/token 更精确的估算，针对中英文混合文本优化。
func estimateTokenCount(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
	total := 0
	for _, msg := range input.Messages {
		total += estimateTextTokens(msg.Content)
		for _, tc := range msg.ToolCalls {
			total += estimateTextTokens(tc.Function.Name)
			total += estimateTextTokens(tc.Function.Arguments)
			total += 4
		}
		if msg.ToolCallID != "" {
			total += estimateTextTokens(msg.ToolCallID) + 3
		}
		if msg.ToolName != "" {
			total += estimateTextTokens(msg.ToolName) + 3
		}
		if msg.Name != "" {
			total += estimateTextTokens(msg.Name) + 3
		}
		total += 6
	}
	for _, tool := range input.Tools {
		total += estimateTextTokens(tool.Desc)
	}
	total = total * 115 / 100
	return total, nil
}

func estimateTextTokens(text string) int {
	total := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		ch := runes[i]
		if ch >= 0x4E00 && ch <= 0x9FFF {
			total += 2
			i++
		} else if ch >= 0x3000 && ch <= 0x303F {
			total += 2
			i++
		} else if ch >= 0xFF00 && ch <= 0xFFEF {
			total += 2
			i++
		} else if ch >= 0x2000 && ch <= 0x206F {
			total += 1
			i++
		} else if ch >= 0x0080 {
			total += 2
			i++
		} else if ch == ' ' || ch == '\n' || ch == '\t' {
			total += 1
			i++
		} else {
			segLen := 1
			for j := i + 1; j < len(runes) && runes[j] < 0x0080 && runes[j] != ' ' && runes[j] != '\n' && runes[j] != '\t'; j++ {
				segLen++
			}
			total += (segLen + 3) / 4
			i += segLen
		}
	}
	if total == 0 && len(text) > 0 {
		total = (len(text) + 3) / 4
	}
	return total
}
