package export

import (
	"context"
	"errors"
	"strings"
	"testing"

	"websql/internal/ai/agent/export/report"
)

func fence(lang, body string) string {
	return "```" + lang + "\n" + body + "\n```\n"
}

func TestExtractDataRefs(t *testing.T) {
	md := "## 摘要\n\n" +
		fence("chart", `{"type":"line","title":"趋势","sql":"SELECT a, b FROM t","x":"a","y":["b"]}`) +
		"\n" + fence("table", `{"sql":"SELECT 1 AS x","title":"明细","limit":20}`) +
		"\n" + fence("chart", `{"type":"bar","categories":["a"],"series":[{"name":"s","data":[1]}]}`)

	refs := ExtractDataRefs(md)
	if len(refs) != 2 {
		t.Fatalf("应提取 2 条内嵌 SQL，实际 %d", len(refs))
	}
	if !strings.Contains(refs[0].SQL, "FROM t") || refs[0].Limit != 0 {
		t.Errorf("图表 ref 解析异常：%+v", refs[0])
	}
	if refs[1].Limit != 20 {
		t.Errorf("表格 ref limit 应为 20，实际 %d", refs[1].Limit)
	}

	if refs := ExtractDataRefs("## 摘要\n\n纯文字报告，无 SQL。"); len(refs) != 0 {
		t.Errorf("无内嵌 SQL 时不应有 ref，实际 %d", len(refs))
	}
}

func TestBindDocument_FillsChartAndTable(t *testing.T) {
	doc := report.BuildDocument("销售报告", "", fence("chart",
		`{"type":"bar","title":"月度","sql":"SELECT 月份, 金额 FROM 销售","x":"月份","y":["金额"]}`)+
		"\n"+fence("table", `{"sql":"SELECT 1","title":"明细"}`))

	var seenSQL []string
	ds := func(_ context.Context, ref *report.DataRef) (*report.Table, error) {
		seenSQL = append(seenSQL, ref.SQL)
		return &report.Table{
			Headers:   []string{"月份", "金额"},
			Rows:      [][]string{{"1月", "120"}, {"2月", "300"}},
			TotalRows: 2,
		}, nil
	}
	BindDocument(context.Background(), doc, ds)

	if len(seenSQL) != 2 {
		t.Fatalf("DataSource 应被调用 2 次，实际 %d", len(seenSQL))
	}
	var chart *report.ChartSpec
	var table *report.Table
	for i := range doc.Blocks {
		switch {
		case doc.Blocks[i].Kind == report.KindChart:
			chart = doc.Blocks[i].Chart
		case doc.Blocks[i].Kind == report.KindTable:
			table = doc.Blocks[i].Table
		}
	}
	if chart == nil || !chart.HasData() {
		t.Fatalf("图表未回填数据：%+v", chart)
	}
	if chart.SQL != "" {
		t.Error("图表 SQL 回填后应清空")
	}
	if len(chart.Categories) != 2 || chart.Categories[0] != "1月" {
		t.Errorf("图表分类异常：%v", chart.Categories)
	}
	if len(chart.Series) != 1 || chart.Series[0].Name != "金额" || chart.Series[0].Data[1] != 300 {
		t.Errorf("图表系列异常：%+v", chart.Series)
	}
	if table == nil || len(table.Rows) != 2 || table.TotalRows != 2 {
		t.Errorf("表格未回填：%+v", table)
	}
}

func TestBindDocument_ErrorRendersAsBlockError(t *testing.T) {
	doc := report.BuildDocument("报告", "", fence("chart",
		`{"type":"line","sql":"SELECT amount FROM secret","x":"a","y":["amount"]}`))

	BindDocument(context.Background(), doc, func(context.Context, *report.DataRef) (*report.Table, error) {
		return nil, errors.New("无权访问表 [secret]")
	})

	if len(doc.Blocks) == 0 || doc.Blocks[0].Error == "" {
		t.Fatalf("绑定失败应写入 Block.Error，实际 %+v", doc.Blocks)
	}
	if !strings.Contains(doc.Blocks[0].Error, "无权访问表") {
		t.Errorf("Block.Error 应保留拒绝原因，实际 %q", doc.Blocks[0].Error)
	}
	if doc.Blocks[0].Chart.SQL != "" {
		t.Error("绑定失败后应清空图表 SQL，避免渲染器误解为未绑定")
	}
}

func TestBindDocument_NilDataSource(t *testing.T) {
	doc := report.BuildDocument("报告", "", fence("table", `{"sql":"SELECT 1","title":"明细"}`))
	BindDocument(context.Background(), doc, nil)
	if len(doc.Blocks) == 0 || !strings.Contains(doc.Blocks[0].Error, "未注入数据源") {
		t.Errorf("未注入数据源时应写 Block.Error，实际 %+v", doc.Blocks)
	}
}

