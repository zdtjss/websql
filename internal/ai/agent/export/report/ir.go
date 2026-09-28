// Package report 是导出子系统的统一文档模型（Report IR）与各格式渲染器。
//
// 设计（一次解析，多端渲染）：
//
//	扩展 Markdown ──Parse──▶ Document(IR) ──数据绑定(export 包)──▶ Document
//	                                   ├─▶ RenderHTML  （自包含 HTML，内联 SVG 图表，离线可用）
//	                                   ├─▶ RenderDOCX  （原生 Word：封面/目录/页眉页脚/原生图表）
//	                                   ├─▶ RenderPPTX  （原生 PPT：封面/目录/KPI/原生图表/分页表格）
//	                                   └─▶ RenderXLSX  （摘要 + 数据表 + 原生图表）
//
// 旧实现的问题：Go 与 Python 两套渲染器各自解析 Markdown、能力不一致；导出工具只能在
// 「sql 原始数据」与「content 纯文本」之间二选一，报告里既没有图表也没有指标卡。
// 本包把「文档是什么」与「文档怎么画」分离：所有格式共享同一份 IR 与同一个解析器。
//
// 本包不访问数据库、不做权限判断：SQL 绑定块（```chart {"sql":...}）由 export 包
// 通过注入的 DataSource（带权限校验）解析后回填到 IR。
package report

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// BlockKind 块类型
type BlockKind string

const (
	KindHeading   BlockKind = "heading"
	KindParagraph BlockKind = "paragraph"
	KindList      BlockKind = "list"
	KindTable     BlockKind = "table"
	KindChart     BlockKind = "chart"
	KindKPI       BlockKind = "kpi"
	KindCallout   BlockKind = "callout"
	KindCode      BlockKind = "code"
	KindMermaid   BlockKind = "mermaid"
	KindQuote     BlockKind = "quote"
	KindDivider   BlockKind = "divider"
	KindPageBreak BlockKind = "pagebreak"
)

// Document 报告文档
type Document struct {
	Title        string  `json:"title"`
	Subtitle     string  `json:"subtitle,omitempty"`
	Author       string  `json:"author,omitempty"`
	Organization string  `json:"organization,omitempty"`
	Date         string  `json:"date,omitempty"`
	Theme        string  `json:"theme,omitempty"`
	Blocks       []Block `json:"blocks"`
}

// Block 文档块
type Block struct {
	Kind    BlockKind  `json:"kind"`
	Level   int        `json:"level,omitempty"`   // heading: 1-4
	Text    string     `json:"text,omitempty"`    // heading/paragraph/quote/callout 正文（行内 Markdown）
	Title   string     `json:"title,omitempty"`   // callout/table 标题
	Style   string     `json:"style,omitempty"`   // callout: note|tip|important|warning|caution
	Ordered bool       `json:"ordered,omitempty"` // list
	Items   []ListItem `json:"items,omitempty"`   // list
	Table   *Table     `json:"table,omitempty"`
	Chart   *ChartSpec `json:"chart,omitempty"`
	KPIs    []KPI      `json:"kpis,omitempty"`
	Code    string     `json:"code,omitempty"` // code/mermaid 源码
	Lang    string     `json:"lang,omitempty"`
	DataRef *DataRef   `json:"dataRef,omitempty"` // 表格 SQL 绑定（待解析）
	Error   string     `json:"error,omitempty"`   // 数据绑定失败说明（渲染为提示框）
}

// ListItem 列表项（支持一层嵌套）
type ListItem struct {
	Text     string     `json:"text"`
	Children []ListItem `json:"children,omitempty"`
}

// Table 表格
type Table struct {
	Headers   []string   `json:"headers"`
	Rows      [][]string `json:"rows"`
	Align     []string   `json:"align,omitempty"`     // left|center|right
	TotalRows int        `json:"totalRows,omitempty"` // 原始总行数（大于 len(Rows) 表示截断展示）
}

