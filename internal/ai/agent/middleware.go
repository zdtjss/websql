package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"websql/internal/ai/agent/sqlutil"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// StripSQLComments 是 sqlutil.StripSQLComments 的本地别名。
//
// 项目内任何 SQL 注释剥离都应通过本别名调用，确保在 agent 与 export
// 子包中行为一致（EINO_DEEP_ANALYSIS §10.1）。新代码请直接用 sqlutil.StripSQLComments。
func StripSQLComments(sql string) string { return sqlutil.StripSQLComments(sql) }

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

func isDangerousSQL(sql string) bool {
	stripped := StripSQLComments(strings.TrimSpace(sql))
	upper := strings.ToUpper(stripped)
	for _, p := range []string{
		"DROP ", "TRUNCATE ", "DELETE ",
		"ALTER ", "CREATE ", "REPLACE ",
		"INSERT ", "UPDATE ", "MERGE ",
	} {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// buildEinoReductionMiddleware 构造 eino 0.9 官方的 reduction 中间件
//
// 优势：
//  1. 保留流式语义（不破坏 StreamableTool 的 Recv 链路）
//  2. 大结果自动 Offload 到文件，LLM 看到的不再是巨大 JSON
//  3. ReadFile 工具与 LLM 协同工作
//
// 替换了项目自实现的 ReductionMiddleware（middleware.go:407-431），后者
// 的"Recv 全部再切片"会破坏流式 UX。
func buildEinoReductionMiddleware() adk.ChatModelAgentMiddleware {
	dir := os.Getenv("REDUCTION_OFFLOAD_DIR")
	if dir == "" {
		dir = filepath.Join("data", "reduction")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[Reduction] 创建 offload 目录失败，使用 tmp - err=%v\n", err)
		dir = os.TempDir()
	}

	backend := &osfsReductionBackend{rootDir: dir}

	mw, err := reduction.New(context.Background(), &reduction.Config{
		Backend:                   backend,
		MaxLengthForTrunc:         50_000, // 单条 tool result 超过 50k 字符触发 truncate
		TokenCounter:              reductionTokenCounter,
		MaxTokensForClear:         160_000, // 总 token 超 160k 触发 clear
		ClearRetentionSuffixLimit: 4,       // 保留最近 4 条不 clear
		ClearExcludeTools: []string{ // 永不 clear 这些工具的结果
			"dangerous_sql_approval",
			"interrupt",
		},
		TruncExcludeTools: nil,
	})
	if err != nil {
		log.Printf("[Reduction] 创建 eino reduction 中间件失败 - err=%v，使用 noop\n", err)
		return nil
	}
	return mw
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
	full := filepath.Join(b.rootDir, req.FilePath)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(req.Content), 0o644)
}

// reductionTokenCounter 适配 reduction 中间件期望的签名。
// 复用项目自实现的 estimateTokenCount，但输入输出签名不同：
//
//	reduction.TokenCounter:     func(ctx, []*schema.Message, []*schema.ToolInfo) (int64, error)
//	summarization.TokenCounter: func(ctx, *summarization.TokenCounterInput) (int, error)
//
// 这里按字符/4 估算每条消息，与项目自实现的启发式一致。
func reductionTokenCounter(_ context.Context, msgs []*schema.Message, _ []*schema.ToolInfo) (int64, error) {
	var total int64
	for _, m := range msgs {
		if m == nil {
			continue
		}
		total += int64(len(m.Content)+3) / 4
	}
	return total, nil
}

func containsDangerousSQL(sql string) bool {
	for _, line := range strings.Split(sql, ";") {
		line = strings.TrimSpace(line)
		if line != "" && isDangerousSQL(line) {
			return true
		}
	}
	return false
}

type DangerousSQLInfo struct {
	SQL       string `json:"sql"`
	RiskLevel string `json:"riskLevel"`
	SQLType   string `json:"sqlType"`
}

func init() {
	schema.RegisterName[*DangerousSQLInfo]("agent.DangerousSQLInfo")
}

type SQLApprovalResult struct {
	Approved         bool   `json:"approved"`
	DisapproveReason string `json:"disapproveReason,omitempty"`
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

type execFunc func(ctx context.Context, args string, opts []tool.Option) (string, error)

func approvalGate(
	ctx context.Context,
	toolName string,
	argumentsInJSON string,
	opts []tool.Option,
	exec execFunc,
) (string, error) {

	wasInterrupted, _, savedArgs := tool.GetInterruptState[string](ctx)

	if !wasInterrupted {
		sql := extractSQLFromArgs(argumentsInJSON)
		if sql == "" || !containsDangerousSQL(sql) {
			return exec(ctx, argumentsInJSON, opts)
		}
		log.Printf("[ApprovalMiddleware] 拦截危险 SQL - tool=%s, sql=%s\n", toolName, sql)
		return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL:       sql,
			RiskLevel: detectRiskLevel(sql),
			SQLType:   detectSQLType(sql),
		}, argumentsInJSON)
	}

	isTarget, hasData, approval := tool.GetResumeContext[SQLApprovalResult](ctx)

	if isTarget && hasData && approval.Approved {
		log.Printf("[ApprovalMiddleware] 用户批准执行 - tool=%s\n", toolName)
		return exec(ctx, savedArgs, opts)
	}

	if isTarget && hasData && !approval.Approved {
		reason := approval.DisapproveReason
		if reason == "" {
			reason = "用户取消执行"
		}
		log.Printf("[ApprovalMiddleware] 用户拒绝执行 - tool=%s, reason=%s\n", toolName, reason)
		return fmt.Sprintf(`{"message": "%s", "affectedRows": 0}`, reason), nil
	}

	sql := extractSQLFromArgs(savedArgs)
	if sql == "" {
		sql = savedArgs
	}
	log.Printf("[ApprovalMiddleware] 重新中断 - tool=%s, isTarget=%v, hasData=%v\n", toolName, isTarget, hasData)
	return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
		SQL:       sql,
		RiskLevel: detectRiskLevel(sql),
		SQLType:   detectSQLType(sql),
	}, savedArgs)
}

