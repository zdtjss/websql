package agent

import (
	system "websql/internal/app/system"
)

// 模型能力判定（vision / video）。
//
// 用途：上传阶段做「前置能力校验」——图片必须由支持视觉输入的模型处理，
// 视频仅在模型声明支持视频输入时才允许进入后续流程（fail-closed：
// 不能确认支持就不放行，避免用户上传后才发现模型无法理解内容）。
//
// 能力来源解析（显式配置优先 / 模型名启发式兜底）统一由 system 包负责
// （见 internal/app/system/ai_capability.go），本文件只做判定与文案。

// 能力标识（与 system 包及前端字符串保持一致）
const (
	// CapVision 支持图片输入（多模态视觉）
	CapVision = system.CapVision
	// CapVideo 支持视频输入
	CapVideo = system.CapVideo
)

// resolveModelCapabilities 是上传/发送阶段使用的能力解析入口。
//
// 独立成变量而非直接调用：单元测试与离线校验场景需要在不依赖管理库的前提下
// 注入能力集合（ModelCapabilities 依赖管理库读取模型配置）。
var resolveModelCapabilities = ModelCapabilities

// ModelCapabilities 返回指定模型（按 modelId 解析配置）的能力集合。
// modelId 为空时使用全局选中模型。
func ModelCapabilities(modelID string) map[string]bool {
	cfg := system.GetSelectedModelConfig(modelID)
	if cfg == nil {
		return map[string]bool{}
	}

	// 1. 管理端显式配置优先（空集合表示「明确声明无附加能力」= 仅文本）
	if cfg.CapabilitiesSource == system.SourceConfigured {
		return capabilitySet(cfg.Capabilities)
	}
	// 2. 已解析的推断结果
	if len(cfg.Capabilities) > 0 {
		return capabilitySet(cfg.Capabilities)
	}
	// 3. 兜底：按模型名再推断一次（例如旧扁平键配置路径未填充能力字段时）
	return capabilitySet(system.CapabilitiesForModelName(cfg.Model))
}

// capabilitySet 把能力集合转为查询用 map
func capabilitySet(caps []string) map[string]bool {
	out := make(map[string]bool, len(caps))
	for _, c := range caps {
		out[c] = true
	}
	return out
}

// RequiredCapability 返回该文件类别在上传/发送阶段所需的模型能力；
// 空字符串表示无能力要求（所有文本模型均可处理）。
func RequiredCapability(kind string) string {
	switch kind {
	case "image":
		return CapVision
	case "video":
		return CapVideo
	default:
		return ""
	}
}

// CapabilityRejectText 返回能力不匹配时的用户提示文案。
func CapabilityRejectText(kind string) string {
	switch kind {
	case "image":
		return "当前所选 AI 模型不支持图片输入，请切换到视觉模型（如 gpt-4o / qwen-vl 系列）后再上传，或移除该文件"
	case "video":
		return "当前所选 AI 模型不支持视频输入，请切换到支持视频的模型（如 gemini / qwen-vl 系列），或移除该文件"
	default:
		return "当前所选 AI 模型不支持该文件类型"
	}
}
