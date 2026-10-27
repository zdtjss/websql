package agent

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"

	system "websql/internal/app/system"

	"github.com/cloudwego/eino/schema"
)

// 多模态媒体注入：把用户上传的图片/视频本体以 base64 形式注入模型输入。
//
// 背景：图片/视频类附件无法用文本表达内容（解析只能得到尺寸/时长等元信息），
// 需要经 UserInputMultiContent 注入模型才能真正「看到」。
//
// 约束（详述）：
//   - 能力门禁：仅注入当前模型声明支持的媒体类别（vision → 图片，video → 视频），
//     fail-closed：模型能力不足以理解媒体时不注入，模型侧仍可经 read_file_data
//     拿到文件元信息；
//   - provider 差异：ollama 适配器只接受 text/image 两类 part（video_url 会直接报错），
//     故 ollama 下不注入视频；
//   - 注入窗口：仅注入最近 mediaInjectWindow 条「含可注入媒体的用户消息」，
//     控制请求体大小与 token 消耗（更早的媒体退化为文件名/fileId 文本上下文）；
//   - fileId 去重：同一文件在多轮消息中重复引用时只注入一次，且取最新一次出现；
//   - 体积预算：单次请求注入的媒体原始字节总量上限 maxMediaInjectBytes，
//     超限的媒体跳过并在消息文本中向模型说明；
//   - 消息结构：注入媒体的消息 Content 置空、正文作为首个 text part
//     （OpenAI 兼容 SDK 禁止 Content 与 MultiContent 同时非空）。

const (
	// mediaInjectWindow 注入窗口：最近 N 条含可注入媒体的用户消息
	mediaInjectWindow = 2
	// maxMediaInjectBytes 单次请求注入的媒体原始字节总量上限（base64 后约 4/3 倍）
	maxMediaInjectBytes int64 = 12 << 20
)

// mediaInjector 单次运行的媒体注入器（能力门禁 + fileId 去重 + 体积预算）
type mediaInjector struct {
	userID     string
	allowImage bool
	allowVideo bool
	budget     int64
	injected   map[string]bool
}

// providerSupportsVideoPart 判断该 provider 的适配器是否支持 video_url part。
// ollama 适配器仅接受 text/image 两类 part（其余类型直接返回错误），故不支持视频。
func providerSupportsVideoPart(provider string) bool {
	return strings.ToLower(strings.TrimSpace(provider)) != "ollama"
}

// newMediaInjector 按当前选中模型的能力与 provider 创建注入器。
func newMediaInjector(modelID, userID string) *mediaInjector {
	caps := resolveModelCapabilities(modelID)
	allowVideo := caps[CapVideo]
	if allowVideo {
		if cfg := system.GetSelectedModelConfig(modelID); cfg != nil && !providerSupportsVideoPart(cfg.Provider) {
			log.Printf("[Media] provider=%s 不支持视频 part，本轮不注入视频 - model=%s\n", cfg.Provider, cfg.Model)
			allowVideo = false
		}
	}
	return &mediaInjector{
		userID:     userID,
		allowImage: caps[CapVision],
		allowVideo: allowVideo,
		budget:     maxMediaInjectBytes,
		injected:   make(map[string]bool),
	}
}

// injectable 过滤出「类别可注入」的附件（不含去重与预算判定）
func (mi *mediaInjector) injectable(refs []AttachmentRef) []AttachmentRef {
	out := make([]AttachmentRef, 0, len(refs))
	for _, ref := range refs {
		switch ref.FileKind {
		case "image":
			if !mi.allowImage {
				continue
			}
		case "video":
			if !mi.allowVideo {
				continue
			}
		default:
			continue
		}
		out = append(out, ref)
	}
	return out
}