func TestBindChartTable_AutoInferColumns(t *testing.T) {
	c := &report.ChartSpec{Type: "bar"}
	tbl := &report.Table{
		Headers: []string{"区域", "销售", "利润", "备注"},
		Rows: [][]string{
			{"华东", "4200", "800", "a"},
			{"华北", "3100", "600", "b"},
		},
	}
	if err := BindChartTable(c, tbl); err != nil {
		t.Fatalf("自动推断绑定失败：%v", err)
	}
	if len(c.Categories) != 2 || c.Categories[1] != "华北" {
		t.Errorf("X 列推断异常：%v", c.Categories)
	}
	// 数值列：销售、利润（备注非数值列）
	if len(c.Series) != 2 || c.Series[0].Name != "销售" || c.Series[1].Name != "利润" {
		t.Errorf("Y 列推断异常：%+v", c.Series)
	}
}

func TestBindChartTable_NoNumericColumn(t *testing.T) {
	c := &report.ChartSpec{Type: "bar"}
	err := BindChartTable(c, &report.Table{
		Headers: []string{"a", "b"},
		Rows:    [][]string{{"x", "y"}},
	})
	if err == nil {
		t.Error("无数值列应返回错误")
	}
}

func TestQueryResultToTable_TruncatesWithTotal(t *testing.T) {
	qr := &QueryResult{
		Columns: []string{"a", "b"},
		Data: []map[string]any{
			{"a": 1, "b": "x"},
			{"a": 2, "b": "y"},
			{"a": 3, "b": "z"},
		},
	}
	tbl := QueryResultToTable(qr, 2)
	if len(tbl.Rows) != 2 || tbl.TotalRows != 3 {
		t.Fatalf("截断异常：rows=%d total=%d", len(tbl.Rows), tbl.TotalRows)
	}
	if tbl.Headers[0] != "a" || tbl.Rows[0][1] != "x" {
		t.Errorf("内容异常：%+v", tbl)
	}
}

func TestBuildDataDocument_AutoReport(t *testing.T) {
	qr := &QueryResult{
		Columns: []string{"月份", "金额"},
		Data: []map[string]any{
			{"月份": "1月", "金额": 120.0},
			{"月份": "2月", "金额": 300.0},
		},
	}
	doc := BuildDataDocument("销售报告", "2026 上半年", qr)
	var hasKPI, hasChart, hasTable bool
	for _, b := range doc.Blocks {
		switch b.Kind {
		case report.KindKPI:
			hasKPI = len(b.KPIs) >= 2
		case report.KindChart:
			hasChart = b.Chart != nil && b.Chart.HasData()
		case report.KindTable:
			hasTable = b.Table != nil && len(b.Table.Rows) == 2
		}
	}
	if !hasKPI || !hasChart || !hasTable {
		t.Fatalf("自动报告应含指标卡/图表/明细表：kpi=%v chart=%v table=%v", hasKPI, hasChart, hasTable)
	}
	if doc.Title != "销售报告" || doc.Subtitle != "2026 上半年" {
		t.Errorf("标题/副标题异常：%q / %q", doc.Title, doc.Subtitle)
	}

	// 端到端：自动报告可被 Word/PPT/HTML 渲染
	if data, err := report.RenderDOCX(doc, report.ResolveTheme("", "business")); err != nil || len(data) == 0 {
		t.Errorf("Word 渲染失败：%v", err)
	}
	if data, err := report.RenderPPTX(doc, report.ResolveTheme("", "business")); err != nil || len(data) == 0 {
		t.Errorf("PPT 渲染失败：%v", err)
	}
	if data := report.RenderHTML(doc, report.ResolveTheme("", "business")); len(data) == 0 {
		t.Error("HTML 渲染为空")
	}
}

func TestDataChartsFromExcelCharts(t *testing.T) {
	qr := &QueryResult{
		Columns: []string{"月份", "金额", "成本"},
		Data:    []map[string]any{{"月份": "1月", "金额": 120.0, "成本": 80.0}},
	}
	charts := []ExcelChart{{
		ChartType:  "line",
		XAxisField: "月份",
		Series: []ExcelChartSeriesDef{
			{YAxisField: "金额"},
			{YAxisField: "成本", Name: "成本(元)"},
		},
	}}
	out := DataChartsFromExcelCharts(qr, charts)
	if len(out) != 1 {
		t.Fatalf("应生成 1 个原生图表，实际 %d", len(out))
	}
	if out[0].XCol != 0 || len(out[0].YCols) != 2 || out[0].YCols[1] != 2 {
		t.Errorf("列索引定位异常：%+v", out[0])
	}
	if out[0].SeriesNames[0] != "金额" || out[0].SeriesNames[1] != "成本(元)" {
		t.Errorf("系列名异常：%v", out[0].SeriesNames)
	}

	// 字段名不匹配 → 不生成图表（不报错）
	bad := DataChartsFromExcelCharts(qr, []ExcelChart{{
		ChartType: "bar", XAxisField: "不存在", Series: []ExcelChartSeriesDef{{YAxisField: "金额"}},
	}})
	if len(bad) != 0 {
		t.Errorf("字段不匹配不应生成图表，实际 %+v", bad)
	}
}
