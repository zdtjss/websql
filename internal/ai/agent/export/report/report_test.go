package report

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleMarkdown = "# 2026 年三季度销售分析\n\n" +
	"本季度整体销售额 **同比增长 12.5%**，华东区贡献最大，退货率下降。\n\n" +
	"```kpi\n" +
	`[{"label":"销售额","value":"¥1,234.5万","change":"+12.5%","trend":"up","note":"同比"},` +
	`{"label":"订单数","value":"8,562","change":"+6.1%"},` +
	`{"label":"客单价","value":"¥1,442","change":"-2.3%"},` +
	`{"label":"退货率","value":"1.8%","change":"-0.4pp","trend":"up"}]` + "\n```\n\n" +
	"## 一、区域表现\n\n" +
	"```chart\n" + `{"type":"bar","title":"各区域销售额","unit":"万元","categories":["华东","华南","华北","西南","东北"],"series":[{"name":"Q2","data":[320,280,210,150,90]},{"name":"Q3","data":[380,300,230,170,85]}]}` + "\n```\n\n" +
	"- 华东区环比增长 **18.8%**，领跑全国\n- 东北区小幅下滑，需关注渠道流失\n  - 主要流失渠道：线下经销\n\n" +
	"> [!WARNING] 风险提示\n> 东北区连续两个季度下滑\n> - 建议复盘经销商政策\n\n" +
	"## 二、品类结构\n\n" +
	"```mermaid\npie title 品类占比\n    \"数码\" : 42\n    \"家电\" : 31\n    \"服饰\" : 27\n```\n\n" +
	"```chart\n" + `{"type":"line","title":"月度趋势","categories":["7月","8月","9月"],"series":[{"name":"销售额","data":[380,420,434.5]}]}` + "\n```\n\n" +
	"| 区域 | 销售额(万) | 同比 |\n|:---|---:|:---:|\n| 华东 | 380 | +18.8% |\n| 华南 | 300 | +7.1% |\n| 东北 | 85 | -5.6% |\n\n" +
	"## 三、结论与建议\n\n1. 加大华东投入\n2. 东北区启动渠道专项\n\n" +
	"```mermaid\nflowchart LR\n  A[诊断] --> B[方案] --> C[复盘]\n```\n\n" +
	"<!-- pagebreak -->\n\n$$\nGMV = \\sum_i price_i \\times qty_i\n$$\n"

func sampleDoc() *Document {
	return BuildDocument("2026 年三季度销售分析", "经营分析 · 管理层汇报", sampleMarkdown)
}

func TestParseMarkdownExtensions(t *testing.T) {
	doc := sampleDoc()
	counts := map[BlockKind]int{}
	for _, b := range doc.Blocks {
		counts[b.Kind]++
	}
	if doc.Title != "2026 年三季度销售分析" {
		t.Fatalf("title = %q", doc.Title)
	}
	if doc.Blocks[0].Kind == KindHeading && doc.Blocks[0].Level == 1 {
		t.Error("与标题相同的首个一级标题应提升为封面标题")
	}
	want := map[BlockKind]int{KindKPI: 1, KindChart: 3, KindCallout: 1, KindTable: 1, KindMermaid: 1, KindPageBreak: 1, KindCode: 1}
	for k, n := range want {
		if counts[k] != n {
			t.Errorf("block %s count = %d, want %d (all=%v)", k, counts[k], n, counts)
		}
	}
	for _, b := range doc.Blocks {
		if b.Kind == KindKPI && (b.KPIs[1].Trend != "up" || b.KPIs[2].Trend != "down") {
			t.Errorf("trend 推断错误：%+v", b.KPIs)
		}
		if b.Kind == KindChart && b.Chart.Title == "品类占比" && b.Chart.Type != "pie" {
			t.Errorf("mermaid pie 应转为 pie 图表，实际 %s", b.Chart.Type)
		}
		if b.Kind == KindList && !b.Ordered && len(b.Items[1].Children) != 1 {
			t.Errorf("嵌套列表解析错误：%+v", b.Items)
		}
		if b.Kind == KindTable && b.Table.Align[1] != "right" {
			t.Errorf("表格对齐解析错误：%v", b.Table.Align)
		}
	}
}

func TestParseChartJSONVariants(t *testing.T) {
	cases := []string{
		`{"chartType":"doughnut","labels":["a","b"],"values":[1,2]}`,
		`{"type":"pie","data":[{"name":"a","value":3},{"name":"b","value":"4"}]}`,
		`{"type":"column","xAxis":["1月","2月"],"series":[{"label":"收入","values":["1,200","3,400"]}]}`,
		`{"type":"line","sql":"SELECT m, v FROM t","x":"m","y":"v"}`,
	}
	for _, c := range cases {
		spec, err := ParseChartJSON(c)
		if err != nil {
			t.Fatalf("ParseChartJSON(%s): %v", c, err)
		}
		if spec.SQL == "" && !spec.HasData() {
			t.Errorf("no data: %s", c)
		}
	}
	if s, _ := ParseChartJSON(cases[2]); s.Series[0].Data[1] != 3400 {
		t.Errorf("千分位数字解析失败：%v", s.Series[0].Data)
	}
	if _, err := ParseChartJSON(`{"type":"bar"}`); err == nil {
		t.Error("无数据图表应报错")
	}
}

