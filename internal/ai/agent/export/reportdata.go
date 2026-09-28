package export

// 报告数据绑定：把扩展 Markdown 中的 SQL 绑定块（```table sql / ```chart sql）
// 解析为实际数据回填到 Report IR；以及「仅给 SQL」时自动构建图文报告文档。
//
// report 包不访问数据库、不做权限判断；本文件通过注入的 DataSource 完成取数，
// DataSource 由 agent 包提供（内部完成只读校验 + 表/列权限校验 + 连接范围校验），
// 未经授权或校验失败的数据不会进入文档。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"websql/internal/ai/agent/export/report"
)

// DataSource 数据源回调：按引用取数并转换为表格。
// ref.Limit 已由调用方归一（>0）；权限/只读校验由实现方负责，失败返回 error。
type DataSource func(ctx context.Context, ref *report.DataRef) (*report.Table, error)

// 文档内嵌数据默认行数：明细表 200 行、图表 100 个数据点（超出部分截断展示，
// 完整数据仍可通过 export_excel 导出）。
const (
	defaultTableRows = 200
	defaultChartRows = 100
)

// BindDocument 遍历文档中的 SQL 绑定块并回填数据。
// 单块失败只写入 Block.Error（渲染为警告提示框），不中断整篇导出。
func BindDocument(ctx context.Context, doc *report.Document, ds DataSource) {
	if doc == nil {
		return
	}
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		switch {
		case b.Kind == report.KindTable && b.DataRef != nil && strings.TrimSpace(b.DataRef.SQL) != "":
			ref := *b.DataRef
			tbl, err := fetchTable(ctx, ds, &ref, defaultTableRows)
			b.DataRef = nil
			if err != nil {
				b.Error = err.Error()
				continue
			}
			b.Table = tbl
		case b.Kind == report.KindChart && b.Chart != nil && strings.TrimSpace(b.Chart.SQL) != "":
			c := b.Chart
			ref := &report.DataRef{SQL: c.SQL, ConnID: c.ConnID, Limit: c.Limit}
			c.SQL = ""
			tbl, err := fetchTable(ctx, ds, ref, defaultChartRows)
			if err != nil {
				b.Error = err.Error()
				continue
			}
			if err := BindChartTable(c, tbl); err != nil {
				b.Error = err.Error()
			}
		}
	}
}

func fetchTable(ctx context.Context, ds DataSource, ref *report.DataRef, defLimit int) (*report.Table, error) {
	if ds == nil {
		return nil, fmt.Errorf("数据绑定不可用：未注入数据源")
	}
	if ref.Limit <= 0 {
		ref.Limit = defLimit
	}
	return ds(ctx, ref)
}

// ExtractDataRefs 提取扩展 Markdown 中所有 SQL 数据绑定（```table sql / ```chart sql）。
// 供权限中间件在导出执行前统一校验，避免 content 模式成为免检取数通道。
func ExtractDataRefs(markdown string) []*report.DataRef {
	var refs []*report.DataRef
	for _, b := range report.ParseMarkdown(markdown) {
		switch {
		case b.Kind == report.KindTable && b.DataRef != nil && strings.TrimSpace(b.DataRef.SQL) != "":
			refs = append(refs, b.DataRef)
		case b.Kind == report.KindChart && b.Chart != nil && strings.TrimSpace(b.Chart.SQL) != "":
			refs = append(refs, &report.DataRef{SQL: b.Chart.SQL, ConnID: b.Chart.ConnID, Limit: b.Chart.Limit})
		}
	}
	return refs
}

// BindChartTable 用查询结果填充图表规格：X 列为分类，Y 列（或全部数值列）为系列。
// 图表未显式指定 X/Y 时自动推断（X=首个非数值列，Y=至多 4 个数值列）。
func BindChartTable(c *report.ChartSpec, t *report.Table) error {
	if c == nil || t == nil || len(t.Headers) == 0 {
		return fmt.Errorf("图表数据为空")
	}
	if len(t.Rows) == 0 {
		return fmt.Errorf("图表查询无数据")
	}

	xIdx := findColumn(t.Headers, c.X)
	if xIdx < 0 {
		xIdx = firstTextColumnIdx(t)
	}
	if xIdx >= 0 {
		cats := make([]string, 0, len(t.Rows))
		for _, row := range t.Rows {
			cats = append(cats, cellAt(row, xIdx))
		}
		c.Categories = cats
	} else {
		cats := make([]string, 0, len(t.Rows))
		for i := range t.Rows {
			cats = append(cats, strconv.Itoa(i+1))
		}
		c.Categories = cats
	}

	yIdxs := resolveYColumns(t, c.Y, xIdx)
	if len(yIdxs) == 0 {
		return fmt.Errorf("图表查询结果中没有可用于 Y 轴的数值列")
	}
	c.Series = c.Series[:0]
	for _, yi := range yIdxs {
		vals := make([]float64, 0, len(t.Rows))
		for _, row := range t.Rows {
			v, _ := report.ParseNumber(cellAt(row, yi))
			vals = append(vals, v)
		}
		c.Series = append(c.Series, report.Series{Name: t.Headers[yi], Data: vals})
	}
	c.Normalize()
	if !c.HasData() {
		return fmt.Errorf("图表数据为空")
	}
	return nil
}

