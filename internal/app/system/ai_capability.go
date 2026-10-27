package system

import "strings"

// AI 模型能力（capability）体系。
//
// 用途：AI 对话中的文件上传需要「前置能力校验」——图片必须由支持视觉输入的模型
// 处理，视频仅在模型声明支持视频输入时才允许进入后续流程。
//
// 解析顺序（显式配置优先，启发式兜底）：
//  1. 模型配置里已填写 capabilities（含空数组=明确声明无附加能力）→ 直接采用；
//  2. 未配置（字段缺失，典型于本次升级前的存量模型）→ 按模型名启发式推断；
//  3. 两者都无法确定 → 视为仅文本能力（fail-closed：不确认支持就不放行图片/视频）。
//
// 能力标识与前端 /system/config/ai/models 返回的字符串保持一致。

// 能力标识
const (
	// CapVision 支持图片输入（多模态视觉）
	CapVision = "vision"
	// CapVideo 支持视频输入
	CapVideo = "video"
)

// 能力来源（供前端提示「该能力来自配置还是推断」）
const (
	// SourceConfigured 能力来自管理端显式配置
	SourceConfigured = "configured"
	// SourceInferred 能力由模型名启发式推断
	SourceInferred = "inferred"
)

// visionModelKeywords 视觉模型名关键词（小写子串匹配）。
//
// 维护约定：只登记「确认支持图片输入」的模型家族，宁缺毋滥；
// 未覆盖的模型请在管理端显式配置 capabilities。
var visionModelKeywords = []string{
	"gpt-4o", "gpt-4.1", "gpt-5", "gpt-4-turbo", "gpt-4-vision",
	"claude-", // claude-3 起全系支持图片
	"gemini",  // Gemini 全系多模态
	"qwen-vl", "qwen2.5-vl", "qwen3-vl", "qwen-omni",
	"glm-4v", "glm-4.1v", "glm-4.5v",
	"internvl", "llava", "minicpm-v", "pixtral",
	"doubao-vision", "step-1v", "yi-vision",
	"ernie-4.5-vl", "moonshot-v1-vision",
}

// videoModelKeywords 视频输入模型名关键词（比视觉更保守：仅少数家族支持 video_url）
var videoModelKeywords = []string{
	"gemini",
	"qwen-vl-max", "qwen2.5-vl", "qwen3-vl",
	"glm-4v-plus",
}

// CapabilitiesForModelName 按模型名推断能力集合（纯函数，便于单测）。
// 返回值已归一化：去重、仅保留已知能力、支持视频时必然包含视觉。
func CapabilitiesForModelName(modelName string) []string {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return []string{}
	}

	caps := make([]string, 0, 2)
	for _, kw := range visionModelKeywords {
		if strings.Contains(name, kw) {
			caps = append(caps, CapVision)
			break
		}
	}
	for _, kw := range videoModelKeywords {
		if strings.Contains(name, kw) {
			caps = append(caps, CapVideo)
			break
		}
	}
	return NormalizeCapabilities(caps)
}

// ResolveModelCapabilities 解析模型配置的生效能力集合。
//
// 返回的第二个值为能力来源（configured / inferred），供接口层向前端说明。
func ResolveModelCapabilities(m AIModelItem) ([]string, string) {
	// 1. 显式配置：非 nil（含空数组）即视为管理端已明确声明
	if m.Capabilities != nil {
		return NormalizeCapabilities(m.Capabilities), SourceConfigured
	}
	// 2. 启发式推断
	return CapabilitiesForModelName(m.Model), SourceInferred
}

// NormalizeCapabilities 归一化能力集合：过滤未知能力、去重、补齐依赖关系。
//
// 依赖关系：支持视频输入的模型必然支持单帧图片输入，因此 video 会自动带出 vision，
// 避免管理端漏勾导致「能看视频却拒绝图片」的自相矛盾状态。
func NormalizeCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return []string{}
	}

	seen := make(map[string]bool, len(caps)+1)
	for _, c := range caps {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case CapVision:
			seen[CapVision] = true
		case CapVideo:
			seen[CapVideo] = true
			seen[CapVision] = true
		}
	}

	out := make([]string, 0, len(seen))
	if seen[CapVision] {
		out = append(out, CapVision)
	}
	if seen[CapVideo] {
		out = append(out, CapVideo)
	}
	return out
}

// HasCapability 判断能力集合中是否包含指定能力。
func HasCapability(caps []string, capability string) bool {
	for _, c := range caps {
		if c == capability {
			return true
		}
	}
	return false
}
