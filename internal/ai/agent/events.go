package agent

import (
	"context"
	"errors"
	"io"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"websql/internal/audit"
	"websql/internal/pkg/mask"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// streamRevisionCheckInterval 流式输出期间的权限复核节流间隔。
const streamRevisionCheckInterval = 500 * time.Millisecond

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

// revisionCheckDue 判断流式输出当前是否需要做一次权限修订号复核（时间节流）。
//
// 一次模型流式响应只对应一个事件，事件级检查覆盖不到 chunk 推送；
// 但每 chunk 都读一次也不必要，因此按固定间隔节流。
func revisionCheckDue(last time.Time, now time.Time) bool {
	return !last.IsZero() && now.Sub(last) >= streamRevisionCheckInterval
}

// safeErrorForLog 返回可安全写入日志的错误摘要。
//
// *url.Error 的 Error() 本身就是 `Op "URL": Err`，即使不用 %#v 也会带出完整 URL；
// 这里只保留 scheme://host，并掩码 query 与 userinfo。
func safeErrorForLog(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		host := ue.URL
		if parsed, parseErr := url.Parse(ue.URL); parseErr == nil && parsed.Host != "" {
			parsed.User = nil
			parsed.RawQuery = ""
			parsed.Fragment = ""
			host = parsed.String()
		}
		return mask.MaskSecret(ue.Op + " " + host + ": " + ue.Err.Error())
	}
	return mask.MaskSecret(err.Error())
}

// logUnwrappedError 逐层解包错误并记录日志（不使用 %#v：*url.Error 等结构会带出完整 URL/凭据）
func logUnwrappedError(err error) {
	if err == nil {
		return
	}
	log.Printf("[Agent] 错误详情 - err=%s, type=%T\n", safeErrorForLog(err), err)
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		err = unwrapped
	}
	log.Printf("[Agent] 根错误 - err=%q, type=%T\n", safeErrorForLog(err), err)

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

// pendingResourceInfo 工具调用对应的资源溯源及其完整性。
type pendingResourceInfo struct {
	refs     []ResourceRef
	complete bool
}