// resolveYColumns 解析 Y 列索引：优先显式列名，其次全部数值列
func resolveYColumns(t *report.Table, names []string, xIdx int) []int {
	var idxs []int
	seen := map[int]bool{}
	for _, name := range names {
		if i := findColumn(t.Headers, name); i >= 0 && !seen[i] {
			idxs = append(idxs, i)
			seen[i] = true
		}
	}
	if len(idxs) > 0 {
		return idxs
	}
	const maxSeries = 4
	for i := range t.Headers {
		if i == xIdx || seen[i] {
			continue
		}
		if report.IsNumericColumn(columnValues(t, i)) {
			idxs = append(idxs, i)
			seen[i] = true
			if len(idxs) >= maxSeries {
				break
			}
		}
	}
	return idxs
}

func findColumn(headers []string, name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	// 二次匹配：模型可能给出不带表别名的列名（如 "t.月份"）
	short := name[strings.LastIndex(name, ".")+1:]
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), short) {
			return i
		}
	}
	return -1
}

func firstTextColumnIdx(t *report.Table) int {
	for i := range t.Headers {
		if !report.IsNumericColumn(columnValues(t, i)) {
			return i
		}
	}
	return -1
}

func columnValues(t *report.Table, idx int) []string {
	vals := make([]string, 0, len(t.Rows))
	for _, row := range t.Rows {
		vals = append(vals, cellAt(row, idx))
	}
	return vals
}

func cellAt(row []string, idx int) string {
	if idx >= 0 && idx < len(row) {
		return row[idx]
	}
	return ""
}

// QueryResultToTable 把导出查询结果转换为表格（最多 limit 行，TotalRows 记录原始行数）
func QueryResultToTable(qr *QueryResult, limit int) *report.Table {
	if qr == nil {
		return nil
	}
	total := len(qr.Data)
	n := total
	if limit > 0 && n > limit {
		n = limit
	}
	rows := make([][]string, 0, n)
	for _, row := range qr.Data[:n] {
		cells := make([]string, len(qr.Columns))
		for i, col := range qr.Columns {
			cells[i] = report.CellString(row[col])
		}
		rows = append(rows, cells)
	}
	return &report.Table{
		Headers:   append([]string(nil), qr.Columns...),
		Rows:      rows,
		TotalRows: total,
	}
}

// BuildDataDocument 由查询结果自动构建图文报告文档（标题 + 指标卡 + 图表 + 明细表）。
// 用于「仅给 SQL」的导出：模型不必手工搬运数据，也能产出完整报告。
func BuildDataDocument(title, subtitle string, qr *QueryResult) *report.Document {
	if strings.TrimSpace(title) == "" {
		title = "数据分析报告"
	}
	doc := &report.Document{Title: title, Subtitle: strings.TrimSpace(subtitle)}
	if qr == nil {
		return doc
	}

	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindHeading, Level: 1, Text: "数据概览"})

	kpis := []report.KPI{
		{Label: "数据行数", Value: report.FormatThousands(float64(len(qr.Data)), 0)},
		{Label: "字段数", Value: strconv.Itoa(len(qr.Columns))},
	}
	numericCols := DetectNumericCols(qr)
	if len(numericCols) > 0 {
		col := numericCols[0]
		_, max, avg, _ := CalcNumericStats(qr, col)
		kpis = append(kpis,
			report.KPI{Label: col + " 平均值", Value: report.FormatCompact(avg)},
			report.KPI{Label: col + " 最大值", Value: report.FormatCompact(max)},
		)
	}
	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindKPI, KPIs: kpis})

	if chart := AutoChart(qr); chart != nil {
		doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindChart, Chart: chart})
	}

	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindHeading, Level: 1, Text: "数据明细"})
	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindTable, Table: QueryResultToTable(qr, defaultTableRows)})
	if len(qr.Data) > defaultTableRows {
		doc.Blocks = append(doc.Blocks, report.Block{
			Kind: report.KindCallout, Style: "note", Title: "数据已截断",
			Text: fmt.Sprintf("明细仅展示前 %d 行（共 %d 行）；完整数据请用 export_excel 导出。", defaultTableRows, len(qr.Data)),
		})
	}
	return doc
}

