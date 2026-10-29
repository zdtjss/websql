package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// buildPatchToolCallsMiddleware 构造 eino 0.9 官方的 patchtoolcalls 中间件。
//
// 解决的问题：
//  1. 进程崩溃 / kill -9 / OOM 后，会话中存在 assistant(tool_calls=[X])
//     但缺少对应的 tool(X) 消息，LLM 收到后会拒绝继续生成或乱答。
//  2. 用户主动取消（Cancel）后，sess.RemoveTrailingIncompleteToolCalls 只清内存，
//     如果在 300ms debounce 窗口内崩溃，DB 中可能残留脏数据。
//  3. loadSessionFromDB 从 DB 加载到历史脏数据时，没有清理入口。
//
// patchtoolcalls 在 BeforeModelRewriteState 钩子里**自动**扫描历史消息，给
// dangling tool_calls 补占位 tool 消息。配合自定义 PatchedContentGenerator，
// 让 LLM 看到 "工具被取消 / 未执行完成" 的明确提示。
func buildPatchToolCallsMiddleware() adk.ChatModelAgentMiddleware {
	mw, err := patchtoolcalls.New(context.Background(), &patchtoolcalls.Config{
		PatchedContentGenerator: func(ctx context.Context, toolName, toolCallID string) (string, error) {
			return fmt.Sprintf(
				`{"status":"patched","message":"工具 %s (call_id=%s) 的结果未生成（可能是上一轮对话被取消、进程崩溃或工具执行失败）。请基于此状态继续回答，必要时重新调用该工具。"}`,
				toolName, toolCallID,
			), nil
		},
	})
	if err != nil {
		log.Printf("[PatchToolCalls] 创建失败，使用 noop - err=%v\n", err)
		return nil
	}
	return mw
}

// buildEinoReductionMiddleware 构造 eino 0.9 官方的 reduction 中间件
//
// 优势：
//  1. 保留流式语义（不破坏 StreamableTool 的 Recv 链路）
//  2. 大结果自动 Offload 到文件，LLM 看到的不再是巨大 JSON
//  3. ReadFile 工具与 LLM 协同工作
//
// 阈值与模型上下文窗口联动：clear 阈值取配置上下文窗口的 70%。
// 历史实现硬编码 160_000，而默认窗口只有 128_000，导致 clear 永不触发，
// 小窗口模型（32k/64k）更是一直靠 summarization 兜底。
func buildEinoReductionMiddleware(maxContextTokens int) adk.ChatModelAgentMiddleware {
	dir := reductionOffloadRoot()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		// 目录不可用时直接关闭 reduction：宁可让上下文治理缺失并记录告警，
		// 也不能写到 read_offload 无法访问的路径，制造“看似可恢复、实际取不回”的假象。
		log.Printf("[Reduction] 创建 offload 目录失败，已禁用 reduction - dir=%s, err=%v\n", dir, err)
		return nil
	}
	// 已有目录可能是旧版本创建的 0755；收紧到仅当前进程用户可访问。
	_ = os.Chmod(dir, 0o700)

	ctxTokens := maxContextTokens
	if ctxTokens <= 0 {
		ctxTokens = defaultContextTokens
	}
	clearTokens := int64(ctxTokens) * 70 / 100
	if clearTokens < 8_000 {
		clearTokens = 8_000
	}

	backend := &osfsReductionBackend{rootDir: dir}
	// AGENT_OFFLOAD_V2=0 可回退到默认首尾截断格式；路径 ACL 不回退。
	toolConfig := buildReductionToolConfig(backend)
	if os.Getenv("AGENT_OFFLOAD_V2") == "0" {
		toolConfig = nil
	}

	mw, err := reduction.New(context.Background(), &reduction.Config{
		Backend:           backend,
		RootDir:           dir,
		MaxLengthForTrunc: 30_000, // 单条 tool result 超过 30k 字节触发 truncate + offload 到文件
		TokenCounter:      reductionTokenCounter,
		MaxTokensForClear: clearTokens, // 总 token 超过窗口 70% 触发 clear
		// 保留最近 4 条不 clear。
		ClearRetentionSuffixLimit: 4,
		// offload 文件按用户/会话隔离，模型必须用 read_offload 回读。
		ReadFileToolName:  "read_offload",
		TruncExcludeTools: []string{"read_offload"},
		GenTruncOffloadFilePath: func(ctx context.Context, detail *reduction.ToolDetail) (string, error) {
			callID := ""
			if detail != nil && detail.ToolContext != nil {
				callID = detail.ToolContext.CallID
			}
			return reductionOffloadPath(ctx, "trunc", callID)
		},
		GenClearOffloadFilePath: func(ctx context.Context, detail *reduction.ToolDetail) (string, error) {
			callID := ""
			if detail != nil && detail.ToolContext != nil {
				callID = detail.ToolContext.CallID
			}
			return reductionOffloadPath(ctx, "clear", callID)
		},
		ToolConfig: toolConfig,
		// 说明：危险 SQL 确认走 eino interrupt 状态而非工具结果，
		// 不在 reduction 的处理范围内，因此无需（也无法）通过工具名排除。
	})
	if err != nil {
		log.Printf("[Reduction] 创建 eino reduction 中间件失败 - err=%v，使用 noop\n", err)
		return nil
	}
	log.Printf("[Reduction] 已启用 - offloadDir=%s, clearTokenThreshold=%d（上下文窗口 %d）\n", dir, clearTokens, ctxTokens)
	return mw
}

