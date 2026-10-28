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
	admin "websql/internal/app/admin"
	connpkg "websql/internal/app/conn"
	system "websql/internal/app/system"
	idgen "websql/internal/pkg/idgen"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
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
	// Thinking 仅用于 type=retract：需要从思考区末尾撤回的文本
	// （模型流式重试时，失败尝试已推送的内容需要撤回，content 字段承载正文部分）
	Thinking string `json:"thinking,omitempty"`
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
	// Attachments 本轮携带的上传文件引用（多文件，新上传链路）。
	// 与 ExcelData（legacy 单文件）并存：新前端走此处，旧契约保持兼容。
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	ModelId     string          `json:"modelId,omitempty"` // 选中的模型 ID
	// AdvancedExport 高级导出模式：前端工具栏开关开启时为 true，
	// 本次对话强制走 export_by_code（仅 Python、只读临时代码）生成文件。
	AdvancedExport bool `json:"advancedExport,omitempty"`
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

// AttachmentRef 会话请求携带的上传文件引用（新上传链路，多文件）。
//
// 仅 FileID 由客户端提供；其余字段由服务端按上传暂存元数据回填
// （见 handler 侧 SanitizeChatAttachments），避免客户端伪造文件名/类别进入系统提示词。
// 该结构同时用于会话消息持久化（历史回看还原文件卡片）。
type AttachmentRef struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName,omitempty"`
	// FileKind 内容类别：table | text | doc | image | video（fileparse.Kind）
	FileKind string `json:"fileKind,omitempty"`
	FileExt  string `json:"fileExt,omitempty"`
	FileSize int64  `json:"fileSize,omitempty"`
	// ExpireAt 暂存过期时间（秒级时间戳）：历史回看时据此标记「文件已过期」
	ExpireAt int64 `json:"expireAt,omitempty"`
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