// DataRef 表格数据绑定
type DataRef struct {
	SQL    string `json:"sql"`
	ConnID string `json:"connId,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Series 图表系列
type Series struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

// ChartSpec 图表规格
type ChartSpec struct {
	Type       string   `json:"type"` // bar|hbar|stacked|line|area|pie|donut|scatter|radar
	Title      string   `json:"title,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Series     []Series `json:"series,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	XLabel     string   `json:"xLabel,omitempty"`
	YLabel     string   `json:"yLabel,omitempty"`
	// SQL 数据绑定（解析后清空 SQL，数据写入 Categories/Series）
	SQL    string   `json:"sql,omitempty"`
	ConnID string   `json:"connId,omitempty"`
	X      string   `json:"x,omitempty"`
	Y      []string `json:"y,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

// KPI 指标卡
type KPI struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Change string `json:"change,omitempty"`
	Trend  string `json:"trend,omitempty"` // up|down|flat
	Note   string `json:"note,omitempty"`
}

// HasData 图表是否已有可渲染数据
func (c *ChartSpec) HasData() bool {
	if c == nil || len(c.Series) == 0 {
		return false
	}
	for _, s := range c.Series {
		if len(s.Data) > 0 {
			return true
		}
	}
	return false
}

// IsPieLike 饼图/环形图
func (c *ChartSpec) IsPieLike() bool {
	return c.Type == "pie" || c.Type == "donut"
}

// Normalize 规范化图表：类型别名、系列长度对齐、分类补齐、数量上限
func (c *ChartSpec) Normalize() {
	c.Type = NormalizeChartType(c.Type)
	maxLen := len(c.Categories)
	for _, s := range c.Series {
		if len(s.Data) > maxLen {
			maxLen = len(s.Data)
		}
	}
	const maxPoints = 500
	if maxLen > maxPoints {
		maxLen = maxPoints
	}
	for len(c.Categories) < maxLen {
		c.Categories = append(c.Categories, strconv.Itoa(len(c.Categories)+1))
	}
	c.Categories = c.Categories[:maxLen]
	const maxSeries = 12
	if len(c.Series) > maxSeries {
		c.Series = c.Series[:maxSeries]
	}
	for i := range c.Series {
		d := c.Series[i].Data
		for len(d) < maxLen {
			d = append(d, 0)
		}
		c.Series[i].Data = d[:maxLen]
		for j, v := range c.Series[i].Data {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				c.Series[i].Data[j] = 0
			}
		}
		if strings.TrimSpace(c.Series[i].Name) == "" {
			c.Series[i].Name = fmt.Sprintf("系列%d", i+1)
		}
	}
	if c.IsPieLike() && len(c.Series) > 1 {
		c.Series = c.Series[:1]
	}
}

// NormalizeChartType 图表类型别名归一
func NormalizeChartType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "bar", "column", "col", "柱状图", "histogram", "heatmap":
		return "bar"
	case "hbar", "horizontal_bar", "horizontalbar", "barh", "horizontal", "条形图":
		return "hbar"
	case "stacked", "stacked_bar", "stackedbar", "stack", "stacked_column", "堆叠柱状图":
		return "stacked"
	case "line", "spline", "折线图", "trend":
		return "line"
	case "area", "面积图":
		return "area"
	case "pie", "饼图":
		return "pie"
	case "donut", "doughnut", "ring", "环形图":
		return "donut"
	case "scatter", "散点图":
		return "scatter"
	case "radar", "雷达图":
		return "radar"
	}
	return "bar"
}

// ─── 宽松 JSON 解码（LLM 输出格式存在差异，统一在此兼容） ─────────────

// ParseChartJSON 解析 ```chart 代码块内容
func ParseChartJSON(src string) (*ChartSpec, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(src)), &raw); err != nil {
		return nil, fmt.Errorf("图表 JSON 解析失败：%v", err)
	}
	c := &ChartSpec{
		Type:   firstString(raw, "type", "chartType", "kind"),
		Title:  firstString(raw, "title", "name", "chartTitle"),
		Unit:   firstString(raw, "unit"),
		XLabel: firstString(raw, "xLabel", "xAxisName", "x_label"),
		YLabel: firstString(raw, "yLabel", "yAxisName", "y_label"),
		SQL:    firstString(raw, "sql", "query"),
		ConnID: firstString(raw, "connId", "connID", "conn_id"),
		X:      firstString(raw, "x", "xField", "xAxisField", "category"),
		Limit:  int(toFloat(raw["limit"])),
	}
	c.Y = toStringSlice(firstValue(raw, "y", "yField", "yFields", "yAxisField", "value", "values_field"))
	c.Categories = toStringSlice(firstValue(raw, "categories", "labels", "xAxis", "x_data", "names"))

	switch s := firstValue(raw, "series", "datasets").(type) {
	case []any:
		for i, item := range s {
			switch v := item.(type) {
			case map[string]any:
				name := firstString(v, "name", "label", "title")
				data := toFloatSlice(firstValue(v, "data", "values", "value", "y"))
				c.Series = append(c.Series, Series{Name: name, Data: data})
			case []any:
				c.Series = append(c.Series, Series{Name: fmt.Sprintf("系列%d", i+1), Data: toFloatSlice(v)})
			}
		}
	case map[string]any:
		// {"series": {"销售额": [1,2,3], "成本": [..]}}
		for name, v := range s {
			c.Series = append(c.Series, Series{Name: name, Data: toFloatSlice(v)})
		}
	}
	// 饼图简写：{"labels": [...], "values": [...]} 或 {"data": [...]}
	if len(c.Series) == 0 {
		if vals := toFloatSlice(firstValue(raw, "values", "data")); len(vals) > 0 {
			c.Series = []Series{{Name: firstNonEmpty(c.Title, "数值"), Data: vals}}
		}
	}
	// {"data": [{"name": "A", "value": 1}, ...]}（ECharts 饼图风格）
	if len(c.Series) == 0 || (len(c.Categories) == 0 && len(c.Series) == 1 && len(c.Series[0].Data) == 0) {
		if arr, ok := firstValue(raw, "data", "items").([]any); ok {
			var cats []string
			var vals []float64
			for _, it := range arr {
				if m, ok := it.(map[string]any); ok {
					cats = append(cats, firstString(m, "name", "label", "category"))
					vals = append(vals, toFloat(firstValue(m, "value", "y", "count")))
				}
			}
			if len(vals) > 0 {
				c.Categories = cats
				c.Series = []Series{{Name: firstNonEmpty(c.Title, "数值"), Data: vals}}
			}
		}
	}
	if c.SQL == "" && !c.HasData() {
		return nil, fmt.Errorf("图表缺少数据：请提供 categories + series，或提供 sql 由服务端取数")
	}
	c.Normalize()
	return c, nil
}