func TestInlineAndSafeURL(t *testing.T) {
	spans := ParseInline("前缀 **粗体** 与 *斜体* `code` [链接](javascript:alert(1)) [ok](https://a.b)")
	var bold, italic, code, link int
	for _, s := range spans {
		if s.Bold {
			bold++
		}
		if s.Italic {
			italic++
		}
		if s.Code {
			code++
		}
		if s.Link != "" {
			link++
			if !strings.HasPrefix(s.Link, "https://") {
				t.Errorf("不安全链接未过滤：%q", s.Link)
			}
		}
	}
	if bold != 1 || italic != 1 || code != 1 || link != 1 {
		t.Errorf("inline parse: bold=%d italic=%d code=%d link=%d spans=%+v", bold, italic, code, link, spans)
	}
}

func TestRenderHTMLEscapes(t *testing.T) {
	doc := BuildDocument("XSS </script><script>alert(1)</script>", "", "正文 <img src=x onerror=alert(1)> </SCRIPT >\n\n| a |\n|---|\n| <b>x</b> |")
	out := string(RenderHTML(doc, ResolveTheme("", "business")))
	for _, bad := range []string{"<script>alert", "<img src=x", "</SCRIPT", "<b>x</b>"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML 未转义：%s", bad)
		}
	}
	if strings.Contains(out, "cdn.jsdelivr") {
		t.Error("无 mermaid/公式 的文档不应引入 CDN 脚本")
	}
	full := string(RenderHTML(sampleDoc(), ResolveTheme("", "business")))
	if !strings.Contains(full, "<svg") || !strings.Contains(full, "kpi-grid") || !strings.Contains(full, "callout warning") {
		t.Error("HTML 缺少图表/指标卡/提示框")
	}
	if !strings.Contains(full, "securityLevel: 'strict'") {
		t.Error("mermaid 必须使用 strict 安全级别")
	}
}

// checkOOXML 校验包内所有 XML 部件格式良好，返回部件名集合
func checkOOXML(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		dec := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := dec.Token(); err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("%s 不是格式良好的 XML：%v", f.Name, err)
			}
		}
	}
	return names
}

func writeSample(t *testing.T, name string, data []byte) {
	if dir := os.Getenv("REPORT_SAMPLE_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}
}

func TestRenderDOCX(t *testing.T) {
	for _, theme := range []string{"business", "tech"} {
		data, err := RenderDOCX(sampleDoc(), ResolveTheme(theme, "business"))
		if err != nil {
			t.Fatal(err)
		}
		names := checkOOXML(t, data)
		for _, part := range []string{"word/document.xml", "word/styles.xml", "word/numbering.xml", "word/footer1.xml", "word/charts/chart3.xml", "word/embeddings/Microsoft_Excel_Worksheet1.xlsx"} {
			if !names[part] {
				t.Errorf("[%s] 缺少部件 %s", theme, part)
			}
		}
		writeSample(t, "sample_"+theme+".docx", data)
	}
}

func TestRenderPPTX(t *testing.T) {
	for _, theme := range []string{"tech", "business"} {
		data, err := RenderPPTX(sampleDoc(), ResolveTheme(theme, "tech"))
		if err != nil {
			t.Fatal(err)
		}
		names := checkOOXML(t, data)
		slides := 0
		for n := range names {
			if strings.HasPrefix(n, "ppt/slides/slide") && strings.HasSuffix(n, ".xml") {
				slides++
			}
		}
		if slides < 8 {
			t.Errorf("[%s] 幻灯片数过少：%d", theme, slides)
		}
		if !names["ppt/charts/chart1.xml"] || !names["ppt/theme/theme1.xml"] {
			t.Errorf("[%s] 缺少图表或主题部件", theme)
		}
		writeSample(t, "sample_"+theme+".pptx", data)
	}
}

func TestRenderXLSX(t *testing.T) {
	data := &SheetData{Name: "数据明细", Headers: []string{"区域", "销售额", "订单数"}, Rows: [][]any{{"华东", 380.5, int64(1200)}, {"华南", "300", 980}, {"东北", 85, 300}}}
	out, err := RenderXLSX(XLSXOptions{
		Title: "测试", Theme: ResolveTheme("", "business"), Summary: sampleDoc(), Data: data,
		DataCharts: []DataChart{{Type: "bar", XCol: 0, YCols: []int{1, 2}}, {Type: "pie", XCol: 0, YCols: []int{1}}},
		SQL:        "SELECT region, amount, cnt FROM t",
	})
	if err != nil {
		t.Fatal(err)
	}
	names := checkOOXML(t, out)
	if !names["xl/charts/chart1.xml"] {
		t.Error("xlsx 缺少原生图表")
	}
	writeSample(t, "sample.xlsx", out)
	writeSample(t, "sample.html", RenderHTML(sampleDoc(), ResolveTheme("", "business")))
}

func TestChartSVGAllTypes(t *testing.T) {
	for _, typ := range []string{"bar", "hbar", "stacked", "line", "area", "pie", "donut", "scatter", "radar"} {
		c := &ChartSpec{Type: typ, Title: typ, Categories: []string{"1", "2", "3", "4"},
			Series: []Series{{Name: "A", Data: []float64{1, 3, 2, 5}}, {Name: "B", Data: []float64{2, 1, 4, 3}}}}
		svg := RenderChartSVG(c, ResolveTheme("", "business"))
		if !strings.HasPrefix(svg, "<svg") {
			t.Errorf("%s: invalid svg", typ)
		}
		dec := xml.NewDecoder(strings.NewReader(svg))
		for {
			if _, err := dec.Token(); err != nil {
				if err != io.EOF {
					t.Errorf("%s: svg 格式错误 %v", typ, err)
				}
				break
			}
		}
	}
}