// SessionDetailMessage 会话消息（展示用：含文件附件引用，供前端还原文件卡片）
type SessionDetailMessage struct {
	Role        string          `json:"role"`
	Content     string          `json:"content"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
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
	promptMW         *DynamicPromptMiddleware       // per-run 动态系统提示词中间件
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

// recheckSecurityBeforeRun 在历史装配前复核权限修订号。
//
// 语义：
//   - unchanged：沿用当前不可变 Scope；
//   - expanded：权限只增，改用新上下文（不追溯解除已隔离历史）；
//   - shrunk：改用更严格的新 Scope，由 filterHistoryForRun 按新旧 digest 隔离历史；
//   - unavailable：不回退旧 Scope，只标记 RevisionUnavailable 由数据工具 fail-closed。
func (a *SQLAgent) recheckSecurityBeforeRun(req ChatRequest, sec *RunSecurityContext) *RunSecurityContext {
	if sec == nil {
		return sec
	}
	rebuild := func() (*RunSecurityContext, error) {
		revision, fresh := admin.SnapshotPermissionRevision()
		if sec.Profile.RequiresRevisionLease() && !fresh {
			return nil, admin.ErrPermissionRevisionUnavailable
		}
		next, buildErr := BuildRunSecurityContext(sec.Profile, sec.UserID, sec.DefaultConnID, req.Schemas, revision, sec.Masker)
		if buildErr != nil {
			return nil, buildErr
		}
		// 运行内固定属性必须继承：灰度模式一次运行固定；输入风险收缩与脱敏可用性不因复核重置。
		next.Mode = sec.Mode
		next.DataEgressBlocked = sec.DataEgressBlocked
		if sec.RestrictWrites {
			next = next.RestrictForInputRisk()
		}
		return next, nil
	}
	state, next, err := sec.Recheck(rebuild)
	if err != nil || next == nil {
		sec.RevisionUnavailable = true
		recordDegraded("revision", "recheck_failed")
		log.Printf("[Agent] 运行中权限复核失败，数据工具将 fail-closed - session=%s, err=%v\n", req.SessionID, err)
		return sec
	}
	switch state {
	case RevisionShrunk:
		recordSecurityEvent(StageRevision, "shrink", "rebuild")
		log.Printf("[Agent] 运行中权限已收缩，重建 Scope 并隔离受影响历史 - session=%s\n", req.SessionID)
	case RevisionExpanded:
		recordSecurityEvent(StageRevision, "expand", "rebuild")
	case RevisionUnavailable:
		next.RevisionUnavailable = true
		recordDegraded("revision", "unavailable_in_run")
	}
	return next
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

	// 安全上下文必须由请求入口构建：缺失时拒绝运行，不在 Agent 内部自行推断权限。
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return "", err
	}
	scope := a.scope
	if s, scopeErr := sec.DefaultScope(); scopeErr == nil {
		scope = s
	}

	sess, err := a.sessions.GetOrCreate(sessionID, req.UserID)
	if err != nil {
		return "", err
	}
	sess.SetLatestSecurity(sec)
	flush(StreamChunk{Type: "session", Content: sess.ID})

	// 同一会话互斥：多 tab / 重复提交并发运行会交错写消息、破坏 tool_calls 配对
	if !sess.TryBeginRun() {
		log.Printf("[Agent] 会话正在处理中，拒绝并发运行 - sessionID=%s\n", sessionID)
		flush(StreamChunk{Type: "error", Content: "该会话正在处理中，请等待当前回复完成后再发送。"})
		return sessionID, nil
	}
	defer sess.EndRun()

	if err := sess.AppendMessage(SessionMessage{
		Role:        "user",
		Content:     req.Question,
		Attachments: req.Attachments, // 空切片时 omitempty 不落库
		Security:    newMessageSecurity(sec, req.Question, nil),
	}); err != nil {
		return sessionID, err
	}

	// 历史装配前先复核权限修订号：必须在 summarization/reduction 之前完成，
	// 否则无权数据会被摘要压缩后失去资源溯源。
	sec = a.recheckSecurityBeforeRun(req, sec)
	sess.SetLatestSecurity(sec)
	if s, scopeErr := sec.DefaultScope(); scopeErr == nil {
		scope = s
	}
	allMsgs := filterHistoryForRun(sess.GetMessages(), sec)
	log.Printf("[Agent] 历史消息 - total=%d, maxContextTokens=%d\n", len(allMsgs), a.maxContextTokens)

	// 数据来源可为数据库或文件：无数据库权限时，只要本次或历史消息中携带了文件，仍可进行文件分析
	hasHistoryFile := false
	for _, msg := range allMsgs {
		if len(msg.Attachments) > 0 {
			hasHistoryFile = true
			break
		}
	}
	if !scope.HasAnyAccess() && !hasFileDataSource(&req) && !hasHistoryFile {
		// done 事件由 handler 统一发送，这里只推送错误，避免前端收到两次 done
		flush(StreamChunk{Type: "error", Content: "您暂时没有可访问的数据表权限，请选择有权限的数据库或上传文件。"})
		return sessionID, nil
	}

	defaultConnID := req.ConnID
	if defaultConnID == "" && len(req.Schemas) > 0 {
		defaultConnID = req.Schemas[0].ConnID
	}
	sysPrompt := buildSystemPrompt(defaultConnID, a.dbType, a.dbSchema, a.dbVersion, req.TableContext, scope, req.Schemas, export.GetSkillEnv() != nil)

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

	// 新上传链路（多文件）：按类别描述 + 不可信数据声明，文件内容仍按需读取（read_file_data）
	if len(req.Attachments) > 0 {
		sysPrompt += buildAttachmentsPrompt(req.Attachments)
		log.Printf("[Agent] 已注入上传文件上下文 - session=%s, files=%d\n", sessionID, len(req.Attachments))
	}

	// 是否选择了数据库：未选择时数据来源只能是用户上传的文件
	hasDBContext := defaultConnID != "" || len(req.Schemas) > 0

	// 高级导出模式（前端工具栏开关）：本次回复强制走 export_by_code（依赖数据库连接）。
	if req.AdvancedExport && hasDBContext {
		sysPrompt += advancedExportDirectivePrompt()
		log.Printf("[Agent] 高级导出模式已启用 - session=%s\n", sessionID)
	}

	// 纯文件分析：置顶文件模式约束（利用 primacy bias），避免模型沿用数据库工作流调用 DB 工具
	if !hasDBContext && (hasFileDataSource(&req) || hasHistoryFile) {
		sysPrompt = fileOnlyPromptPrefix() + sysPrompt
		log.Printf("[Agent] 未选择数据库，切换为文件分析模式 - session=%s\n", sessionID)
	}

	// 通过动态提示词中间件注入（BeforeAgent 覆盖 Instruction）：
	// 不再在输入消息中手动携带 System 消息，避免与 Instruction 双份注入。
	// 必须在 sysPrompt 全部追加（历史引导/上传文件）完成后设置。
	// 提示词经 context 传入（而非中间件实例字段），保证同一共享 Agent 被并发 Run 时互不干扰。
	ctx = WithRunPrompt(ctx, sysPrompt)
	log.Printf("[Agent] 系统提示词已构建并注入 - len=%d\n", len(sysPrompt))

	// 多模态媒体注入计划（图片/视频）：仅模型具备对应能力时注入，
	// 仅覆盖最近 mediaInjectWindow 条含媒体的用户消息，同一 fileId 只注入一次。
	mediaInjector := newMediaInjector(req.ModelId, req.UserID)
	mediaPlan := planMediaInjection(allMsgs, mediaInjector)
	if len(mediaPlan) > 0 {
		log.Printf("[Media] 媒体注入计划 - session=%s, 消息数=%d, allowImage=%v, allowVideo=%v\n",
			sessionID, len(mediaPlan), mediaInjector.allowImage, mediaInjector.allowVideo)
	}

	// 构建 Eino 消息列表：仅用户/助手/工具消息，系统提示词已由 promptMW 注入 Instruction
	messages := make([]adk.Message, 0, len(allMsgs))
	for i, msg := range allMsgs {
		switch msg.Role {
		case "user":
			// 历史轮次携带的文件引用以紧凑文本附带，保证后续追问仍能拿到 fileId
			// 调用 read_file_data（文件内容不在消息体中，按需读取）。
			content := msg.Content + formatMessageAttachments(msg.Attachments)
			if refs, ok := mediaPlan[i]; ok {
				messages = append(messages, mediaInjector.buildUserMessage(content, refs))
			} else {
				messages = append(messages, &schema.Message{Role: schema.User, Content: content})
			}
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

	log.Printf("[Agent] LLM 输入消息 - total=%d（系统提示词 %d 字符经由 Instruction 注入）\n", len(messages), len(sysPrompt))
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

	// 绑定本次 Run 的会话同步状态（经 context 传入，避免共享 Agent 上的状态串号）
	if a.sessionSync != nil {
		ctx = WithRunSessionState(ctx, newRunSessionState(sess, len(allMsgs)))
	}
	// 本次运行的会话/用户经 context 传给工具（审计记录使用真实 SessionID，
	// 而不是缓存 Agent 构建时烘焙进工具的旧值）
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: sessionID, UserID: req.UserID})

	// 登记 checkpoint 归属：恢复时校验用户/会话/连接与权限快照一致，并保证一次性消费
	globalCheckPointStore.Bind(checkPointID, CheckpointBinding{
		UserID:      req.UserID,
		SessionID:   sessionID,
		ConnID:      defaultConnID,
		Revision:    sec.Revision,
		ScopeDigest: sec.ScopeDigest,
	})

	iter := a.runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID), cancelOpt)

	interrupted := a.processEvents(ctx, runID, iter, flush, sess, checkPointID)
	// 未停在「等待确认」上的 checkpoint（含取消时保存的不可恢复状态）立即释放
	globalCheckPointStore.Release(checkPointID, interrupted)

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] 保存会话失败 - err=%v\n", err)
	}

	log.Printf("[Agent] 执行完毕 - sessionID=%s, awaitingConfirm=%v\n", sessionID, interrupted)

	return sessionID, nil
}

// ResumeStream 恢复被中断的执行（用户确认/取消后）。
//
// 安全语义：
//   - BeginResume 校验 checkpoint 归属（用户 + 会话 + 连接）并加「恢复中」标记，
//     并发重复提交直接拒绝；
//   - EndResume 在恢复结束后消费 checkpoint：同一次审批只能执行一次，
//     重放同一 resume 请求会得到 ErrCheckpointNotFound；
//   - 恢复过程中若再次中断（新的危险 SQL），Runner 以同一 ID 保存新状态，前端可继续确认。
func (a *SQLAgent) ResumeStream(ctx context.Context, runID, checkPointID string, binding CheckpointBinding, targets map[string]bool, flush func(StreamChunk), sess *Session) error {
	log.Printf("[Agent] resume 开始 - cpID=%s, targets=%d, sessionMsgs=%d\n", checkPointID, len(targets), len(sess.GetMessages()))

	// 同一会话互斥：确认操作与新的对话请求不可并发运行。
	// 用 BeginRunWait 容忍「中断事件已推送、RunStream 尚在收尾」的毫秒级窗口。
	if sess != nil {
		if !sess.BeginRunWait(2 * time.Second) {
			return errors.New("该会话正在处理中，请等待当前回复完成后再确认")
		}
		defer sess.EndRun()
	}

	version, err := globalCheckPointStore.BeginResume(checkPointID, binding)
	if err != nil {
		return err
	}
	defer globalCheckPointStore.EndResume(checkPointID, version)

	// 恢复链路必须与 RunStream 共用同一安全上下文：输出审计、工具授权、持久化守卫
	// 都从 context 取事实，不能因为走 Resume 而绕过。
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return err
	}
	if sess != nil {
		sess.SetLatestSecurity(sec)
	}

	targetsAny := make(map[string]any, len(targets))
	for id, approved := range targets {
		targetsAny[id] = SQLApprovalResult{Approved: approved}
	}

	if runID == "" {
		runID = fmt.Sprintf("%s_resume_%d", binding.SessionID, time.Now().UnixNano())
	}
	cancelOpt, cancelFn := adk.WithCancel()
	a.registerCancel(runID, cancelFn)
	defer a.unregisterCancel(runID)

	if a.sessionSync != nil {
		ctx = WithRunSessionState(ctx, newRunSessionState(sess, len(sess.GetMessages())))
	}
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: binding.SessionID, UserID: binding.UserID})

	resumeStart := time.Now()
	iter, err := a.runner.ResumeWithParams(ctx, checkPointID, &adk.ResumeParams{
		Targets: targetsAny,
	}, cancelOpt)
	if err != nil {
		return fmt.Errorf("恢复执行失败：%w", err)
	}

	interrupted := a.processEvents(ctx, runID, iter, flush, sess, checkPointID)

	log.Printf("[Agent] resume 完成 - elapsed=%v, awaitingConfirm=%v\n", time.Since(resumeStart), interrupted)

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] 保存会话失败 - err=%v\n", err)
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

// NewAuthClient 返回带 Bearer 鉴权与整体超时的 HTTP 客户端。
// 注意：eino-ext ollama 在设置 HTTPClient 后会忽略 ChatModelConfig.Timeout，
// 因此超时必须设置在客户端上；token 为空时不发送 Authorization 头（避免发送 "Bearer "）。
func NewAuthClient(token string, timeout time.Duration) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport
	if strings.TrimSpace(token) != "" {
		transport = &authTransport{token: token, transport: http.DefaultTransport}
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// ──────────────────────────────────────────────
// 模型与工具构建
// ──────────────────────────────────────────────

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
		if cfg.Temperature > 0 || cfg.FrequencyPenalty > 0 || cfg.PresencePenalty > 0 {
			opts := &ollama.Options{Temperature: cfg.Temperature}
			if cfg.FrequencyPenalty > 0 {
				opts.FrequencyPenalty = cfg.FrequencyPenalty
			}
			if cfg.PresencePenalty > 0 {
				opts.PresencePenalty = cfg.PresencePenalty
			}
			ollamaCfg.Options = opts
		}
		ollamaCfg.HTTPClient = NewAuthClient(cfg.ApiKey, 30*time.Minute)
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
		if cfg.FrequencyPenalty > 0 {
			fp := cfg.FrequencyPenalty
			openaiCfg.FrequencyPenalty = &fp
		}
		if cfg.PresencePenalty > 0 {
			pp := cfg.PresencePenalty
			openaiCfg.PresencePenalty = &pp
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
	// 导出工具的最小审计上下文（export 包不能反向依赖 agent 包，故做一次转换）
	exportAuditCtx := toExportAuditCtx(auditCtx)
	queryTool, qErr := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", NewQueryFunc(connID, schemas, auditCtx, scope.UserID))
	schemaTool, sErr := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息，支持一次传入多个表名", NewSchemaFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	listTablesTool, lErr := utils.InferTool("list_tables", "获取当前数据库的所有表名及表注释", NewListTablesFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	currentDateInfoTool, dErr := utils.InferTool("get_current_date_info", "获取当前日期、星期几和时间", GetCurrentDateInfo())
	// read_file_data 为只读工具，用于分析用户上传的数据文件，不依赖写权限，常驻核心工具
	readFileTool, rfErr := utils.InferTool("read_file_data", "读取已上传的数据文件内容（只读，用于数据分析，不会导入数据库）。可传 limit(默认100,最大500)/offset 分页读取", NewReadFileDataFunc(scope.UserID))
	// cross_db_join 为跨库关联分析工具：服务端完成两侧表 Hash Join，只返回统计与少量样本，避免大量明细进入上下文
	crossJoinTool, cjErr := utils.InferTool("cross_db_join", "跨库关联分析：在服务端对两个连接（connId 可传 Schema 名或连接 ID）的表执行 Hash Join，仅返回匹配统计（matched/leftOnly/rightOnly 行数）、可选聚合指标（metrics: count/sum/avg/min/max，column 用 left./right. 前缀）和少量样本行（sampleLimit 默认30）。支持：joinKey/joinKeys 复合 key（多列联合匹配）、leftWhere/rightWhere 过滤（仅支持「列 操作符 字面量」+ AND/OR/NOT/IN/BETWEEN/LIKE/IS NULL，如 status='ACTIVE' AND amount>100）。两侧各取前 limit 行（默认10000），结果属抽样关联。适用于跨库/跨连接的表关联分析，避免海量数据进入上下文", NewCrossDBJoinFunc(connID, schemas, auditCtx, scope.UserID))

	for _, t := range []tool.BaseTool{queryTool, schemaTool, listTablesTool, currentDateInfoTool, readFileTool, crossJoinTool} {
		if t != nil {
			coreTools = append(coreTools, t)
		}
	}
	if qErr != nil || sErr != nil || lErr != nil || dErr != nil || rfErr != nil || cjErr != nil {
		return nil, nil, fmt.Errorf("创建核心工具失败：query=%v schema=%v list=%v date=%v readFile=%v crossJoin=%v", qErr, sErr, lErr, dErr, rfErr, cjErr)
	}

	// 导出工具：全部走 Go 原生 report 渲染引擎（封面/目录/指标卡/原生可编辑图表/
	// 提示框/分页表格），无 Python 依赖、离线可用、导出永不因环境而失败。
	// docx/ppt/html 的 content 模式支持扩展 Markdown（```kpi/```chart/```table），
	// 其中内嵌 SQL 由 DataSource 经只读 + 表/列权限校验后取数。
	auditUserID := ""
	if auditCtx != nil {
		auditUserID = auditCtx.UserID
	}
	dataSource := newReportDataSource(connID, auditUserID, schemas, scope)
	// 原生导出取数统一走安全执行器：只读 + 表/列权限 + 结果脱敏后再落盘。
	exportRunner := newExportQueryRunner(conn, scope, scope.UserID)
	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 数据表格（Go 原生渲染：表头样式/冻结首行/筛选/数字格式）。须传入 sql 参数", export.NewExportExcelFunc(conn, exportAuditCtx, exportRunner))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出含图表的 Excel（Go 原生可编辑图表 + 分析摘要 + 数据明细）。须传入 sql 与 charts（X/Y 字段名须与查询结果列一致）", export.NewExportExcelWithChartFunc(conn, exportAuditCtx, exportRunner))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿（Go 原生渲染：封面/目录/章节页/指标卡/原生可编辑图表/分页表格，theme=tech 为深色科技风）。content 传扩展 Markdown（kpi 指标卡/chart 图表/table 数据表），或仅传 sql 自动生成图文报告。优先使用 content 模式避免重复查询", export.NewExportPPTFunc(conn, exportAuditCtx, dataSource, exportRunner))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成 Word 数据分析报告（Go 原生渲染：封面/目录/页眉页脚/指标卡/原生可编辑图表/分页表格）。content 传扩展 Markdown（kpi 指标卡/chart 图表/table 数据表/提示框），或仅传 sql 自动生成图文报告。优先使用 content 模式", export.NewExportAnalysisDocxFunc(conn, exportAuditCtx, dataSource, exportRunner))
	exportHTMLTool, _ := utils.InferTool("export_html", "生成自包含 HTML 报告（服务端渲染、内联 SVG 图表、断网可读、支持 Mermaid 图示与公式）。content 传扩展 Markdown（kpi 指标卡/chart 图表/table 数据表），或仅传 sql 自动生成图文报告。优先使用 content 模式", export.NewExportHTMLFunc(conn, exportAuditCtx, dataSource, exportRunner))

	for _, t := range []tool.BaseTool{exportExcelTool, exportExcelChartTool, exportPPTTool, exportDocxTool, exportHTMLTool} {
		if t != nil {
			deferredTools = append(deferredTools, t)
		}
	}

	// 高级导出（export_by_code）：由模型编写 Python 连接「用户选中连接」（只读）查询并生成
	// 任意格式文件。仅在满足以下条件时注册（fail-closed）：
	//   1. 用户对该连接拥有完整权限（SkipChecks）——直连会绕过应用层表/列权限；
	//   2. 检测到可用的 Python 运行环境；
	//   3. 存在可用的 OS 级隔离（Windows 低完整性令牌 / Linux bwrap）；
	//   4. 不存在生效的脱敏规则（直连脚本无法传递脱敏策略）。
	if exportByCodeAllowed(scope) {
		if dbInfo := resolveCodegenDBInfo(connID, dbSchema); dbInfo != nil {
			exportByCodeTool, _ := utils.InferTool(
				"export_by_code",
				"高级导出：编写 Python 代码连接当前数据库（只读，仅允许查询）并按用户描述的格式生成文件，"+
					"用于现有工具不支持的格式（如 PDF/CSV/Markdown/XML/ZIP 等）。"+
					"只读约束：仅能通过 websql_db 只读助手查询数据库，禁止写库/改结构；禁止修改或删除系统与用户文件；禁止联网；"+
					"只能在当前工作目录内创建文件。连接信息（host/port/user/password/库）由环境变量 WEBSQL_DB_* 提供，"+
					"当前库的表结构见工作目录下的 schema.json。脚本必须在 outputFile 指定的相对路径产出目标文件。",
				export.NewExportByCodeFunc(conn, dbInfo, exportAuditCtx),
			)
			if exportByCodeTool != nil {
				deferredTools = append(deferredTools, exportByCodeTool)
			}
		}
	}

	// exec_sql 的注册条件是「AI 数据写能力」而非全局 AllowModify：
	// ai_allow_modify 打开后，只有同一角色同时允许两项能力才注册。
	// 模型看不到未授权的写工具，Middleware 再做一层伪造调用拦截（双保险）。
	if scope.AllowModify && scope.CanAIModifyData() {
		execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID, schemas, auditCtx, scope.UserID))
		if execTool != nil {
			deferredTools = append(deferredTools, execTool)
		}
	}

	return coreTools, deferredTools, nil
}

