// events.go — event processing logic extracted from agent.go
package agent

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

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

// logUnwrappedError 逐层解包错误并记录日志
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
		log.Printf("[Agent] 错误原因 - err=%v, type=%T\n", unwrapped, unwrapped)
		err = unwrapped
	}
	log.Printf("[Agent] 根错误 - err=%q, type=%T\n", err, err)

	// 尝试提取 APIError 的详细信息
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

	// 使用 fmt.Sprintf("%#v") 打印完整结构
	log.Printf("[Agent] 根错误结构 - %#v\n", err)
}

// processEvents 处理 Agent 事件流
func (a *SQLAgent) processEvents(iter *adk.AsyncIterator[*adk.AgentEvent], flush func(StreamChunk), sess *Session, checkPointID string) (strings.Builder, bool) {
	var fullResponse strings.Builder
	interrupted := false
	eventIdx := 0

	for {
		eventStart := time.Now()
		event, ok := iter.Next()
		if !ok {
			log.Printf("[Agent] 事件迭代结束 - totalEvents=%d\n", eventIdx)
			break
		}
		eventIdx++
		if event.Err != nil {
			log.Printf("[Agent] 事件错误 - err=%+v\n", event.Err)
			logUnwrappedError(event.Err)
			if errors.Is(event.Err, context.Canceled) || errors.Is(event.Err, context.DeadlineExceeded) {
				// 原子地"清理+落库"，避免 debounce 在清理与保存之间写脏数据
				_ = sess.CleanAndSave()
				if errors.Is(event.Err, context.DeadlineExceeded) {
					flush(StreamChunk{Type: "error", Content: "AI 处理超时，部分操作可能未完成。你可以在对话框中继续提问，AI 会基于已有的对话历史继续处理。"})
				}
				break
			}
			// Eino v0.9: 区分主动取消（CancelError）与普通业务失败
			var cancelErr *adk.CancelError
			if errors.As(event.Err, &cancelErr) {
				_ = sess.CleanAndSave()
				log.Printf("[Agent] Agent 被主动取消\n")
				flush(StreamChunk{Type: "cancelled", Content: "已停止生成"})
				break
			}
			if errors.Is(event.Err, adk.ErrExceedMaxIterations) || strings.Contains(event.Err.Error(), "exceeds max iterations") {
				_ = sess.CleanAndSave()
				flush(StreamChunk{Type: "error", Content: "AI 处理步骤过多，部分查询尝试未完成。已执行的操作可能已生效。你可以在对话框中继续提问，AI 会基于已有的对话历史继续处理。"})
				break
			}
			if strings.Contains(event.Err.Error(), "stream reader is empty") || strings.Contains(event.Err.Error(), "concat stream reader fail") {
				_ = sess.CleanAndSave()
				flush(StreamChunk{Type: "error", Content: "AI 处理遇到内部错误，前置工具调用可能未成功。你可以重新提问或提供更具体的指令，AI 会重新尝试处理。"})
				break
			}
			errMsg := extractRootErrorMessage(event.Err)
			flush(StreamChunk{Type: "error", Content: "AI 处理出错：" + errMsg})
			break
		}

		// 检查是否被中断
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
			hasDangerConfirm := false
			for _, ictx := range event.Action.Interrupted.InterruptContexts {
				if !ictx.IsRootCause {
					continue
				}
				if sqlInfo, ok := ictx.Info.(*DangerousSQLInfo); ok {
					hasDangerConfirm = true
					log.Printf("[Agent] 危险 SQL 中断 - id=%s, sql=%s\n", ictx.ID, sqlInfo.SQL)
					flush(StreamChunk{
						Type:         "danger_confirm",
						Content:      "检测到危险 SQL，需要用户确认",
						SQL:          sqlInfo.SQL,
						InterruptID:  ictx.ID,
						CheckPointID: checkPointID,
					})
				} else {
					log.Printf("[Agent] 未知类型中断 - id=%s, info=%T\n", ictx.ID, ictx.Info)
				}
			}
			if !hasDangerConfirm {
				// 中断事件中没有 DangerousSQLInfo，属于异常情况
				// 标记为非中断，让调用方发送 done，避免前端永远卡住
				interrupted = false
				log.Printf("[Agent] 中断事件无 DangerousSQLInfo，视为非中断\n")
				flush(StreamChunk{Type: "error", Content: "AI 处理出现异常中断，请重试"})
			}
			if fullResponse.Len() > 0 {
				_ = sess.SaveToDB()
			}
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

		log.Printf("[Agent] 事件输出 [#%d] - role=%s, isStreaming=%v, hasStream=%v, hasMsg=%v, toolCalls=%d, exit=%v, waitTime=%v\n",
			eventIdx, role, mo.IsStreaming, mo.MessageStream != nil, mo.Message != nil, func() int {
				if mo.Message != nil {
					return len(mo.Message.ToolCalls)
				}
				return 0
			}(), hasExit, time.Since(eventStart))

		if mo.IsStreaming && mo.MessageStream != nil {
			var accContent strings.Builder
			var accToolCalls []schema.ToolCall
			chunkIdx := 0
			streamStart := time.Now()
			repeatCount := 0
			lastChunkContent := ""
			const maxRepeats = 10
			for {
				chunk, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					elapsed := time.Since(streamStart)
					log.Printf("[Agent] MessageStream.Recv 结束 - role=%s, accLen=%d, chunks=%d, toolCalls=%d, elapsed=%v, err=%v\n",
						role, accContent.Len(), chunkIdx, len(accToolCalls), elapsed, recvErr)
					if accContent.Len() > 0 && accContent.Len() < 10 {
						log.Printf("[Agent] MessageStream 异常短内容 - accContent=%q\n", accContent.String())
					}
					break
				}
				chunkIdx++
				contentLen := len(chunk.Content)
				reasoningLen := len(chunk.ReasoningContent)
				tcCount := len(chunk.ToolCalls)
				if chunkIdx <= 5 || contentLen > 0 || reasoningLen > 0 || tcCount > 0 || chunkIdx%20 == 0 {
					contentPreview := chunk.Content
					if len(contentPreview) > 80 {
						contentPreview = contentPreview[:80] + "..."
					}
					log.Printf("[Agent] MessageStream chunk[%d] - contentLen=%d, reasoningLen=%d, toolCalls=%d, content=%q\n",
						chunkIdx, contentLen, reasoningLen, tcCount, contentPreview)
				}
				if chunk.ReasoningContent != "" {
					flush(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
				}
				if chunk.Content != "" {
					if chunk.Content == lastChunkContent && len(chunk.Content) > 0 {
						repeatCount++
						if repeatCount >= maxRepeats {
							log.Printf("[Agent] MessageStream 检测到重复输出，中断流 - repeatCount=%d, content=%q, accLen=%d\n",
								repeatCount, chunk.Content, accContent.Len())
							flush(StreamChunk{Type: "error", Content: "模型输出异常（重复内容），已自动中断"})
							break
						}
					} else {
						repeatCount = 0
					}
					lastChunkContent = chunk.Content
					accContent.WriteString(chunk.Content)
					flush(StreamChunk{Type: "content", Content: chunk.Content})
				}
				if len(chunk.ToolCalls) > 0 {
					accToolCalls = append(accToolCalls, chunk.ToolCalls...)
				}
			}
			if accContent.Len() > 0 || len(accToolCalls) > 0 {
				fullResponse.WriteString(accContent.String())
				sm := SessionMessage{Role: string(role), Content: accContent.String()}
				if len(accToolCalls) > 0 {
					sm.ToolCalls = sessionToolCallsFromSchema(mergeToolCalls(accToolCalls))
				}
				sess.AppendMessageNoSave(sm)
			}
		} else if role == schema.Tool {
			msg := mo.Message
			if msg != nil {
				sess.AppendMessageNoSave(SessionMessage{
					Role:       "tool",
					Content:    msg.Content,
					ToolCallID: msg.ToolCallID,
					ToolName:   msg.ToolName,
				})
			}
		} else if role == schema.Assistant && mo.Message != nil {
			msg := mo.Message
			if len(msg.ToolCalls) > 0 {
				sess.AppendMessageNoSave(SessionMessage{
					Role:      "assistant",
					Content:   msg.Content,
					ToolCalls: sessionToolCallsFromSchema(msg.ToolCalls),
				})
			} else if msg.Content != "" {
				fullResponse.WriteString(msg.Content)
				flush(StreamChunk{Type: "content", Content: msg.Content})
				sess.AppendMessageNoSave(SessionMessage{Role: string(role), Content: msg.Content})
			}
		}

		if hasExit {
			break
		}
	}

	return fullResponse, interrupted
}