// AutoChart 由查询结果自动生成图表：X=首个非数值列（或序号），Y=至多 3 个数值列。
// 无数值列时返回 nil。
func AutoChart(qr *QueryResult) *report.ChartSpec {
	if qr == nil || len(qr.Data) == 0 {
		return nil
	}
	numericCols := DetectNumericCols(qr)
	if len(numericCols) == 0 {
		return nil
	}
	numericSet := map[string]bool{}
	for _, c := range numericCols {
		numericSet[c] = true
	}
	xCol := ""
	for _, c := range qr.Columns {
		if !numericSet[c] {
			xCol = c
			break
		}
	}

	spec := &report.ChartSpec{Type: "bar", Title: "数据概览"}
	limit := len(qr.Data)
	if limit > defaultChartRows {
		limit = defaultChartRows
	}
	for _, row := range qr.Data[:limit] {
		if xCol != "" {
			spec.Categories = append(spec.Categories, report.CellString(row[xCol]))
		}
	}
	if len(spec.Categories) == 0 {
		for i := 1; i <= limit; i++ {
			spec.Categories = append(spec.Categories, strconv.Itoa(i))
		}
	}
	const maxSeries = 3
	for i, col := range numericCols {
		if i >= maxSeries {
			break
		}
		vals := make([]float64, 0, limit)
		for _, row := range qr.Data[:limit] {
			v, err := ToFloat64(row[col])
			if err != nil {
				v = 0
			}
			vals = append(vals, v)
		}
		spec.Series = append(spec.Series, report.Series{Name: col, Data: vals})
	}
	spec.Normalize()
	return spec
}

// DataChartsFromExcelCharts 把 Excel 图表定义转换为按列索引定位的原生图表规格。
func DataChartsFromExcelCharts(qr *QueryResult, charts []ExcelChart) []report.DataChart {
	if qr == nil {
		return nil
	}
	var out []report.DataChart
	for _, ch := range charts {
		xIdx := FindFieldIndex(qr.Columns, ch.XAxisField)
		if xIdx < 0 {
			continue
		}
		dc := report.DataChart{Type: ch.ChartType, Title: ch.ChartTitle, XCol: xIdx}
		for _, s := range ch.Series {
			yIdx := FindFieldIndex(qr.Columns, s.YAxisField)
			if yIdx < 0 {
				continue
			}
			dc.YCols = append(dc.YCols, yIdx)
			name := s.Name
			if strings.TrimSpace(name) == "" {
				name = s.YAxisField
			}
			dc.SeriesNames = append(dc.SeriesNames, name)
		}
		if len(dc.YCols) > 0 {
			out = append(out, dc)
		}
	}
	return out
}

// BuildExcelSummary 由查询结果生成 Excel「分析摘要」工作表内容（指标卡 + 数值列统计）。
func BuildExcelSummary(title string, qr *QueryResult) *report.Document {
	if qr == nil {
		return nil
	}
	if strings.TrimSpace(title) == "" {
		title = "数据概览"
	}
	doc := &report.Document{Title: title}
	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindHeading, Level: 1, Text: "数据概览"})
	doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindKPI, KPIs: []report.KPI{
		{Label: "数据行数", Value: report.FormatThousands(float64(len(qr.Data)), 0)},
		{Label: "字段数", Value: strconv.Itoa(len(qr.Columns))},
	}})

	numericCols := DetectNumericCols(qr)
	if len(numericCols) > 0 {
		const maxStats = 8
		tbl := &report.Table{Headers: []string{"数值字段", "最小值", "最大值", "平均值"}}
		for i, col := range numericCols {
			if i >= maxStats {
				break
			}
			min, max, avg, _ := CalcNumericStats(qr, col)
			tbl.Rows = append(tbl.Rows, []string{
				col,
				report.FormatPlainNumber(min),
				report.FormatPlainNumber(max),
				report.FormatPlainNumber(avg),
			})
		}
		doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindHeading, Level: 2, Text: "数值字段统计"})
		doc.Blocks = append(doc.Blocks, report.Block{Kind: report.KindTable, Table: tbl})
	}
	doc.Blocks = append(doc.Blocks, report.Block{
		Kind: report.KindCallout, Style: "note", Title: "说明",
		Text: "数据明细见「数据明细」工作表，取数 SQL 见「说明」工作表。",
	})
	return doc
}