// planMediaInjection 计算媒体注入计划：map[消息下标]待注入附件。
//
// 扫描方式：从最新消息往前找「含可注入媒体的用户消息」，最多 mediaInjectWindow 条；
// 同一 fileId 取最新一次出现（更早的重复引用不再注入）。
func planMediaInjection(msgs []SessionMessage, mi *mediaInjector) map[int][]AttachmentRef {
	plan := make(map[int][]AttachmentRef)
	if !mi.allowImage && !mi.allowVideo {
		return plan
	}

	seen := make(map[string]bool)
	used := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if used >= mediaInjectWindow {
			break
		}
		msg := msgs[i]
		if msg.Role != "user" || len(msg.Attachments) == 0 {
			continue
		}
		refs := mi.injectable(msg.Attachments)
		if len(refs) == 0 {
			continue
		}
		fresh := make([]AttachmentRef, 0, len(refs))
		for _, ref := range refs {
			if seen[ref.FileID] {
				continue
			}
			seen[ref.FileID] = true
			fresh = append(fresh, ref)
		}
		if len(fresh) == 0 {
			continue
		}
		plan[i] = fresh
		used++
	}
	return plan
}

// buildUserMessage 构建用户消息（含多模态内容）。
//
// 无可成功注入的媒体时返回纯文本消息（保持既有消息结构）。
// 注入失败/超预算的媒体会在正文末尾追加一行系统提示，避免模型误以为能看到内容。
func (mi *mediaInjector) buildUserMessage(content string, refs []AttachmentRef) *schema.Message {
	parts := make([]schema.MessageInputPart, 0, len(refs)+1)
	parts = append(parts, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: content})
	var notes []string

	for _, ref := range refs {
		if mi.injected[ref.FileID] {
			continue
		}
		meta, ok := globalUploadStore.Get(ref.FileID)
		if !ok {
			// 已过期/已删除：降级为文本上下文（消息正文中仍有文件名与 fileId）
			continue
		}
		if meta.UserID != "" && meta.UserID != mi.userID {
			log.Printf("[Media] 拒绝注入非本人上传文件 - user=%s, fileId=%s\n", mi.userID, ref.FileID)
			mi.injected[ref.FileID] = true
			continue
		}
		if meta.Size > 0 && meta.Size > mi.budget {
			notes = append(notes, fmt.Sprintf("%s（超出本轮注入体积上限）", meta.FileName))
			mi.injected[ref.FileID] = true
			continue
		}
		data, err := os.ReadFile(meta.DiskPath)
		if err != nil {
			log.Printf("[Media] 读取待注入文件失败 - id=%s, err=%v\n", ref.FileID, err)
			notes = append(notes, fmt.Sprintf("%s（读取失败）", meta.FileName))
			mi.injected[ref.FileID] = true
			continue
		}

		mimeType := meta.MIME
		if mimeType == "" {
			mimeType = mimeOfExt(meta.Ext)
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		common := schema.MessagePartCommon{Base64Data: &b64, MIMEType: mimeType}
		switch ref.FileKind {
		case "image":
			parts = append(parts, schema.MessageInputPart{
				Type:  schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{MessagePartCommon: common, Detail: schema.ImageURLDetailAuto},
			})
		case "video":
			parts = append(parts, schema.MessageInputPart{
				Type:  schema.ChatMessagePartTypeVideoURL,
				Video: &schema.MessageInputVideo{MessagePartCommon: common},
			})
		default:
			continue
		}
		mi.injected[ref.FileID] = true
		mi.budget -= int64(len(data))
		log.Printf("[Media] 已注入媒体 - id=%s, kind=%s, size=%d, 剩余预算=%d\n",
			ref.FileID, ref.FileKind, len(data), mi.budget)
	}

	noteText := ""
	if len(notes) > 0 {
		noteText = "\n\n[系统提示] 以下文件未随本次消息提供内容：" + strings.Join(notes, "、") +
			"。如用户要求分析这些文件，请说明需重新上传较小的文件。"
	}

	if len(parts) == 1 {
		return &schema.Message{Role: schema.User, Content: content + noteText}
	}
	if noteText != "" {
		parts[0].Text = content + noteText
	}
	// Content 置空：OpenAI 兼容 SDK 禁止 Content 与 MultiContent 同时非空，
	// 正文已作为首个 text part（ollama 适配器同样从 text part 拼接正文）。
	return &schema.Message{Role: schema.User, UserInputMultiContent: parts}
}