// streamAttempt 累积单次模型流式输出（一次尝试）。
//
// 安全语义：
//   - content/thinking 为原始接收内容，只用于长度统计与重复检测，禁止落库或推给前端；
//   - safeContent/safeThinking 为经过 OutputGuard 审计后的内容（可推送、可落库）；
//   - emittedContent/emittedThinking 只记录「已推送给用户」的净化前缀，供 retract 使用。
type streamAttempt struct {
	content  strings.Builder
	thinking strings.Builder

	safeContent  strings.Builder
	safeThinking strings.Builder

	emittedContent  strings.Builder
	emittedThinking strings.Builder

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
func (a *SQLAgent) processEvents(ctx context.Context, runID string, iter *adk.AsyncIterator[*adk.AgentEvent], flush func(StreamChunk), sess *Session, checkPointID string) bool {
	interrupted := false
	eventIdx, toolResultCnt, assistantCnt := 0, 0, 0
	overallStart := time.Now()

	// 安全上下文必须在整个事件消费期间可用；缺失时立即终止，不推送任何内容。
	sec, secErr := RunSecurityFrom(ctx)
	if secErr != nil {
		log.Printf("[Agent] 事件消费缺少安全上下文 - err=%v\n", secErr)
		flush(StreamChunk{Type: "error", Content: "安全上下文无效，本次请求已终止"})
		return false
	}
	// toolCallID → 资源溯源：assistant 消息里的 tool_calls 先登记，tool 消息到达时附带。
	pendingResources := make(map[string]pendingResourceInfo)

	for {
		event, ok := iter.Next()
		if !ok {
			log.Printf("[Agent] 事件迭代结束 - totalEvents=%d, toolResults=%d, assistantMsgs=%d, elapsed=%v\n",
				eventIdx, toolResultCnt, assistantCnt, time.Since(overallStart))
			break
		}
		eventIdx++

		// 每个安全批次推送前复核权限修订号：旧 Scope 不得继续放行数据。
		if !sec.RevisionStillValid() {
			log.Printf("[Agent] 运行中权限变更或租约过期，终止输出 - eventIdx=%d\n", eventIdx)
			recordSecurityEvent(StageRevision, "stream", "stop")
			auditSecurityDenied("revision", "stream", "permission_revision_changed", sec.UserID)
			flush(StreamChunk{Type: "error", Content: "权限已发生变化，本次回复已停止，请重新发起"})
			_ = sess.CleanAndSave()
			return false
		}

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
			a.consumeToolStream(eventIdx, mo, sess, sec, pendingResources)
		case mo.IsStreaming && mo.MessageStream != nil:
			assistantCnt++
			if a.consumeAssistantStream(runID, eventIdx, mo.MessageStream, flush, sess, sec, pendingResources) {
				// 检测到模型输出退化（重复内容）或审计异常，已请求取消本次运行
				return false
			}
		case role == schema.Tool && mo.Message != nil:
			toolResultCnt++
			msg := mo.Message
			log.Printf("[Agent] 工具结果 - eventIdx=%d, toolName=%s, callID=%s, contentLen=%d\n",
				eventIdx, msg.ToolName, msg.ToolCallID, len(msg.Content))
			safe, guardErr := guardWholeText(sec, msg.Content)
			if guardErr != nil {
				log.Printf("[Agent] 工具结果审计失败，拒绝落库 - eventIdx=%d, err=%v\n", eventIdx, guardErr)
				recordSecurityEvent(StageOutput, "tool_result", "deny")
				auditSecurityDenied("output", "tool_result", "output_guard_failed", sec.UserID)
				flush(StreamChunk{Type: "error", Content: "工具结果未通过安全检查，已停止本次生成"})
				_ = sess.CleanAndSave()
				return false
			}
			sess.AppendMessageNoSave(SessionMessage{
				Role:       "tool",
				Content:    safe,
				ToolCallID: msg.ToolCallID,
				ToolName:   firstNonEmptyStr(msg.ToolName, mo.ToolName),
				Security:   newMessageSecurityWithTrace(sec, safe, pendingResources[msg.ToolCallID].refs, !pendingResources[msg.ToolCallID].complete),
			})
		case role == schema.Assistant && mo.Message != nil:
			assistantCnt++
			msg := mo.Message
			// 非流式正文与流式共用同一审计算法（整段 Push + Close）。
			safeContent, guardErr := guardWholeText(sec, msg.Content)
			if guardErr != nil {
				log.Printf("[Agent] 助手正文审计失败，拒绝推送 - eventIdx=%d, err=%v\n", eventIdx, guardErr)
				recordSecurityEvent(StageOutput, "assistant", "deny")
				auditSecurityDenied("output", "assistant", "output_guard_failed", sec.UserID)
				flush(StreamChunk{Type: "error", Content: "回复内容未通过安全检查，已停止本次生成"})
				_ = sess.CleanAndSave()
				return false
			}
			if len(msg.ToolCalls) > 0 {
				names := make([]string, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					names = append(names, tc.Function.Name)
					refs, complete := resourcesFromToolCall(tc.Function.Name, tc.Function.Arguments, sec)
					pendingResources[tc.ID] = pendingResourceInfo{refs: refs, complete: complete}
				}
				log.Printf("[Agent] 助手工具调用请求 - eventIdx=%d, funcs=%v, contentLen=%d\n", eventIdx, names, len(msg.Content))
				if safeContent != "" {
					flush(StreamChunk{Type: "content", Content: safeContent})
				}
				sess.AppendMessageNoSave(SessionMessage{
					Role:      "assistant",
					Content:   safeContent,
					ToolCalls: sanitizeToolCallsForPersist(sec, msg.ToolCalls),
					// 工具调用承载的数据来源无法逐列枚举，权限变化后必须隔离
					Security: newMessageSecurityWithTrace(sec, safeContent, nil, true),
				})
			} else if safeContent != "" {
				flush(StreamChunk{Type: "content", Content: safeContent})
				sess.AppendMessageNoSave(SessionMessage{
					Role:     "assistant",
					Content:  safeContent,
					Security: newMessageSecurity(sec, safeContent, nil),
				})
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
			// 文件删除类编辑：复用 danger_confirm 交互（展示摘要 + resume 批准）。
			if editInfo, editOK := ictx.Info.(*EditApprovalInfo); editOK {
				hasDangerConfirm = true
				log.Printf("[Agent] 文件删除中断 - eventIdx=%d, id=%s, ops=%d", eventIdx, ictx.ID, len(editInfo.Summary))
				flush(StreamChunk{
					Type:         "danger_confirm",
					Content:      "检测到需要确认的文件删除操作",
					SQL:          editInfo.Summary,
					InterruptID:  ictx.ID,
					CheckPointID: checkPointID,
				})
				continue
			}
			log.Printf("[Agent] 未知类型中断 - eventIdx=%d, id=%s, info=%T\n", eventIdx, ictx.ID, ictx.Info)
			continue
		}
		hasDangerConfirm = true
		log.Printf("[Agent] 危险 SQL 中断 - eventIdx=%d, id=%s, riskLevel=%s, sqlType=%s, sqlLen=%d\n",
			eventIdx, ictx.ID, sqlInfo.RiskLevel, sqlInfo.SQLType, len(sqlInfo.SQL))
		// 展示副本只保留 SQL 结构，字面量内容脱敏；待执行的原始语句只保存在服务端 checkpoint。
		flush(StreamChunk{
			Type:         "danger_confirm",
			Content:      "检测到需要确认的写操作",
			SQL:          audit.MaskSQL(sqlInfo.SQL),
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

// consumeToolStream 消费流式工具结果并按 tool 消息落库。
//
// 落库副本必须经脱敏；工具结果可能包含查询到的明细数据与错误原文。
func (a *SQLAgent) consumeToolStream(eventIdx int, mo *adk.MessageVariant, sess *Session, sec *RunSecurityContext, pendingResources map[string]pendingResourceInfo) {
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
	safe, guardErr := guardWholeText(sec, content.String())
	if guardErr != nil {
		// fail-closed：不落库、不返回；用一个安全占位代替原始内容
		log.Printf("[Agent] 流式工具结果审计失败 - eventIdx=%d, err=%v\n", eventIdx, guardErr)
		recordSecurityEvent(StageOutput, "tool_result_stream", "deny")
		auditSecurityDenied("output", "tool_result_stream", "output_guard_failed", sec.UserID)
		safe = HistoryQuarantinePlaceholder
	}
	info := pendingResources[callID]
	sess.AppendMessageNoSave(SessionMessage{
		Role:       "tool",
		Content:    safe,
		ToolCallID: callID,
		ToolName:   toolName,
		Security:   newMessageSecurityWithTrace(sec, safe, info.refs, !info.complete),
	})
}

// consumeAssistantStream 消费一次模型流式输出。返回 true 表示检测到输出退化或审计异常并已取消运行。
//
// 关键安全语义：
//   - content 与 thinking 使用独立 guard，防止跨通道拼接绕过检测；
//   - 只有通过审计的前缀才会推送与落库；holdback 在 EOF 时审计后释放；
//   - 重试只撤回「已推送的净化文本」，绝不引用原始 attempt 内容；
//   - 审计异常时丢弃未推送缓冲并终止运行，不回退为推送原文。
func (a *SQLAgent) consumeAssistantStream(runID string, eventIdx int, stream *schema.StreamReader[*schema.Message], flush func(StreamChunk), sess *Session, sec *RunSecurityContext, pendingResources map[string]pendingResourceInfo) bool {
	defer stream.Close()
	var att streamAttempt
	start := time.Now()
	repeatCount, lastChunk := 0, ""
	const maxRepeats = 10

	masker := sec.Masker
	if masker == nil {
		masker = NewNoopMasker()
	}
	contentGuard := NewOutputGuard(masker, 0)
	thinkingGuard := NewOutputGuard(masker, 0)

	// 长流式响应期间按时间节流复核权限修订号：
	// 一次模型响应是一个事件，外层事件循环的检查覆盖不到 chunk 级推送。
	lastRevCheck := time.Now()
	var errRevisionStopped = errors.New("permission revision changed during stream")
	failRevision := func() bool {
		log.Printf("[Agent] 流式输出期间权限变更，终止输出 - eventIdx=%d\n", eventIdx)
		recordSecurityEvent(StageRevision, "stream", "stop")
		auditSecurityDenied("revision", "stream", "permission_revision_changed", sec.UserID)
		flush(StreamChunk{Type: "error", Content: "权限已发生变化，本次回复已停止，请重新发起"})
		a.Cancel(runID)
		return true
	}
	checkRevision := func() bool {
		if !revisionCheckDue(lastRevCheck, time.Now()) {
			return false
		}
		lastRevCheck = time.Now()
		return !sec.RevisionStillValid()
	}

	// push 把审计后的安全前缀写入累加器并推送。
	push := func(guard *OutputGuard, text string, safeBuilder, emitted *strings.Builder, typ string) error {
		if checkRevision() {
			return errRevisionStopped
		}
		safe, err := guard.Push(text)
		if err != nil {
			return err
		}
		if safe == "" {
			return nil
		}
		safeBuilder.WriteString(safe)
		emitted.WriteString(safe)
		flush(StreamChunk{Type: typ, Content: safe})
		return nil
	}
	// closeGuard 在 EOF / 流异常时释放 holdback 并推送尾部。
	closeGuard := func(guard *OutputGuard, safeBuilder, emitted *strings.Builder, typ string) error {
		if checkRevision() {
			return errRevisionStopped
		}
		safe, err := guard.Close()
		if err != nil {
			return err
		}
		if safe == "" {
			return nil
		}
		safeBuilder.WriteString(safe)
		emitted.WriteString(safe)
		flush(StreamChunk{Type: typ, Content: safe})
		return nil
	}

	failClosed := func(err error) bool {
		log.Printf("[Agent] 输出审计失败，丢弃未推送缓冲并终止 - eventIdx=%d, err=%v\n", eventIdx, err)
		recordSecurityEvent(StageOutput, "guard", "deny")
		auditSecurityDenied("output", "stream", "output_guard_failed", sec.UserID)
		flush(StreamChunk{Type: "error", Content: "回复内容未通过安全检查，已停止本次生成"})
		a.Cancel(runID)
		return true
	}

	for {
		chunk, err := stream.Recv()
		if err != nil {
			var retryErr *adk.WillRetryError
			switch {
			case errors.Is(err, io.EOF):
				if err := closeGuard(contentGuard, &att.safeContent, &att.emittedContent, "content"); err != nil {
					if errors.Is(err, errRevisionStopped) {
						return failRevision()
					}
					return failClosed(err)
				}
				if err := closeGuard(thinkingGuard, &att.safeThinking, &att.emittedThinking, "thinking"); err != nil {
					if errors.Is(err, errRevisionStopped) {
						return failRevision()
					}
					return failClosed(err)
				}
				a.persistAttempt(&att, sess, sec, pendingResources)
			case errors.As(err, &retryErr):
				// 该次尝试作废：只撤回已推送的净化文本，不落库；guard 直接丢弃（不 Close，不释放缓冲）。
				log.Printf("[Agent] 模型流式输出将重试，撤回本次输出 - eventIdx=%d, attempt=%d, emittedLen=%d\n",
					eventIdx, retryErr.RetryAttempt, att.emittedContent.Len())
				if att.emittedContent.Len() > 0 || att.emittedThinking.Len() > 0 {
					flush(StreamChunk{Type: "retract", Content: att.emittedContent.String(), Thinking: att.emittedThinking.String()})
				}
			default:
				// 流中途异常：释放已审计前缀，丢弃可能残缺的 tool_calls
				log.Printf("[Agent] 模型流异常结束 - eventIdx=%d, chunks=%d, err=%v\n", eventIdx, att.chunks, err)
				att.toolCalls = nil
				if err := closeGuard(contentGuard, &att.safeContent, &att.emittedContent, "content"); err != nil {
					if errors.Is(err, errRevisionStopped) {
						return failRevision()
					}
					return failClosed(err)
				}
				if err := closeGuard(thinkingGuard, &att.safeThinking, &att.emittedThinking, "thinking"); err != nil {
					if errors.Is(err, errRevisionStopped) {
						return failRevision()
					}
					return failClosed(err)
				}
				a.persistAttempt(&att, sess, sec, pendingResources)
			}
			log.Printf("[Agent] 模型流结束 - eventIdx=%d, chunks=%d, contentLen=%d, toolCalls=%d, elapsed=%v\n",
				eventIdx, att.chunks, att.safeContent.Len(), len(att.toolCalls), time.Since(start))
			return false
		}
		if chunk == nil {
			continue
		}
		att.chunks++
		if chunk.ReasoningContent != "" {
			att.thinking.WriteString(chunk.ReasoningContent)
			if err := push(thinkingGuard, chunk.ReasoningContent, &att.safeThinking, &att.emittedThinking, "thinking"); err != nil {
				if errors.Is(err, errRevisionStopped) {
					return failRevision()
				}
				return failClosed(err)
			}
		}
		if chunk.Content != "" {
			if chunk.Content == lastChunk {
				repeatCount++
				if repeatCount >= maxRepeats {
					log.Printf("[Agent] 检测到模型重复输出，取消本次运行 - eventIdx=%d, chunkLen=%d\n",
						eventIdx, len(chunk.Content))
					att.toolCalls = nil
					a.persistAttempt(&att, sess, sec, pendingResources)
					flush(StreamChunk{Type: "error", Content: "模型输出异常（重复内容），已自动停止本次生成"})
					a.Cancel(runID)
					return true
				}
			} else {
				repeatCount = 0
			}
			lastChunk = chunk.Content
			att.content.WriteString(chunk.Content)
			if err := push(contentGuard, chunk.Content, &att.safeContent, &att.emittedContent, "content"); err != nil {
				if errors.Is(err, errRevisionStopped) {
					return failRevision()
				}
				return failClosed(err)
			}
		}
		if len(chunk.ToolCalls) > 0 {
			att.toolCalls = append(att.toolCalls, chunk.ToolCalls...)
		}
	}
}

// persistAttempt 把一次完整的模型输出写入会话（不立即落库，由 debounce/运行结束统一保存）。
//
// 落库副本必须使用审计后的净化内容；原始 att.content 永不写库。
func (a *SQLAgent) persistAttempt(att *streamAttempt, sess *Session, sec *RunSecurityContext, pendingResources map[string]pendingResourceInfo) {
	safe := att.safeContent.String()
	if safe == "" && len(att.toolCalls) == 0 {
		return
	}
	sm := SessionMessage{
		Role:     "assistant",
		Content:  safe,
		Security: newMessageSecurity(sec, safe, nil),
	}
	if len(att.toolCalls) > 0 {
		calls := mergeToolCalls(att.toolCalls)
		// 与非流式路径一致：先登记每个 tool_call 的资源溯源，后续 tool 结果
		// 到达时才能附带 Resources；无法逐列枚举，因此 assistant 标记为不可溯源。
		var refs []ResourceRef
		for _, tc := range calls {
			callRefs, complete := resourcesFromToolCall(tc.Function.Name, tc.Function.Arguments, sec)
			if pendingResources != nil {
				pendingResources[tc.ID] = pendingResourceInfo{refs: callRefs, complete: complete}
			}
			refs = append(refs, callRefs...)
		}
		sm.ToolCalls = sanitizeToolCallsForPersist(sec, calls)
		sm.Security = newMessageSecurityWithTrace(sec, safe, refs, true)
	} else if len(pendingResources) > 0 {
		// 纯正文 assistant（无 tool_calls）往往是本轮工具结果的汇总：
		// 若不给它挂资源溯源，权限收缩后工具结果会被占位，但模型仍能从这段正文
		// 引用已失权的数据（实测：回收 bizb 后模型仍能报出 notes=8 行）。
		// 因此把本轮已登记的资源聚合到正文上，让 filterHistoryForRun 能逐条再授权。
		refs := runResourceRefs(pendingResources)
		if len(refs) > 0 {
			// 任一工具结果无法完整枚举来源（复杂 SQL）时，正文仍按不可溯源处理。
			complete := true
			for _, info := range pendingResources {
				if !info.complete {
					complete = false
					break
				}
			}
			sm.Security = newMessageSecurityWithTrace(sec, safe, refs, !complete)
		}
	}
	sess.AppendMessageNoSave(sm)
}

// runResourceRefs 汇总本轮已登记工具结果命中的资源并去重（保持稳定顺序）。
//
// 任何一条工具结果的来源无法完整枚举（complete=false）时，调用方应保守处理：
// 这里只负责合并，不负责决定是否可溯源；目前正文汇总只在所有来源完整时挂 refs。
func runResourceRefs(pending map[string]pendingResourceInfo) []ResourceRef {
	seen := make(map[string]bool)
	refs := make([]ResourceRef, 0, len(pending))
	for _, info := range pending {
		for _, ref := range info.refs {
			key := ref.ConnID + "\x00" + ref.Schema + "\x00" + ref.Table
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	// 同一批资源的落库顺序必须稳定（map 遍历无序），否则审计比对与 diff 会出现噪声。
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ConnID != refs[j].ConnID {
			return refs[i].ConnID < refs[j].ConnID
		}
		if refs[i].Schema != refs[j].Schema {
			return refs[i].Schema < refs[j].Schema
		}
		if refs[i].Table != refs[j].Table {
			return refs[i].Table < refs[j].Table
		}
		return refs[i].Column < refs[j].Column
	})
	return refs
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