// CleanupReductionOffload 清理 reduction offload 目录中超过 maxAge 的文件。
// offload 文件是大结果被裁剪时落盘的副本，仅用于模型按提示回读，过期即可删除；
// 由启动时的导出清理任务周期调用（避免 data/reduction 无限增长）。
func CleanupReductionOffload(maxAge time.Duration) {
	dir := os.Getenv("REDUCTION_OFFLOAD_DIR")
	if dir == "" {
		dir = filepath.Join("data", "reduction")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // 目录不存在/权限不足时静默跳过
		}
		info, statErr := d.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if os.Remove(path) == nil {
			removed++
		}
		return nil
	})
	if removed > 0 {
		log.Printf("[Cleanup] 已清理 %d 个 reduction offload 文件\n", removed)
	}
}

// osfsReductionBackend 是一个最小化的本地文件 reduction.Backend 实现。
// 满足 reduction.Backend 唯一方法：Write(ctx, *filesystem.WriteRequest) error。
type osfsReductionBackend struct {
	rootDir string
	mu      sync.Mutex
}

func (b *osfsReductionBackend) Write(ctx context.Context, req *filesystem.WriteRequest) error {
	if req == nil || req.FilePath == "" {
		return errors.New("WriteRequest 必须包含 FilePath")
	}
	full := req.FilePath
	if !filepath.IsAbs(full) {
		full = filepath.Join(b.rootDir, full)
	}
	full = filepath.Clean(full)
	if rel, err := filepath.Rel(b.rootDir, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("offload 路径 %q 超出 reduction 目录", req.FilePath)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	// 拒绝通过符号链接把 offload 写到 root 之外。
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("offload 目标 %q 是符号链接，拒绝写入", req.FilePath)
	}
	if hasSymlinkComponent(filepath.Dir(full), b.rootDir) {
		return fmt.Errorf("offload 父目录 %q 包含符号链接，拒绝写入", req.FilePath)
	}
	return os.WriteFile(full, []byte(req.Content), 0o600)
}

// reductionTokenCounter 适配 reduction 中间件期望的签名。
// 统一使用 CountMessages（中文感知 token 计数），与 summarization 共用同一套计数逻辑，
// 确保两者的触发阈值可比。
func reductionTokenCounter(ctx context.Context, msgs []*schema.Message, tools []*schema.ToolInfo) (int64, error) {
	return CountMessages(ctx, msgs, tools)
}

type DangerousSQLApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *DangerousSQLApprovalMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		return approvalGate(ctx, tCtx.Name, argumentsInJSON, opts,
			func(ctx context.Context, args string, opts []tool.Option) (string, error) {
				return endpoint(ctx, args, opts...)
			},
		)
	}, nil
}

func (m *DangerousSQLApprovalMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if tCtx.Name != "exec_sql" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := approvalGate(ctx, tCtx.Name, argumentsInJSON, opts,
			func(ctx context.Context, args string, opts []tool.Option) (string, error) {
				reader, err := endpoint(ctx, args, opts...)
				if err != nil {
					return "", err
				}
				var sb strings.Builder
				for {
					chunk, recvErr := reader.Recv()
					if recvErr != nil {
						break
					}
					sb.WriteString(chunk)
				}
				return sb.String(), nil
			},
		)
		if err != nil {
			return nil, err
		}
		return schema.StreamReaderFromArray([]string{result}), nil
	}, nil
}

type ToolErrorRecoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func isInterruptErr(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := compose.ExtractInterruptInfo(err); ok {
		return true
	}
	if _, ok := compose.IsInterruptRerunError(err); ok {
		return true
	}
	var is *adk.InterruptSignal
	return errors.As(err, &is)
}

func (m *ToolErrorRecoveryMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return "", err
			}
			hint := recoveryHintWithSkillMeta(ctx, tCtx.Name, argumentsInJSON, err)
			log.Printf("[ToolErrorRecovery] tool %s error - err=%v\n", tCtx.Name, err)
			// 将错误转化为工具结果，让 LLM 看到错误并有机会重试
			return formatToolErrorAsResult(tCtx.Name, err, hint), nil
		}
		return result, nil
	}, nil
}

func (m *ToolErrorRecoveryMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if isInterruptErr(err) {
				return nil, err
			}
			hint := recoveryHintWithSkillMeta(ctx, tCtx.Name, argumentsInJSON, err)
			log.Printf("[ToolErrorRecovery:Stream] tool %s error - err=%v\n", tCtx.Name, err)
			errorMsg := formatToolErrorAsResult(tCtx.Name, err, hint)
			return schema.StreamReaderFromArray([]string{errorMsg}), nil
		}
		return result, nil
	}, nil
}

// formatToolErrorAsResult 将工具错误格式化为 JSON 结果字符串，让 LLM 能看到错误并重试
func formatToolErrorAsResult(toolName string, err error, hint string) string {
	return fmt.Sprintf(
		`{"error":true,"tool":"%s","message":"工具执行失败：%s","recovery_hint":"%s"}`,
		toolName, escapeJSONString(err.Error()), escapeJSONString(hint),
	)
}

// escapeJSONString 转义 JSON 字符串中的特殊字符
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// recoveryHintWithSkillMeta 先尝试从活跃 Skill 的 errorHints 匹配，
// 再回退到全局 errorHints 匹配，最后回退到原有的硬编码 recoveryHint。
//
// 优先级：
//  1. 活跃 Skill 的 errorHints（精确匹配当前 Skill 场景）
//  2. 全局 errorHints（所有 Skill 的 hints 聚合匹配）
//  3. 硬编码 recoveryHint（SQL 方言错误等细节提示，保持向后兼容）
func recoveryHintWithSkillMeta(ctx context.Context, toolName, args string, err error) string {
	if err == nil {
		return ""
	}
	// 0. 权限拒绝：明确告知模型不要重试或绕过
	var permErr *PermissionError
	if errors.As(err, &permErr) {
		return "权限不足，属于安全策略拒绝而非 SQL 错误。禁止重试相同操作，禁止改用其他表、视图、子查询或工具绕过；" +
			"请直接向用户说明缺少哪些表/字段/操作的权限，并建议联系管理员开通。"
	}
	// 1. 尝试活跃 Skill 的 errorHints
	if activeSkill := getActiveSkillFromContext(ctx); activeSkill != "" {
		if meta := globalSkillMetaRegistry.GetSkillMeta(activeSkill); meta != nil {
			if hint := MatchErrorHint(meta, err); hint != "" {
				log.Printf("[ToolErrorRecovery] 命中活跃 Skill errorHint - skill=%s\n", activeSkill)
				return hint
			}
		}
	}
	// 2. 尝试全局 errorHints 匹配
	if hint := MatchGlobalErrorHint(err); hint != "" {
		log.Printf("[ToolErrorRecovery] 命中全局 errorHint\n")
		return hint
	}
	// 3. 回退到原有的硬编码提示（保持向后兼容）
	return recoveryHint(toolName, args, err)
}

