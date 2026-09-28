package agent

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	"websql/internal/pkg/strutil"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// extractRootErrorMessage 从错误链中提取根错误消息
func extractRootErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err.Error()
		}
		err = unwrapped
	}
}

// logUnwrappedError 逐层解包错误并记录日志（不使用 %#v：*url.Error 等结构会带出完整 URL/凭据）
func logUnwrappedError(err error) {
	if err == nil {
		return
	}
	log.Printf("[Agent] 错误详情 - err=%v, type=%T\n", err, err)
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		err = unwrapped
	}
	log.Printf("[Agent] 根错误 - err=%q, type=%T\n", err, err)

	type apiError interface {
		GetCode() any
		GetMessage() string
		GetType() string
		GetHTTPStatusCode() int
	}
	if ae, ok := err.(apiError); ok {
		log.Printf("[Agent] APIError - code=%v, message=%q, type=%q, httpStatus=%d\n",
			ae.GetCode(), ae.GetMessage(), ae.GetType(), ae.GetHTTPStatusCode())
	}
}

// userFacingError 把内部错误转换为可展示给用户的文案，避免把驱动/网络错误原文
// （可能含内网地址、SQL 片段、密钥前缀）直接推送到前端。
func userFacingError(err error) string {
	msg := extractRootErrorMessage(err)
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "unauthorized"):
		return "AI 服务鉴权失败，请联系管理员检查模型配置（API Key）"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests"):
		return "AI 服务繁忙（限流），请稍后重试"
	case strings.Contains(lower, "context length") || strings.Contains(lower, "maximum context") || strings.Contains(lower, "too long"):
		return "对话上下文过长，请新建会话或缩小查询范围后重试"
	case strings.Contains(lower, "content_filter") || strings.Contains(lower, "content_policy") || strings.Contains(lower, "safety"):
		return "请求触发了模型的内容安全策略，请调整提问后重试"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "AI 服务响应超时，请稍后重试"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "dial tcp"):
		return "无法连接 AI 服务，请联系管理员检查模型服务地址"
	}
	return "AI 处理出错，请稍后重试或换一种问法"
}

// streamAttempt 累积单次模型流式输出（一次尝试）
type streamAttempt struct {
	content   strings.Builder
	thinking  strings.Builder
	toolCalls []schema.ToolCall
	chunks    int
}

// processEvents 处理 Agent 事件流，返回本次运行是否停在「等待用户确认」的中断上。
//
// 关键语义（对照 eino v0.9.21 源码）：
//   - 配置 ModelRetryConfig 后，被判定需要重试的那次模型调用，其 MessageStream 以
//     *adk.WillRetryError 结束（adk/wrappers.go buildStreamConvertOptions）。eino 自己
//     构造历史时会跳过这类事件（adk/flow.go），消费方也必须丢弃该次尝试的内容，
//     否则前端会看到重复/拼接的输出，会话历史里也会多出残缺的 assistant 消息。
//     这里在收到 WillRetryError 时向前端发送 retract 事件撤回该次尝试已推送的文本。
//   - 流式 Tool 事件（Streamable 工具）的 role=tool，必须按工具结果落库（带 ToolCallID），
//     不能当作助手正文推给前端。
//   - 流中途出现非 EOF 错误时，不完整的 tool_calls 参数不落库（下一轮会因 dangling
//     tool_calls 被 patchtoolcalls 补占位，而残缺的 JSON 参数会污染后续上下文）。
func (a *SQLAgent) processEvents(runID string, iter *adk.AsyncIterator[*adk.AgentEvent], flush func(StreamChunk), sess *Session, checkPointID string) bool {
	interrupted := false
	eventIdx, toolResultCnt, assistantCnt := 0, 0, 0
	overallStart := time.Now()

	for {
		event, ok := iter.Next()
		if !ok {
			log.Printf("[Agent] 事件迭代结束 - totalEvents=%d, toolResults=%d, assistantMsgs=%d, elapsed=%v\n",
				eventIdx, toolResultCnt, assistantCnt, time.Since(overallStart))
			break
		}
		eventIdx++

		if event.Err != nil {
			if a.handleEventError(eventIdx, event.Err, flush, sess) {
				break
			}
			continue
		}

		// 中断：危险 SQL 待确认
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = a.handleInterrupt(eventIdx, event.Action.Interrupted, flush, checkPointID)
			_ = sess.SaveToDB()
			break
		}

		hasOutput := event.Output != nil && event.Output.MessageOutput != nil
		hasExit := event.Action != nil && event.Action.Exit
		if !hasOutput {
			if hasExit {
				break
			}
			continue
		}

		mo := event.Output.MessageOutput
		role := mo.Role
		if role == "" && mo.Message != nil {
			role = mo.Message.Role
		}

		switch {
		case mo.IsStreaming && mo.MessageStream != nil && role == schema.Tool:
			toolResultCnt++
			a.consumeToolStream(eventIdx, mo, sess)
		case mo.IsStreaming && mo.MessageStream != nil:
			assistantCnt++
			if a.consumeAssistantStream(runID, eventIdx, mo.MessageStream, flush, sess) {
				// 检测到模型输出退化（重复内容），已请求取消本次运行
				return false
			}
		case role == schema.Tool && mo.Message != nil:
			toolResultCnt++
			msg := mo.Message
			log.Printf("[Agent] 工具结果 - eventIdx=%d, toolName=%s, callID=%s, contentLen=%d\n",
				eventIdx, msg.ToolName, msg.ToolCallID, len(msg.Content))
			sess.AppendMessageNoSave(SessionMessage{
				Role:       "tool",
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
				ToolName:   firstNonEmptyStr(msg.ToolName, mo.ToolName),
			})
		case role == schema.Assistant && mo.Message != nil:
			assistantCnt++
			msg := mo.Message
			if len(msg.ToolCalls) > 0 {
				names := make([]string, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					names = append(names, tc.Function.Name)
				}
				log.Printf("[Agent] 助手工具调用请求 - eventIdx=%d, funcs=%v, contentLen=%d\n", eventIdx, names, len(msg.Content))
				if msg.Content != "" {
					flush(StreamChunk{Type: "content", Content: msg.Content})
				}
				sess.AppendMessageNoSave(SessionMessage{
					Role:      "assistant",
					Content:   msg.Content,
					ToolCalls: sessionToolCallsFromSchema(msg.ToolCalls),
				})
			} else if msg.Content != "" {
				flush(StreamChunk{Type: "content", Content: msg.Content})
				sess.AppendMessageNoSave(SessionMessage{Role: "assistant", Content: msg.Content})
			}
		}

		if hasExit {
			break
		}
	}
	return interrupted
}