// ParseKPIs 解析 ```kpi 代码块：JSON 数组/对象，或每行 "标签 | 数值 | 变化 | 趋势 | 备注"
func ParseKPIs(src string) ([]KPI, error) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "[") || strings.HasPrefix(src, "{") {
		var raw any
		if err := json.Unmarshal([]byte(src), &raw); err != nil {
			return nil, fmt.Errorf("指标卡 JSON 解析失败：%v", err)
		}
		var arr []any
		switch v := raw.(type) {
		case []any:
			arr = v
		case map[string]any:
			if items, ok := firstValue(v, "items", "kpis", "data").([]any); ok {
				arr = items
			} else {
				arr = []any{v}
			}
		}
		var out []KPI
		for _, it := range arr {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			k := KPI{
				Label:  firstString(m, "label", "name", "title"),
				Value:  firstString(m, "value", "val"),
				Change: firstString(m, "change", "delta", "diff", "yoy", "mom"),
				Trend:  firstString(m, "trend", "direction"),
				Note:   firstString(m, "note", "desc", "description", "sub"),
			}
			if k.Label != "" || k.Value != "" {
				k.Trend = NormalizeTrend(k.Trend, k.Change)
				out = append(out, k)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("指标卡为空")
		}
		return out, nil
	}
	var out []KPI
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*"))
		if line == "" {
			continue
		}
		var parts []string
		if strings.Contains(line, "|") {
			for _, p := range strings.Split(strings.Trim(line, "|"), "|") {
				parts = append(parts, strings.TrimSpace(p))
			}
		} else if idx := strings.IndexAny(line, ":："); idx > 0 {
			parts = []string{strings.TrimSpace(line[:idx]), strings.TrimSpace(strings.TrimLeft(line[idx:], ":："))}
		} else {
			continue
		}
		k := KPI{Label: at(parts, 0), Value: at(parts, 1), Change: at(parts, 2), Trend: at(parts, 3), Note: at(parts, 4)}
		k.Trend = NormalizeTrend(k.Trend, k.Change)
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("指标卡为空")
	}
	return out, nil
}

