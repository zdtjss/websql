// transcript.go — 摘要压缩时保留完整对话 transcript 的路径。
//
// 目的：summarization 压缩后，摘要只保留结论；被压缩掉的细节仍可通过
// read_offload 按行回读。transcript 写入当前用户/会话的 offload 目录，
// 与普通 offload 共用路径 ACL 和清理策略。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

// buildTranscriptFinalize 在摘要正文中追加 transcript 路径，再由默认 finalizer 收尾。
// 不额外插入消息，避免部分模型对 summary 后出现额外 system/user 消息的兼容性问题。
func buildTranscriptFinalize() summarization.FinalizeFunc {
	return func(ctx context.Context, originalMessages []*schema.Message, summary *schema.Message) ([]*schema.Message, error) {
		path, writeErr := writeTranscript(ctx, originalMessages)
		if writeErr != nil {
			log.Printf("[Summarization] 写入 transcript 失败，保留默认摘要 - err=%v\n", writeErr)
		}
		if path != "" && summary != nil {
			summary.Content += fmt.Sprintf(
				"\n\n[完整对话已保存到 %q（JSONL，每行一条消息）。需要核对被摘要压缩的细节时，可调用 read_offload(path=%q, offset=1, limit=按需)，不要凭记忆编造。]",
				path, path,
			)
		}
		return summarization.DefaultFinalize(ctx, originalMessages, summary)
	}
}

func writeTranscript(ctx context.Context, messages []*schema.Message) (string, error) {
	root := offloadRootForContext(ctx)
	if root == "" {
		return "", nil
	}
	// transcript 是落盘副本，必须与 SSE/落库使用同一脱敏策略。
	sec, _ := RunSecurityFrom(ctx)
	dir := filepath.Join(root, "transcript")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	if hasSymlinkComponent(dir, root) {
		return "", fmt.Errorf("transcript 目录包含符号链接，拒绝写入")
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.jsonl", time.Now().UnixNano()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		record := map[string]any{
			"role":    msg.Role,
			"content": maskTextOrRaw(sec, msg.Content),
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]schema.ToolCall, len(msg.ToolCalls))
			copy(toolCalls, msg.ToolCalls)
			for i := range toolCalls {
				toolCalls[i].Function.Arguments = maskTextOrRaw(sec, toolCalls[i].Function.Arguments)
			}
			record["toolCalls"] = toolCalls
		}
		if msg.ToolCallID != "" {
			record["toolCallId"] = msg.ToolCallID
		}
		if msg.ToolName != "" {
			record["toolName"] = msg.ToolName
		}
		if err := enc.Encode(record); err != nil {
			return "", err
		}
	}
	return path, nil
}
