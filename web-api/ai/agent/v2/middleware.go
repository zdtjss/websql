package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────
// 危险 SQL 检测
// ──────────────────────────────────────────────

// isDangerousSQL 检查 SQL 是否为写操作。
// 先去除前导注释，防止通过 "-- \nDELETE" 或 "/* */DELETE" 绕过。
func isDangerousSQL(sql string) bool {
	stripped := stripSQLComments(strings.TrimSpace(sql))
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

// containsDangerousSQL 检查一段可能包含多条 SQL（分号分隔）的文本中是否有危险 SQL。
func containsDangerousSQL(sql string) bool {
	for _, line := range strings.Split(sql, ";") {
		line = strings.TrimSpace(line)
		if line != "" && isDangerousSQL(line) {
			return true
		}
	}
	return false
}

// stripSQLComments 去除 SQL 开头的注释（行注释和块注释），防止注释绕过安全检测。
func stripSQLComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		if sql == "" {
			return sql
		}
		if strings.HasPrefix(sql, "--") {
			idx := strings.Index(sql, "\n")
			if idx == -1 {
				return ""
			}
			sql = sql[idx+1:]
			continue
		}
		if strings.HasPrefix(sql, "/*") {
			idx := strings.Index(sql, "*/")
			if idx == -1 {
				return ""
			}
			sql = sql[idx+2:]
			continue
		}
		break
	}
	return strings.TrimSpace(sql)
}

// ──────────────────────────────────────────────
// DangerousSQLInfo — 中断时传递给前端的信息
// ──────────────────────────────────────────────

type DangerousSQLInfo struct {
	SQL       string `json:"sql"`
	RiskLevel string `json:"riskLevel"`
	SQLType   string `json:"sqlType"`
}

func init() {
	schema.RegisterName[*DangerousSQLInfo]("agentv2.DangerousSQLInfo")
}

// ──────────────────────────────────────────────
// SQLApprovalResult — 用户审批结果
// ──────────────────────────────────────────────

type SQLApprovalResult struct {
	Approved         bool   `json:"approved"`
	DisapproveReason string `json:"disapproveReason,omitempty"`
}

// ──────────────────────────────────────────────
// DangerousSQLApprovalMiddleware
// ──────────────────────────────────────────────
//
// 拦截 exec_sql 工具调用，对所有危险 SQL 强制用户确认。
//
// 设计原则（安全底线）：
//   任何不确定的情况一律中断，宁可多问一次用户，绝不放过一条未确认的写操作。
//
// 严格遵循 eino ApprovalMiddleware 三段式模式：
//   1. !wasInterrupted → 首次调用，检测危险则中断
//   2. isTarget && hasData && approved → 用户已确认，放行执行
//   3. 其他所有情况 → 重新中断（包括 isTarget 但类型不匹配、非 target 等）

type DangerousSQLApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// WrapInvokableToolCall 拦截同步工具调用。
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

// WrapStreamableToolCall 拦截流式工具调用。
// 必须实现，否则 EnableStreaming=true 时 exec_sql 走流式通道会绕过审批。
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
				// 消费流式结果为完整字符串
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

// execFunc 统一的执行函数签名，屏蔽同步/流式差异。
type execFunc func(ctx context.Context, args string, opts []tool.Option) (string, error)

// approvalGate 是审批逻辑的唯一实现，同步和流式共用，消除重复。
//
// 核心安全原则：只有一条路径能放行执行 —— isTarget && hasData && approved。
// 其他所有路径要么中断，要么返回拒绝信息。绝不存在"兜底放行"。
func approvalGate(
	ctx context.Context,
	toolName string,
	argumentsInJSON string,
	opts []tool.Option,
	exec execFunc,
) (string, error) {

	wasInterrupted, _, savedArgs := tool.GetInterruptState[string](ctx)

	// ── 阶段 1：首次调用 ──
	if !wasInterrupted {
		sql := extractSQLFromArgs(argumentsInJSON)
		if sql == "" || !containsDangerousSQL(sql) {
			// 安全 SQL，直接放行
			return exec(ctx, argumentsInJSON, opts)
		}
		// 危险 SQL → 中断，保存原始参数
		log.Printf("[ApprovalMiddleware] 拦截危险 SQL - tool=%s, sql=%s\n", toolName, sql)
		return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
			SQL:       sql,
			RiskLevel: detectRiskLevel(sql),
			SQLType:   detectSQLType(sql),
		}, argumentsInJSON)
	}

	// ── 阶段 2：从中断恢复 ──
	isTarget, hasData, approval := tool.GetResumeContext[SQLApprovalResult](ctx)

	// 唯一的放行路径：是恢复目标 + 有审批数据 + 用户批准
	if isTarget && hasData && approval.Approved {
		log.Printf("[ApprovalMiddleware] 用户批准执行 - tool=%s\n", toolName)
		return exec(ctx, savedArgs, opts)
	}

	// 用户明确拒绝
	if isTarget && hasData && !approval.Approved {
		reason := approval.DisapproveReason
		if reason == "" {
			reason = "用户取消执行"
		}
		log.Printf("[ApprovalMiddleware] 用户拒绝执行 - tool=%s, reason=%s\n", toolName, reason)
		return fmt.Sprintf(`{"message": "%s", "affectedRows": 0}`, reason), nil
	}

	// 其他所有情况（非 target、无数据、类型不匹配等）→ 重新中断
	// 安全原则：不确定就中断，绝不放行
	sql := extractSQLFromArgs(savedArgs)
	if sql == "" {
		sql = savedArgs // 兜底：无法解析时用原始 JSON
	}
	log.Printf("[ApprovalMiddleware] 重新中断 - tool=%s, isTarget=%v, hasData=%v\n", toolName, isTarget, hasData)
	return "", tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
		SQL:       sql,
		RiskLevel: detectRiskLevel(sql),
		SQLType:   detectSQLType(sql),
	}, savedArgs)
}

