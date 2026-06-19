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
			hint := recoveryHint(tCtx.Name, argumentsInJSON, err)
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
			hint := recoveryHint(tCtx.Name, argumentsInJSON, err)
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
		return "Python skill execution failed on Windows. You have two options: (1) retry the skill workflow with corrected parameters, or (2) fall back to Go native tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. Go native tools require no Python and produce basic but valid output."
	}

	if isSkillExport {
		return "Skill script execution failed. You can retry with corrected parameters, or fall back to Go native tools: 'export_ppt' for PPT, 'export_analysis_docx' for Word. Go native tools produce basic but valid output without Python dependency."
	}

	return "Please adjust parameters and retry."
}

type contextKeyStartTime struct{}

type ToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// AfterAgent 在 Agent 成功结束后记录总耗时和统计信息（Eino v0.9 新增）
func (m *ToolCallLoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	if startTime, ok := ctx.Value(contextKeyStartTime{}).(time.Time); ok && !startTime.IsZero() {
		elapsed := time.Since(startTime)
		log.Printf("[ToolCallLogging] Agent 执行完毕 - 总耗时=%v, 消息数=%d\n", elapsed, len(state.Messages))
	}
	return ctx, nil
}

// BeforeModelRewriteState 记录 Agent 开始时间到 context 中（线程安全）
func (m *ToolCallLoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if _, ok := ctx.Value(contextKeyStartTime{}).(time.Time); !ok {
		ctx = context.WithValue(ctx, contextKeyStartTime{}, time.Now())
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