// toExportAuditCtx 把 agent 的审计上下文转换为 export 包的最小审计上下文
// （export 包不能依赖 agent 包，避免循环导入）。auditCtx 为 nil 时返回 nil，
// 导出工具仍会写入不含用户上下文的审计记录。
func toExportAuditCtx(auditCtx *ExecAuditCtx) *export.AuditCtx {
	if auditCtx == nil {
		return nil
	}
	return &export.AuditCtx{
		ConnID:    auditCtx.ConnID,
		UserID:    auditCtx.UserID,
		UserName:  auditCtx.UserName,
		SessionID: auditCtx.SessionID,
	}
}

// resolveCodegenDBInfo 读取「用户选中连接」的连接信息，供高级导出注入子进程环境变量。
// 读取失败（管理库未就绪/密码解密失败等）返回 nil，此时不注册高级导出工具（fail-closed）。
// 【安全约定】返回值含明文密码，仅注入本地子进程环境变量，不得写入日志或返回给前端。
func resolveCodegenDBInfo(connID, dbSchema string) *export.CodegenDBInfo {
	cfg, err := connpkg.GetConnCfgWithPwd(connID)
	if err != nil {
		log.Printf("[Agent] 高级导出不可用（读取连接配置失败）- conn=%s, err=%v\n", connID, err)
		return nil
	}
	info := &export.CodegenDBInfo{DBType: cfg.DbType, Schema: dbSchema}
	if cfg.Url != nil {
		info.URL = *cfg.Url
	}
	if cfg.User != nil {
		info.User = *cfg.User
	}
	if cfg.Pwd != nil {
		info.Password = *cfg.Pwd
	}
	if info.Schema == "" && cfg.DbSchema != nil {
		info.Schema = *cfg.DbSchema
	}
	return info
}