// extractSQLFromArgs 从工具参数 JSON 中提取 SQL 字段。
func extractSQLFromArgs(argumentsInJSON string) string {
	var input ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.SQL)
}

// ──────────────────────────────────────────────
// ToolErrorRecoveryMiddleware
// ──────────────────────────────────────────────
//
// 将业务错误（SQL 语法错误等）转为正常 tool result，让 ReAct 循环继续。
// Interrupt 错误必须原样传播，由 Runner 捕获并 checkpoint。

type ToolErrorRecoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// isInterruptErr 使用三种方式可靠检测 eino 中断错误：
//  1. compose.ExtractInterruptInfo — graph 级中断
//  2. compose.IsInterruptRerunError — tool 级中断 (core.InterruptSignal)
//  3. errors.As for *adk.InterruptSignal — 直接类型检查兜底
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
				return "", err // 中断错误必须原样传播
			}
			log.Printf("[ToolErrorRecovery] tool %s error → result - err=%v\n", tCtx.Name, err)
			return fmt.Sprintf("[tool call failed] %s\nPlease adjust parameters and retry.", err.Error()), nil
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
				return nil, err // 中断错误必须原样传播
			}
			log.Printf("[ToolErrorRecovery:Stream] tool %s error → result - err=%v\n", tCtx.Name, err)
			errMsg := fmt.Sprintf("[tool call failed] %s\nPlease adjust parameters and retry.", err.Error())
			return schema.StreamReaderFromArray([]string{errMsg}), nil
		}
		return result, nil
	}, nil
}

// ──────────────────────────────────────────────
// ToolCallLoggingMiddleware
// ──────────────────────────────────────────────
//
// 统一记录所有工具（含 Skill）的调用日志，包括入参、出参和执行耗时。

type ToolCallLoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
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
			// 暂时打印完整的信息吧 result
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
		log.Printf("[ToolCall:Stream] 开始调用 - name=%s, args=%s\n", tCtx.Name, truncateStr(argumentsInJSON, 500))

		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			elapsed := time.Since(startTime)
			log.Printf("[ToolCall:Stream] 调用失败 - name=%s, duration=%v, err=%v\n", tCtx.Name, elapsed, err)
			return nil, err
		}

		var contentBuf strings.Builder
		wrapped := schema.StreamReaderWithConvert(reader, func(s string) (string, error) {
			contentBuf.WriteString(s)
			return s, nil
		})

		go func() {
			for {
				_, err := wrapped.Recv()
				if err != nil {
					elapsed := time.Since(startTime)
					content := contentBuf.String()
					log.Printf("[ToolCall:Stream] 流结束 - name=%s, duration=%v, resultLen=%d, result=%s\n",
						tCtx.Name, elapsed, len(content), truncateStr(content, 500))
					return
				}
			}
		}()

		return reader, nil
	}, nil
}

// truncateStr 截断过长的字符串，避免日志输出过多
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ──────────────────────────────────────────────
// ReductionMiddleware — 工具返回结果精简
// ──────────────────────────────────────────────
//
// 对 query_data 返回的大量数据行自动截断，只保留前 MaxRows 行 + 总行数提示。
// 显著减少 LLM 上下文中的 Token 消耗。

const defaultReductionMaxRows = 100

type ReductionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	MaxRows int
}

func (m *ReductionMiddleware) maxRows() int {
	if m.MaxRows > 0 {
		return m.MaxRows
	}
	return defaultReductionMaxRows
}