func recoveryHint(toolName, args string, err error) string {
	errStr := err.Error()

	// SQL 工具的错误恢复提示
	if toolName == "query_data" || toolName == "exec_sql" {
		// 方言不兼容错误（来自预检器）
		if strings.Contains(errStr, "方言不兼容") {
			return errStr + "\n\n请根据上述替代方案重写 SQL 后重试。不要使用相同的语法再次尝试。"
		}
		// MySQL 语法错误
		if strings.Contains(errStr, "Error 1064") {
			if strings.Contains(errStr, "PERCENTILE_CONT") || strings.Contains(errStr, "WITHIN GROUP") {
				return "MySQL 不支持 PERCENTILE_CONT/WITHIN GROUP 语法（Oracle 专有）。\n" +
					"计算中位数：SELECT AVG(x) FROM (SELECT x FROM t ORDER BY x LIMIT 2 OFFSET (cnt-1)/2) tmp\n" +
					"计算分位数：用 PERCENT_RANK() 窗口函数（MySQL 8.0+）\n" +
					"示例：SELECT PERCENT_RANK() OVER (ORDER BY col) as pct FROM t"
			}
			if strings.Contains(errStr, "STRING_AGG") {
				return "MySQL 不支持 STRING_AGG，请用 GROUP_CONCAT(col SEPARATOR ',') 替代。"
			}
			if strings.Contains(errStr, "LISTAGG") {
				return "MySQL 不支持 LISTAGG，请用 GROUP_CONCAT(col SEPARATOR ',') 替代。"
			}
			if strings.Contains(errStr, "DATE_TRUNC") {
				return "MySQL 不支持 DATE_TRUNC，请用 DATE_FORMAT(date, '%Y-%m-01') 按月截断，DATE(date) 按天截断。"
			}
			if strings.Contains(errStr, "ARRAY_AGG") {
				return "MySQL 不支持 ARRAY_AGG，请用 GROUP_CONCAT 或 JSON_ARRAYAGG 替代。"
			}
			if strings.Contains(errStr, "RETURNING") {
				return "MySQL 不支持 RETURNING 子句，需要单独的 SELECT 查询获取数据。"
			}
			if strings.Contains(errStr, "FETCH") && strings.Contains(errStr, "ROWS ONLY") {
				return "MySQL 不支持 FETCH FIRST/NEXT 语法，请用 LIMIT n 替代。"
			}
			return "SQL 语法错误(Error 1064)。请检查：\n1) 是否使用了其他数据库专有函数（PERCENTILE_CONT/STRING_AGG/LISTAGG/DATE_TRUNC 等）\n2) 引号是否匹配\n3) 关键字拼写\n4) 错误信息 'near' 后指示出错位置"
		}
		// 表不存在
		if strings.Contains(errStr, "Error 1146") {
			return "表不存在(Error 1146)。请先调用 list_tables 确认正确表名，注意大小写和 schema 前缀。"
		}
		// 字段不存在
		if strings.Contains(errStr, "Error 1054") {
			return "字段不存在(Error 1054)。请先调用 get_table_schema 获取正确字段名，注意大小写。"
		}
		// 列歧义
		if strings.Contains(errStr, "Error 1052") {
			return "列名歧义(Error 1052)。请在列名前加表别名前缀，如 t1.column_name。"
		}
		// GROUP BY 错误
		if strings.Contains(errStr, "Error 1140") {
			return "GROUP BY 错误(Error 1140)。ONLY_FULL_GROUP_BY 模式要求 SELECT 中的非聚合列必须出现在 GROUP BY 中。请将所有非聚合列加入 GROUP BY，或用 ANY_VALUE() 包裹。"
		}
		// 连接错误
		if strings.Contains(errStr, "connection") || strings.Contains(errStr, "timeout") {
			return "数据库连接超时。请简化 SQL，减少扫描量，或添加 WHERE 条件限制数据范围。"
		}
		return "SQL 执行失败，请根据错误信息调整 SQL 后重试。"
	}

	isExecuteOrSkill := toolName == "execute" || toolName == "skill"
	if !isExecuteOrSkill {
		return "Please adjust parameters and retry."
	}

	isPipOrPython := strings.Contains(args, "pip") ||
		strings.Contains(args, "python") ||
		strings.Contains(args, "matplotlib") ||
		strings.Contains(args, "pptx") ||
		strings.Contains(args, "docx")

	isEncodingOrSyntax := strings.Contains(errStr, "UnicodeEncodeError") ||
		strings.Contains(errStr, "SyntaxError") ||
		strings.Contains(errStr, "gbk") ||
		strings.Contains(errStr, "unterminated string") ||
		strings.Contains(errStr, "illegal multibyte")

	isSkillExport := strings.Contains(args, "export-word") ||
		strings.Contains(args, "export-ppt") ||
		strings.Contains(args, "word_generator") ||
		strings.Contains(args, "export_ppt.py") ||
		strings.Contains(args, "generate_notice") ||
		strings.Contains(args, "check_fonts") ||
		strings.Contains(args, "check_deps")

	if isPipOrPython && (isEncodingOrSyntax || isSkillExport) {
		return "Python skill execution failed on Windows. Retry the skill workflow with corrected parameters, or use the 'export_ppt' / 'export_analysis_docx' / 'export_html' tools instead (they render with the built-in Go engine, no Python required)."
	}

	if isSkillExport {
		return "Skill script execution failed. Retry with corrected parameters, or use the 'export_ppt' / 'export_analysis_docx' tools instead (built-in Go renderer, no Python required)."
	}

	return "Please adjust parameters and retry."
}

