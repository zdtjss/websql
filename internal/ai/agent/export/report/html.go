package report

import (
	"fmt"
	"strings"
	"time"
)

// RenderHTML 渲染自包含 HTML 报告。
//
// 安全与可用性：
//   - 服务端完成全部 Markdown 渲染，所有文本经统一转义，不输出任何原始 HTML（杜绝 XSS）；
//   - 图表为内联 SVG，样式内联——断网/内网环境完整可读、可打印；
//   - 仅当文档包含 mermaid 流程图或数学公式时，才引入 CDN 脚本做渐进增强
//     （mermaid 使用 securityLevel=strict；加载失败时回退显示源码）。
func RenderHTML(doc *Document, theme Theme) []byte {
	h := &htmlRenderer{doc: doc, t: theme}
	return []byte(h.render())
}

type htmlRenderer struct {
	doc        *Document
	t          Theme
	sb         strings.Builder
	figN, tblN int
	hasMermaid bool
	hasMath    bool
}

func (h *htmlRenderer) render() string {
	d := h.doc
	date := d.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	var body strings.Builder
	h.sb.Reset()
	// 目录（≥3 个一二级标题时生成）
	heads := d.Headings(2)
	ids := make([]string, 0, len(heads))
	for i := range heads {
		ids = append(ids, fmt.Sprintf("sec-%d", i+1))
	}
	if len(heads) >= 3 {
		body.WriteString(`<nav class="toc"><div class="toc-title">目录</div><ol>`)
		for i, hd := range heads {
			cls := ""
			if hd.Level == 2 {
				cls = ` class="sub"`
			}
			fmt.Fprintf(&body, `<li%s><a href="#%s">%s</a></li>`, cls, ids[i], xmlEscape(PlainText(hd.Text)))
		}
		body.WriteString(`</ol></nav>`)
	}
	hi := 0
	for _, b := range d.Blocks {
		if b.Kind == KindHeading && b.Level <= 2 {
			h.block(&body, b, ids[hi])
			hi++
			continue
		}
		h.block(&body, b, "")
	}

	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"UTF-8\">\n")
	out.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1.0">` + "\n")
	out.WriteString(`<meta name="generator" content="WebSQL Report">` + "\n")
	fmt.Fprintf(&out, "<title>%s</title>\n<style>%s</style>\n</head>\n<body>\n", xmlEscape(d.Title), h.css())
	out.WriteString(`<header class="cover"><div class="cover-inner">`)
	fmt.Fprintf(&out, `<div class="kicker">数据分析报告</div><h1 class="title">%s</h1>`, xmlEscape(d.Title))
	if d.Subtitle != "" {
		fmt.Fprintf(&out, `<p class="subtitle">%s</p>`, xmlEscape(d.Subtitle))
	}
	meta := []string{"生成日期：" + date}
	if d.Author != "" {
		meta = append(meta, "作者："+d.Author)
	}
	if d.Organization != "" {
		meta = append(meta, d.Organization)
	}
	fmt.Fprintf(&out, `<div class="meta">%s</div></div></header>`, xmlEscape(strings.Join(meta, "　·　")))
	out.WriteString(`<main class="content">`)
	out.WriteString(body.String())
	out.WriteString(`</main><footer class="footer">由 WebSQL 智能分析生成 · ` + xmlEscape(date) + `</footer>`)
	out.WriteString(h.scripts())
	out.WriteString("\n</body>\n</html>\n")
	return out.String()
}

func (h *htmlRenderer) inline(text string) string {
	var sb strings.Builder
	lines := strings.Split(text, "\n")
	for li, line := range lines {
		if li > 0 {
			sb.WriteString("<br>")
		}
		if strings.Contains(line, "$") {
			h.hasMath = h.hasMath || strings.Count(line, "$") >= 2
		}
		for _, s := range ParseInline(line) {
			t := xmlEscape(s.Text)
			if s.Code {
				t = "<code>" + t + "</code>"
			}
			if s.Bold {
				t = "<strong>" + t + "</strong>"
			}
			if s.Italic {
				t = "<em>" + t + "</em>"
			}
			if s.Strike {
				t = "<del>" + t + "</del>"
			}
			if s.Link != "" {
				t = fmt.Sprintf(`<a href="%s" rel="noopener noreferrer" target="_blank">%s</a>`, xmlEscape(s.Link), t)
			}
			sb.WriteString(t)
		}
	}
	return sb.String()
}

