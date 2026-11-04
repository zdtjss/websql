package agent

import "testing"

// 数据库管理工具需要数据原貌：生产链路使用的禁用态脱敏器不得修改任何值。
func TestDisabledMaskerKeepsRawData(t *testing.T) {
	m := NewDisabledMasker()

	if m.HasRules() {
		t.Fatal("禁用态不应报告存在脱敏规则")
	}
	if m.Version() != "" {
		t.Fatalf("禁用态不应返回规则版本，实际 %q", m.Version())
	}
	if got := m.MaskText("13800000001"); got != "13800000001" {
		t.Fatalf("文本被意外脱敏：%q", got)
	}

	out := &QueryOutput{
		Columns: []string{"phone"},
		Data:    []map[string]any{{"phone": "13800000001"}},
		Count:   1,
	}
	m.MaskQueryOutput(out, []string{"employees"}, nil, nil)
	if got := out.Data[0]["phone"]; got != "13800000001" {
		t.Fatalf("查询结果被意外脱敏：%v", got)
	}

	rows := [][]string{{"13800000001"}}
	if got := m.MaskTable([]string{"phone"}, rows, nil, []string{"employees"}); got[0][0] != "13800000001" {
		t.Fatalf("导出结果被意外脱敏：%v", got)
	}
}