func extractSQLFromArgs(argumentsInJSON string) string {
	var input ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.SQL)
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
			hint := recoveryHint(tCtx.Name, argumentsInJSON, err)
			log.Printf("[ToolErrorRecovery] tool %s error - err=%v\n", tCtx.Name, err)
			return "", fmt.Errorf("[%s] %w\n%s", tCtx.Name, err, hint)
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
			hint := recoveryHint(tCtx.Name, argumentsInJSON, err)
			log.Printf("[ToolErrorRecovery:Stream] tool %s error - err=%v\n", tCtx.Name, err)
			return nil, fmt.Errorf("[%s] %w\n%s", tCtx.Name, err, hint)
		}
		return result, nil
	}, nil
}

func recoveryHint(toolName, args string, err error) string {
	errStr := err.Error()
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
		return "Python skill execution failed on Windows. Use dedicated export tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. These tools internally prefer Python Skill (same quality) with automatic Go fallback - no manual script assembly needed."
	}

	if isSkillExport {
		return "Skill script execution failed. Use dedicated export tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. They automatically try Python Skill first and fallback to Go if needed."
	}

	return "Please adjust parameters and retry."
}

type ToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	startTime time.Time
}

// AfterAgent 在 Agent 成功结束后记录总耗时和统计信息（Eino v0.9 新增）
func (m *ToolCallLoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	if !m.startTime.IsZero() {
		elapsed := time.Since(m.startTime)
		log.Printf("[ToolCallLogging] Agent 执行完毕 - 总耗时=%v, 消息数=%d\n", elapsed, len(state.Messages))
	}
	return ctx, nil
}

// BeforeModelRewriteState 记录 Agent 开始时间
func (m *ToolCallLoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m.startTime.IsZero() {
		m.startTime = time.Now()
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
		log.Printf("[ToolCall] 开始调用 - name=%s, args=%s\n", tCtx.Name, argumentsInJSON)

		result, err := endpoint(ctx, argumentsInJSON, opts...)

		elapsed := time.Since(startTime)
		if err != nil {
			log.Printf("[ToolCall] 调用失败 - name=%s, duration=%v, err=%v\n", tCtx.Name, elapsed, err)
		} else {
			log.Printf("[ToolCall] 调用完成 - name=%s, duration=%v, result=%s\n", tCtx.Name, elapsed, result)
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
		log.Printf("[ToolCall:Stream] 开始调用 - name=%s, args=%s\n", tCtx.Name, argumentsInJSON)

		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			elapsed := time.Since(startTime)
			log.Printf("[ToolCall:Stream] 调用失败 - name=%s, duration=%v, err=%v\n", tCtx.Name, elapsed, err)
			return nil, err
		}

		// Copy(2) 把流扇出为两个独立的 StreamReader：
		//   1) consumer — 返回给下游消费者，保证原流被完整消费
		//   2) logger   — 在后台 goroutine 里消费，用于日志
		//
		// 解决 v1 实现的"双消费者从同一底层 channel 抢数据"问题：
		// 之前代码 wrapped.Recv() 与下游 reader.Recv() 共享同一底层，
		// 导致数据被瓜分、前端拿到的数据不完整。
		copies := reader.Copy(2)
		if len(copies) < 2 {
			// Copy 失败兜底：原样返回，仅打日志
			return reader, nil
		}
		consumer := copies[0]
		logger := copies[1]

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[ToolCall:Stream] logger panic recovered - name=%s, panic=%v\n", tCtx.Name, r)
				}
			}()
			var contentBuf strings.Builder
			for {
				chunk, err := logger.Recv()
				if err != nil {
					break
				}
				contentBuf.WriteString(chunk)
			}
			elapsed := time.Since(startTime)
			content := contentBuf.String()
			// 截断过长日志，避免日志爆炸
			const maxLog = 4000
			if len(content) > maxLog {
				content = content[:maxLog] + "...(truncated)"
			}
			log.Printf("[ToolCall:Stream] 流结束 - name=%s, duration=%v, resultLen=%d, result=%s\n",
				tCtx.Name, elapsed, len(content), content)
		}()

		return consumer, nil
	}, nil
}