// handleEventError 处理事件级错误，返回 true 表示应终止事件循环
func (a *SQLAgent) handleEventError(eventIdx int, err error, flush func(StreamChunk), sess *Session) bool {
	var retryErr *adk.WillRetryError
	if errors.As(err, &retryErr) {
		// 非流式模式下的重试通知：内容尚未推送，直接忽略
		log.Printf("[Agent] 模型调用将重试 - eventIdx=%d, attempt=%d\n", eventIdx, retryErr.RetryAttempt)
		return false
	}
	log.Printf("[Agent] 事件错误 - eventIdx=%d\n", eventIdx)
	logUnwrappedError(err)

	_ = sess.CleanAndSave() // 原子地「清理末尾不完整 tool_calls + 落库」

	var cancelErr *adk.CancelError
	switch {
	case errors.As(err, &cancelErr):
		flush(StreamChunk{Type: "cancelled", Content: "已停止生成"})
	case errors.Is(err, context.DeadlineExceeded):
		flush(StreamChunk{Type: "error", Content: "AI 处理超时，部分操作可能未完成。你可以继续提问，AI 会基于已有对话历史继续处理。"})
	case errors.Is(err, context.Canceled):
		// 客户端断开：无需推送
	case errors.Is(err, adk.ErrExceedMaxIterations) || strings.Contains(err.Error(), "exceeds max iterations"):
		flush(StreamChunk{Type: "error", Content: "AI 处理步骤过多，部分查询尝试未完成，已执行的操作可能已生效。你可以继续提问，AI 会基于已有对话历史继续处理。"})
	case strings.Contains(err.Error(), "stream reader is empty") || strings.Contains(err.Error(), "concat stream reader fail"):
		flush(StreamChunk{Type: "error", Content: "AI 处理遇到内部错误，前置工具调用可能未成功。你可以重新提问或提供更具体的指令。"})
	default:
		flush(StreamChunk{Type: "error", Content: userFacingError(err)})
	}
	return true
}

// handleInterrupt 处理中断事件，返回是否存在需要用户确认的危险 SQL
func (a *SQLAgent) handleInterrupt(eventIdx int, info *adk.InterruptInfo, flush func(StreamChunk), checkPointID string) bool {
	hasDangerConfirm := false
	for _, ictx := range info.InterruptContexts {
		if !ictx.IsRootCause {
			continue
		}
		sqlInfo, ok := ictx.Info.(*DangerousSQLInfo)
		if !ok {
			log.Printf("[Agent] 未知类型中断 - eventIdx=%d, id=%s, info=%T\n", eventIdx, ictx.ID, ictx.Info)
			continue
		}
		hasDangerConfirm = true
		log.Printf("[Agent] 危险 SQL 中断 - eventIdx=%d, id=%s, riskLevel=%s, sqlType=%s, sqlLen=%d\n",
			eventIdx, ictx.ID, sqlInfo.RiskLevel, sqlInfo.SQLType, len(sqlInfo.SQL))
		flush(StreamChunk{
			Type:         "danger_confirm",
			Content:      "检测到需要确认的写操作",
			SQL:          sqlInfo.SQL,
			InterruptID:  ictx.ID,
			CheckPointID: checkPointID,
		})
	}
	if !hasDangerConfirm {
		log.Printf("[Agent] 中断事件无 DangerousSQLInfo，视为异常结束 - eventIdx=%d\n", eventIdx)
		flush(StreamChunk{Type: "error", Content: "AI 处理出现异常中断，请重试"})
	}
	return hasDangerConfirm
}