func (m *ReductionMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != "query_data" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return result, err
		}
		return m.reduceQueryResult(result), nil
	}, nil
}

func (m *ReductionMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if tCtx.Name != "query_data" {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		reader, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		for {
			chunk, recvErr := reader.Recv()
			if recvErr != nil {
				break
			}
			sb.WriteString(chunk)
		}
		reduced := m.reduceQueryResult(sb.String())
		return schema.StreamReaderFromArray([]string{reduced}), nil
	}, nil
}

func (m *ReductionMiddleware) reduceQueryResult(result string) string {
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result
	}
	maxRows := m.maxRows()
	if len(output.Data) <= maxRows {
		return result
	}
	totalRows := len(output.Data)
	output.Data = output.Data[:maxRows]
	output.Count = totalRows
	reducedJSON, _ := json.Marshal(output)
	log.Printf("[Reduction] query_data 结果精简 - 原始行数=%d, 保留行数=%d\n", totalRows, maxRows)
	return string(reducedJSON)
}

// ──────────────────────────────────────────────
// repairToolMessageSequence — 历史消息序列修复
// ──────────────────────────────────────────────
//
// 修复截断历史后 tool/assistant(tool_calls) 不匹配的问题，确保符合 OpenAI API 规范：
//   - 每个 role=tool 消息必须紧跟在含 tool_calls 的 assistant 消息之后
//   - tool_call_id 必须与前面 assistant 的某个 tool_call.id 匹配
//   - assistant(tool_calls) 的每个 tool_call 都必须有对应的 tool 响应
//
// 注意：此函数仅在 RunStream 入口对历史消息调用一次，
// 不可在 BeforeModelRewriteState 中使用，否则会破坏 ReAct 循环内部状态。

func repairToolMessageSequence(msgs []*schema.Message) []*schema.Message {
	if len(msgs) <= 1 {
		return msgs
	}

	validToolCallIDs := make(map[string]bool, len(msgs))
	for _, msg := range msgs {
		if msg.Role == schema.Assistant {
			for _, tc := range msg.ToolCalls {
				validToolCallIDs[tc.ID] = true
			}
		}
	}

	var filtered []*schema.Message
	for _, msg := range msgs {
		if msg.Role != schema.Tool {
			filtered = append(filtered, msg)
		} else if msg.ToolCallID != "" && validToolCallIDs[msg.ToolCallID] {
			filtered = append(filtered, msg)
		} else {
			log.Printf("[MessageRepair] 跳过孤立 tool 消息 - toolCallID=%s, toolName=%s\n",
				msg.ToolCallID, msg.ToolName)
		}
	}

	ordered := repairToolMessageOrder(filtered)

	return repairUnansweredToolCalls(ordered)
}

func repairToolMessageOrder(msgs []*schema.Message) []*schema.Message {
	var result []*schema.Message
	for _, msg := range msgs {
		if msg.Role != schema.Tool {
			result = append(result, msg)
			continue
		}

		if len(result) == 0 {
			log.Printf("[MessageRepair] 跳过首条 tool 消息 - toolCallID=%s\n", msg.ToolCallID)
			continue
		}

		prev := result[len(result)-1]
		if prev.Role == schema.Assistant && len(prev.ToolCalls) > 0 {
			result = append(result, msg)
		} else if prev.Role == schema.Tool {
			result = append(result, msg)
		} else {
			log.Printf("[MessageRepair] 跳过位置不当的 tool 消息 - toolCallID=%s, prevRole=%s\n",
				msg.ToolCallID, prev.Role)
		}
	}
	return result
}

func repairUnansweredToolCalls(msgs []*schema.Message) []*schema.Message {
	if len(msgs) == 0 {
		return msgs
	}

	answeredToolCalls := make(map[string]bool, len(msgs))
	for _, msg := range msgs {
		if msg.Role == schema.Tool && msg.ToolCallID != "" {
			answeredToolCalls[msg.ToolCallID] = true
		}
	}

	var result []*schema.Message
	for _, msg := range msgs {
		if msg.Role != schema.Assistant || len(msg.ToolCalls) == 0 {
			result = append(result, msg)
			continue
		}

		var remaining []schema.ToolCall
		for _, tc := range msg.ToolCalls {
			if answeredToolCalls[tc.ID] {
				remaining = append(remaining, tc)
			}
		}

		switch {
		case len(remaining) == 0:
			result = append(result, schema.AssistantMessage(msg.Content, nil))
			log.Printf("[MessageRepair] 移除无响应的 assistant tool_calls - content=%q\n",
				truncateStr(msg.Content, 50))
		case len(remaining) < len(msg.ToolCalls):
			result = append(result, schema.AssistantMessage(msg.Content, remaining))
		default:
			result = append(result, msg)
		}
	}

	return result
}