// NormalizeTrend 趋势归一：up|down|flat；未指定时按变化值符号推断
func NormalizeTrend(trend, change string) string {
	t := strings.ToLower(strings.TrimSpace(trend))
	switch t {
	case "up", "rise", "increase", "上升", "增长", "↑", "+", "positive", "good":
		return "up"
	case "down", "fall", "decrease", "下降", "减少", "↓", "-", "negative", "bad":
		return "down"
	case "flat", "持平", "stable", "neutral", "=":
		return "flat"
	}
	c := strings.TrimSpace(change)
	switch {
	case c == "":
		return ""
	case strings.HasPrefix(c, "+") || strings.HasPrefix(c, "↑") || strings.HasPrefix(c, "▲") || strings.Contains(c, "增"):
		return "up"
	case strings.HasPrefix(c, "-") || strings.HasPrefix(c, "−") || strings.HasPrefix(c, "↓") || strings.HasPrefix(c, "▼") || strings.Contains(c, "降"):
		return "down"
	}
	return "flat"
}

// ParseDataTableJSON 解析 ```table 代码块：{"sql": "...", "title": "...", "limit": 20} 或 {"headers": [...], "rows": [[...]]}
func ParseDataTableJSON(src string) (*Block, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(src)), &raw); err != nil {
		return nil, fmt.Errorf("数据表 JSON 解析失败：%v", err)
	}
	b := &Block{Kind: KindTable, Title: firstString(raw, "title", "caption", "name")}
	if sql := firstString(raw, "sql", "query"); sql != "" {
		b.DataRef = &DataRef{SQL: sql, ConnID: firstString(raw, "connId", "connID", "conn_id"), Limit: int(toFloat(raw["limit"]))}
		return b, nil
	}
	headers := toStringSlice(firstValue(raw, "headers", "columns"))
	var rows [][]string
	if arr, ok := firstValue(raw, "rows", "data").([]any); ok {
		for _, r := range arr {
			switch rv := r.(type) {
			case []any:
				rows = append(rows, toStringSlice(rv))
			case map[string]any:
				row := make([]string, len(headers))
				for i, h := range headers {
					row[i] = CellString(rv[h])
				}
				rows = append(rows, row)
			}
		}
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("数据表缺少 sql 或 headers/rows")
	}
	b.Table = &Table{Headers: headers, Rows: AlignRows(rows, len(headers))}
	return b, nil
}

// AlignRows 补齐/截断行单元格数
func AlignRows(rows [][]string, n int) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells := make([]string, n)
		copy(cells, r)
		out = append(out, cells)
	}
	return out
}

// CellString 单元格值转字符串（NULL → 空，[]byte → 文本，时间 → 标准格式）
func CellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	case float64:
		return FormatPlainNumber(x)
	case float32:
		return FormatPlainNumber(float64(x))
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format("2006-01-02 15:04:05")
	case bool:
		if x {
			return "是"
		}
		return "否"
	}
	return fmt.Sprint(v)
}

// FormatPlainNumber 数字转字符串（整数不带小数，其余最多 4 位小数）
func FormatPlainNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 4, 64), "0"), ".")
}

// ParseNumber 宽松解析数字（支持千分位、百分号、货币符号、中文单位 万/亿）
func ParseNumber(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(t, "%"):
		t = strings.TrimSuffix(t, "%")
	case strings.HasSuffix(t, "亿"):
		t, mult = strings.TrimSuffix(t, "亿"), 1e8
	case strings.HasSuffix(t, "万"):
		t, mult = strings.TrimSuffix(t, "万"), 1e4
	}
	t = strings.NewReplacer(",", "", "，", "", "¥", "", "￥", "", "$", "", " ", "").Replace(t)
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f * mult, true
}

// ─── 通用取值辅助 ─────────────────────────────────────────────

func firstValue(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	v := firstValue(m, keys...)
	if v == nil {
		return ""
	}
	return strings.TrimSpace(CellString(v))
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			out = append(out, CellString(it))
		}
		return out
	case []string:
		return x
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		parts := strings.Split(x, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return nil
}

func toFloatSlice(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, it := range arr {
		out = append(out, toFloat(it))
	}
	return out
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := ParseNumber(x)
		return f
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func at(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