// consumeToolStream 消费流式工具结果并按 tool 消息落库
func (a *SQLAgent) consumeToolStream(eventIdx int, mo *adk.MessageVariant, sess *Session) {
	defer mo.MessageStream.Close()
	var content strings.Builder
	callID, toolName := "", mo.ToolName
	for {
		chunk, err := mo.MessageStream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("[Agent] 工具结果流异常结束 - eventIdx=%d, err=%v\n", eventIdx, err)
			}
			break
		}
		if chunk == nil {
			continue
		}
		content.WriteString(chunk.Content)
		if chunk.ToolCallID != "" {
			callID = chunk.ToolCallID
		}
		if chunk.ToolName != "" {
			toolName = chunk.ToolName
		}
	}
	log.Printf("[Agent] 工具结果（流式）- eventIdx=%d, toolName=%s, callID=%s, contentLen=%d\n",
		eventIdx, toolName, callID, content.Len())
	sess.AppendMessageNoSave(SessionMessage{
		Role:       "tool",
		Content:    content.String(),
		ToolCallID: callID,
		ToolName:   toolName,
	})
}

// consumeAssistantStream 消费一次模型流式输出。返回 true 表示检测到输出退化并已取消运行。
func (a *SQLAgent) consumeAssistantStream(runID string, eventIdx int, stream *schema.StreamReader[*schema.Message], flush func(StreamChunk), sess *Session) bool {
	defer stream.Close()
	var att streamAttempt
	start := time.Now()
	repeatCount, lastChunk := 0, ""
	const maxRepeats = 10

	for {
		chunk, err := stream.Recv()
		if err != nil {
			var retryErr *adk.WillRetryError
			switch {
			case errors.Is(err, io.EOF):
				a.persistAttempt(&att, sess)
			case errors.As(err, &retryErr):
				// 该次尝试作废：撤回已推送的文本，不落库，等待下一次尝试的事件
				log.Printf("[Agent] 模型流式输出将重试，撤回本次输出 - eventIdx=%d, attempt=%d, contentLen=%d\n",
					eventIdx, retryErr.RetryAttempt, att.content.Len())
				if att.content.Len() > 0 || att.thinking.Len() > 0 {
					flush(StreamChunk{Type: "retract", Content: att.content.String(), Thinking: att.thinking.String()})
				}
			default:
				// 流中途异常：保留已输出的正文，丢弃可能残缺的 tool_calls
				log.Printf("[Agent] 模型流异常结束 - eventIdx=%d, chunks=%d, err=%v\n", eventIdx, att.chunks, err)
				att.toolCalls = nil
				a.persistAttempt(&att, sess)
			}
			log.Printf("[Agent] 模型流结束 - eventIdx=%d, chunks=%d, contentLen=%d, toolCalls=%d, elapsed=%v\n",
				eventIdx, att.chunks, att.content.Len(), len(att.toolCalls), time.Since(start))
			return false
		}
		if chunk == nil {
			continue
		}
		att.chunks++
		if chunk.ReasoningContent != "" {
			att.thinking.WriteString(chunk.ReasoningContent)
			flush(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
		}
		if chunk.Content != "" {
			if chunk.Content == lastChunk {
				repeatCount++
				if repeatCount >= maxRepeats {
					log.Printf("[Agent] 检测到模型重复输出，取消本次运行 - eventIdx=%d, chunk=%q\n",
						eventIdx, strutil.TruncateBytes(chunk.Content, 40))
					att.toolCalls = nil
					a.persistAttempt(&att, sess)
					flush(StreamChunk{Type: "error", Content: "模型输出异常（重复内容），已自动停止本次生成"})
					a.Cancel(runID)
					return true
				}
			} else {
				repeatCount = 0
			}
			lastChunk = chunk.Content
			att.content.WriteString(chunk.Content)
			flush(StreamChunk{Type: "content", Content: chunk.Content})
		}
		if len(chunk.ToolCalls) > 0 {
			att.toolCalls = append(att.toolCalls, chunk.ToolCalls...)
		}
	}
}

// persistAttempt 把一次完整的模型输出写入会话（不立即落库，由 debounce/运行结束统一保存）
func (a *SQLAgent) persistAttempt(att *streamAttempt, sess *Session) {
	if att.content.Len() == 0 && len(att.toolCalls) == 0 {
		return
	}
	sm := SessionMessage{Role: "assistant", Content: att.content.String()}
	if len(att.toolCalls) > 0 {
		sm.ToolCalls = sessionToolCallsFromSchema(mergeToolCalls(att.toolCalls))
	}
	sess.AppendMessageNoSave(sm)
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