func (h *htmlRenderer) list(sb *strings.Builder, items []ListItem, ordered bool) {
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	sb.WriteString("<" + tag + ">")
	for _, it := range items {
		sb.WriteString("<li>" + h.inline(it.Text))
		if len(it.Children) > 0 {
			h.list(sb, it.Children, false)
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</" + tag + ">")
}

func (h *htmlRenderer) block(sb *strings.Builder, b Block, id string) {
	switch b.Kind {
	case KindHeading:
		idAttr := ""
		if id != "" {
			idAttr = fmt.Sprintf(` id="%s"`, id)
		}
		fmt.Fprintf(sb, `<h%d%s>%s</h%d>`, b.Level+1, idAttr, h.inline(b.Text), b.Level+1)
	case KindParagraph:
		sb.WriteString("<p>" + h.inline(b.Text) + "</p>")
	case KindList:
		h.list(sb, b.Items, b.Ordered)
	case KindQuote:
		sb.WriteString("<blockquote>" + h.inline(b.Text) + "</blockquote>")
	case KindCallout:
		title := b.Title
		if title == "" {
			title = CalloutLabel(b.Style)
		}
		fmt.Fprintf(sb, `<div class="callout %s"><div class="callout-title">%s</div>`, xmlEscape(b.Style), h.inline(title))
		if b.Text != "" {
			sb.WriteString(`<div class="callout-body">` + h.calloutBody(b.Text) + `</div>`)
		}
		sb.WriteString(`</div>`)
	case KindDivider:
		sb.WriteString("<hr>")
	case KindPageBreak:
		sb.WriteString(`<div class="page-break"></div>`)
	case KindCode:
		if b.Lang == "math" {
			h.hasMath = true
			sb.WriteString(`<div class="math">$$` + xmlEscape(b.Code) + `$$</div>`)
			return
		}
		lang := ""
		if b.Lang != "" {
			lang = fmt.Sprintf(`<span class="lang">%s</span>`, xmlEscape(b.Lang))
		}
		sb.WriteString(`<pre class="code">` + lang + `<code>` + xmlEscape(b.Code) + `</code></pre>`)
	case KindMermaid:
		h.hasMermaid = true
		fmt.Fprintf(sb, `<figure class="diagram"><pre class="mermaid">%s</pre></figure>`, xmlEscape(b.Code))
	case KindKPI:
		sb.WriteString(`<div class="kpi-grid">`)
		for _, k := range b.KPIs {
			sb.WriteString(`<div class="kpi">`)
			fmt.Fprintf(sb, `<div class="kpi-label">%s</div><div class="kpi-value">%s</div>`, xmlEscape(k.Label), xmlEscape(k.Value))
			if k.Change != "" {
				arrow := ""
				switch k.Trend {
				case "up":
					arrow = "▲ "
				case "down":
					arrow = "▼ "
				}
				fmt.Fprintf(sb, `<div class="kpi-change %s">%s%s</div>`, xmlEscape(k.Trend), arrow, xmlEscape(k.Change))
			}
			if k.Note != "" {
				fmt.Fprintf(sb, `<div class="kpi-note">%s</div>`, xmlEscape(k.Note))
			}
			sb.WriteString(`</div>`)
		}
		sb.WriteString(`</div>`)
	case KindChart:
		if b.Error != "" || !b.Chart.HasData() {
			h.errorBox(sb, "图表未能生成", firstNonEmpty(b.Error, "无数据"))
			return
		}
		h.figN++
		sb.WriteString(`<figure class="chart">`)
		if b.Chart.Title != "" {
			fmt.Fprintf(sb, `<div class="chart-title">%s</div>`, xmlEscape(b.Chart.Title))
		}
		sb.WriteString(RenderChartSVG(b.Chart, h.t))
		cap := fmt.Sprintf("图 %d", h.figN)
		if b.Chart.Title != "" {
			cap += "　" + b.Chart.Title
		}
		fmt.Fprintf(sb, `<figcaption>%s</figcaption></figure>`, xmlEscape(cap))
	case KindTable:
		if b.Error != "" || b.Table == nil {
			h.errorBox(sb, "数据表未能生成", firstNonEmpty(b.Error, "无数据"))
			return
		}
		h.table(sb, b)
	}
}

func (h *htmlRenderer) calloutBody(text string) string {
	// 提示框内允许简单列表（- 开头的行）
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	inList := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			if !inList {
				sb.WriteString("<ul>")
				inList = true
			}
			sb.WriteString("<li>" + h.inline(t[2:]) + "</li>")
			continue
		}
		if inList {
			sb.WriteString("</ul>")
			inList = false
		}
		if t != "" {
			sb.WriteString("<p>" + h.inline(t) + "</p>")
		}
	}
	if inList {
		sb.WriteString("</ul>")
	}
	return sb.String()
}

func (h *htmlRenderer) errorBox(sb *strings.Builder, title, msg string) {
	fmt.Fprintf(sb, `<div class="callout warning"><div class="callout-title">%s</div><div class="callout-body"><p>%s</p></div></div>`,
		xmlEscape(title), xmlEscape(msg))
}