type contextKeyStartTime struct{}
type contextKeyIteration struct{}

// InjectedContextFilterMiddleware 在每次模型调用前移除中间件注入的瞬态消息
// （AGENTS.md、toolsearch 提醒），避免它们被 summarization 计入/摘要，
// 也避免进入 transcript 与持久化历史。
//
// 注册位置在 summarization 之前、agentsmd 之前：
//   - 本中间件先删除上一轮注入的消息；
//   - summarization 只看到真实对话；
//   - agentsmd 随后按需重新注入最新内容。
//
// 该中间件无状态，可被共享 Agent 并发复用。
type InjectedContextFilterMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *InjectedContextFilterMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	filtered := make([]*schema.Message, 0, len(state.Messages))
	removed := 0
	for _, msg := range state.Messages {
		if msg != nil && msg.Extra != nil {
			if _, ok := msg.Extra[agentsMDExtraKey]; ok {
				removed++
				continue
			}
			if _, ok := msg.Extra[toolSearchReminderExtraKey]; ok {
				removed++
				continue
			}
		}
		filtered = append(filtered, msg)
	}
	if removed == 0 {
		return ctx, state, nil
	}
	nState := *state
	nState.Messages = filtered
	return ctx, &nState, nil
}

type ToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// BeforeAgent 在 Agent 开始执行前初始化计时和迭代计数到 context 中。
func (m *ToolCallLoggingMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	ctx = context.WithValue(ctx, contextKeyStartTime{}, time.Now())
	ctx = context.WithValue(ctx, contextKeyIteration{}, new(int))
	log.Printf("[AgentLifecycle] Agent 开始执行\n")
	return ctx, runCtx, nil
}

// AfterAgent 在 Agent 成功结束后记录总耗时和迭代次数。
func (m *ToolCallLoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
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
		log.Printf("[AgentLifecycle] Agent 执行完毕 - 总耗时=%v, 迭代次数=%d, 消息数=%d\n",
			elapsed, iterCount, msgCount)
	}
	return ctx, nil
}

// BeforeModelRewriteState 在每次模型调用前递增迭代计数器并记录日志。
func (m *ToolCallLoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
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
		log.Printf("[AgentLifecycle] 模型调用 #%d - 消息数=%d, 可见工具数=%d\n", *ic, msgCount, toolCount)
	}
	return ctx, state, nil
}

func (m *ToolCallLoggingMiddleware) WrapInvokableToolCall(
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
		argsPreview := safeTextSummary(argumentsInJSON)
		log.Printf("[ToolCall] 开始调用 - iter=%d, name=%s, callID=%s, args{%s}\n",
			iterNum, tCtx.Name, tCtx.CallID, argsPreview)

		result, err := endpoint(ctx, argumentsInJSON, opts...)

		elapsed := time.Since(startTime)
		if err != nil {
			log.Printf("[ToolCall] 调用失败 - iter=%d, name=%s, callID=%s, duration=%v, err=%v\n",
				iterNum, tCtx.Name, tCtx.CallID, elapsed, err)
		} else {
			log.Printf("[ToolCall] 调用完成 - iter=%d, name=%s, callID=%s, duration=%v, resultLen=%d\n",
				iterNum, tCtx.Name, tCtx.CallID, elapsed, len(result))
		}

		return result, err
	}, nil
}

func (m *ToolCallLoggingMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		startTime := time.Now()
		iterNum := 0
		if ic, ok := ctx.Value(contextKeyIteration{}).(*int); ok && ic != nil {
			iterNum = *ic
		}
		argsPreview := safeTextSummary(argumentsInJSON)
		log.Printf("[ToolCall:Stream] 开始调用 - iter=%d, name=%s, callID=%s, args{%s}\n",
			iterNum, tCtx.Name, tCtx.CallID, argsPreview)

		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			elapsed := time.Since(startTime)
			log.Printf("[ToolCall:Stream] 调用失败 - iter=%d, name=%s, callID=%s, duration=%v, err=%v\n",
				iterNum, tCtx.Name, tCtx.CallID, elapsed, err)
			return nil, err
		}

		// 不再 Copy(2) 扇出流内容做日志，避免每个 chunk 起后台 goroutine 消费。
		// 流式工具调用的日志仅记录开始/失败，结束日志由调用方在消费 reader 后按需打印。
		return reader, nil
	}, nil
}
