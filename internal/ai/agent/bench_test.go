package agent

// 安全链路的基准测试：用于校准文档 9.2 的初始性能预算。
//
// 运行：go test ./internal/ai/agent/ -bench 'BenchmarkSecurity' -benchmem

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkSecurityPreprocessQuestion32KiB(b *testing.B) {
	base := "请分析最近 30 天的订单趋势并给出建议。"
	raw := strings.Repeat(base, (32*1024)/len(base))
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PreprocessQuestion(raw, nil); err != nil {
			b.Fatalf("预处理失败：%v", err)
		}
	}
}

func BenchmarkSecurityMaskQueryResult1000x50(b *testing.B) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "t", Column: "col_10", Action: MaskPartial},
		{ConnID: "c1", Schema: "s1", Table: "t", Column: "col_20", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		b.Fatalf("构建脱敏器失败：%v", err)
	}

	columns := make([]string, 50)
	for i := range columns {
		columns[i] = fmt.Sprintf("col_%d", i)
	}
	rows := make([]map[string]any, 1000)
	for r := range rows {
		row := make(map[string]any, len(columns))
		for _, col := range columns {
			row[col] = "value-13800138000"
		}
		rows[r] = row
	}
	payload, _ := json.Marshal(QueryOutput{Columns: columns, Data: rows, Count: len(rows)})
	origins := map[string]ResourceRef{
		"col_10": {ConnID: "c1", Schema: "s1", Table: "t", Column: "col_10"},
		"col_20": {ConnID: "c1", Schema: "s1", Table: "t", Column: "col_20"},
	}
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.MaskQueryResult(string(payload), nil, origins, nil); err != nil {
			b.Fatalf("脱敏失败：%v", err)
		}
	}
}

// BenchmarkSecurityFilterMaskOnly1000x50 度量「解析 + 权限过滤 + 脱敏」（不含序列化）。
//
// 每次迭代都对原始 JSON 重新解析，避免复用已脱敏结果造成低估。
func BenchmarkSecurityFilterMaskOnly1000x50(b *testing.B) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "t", Column: "col_10", Action: MaskPartial},
		{ConnID: "c1", Schema: "s1", Table: "t", Column: "col_20", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		b.Fatalf("构建脱敏器失败：%v", err)
	}
	columns := make([]string, 50)
	for i := range columns {
		columns[i] = fmt.Sprintf("col_%d", i)
	}
	rows := make([]map[string]any, 1000)
	for r := range rows {
		row := make(map[string]any, len(columns))
		for _, col := range columns {
			row[col] = "value-13800138000"
		}
		rows[r] = row
	}
	raw, _ := json.Marshal(QueryOutput{Columns: columns, Data: rows, Count: len(rows)})
	origins := map[string]ResourceRef{
		"col_10": {ConnID: "c1", Schema: "s1", Table: "t", Column: "col_10"},
		"col_20": {ConnID: "c1", Schema: "s1", Table: "t", Column: "col_20"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out QueryOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			b.Fatalf("解析失败：%v", err)
		}
		m.MaskQueryOutput(&out, nil, origins, nil)
	}
}

// BenchmarkSecurityFilterMaskTypical1000x50 典型密度：50 列中 2 列含敏感值、其余为普通名称/金额。
//
// 与全列敏感的压力上界（BenchmarkSecurityFilterMaskOnly1000x50）共同用于预算校准。
func BenchmarkSecurityFilterMaskTypical1000x50(b *testing.B) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "t", Column: "col_0", Action: MaskPartial},
	}, key, "v1")
	if err != nil {
		b.Fatalf("构建脱敏器失败：%v", err)
	}
	columns := make([]string, 50)
	for i := range columns {
		columns[i] = fmt.Sprintf("col_%d", i)
	}
	rows := make([]map[string]any, 1000)
	for r := range rows {
		row := make(map[string]any, len(columns))
		for c, col := range columns {
			switch c {
			case 0:
				row[col] = "13800138000"
			case 1:
				row[col] = "zhangsan@example.com"
			default:
				row[col] = fmt.Sprintf("name-%d-%d", r, c)
			}
		}
		rows[r] = row
	}
	raw, _ := json.Marshal(QueryOutput{Columns: columns, Data: rows, Count: len(rows)})
	origins := map[string]ResourceRef{
		"col_0": {ConnID: "c1", Schema: "s1", Table: "t", Column: "col_0"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out QueryOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			b.Fatalf("解析失败：%v", err)
		}
		m.MaskQueryOutput(&out, nil, origins, nil)
	}
}

func BenchmarkSecurityOutputGuardChunk(b *testing.B) {
	chunk := "这是模型输出的一个普通片段，包含少量中文与英文 SQL 关键字 SELECT。"
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		guard := NewOutputGuard(NewNoopMasker(), 0)
		if _, err := guard.Push(chunk); err != nil {
			b.Fatalf("guard 失败：%v", err)
		}
		if _, err := guard.Close(); err != nil {
			b.Fatalf("guard close 失败：%v", err)
		}
	}
}

func BenchmarkSecurityScopeAuthorize(b *testing.B) {
	scope := newScopeForContract("c1",
		map[string]bool{"s1.full_table": true},
		map[string]map[string]bool{"s1.orders": {"id": true}},
	)
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Scopes:        map[string]*PermissionScope{"c1": scope},
		Masker:        NewNoopMasker(),
	}
	req := AccessRequest{Action: ActionRead, ConnID: "c1", Schema: "s1", Table: "orders", Column: "id"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := sec.Authorize(req); err != nil {
			b.Fatalf("授权失败：%v", err)
		}
	}
}
