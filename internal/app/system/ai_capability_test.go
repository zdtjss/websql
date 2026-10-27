package system

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ─── 能力推断（启发式） ──────────────────────────────────────

func TestCapabilitiesForModelName(t *testing.T) {
	cases := []struct {
		model string
		want  []string
	}{
		{"gpt-4o", []string{CapVision}},
		{"GPT-4O-mini", []string{CapVision}},
		{"gpt-4.1-turbo", []string{CapVision}},
		{"claude-3-5-sonnet-20241022", []string{CapVision}},
		{"gemini-1.5-pro", []string{CapVision, CapVideo}},
		{"qwen2.5-vl-72b", []string{CapVision, CapVideo}},
		{"qwen-vl-max", []string{CapVision, CapVideo}},
		{"glm-4v", []string{CapVision}},
		{"deepseek-chat", []string{}},
		{"llama-3.1-70b", []string{}},
		{"", []string{}},
		{"  ", []string{}},
	}
	for _, c := range cases {
		got := CapabilitiesForModelName(c.model)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("CapabilitiesForModelName(%q) = %v，期望 %v", c.model, got, c.want)
		}
	}
}

// ─── 能力归一化 ──────────────────────────────────────────────

func TestNormalizeCapabilities(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, []string{}},
		{[]string{}, []string{}},
		{[]string{"unknown"}, []string{}},
		{[]string{"vision"}, []string{CapVision}},
		{[]string{"VISION"}, []string{CapVision}},
		{[]string{" vision "}, []string{CapVision}},
		// 视频能力自动带出视觉能力（避免「能看视频却拒绝图片」）
		{[]string{"video"}, []string{CapVision, CapVideo}},
		{[]string{"vision", "video", "vision"}, []string{CapVision, CapVideo}},
	}
	for _, c := range cases {
		if got := NormalizeCapabilities(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeCapabilities(%v) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

// ─── 生效能力解析（显式配置优先，启发式兜底） ─────────────────

func TestResolveModelCapabilities(t *testing.T) {
	// 1. 显式配置了能力 → 采用配置值
	caps, source := ResolveModelCapabilities(AIModelItem{
		Model:        "gpt-4o",
		Capabilities: []string{CapVision, CapVideo},
	})
	if source != SourceConfigured || !reflect.DeepEqual(caps, []string{CapVision, CapVideo}) {
		t.Errorf("显式配置应优先：caps=%v source=%s", caps, source)
	}

	// 2. 显式声明「无附加能力」（空数组）→ 即使模型名像视觉模型也不放行
	caps, source = ResolveModelCapabilities(AIModelItem{
		Model:        "gpt-4o",
		Capabilities: []string{},
	})
	if source != SourceConfigured || len(caps) != 0 {
		t.Errorf("显式空配置应视为仅文本能力：caps=%v source=%s", caps, source)
	}

	// 3. 未配置（字段缺失，存量模型）→ 启发式推断
	caps, source = ResolveModelCapabilities(AIModelItem{Model: "gemini-2.0-flash"})
	if source != SourceInferred || !reflect.DeepEqual(caps, []string{CapVision, CapVideo}) {
		t.Errorf("未配置时应启发式推断：caps=%v source=%s", caps, source)
	}

	// 4. 未配置且模型名无法识别 → 仅文本能力（fail-closed）
	caps, source = ResolveModelCapabilities(AIModelItem{Model: "my-private-model"})
	if source != SourceInferred || len(caps) != 0 {
		t.Errorf("无法识别的模型应为仅文本能力：caps=%v source=%s", caps, source)
	}

	// 5. 非法能力值被过滤
	caps, _ = ResolveModelCapabilities(AIModelItem{
		Model:        "custom",
		Capabilities: []string{"vision", "audio", ""},
	})
	if !reflect.DeepEqual(caps, []string{CapVision}) {
		t.Errorf("未知能力应被过滤：caps=%v", caps)
	}
}

// ─── JSON 兼容性 ─────────────────────────────────────────────
//
// 存量模型配置（无 capabilities 字段）反序列化后必须为 nil，
// 才能与「显式空数组」区分开，从而保留启发式兜底。

func TestAIModelItemCapabilitiesJSONCompat(t *testing.T) {
	var legacy AIModelItem
	if err := json.Unmarshal([]byte(`{"id":"m1","model":"gpt-4o"}`), &legacy); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if legacy.Capabilities != nil {
		t.Errorf("缺少 capabilities 字段时应为 nil（保留启发式兜底），实际 %#v", legacy.Capabilities)
	}

	var explicit AIModelItem
	if err := json.Unmarshal([]byte(`{"id":"m1","model":"gpt-4o","capabilities":[]}`), &explicit); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if explicit.Capabilities == nil || len(explicit.Capabilities) != 0 {
		t.Errorf("显式空数组应解析为非 nil 空切片，实际 %#v", explicit.Capabilities)
	}

	// 两条路径的解析结果必须能区分：一个走推断、一个为仅文本
	capsLegacy, srcLegacy := ResolveModelCapabilities(legacy)
	capsExplicit, srcExplicit := ResolveModelCapabilities(explicit)
	if srcLegacy != SourceInferred || len(capsLegacy) == 0 {
		t.Errorf("存量配置应走启发式：caps=%v source=%s", capsLegacy, srcLegacy)
	}
	if srcExplicit != SourceConfigured || len(capsExplicit) != 0 {
		t.Errorf("显式空配置应为仅文本：caps=%v source=%s", capsExplicit, srcExplicit)
	}
}