func (h *htmlRenderer) table(sb *strings.Builder, b Block) {
	tb := b.Table
	h.tblN++
	numeric := numericColumns(tb)
	if b.Title != "" {
		fmt.Fprintf(sb, `<div class="table-caption">表 %d　%s</div>`, h.tblN, xmlEscape(b.Title))
	}
	sb.WriteString(`<div class="table-wrap"><table><thead><tr>`)
	for i, hd := range tb.Headers {
		fmt.Fprintf(sb, `<th%s>%s</th>`, alignAttr(tb, numeric, i), h.inline(hd))
	}
	sb.WriteString(`</tr></thead><tbody>`)
	for _, row := range tb.Rows {
		sb.WriteString("<tr>")
		for i := range tb.Headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			fmt.Fprintf(sb, `<td%s>%s</td>`, alignAttr(tb, numeric, i), h.inline(cell))
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString(`</tbody></table></div>`)
	if tb.TotalRows > len(tb.Rows) {
		fmt.Fprintf(sb, `<div class="table-note">共 %d 行，此处展示前 %d 行</div>`, tb.TotalRows, len(tb.Rows))
	}
}

func alignAttr(tb *Table, numeric []bool, i int) string {
	a := ""
	if i < len(tb.Align) {
		a = tb.Align[i]
	}
	if a == "" && i < len(numeric) && numeric[i] {
		a = "right"
	}
	if a == "" || a == "left" {
		return ""
	}
	return fmt.Sprintf(` class="%s"`, a)
}

// numericColumns 判断表格每一列是否为数值列
func numericColumns(tb *Table) []bool {
	out := make([]bool, len(tb.Headers))
	for i := range tb.Headers {
		vals := make([]string, 0, len(tb.Rows))
		for _, r := range tb.Rows {
			if i < len(r) {
				vals = append(vals, r[i])
			}
		}
		out[i] = IsNumericColumn(vals)
	}
	return out
}

func (h *htmlRenderer) scripts() string {
	var sb strings.Builder
	if h.hasMermaid {
		theme := "default"
		if h.t.Dark {
			theme = "dark"
		}
		// 渐进增强：离线时 import 失败，保留 <pre> 源码显示
		fmt.Fprintf(&sb, `<script type="module">
try {
  const m = await import('https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs');
  m.default.initialize({ startOnLoad: false, securityLevel: 'strict', theme: '%s' });
  await m.default.run({ querySelector: 'pre.mermaid' });
} catch (e) { document.querySelectorAll('pre.mermaid').forEach(el => el.classList.add('fallback')); }
</script>`, theme)
	}
	if h.hasMath {
		sb.WriteString(`<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css">
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/contrib/auto-render.min.js" onload="try{renderMathInElement(document.querySelector('main'),{delimiters:[{left:'$$',right:'$$',display:true},{left:'$',right:'$',display:false}],throwOnError:false})}catch(e){}"></script>`)
	}
	return sb.String()
}

func (h *htmlRenderer) css() string {
	t := h.t
	calloutCSS := ""
	for _, st := range []string{"note", "tip", "important", "warning", "caution"} {
		c := CalloutColor(st)
		bg := Tint(c, 0.9)
		if t.Dark {
			bg = Shade(c, 0.75)
		}
		calloutCSS += fmt.Sprintf(".callout.%s{border-left-color:#%s;background:#%s}.callout.%s .callout-title{color:#%s}", st, c, bg, st, c)
	}
	coverBg := fmt.Sprintf("linear-gradient(135deg,#%s 0%%,#%s 100%%)", t.Primary, t.Accent)
	if t.Dark {
		coverBg = fmt.Sprintf("linear-gradient(135deg,#%s 0%%,#%s 100%%)", Shade(t.Background, 0.2), t.Surface)
	}
	return fmt.Sprintf(`
:root{--bg:#%[1]s;--surface:#%[2]s;--fg:#%[3]s;--muted:#%[4]s;--primary:#%[5]s;--accent:#%[6]s;--border:#%[7]s;--th-bg:#%[8]s;--th-fg:#%[9]s;--zebra:#%[10]s;--up:#%[11]s;--down:#%[12]s}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.75 -apple-system,"Segoe UI","Microsoft YaHei","PingFang SC","Noto Sans CJK SC",sans-serif;-webkit-font-smoothing:antialiased}
.cover{background:%[13]s;color:#fff;padding:56px 24px 48px}
.cover-inner{max-width:1040px;margin:0 auto}
.kicker{letter-spacing:.2em;font-size:13px;opacity:.8;text-transform:uppercase}
.title{font-size:34px;line-height:1.3;margin:10px 0 8px;font-weight:700}
.subtitle{font-size:17px;opacity:.92;margin:0 0 18px}
.meta{font-size:13px;opacity:.8}
.content{max-width:1040px;margin:0 auto;padding:32px 24px 48px}
h2,h3,h4,h5{line-height:1.4;margin:1.8em 0 .7em;color:var(--primary)}
h2{font-size:24px;padding-bottom:8px;border-bottom:2px solid var(--border)}
h3{font-size:19px}h4{font-size:16px}h5{font-size:15px}
p{margin:.6em 0}
a{color:var(--accent)}
code{font-family:Consolas,"SFMono-Regular",Menlo,monospace;font-size:.9em;background:var(--surface);padding:1px 5px;border-radius:4px}
pre.code{position:relative;background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px 16px;overflow:auto;font-size:13px;line-height:1.55}
pre.code code{background:none;padding:0}
pre.code .lang{position:absolute;top:6px;right:10px;font-size:11px;color:var(--muted)}
blockquote{margin:1em 0;padding:8px 16px;border-left:4px solid var(--border);color:var(--muted);background:var(--surface);border-radius:0 6px 6px 0}
hr{border:0;border-top:1px solid var(--border);margin:28px 0}
ul,ol{padding-left:1.6em}li{margin:.25em 0}
.toc{background:var(--surface);border:1px solid var(--border);border-radius:10px;padding:16px 22px;margin-bottom:28px}
.toc-title{font-weight:700;color:var(--primary);margin-bottom:6px}
.toc ol{margin:0;padding-left:1.4em}.toc li.sub{margin-left:1.2em;list-style:circle;font-size:14px}
.toc a{color:var(--fg);text-decoration:none}.toc a:hover{color:var(--accent)}
.kpi-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:14px;margin:18px 0}
.kpi{background:var(--surface);border:1px solid var(--border);border-top:3px solid var(--accent);border-radius:10px;padding:14px 16px}
.kpi-label{font-size:13px;color:var(--muted)}
.kpi-value{font-size:26px;font-weight:700;color:var(--primary);line-height:1.35;margin-top:2px;word-break:break-all}
.kpi-change{font-size:13px;font-weight:600;color:var(--muted)}.kpi-change.up{color:var(--up)}.kpi-change.down{color:var(--down)}
.kpi-note{font-size:12px;color:var(--muted);margin-top:2px}
.callout{border-left:4px solid;border-radius:0 8px 8px 0;padding:10px 16px;margin:16px 0}
.callout-title{font-weight:700;margin-bottom:2px}.callout-body p{margin:.3em 0}.callout-body ul{margin:.3em 0}
%[14]s
figure{margin:22px 0}
.chart{background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:14px 12px 8px}
.chart svg{width:100%%;height:auto;display:block}
.chart-title{font-weight:600;text-align:center;margin-bottom:4px}
figcaption{text-align:center;color:var(--muted);font-size:13px;margin-top:6px}
.diagram{border:1px dashed var(--border);border-radius:10px;padding:12px;overflow:auto}
pre.mermaid{margin:0;text-align:center;background:none}
pre.mermaid.fallback{text-align:left;font-size:13px;white-space:pre-wrap;color:var(--muted)}
.table-caption{font-weight:600;margin:18px 0 6px;color:var(--primary)}
.table-wrap{overflow:auto;border:1px solid var(--border);border-radius:8px;margin:8px 0;max-height:680px}
table{border-collapse:collapse;width:100%%;font-size:14px}
th{background:var(--th-bg);color:var(--th-fg);font-weight:600;position:sticky;top:0;white-space:nowrap}
th,td{padding:8px 12px;border-bottom:1px solid var(--border);text-align:left;vertical-align:top}
tbody tr:nth-child(even){background:var(--zebra)}
td.right,th.right{text-align:right;font-variant-numeric:tabular-nums}td.center,th.center{text-align:center}
.table-note{font-size:12px;color:var(--muted);margin-top:4px}
.math{overflow:auto;margin:12px 0;text-align:center}
.footer{max-width:1040px;margin:0 auto;padding:18px 24px 40px;color:var(--muted);font-size:12px;border-top:1px solid var(--border)}
.page-break{height:0}
@media print{
 body{font-size:12pt;background:#fff}
 .cover{padding:40px 24px;-webkit-print-color-adjust:exact;print-color-adjust:exact}
 .content{max-width:none;padding:0 8mm}
 h2{break-before:auto;break-after:avoid}h3,h4{break-after:avoid}
 .kpi,.chart,.callout,figure,tr{break-inside:avoid}
 .table-wrap{max-height:none;overflow:visible}
 th{position:static;-webkit-print-color-adjust:exact;print-color-adjust:exact}
 .page-break{break-after:page}
 a{color:inherit;text-decoration:none}
}
@media (max-width:640px){.title{font-size:26px}.content{padding:20px 14px}}
`, t.Background, t.Surface, t.Text, t.Muted, t.Primary, t.Accent, t.Border, t.HeaderBg, t.HeaderText, t.Zebra, t.Up, t.Down, coverBg, calloutCSS)
}
