package report

import (
	"fmt"
	"math"
	"strings"
)

// RenderChartSVG 把图表渲染为自包含 SVG（纯 Go，无 JS 依赖，离线可用、可打印）。
func RenderChartSVG(c *ChartSpec, t Theme) string {
	if c == nil || !c.HasData() {
		return ""
	}
	cc := *c
	cc.Normalize()
	r := &svgCanvas{W: 760, H: 420, theme: t}
	switch cc.Type {
	case "pie", "donut":
		r.pie(&cc)
	case "radar":
		r.radar(&cc)
	case "hbar":
		r.hbar(&cc)
	case "scatter":
		r.scatter(&cc)
	default:
		r.cartesian(&cc)
	}
	return r.String()
}

type svgCanvas struct {
	W, H  float64
	theme Theme
	sb    strings.Builder
}

const svgFont = `-apple-system,'Segoe UI','Microsoft YaHei','PingFang SC','Noto Sans CJK SC',sans-serif`

func (r *svgCanvas) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" role="img" font-family="%s" font-size="12">`, r.W, r.H, svgFont)
	out.WriteString(r.sb.String())
	out.WriteString(`</svg>`)
	return out.String()
}

func (r *svgCanvas) text(x, y float64, s, anchor, color string, size float64, extra string) {
	fmt.Fprintf(&r.sb, `<text x="%.1f" y="%.1f" text-anchor="%s" fill="#%s" font-size="%.0f"%s>%s</text>`,
		x, y, anchor, color, size, extra, xmlEscape(s))
}

func (r *svgCanvas) line(x1, y1, x2, y2 float64, color string, width float64, dash bool) {
	d := ""
	if dash {
		d = ` stroke-dasharray="3,3"`
	}
	fmt.Fprintf(&r.sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#%s" stroke-width="%.1f"%s/>`, x1, y1, x2, y2, color, width, d)
}

// legend 绘制图例，返回占用高度
func (r *svgCanvas) legend(names []string, y float64) float64 {
	if len(names) <= 1 {
		return 0
	}
	x := 16.0
	row := 0.0
	for i, n := range names {
		label := TruncateRunes(n, 14)
		w := float64(DisplayWidth(label))*6.5 + 28
		if x+w > r.W-16 {
			x = 16
			row += 20
		}
		fmt.Fprintf(&r.sb, `<rect x="%.1f" y="%.1f" width="12" height="12" rx="2" fill="#%s"/>`, x, y+row-10, r.theme.SeriesColor(i))
		r.text(x+17, y+row, label, "start", r.theme.Muted, 12, "")
		x += w
	}
	return row + 24
}

func seriesNames(c *ChartSpec) []string {
	names := make([]string, len(c.Series))
	for i, s := range c.Series {
		names[i] = s.Name
	}
	return names
}

// categoryLabels 计算 X 轴标签的显示方式
func categoryLabels(cats []string, band float64) (step int, rotate bool, maxRunes int) {
	step, maxRunes = 1, 12
	maxW := 0
	for _, c := range cats {
		if w := DisplayWidth(c); w > maxW {
			maxW = w
		}
	}
	if float64(maxW)*6.5 > band*0.95 {
		rotate = true
	}
	if n := len(cats); n > 24 {
		step = int(math.Ceil(float64(n) / 24))
	}
	return
}

func (r *svgCanvas) cartesian(c *ChartSpec) {
	t := r.theme
	top := 16.0
	top += r.legend(seriesNames(c), 22)
	left, right, bottom := 64.0, 20.0, 40.0
	n := len(c.Categories)
	band := (r.W - left - right) / float64(n)
	step, rotate, maxRunes := categoryLabels(c.Categories, band)
	if rotate {
		bottom = 78
	}
	x0, x1, y0, y1 := left, r.W-right, top+8, r.H-bottom

	// 数值范围
	minV, maxV := 0.0, 0.0
	if c.Type == "stacked" {
		for i := 0; i < n; i++ {
			pos, neg := 0.0, 0.0
			for _, s := range c.Series {
				if s.Data[i] >= 0 {
					pos += s.Data[i]
				} else {
					neg += s.Data[i]
				}
			}
			maxV, minV = math.Max(maxV, pos), math.Min(minV, neg)
		}
	} else {
		for _, s := range c.Series {
			for _, v := range s.Data {
				maxV, minV = math.Max(maxV, v), math.Min(minV, v)
			}
		}
	}
	lo, hi, stepV := NiceScale(minV, maxV, 6)
	yOf := func(v float64) float64 { return y1 - (v-lo)/(hi-lo)*(y1-y0) }

	// 网格与 Y 轴刻度
	for v := lo; v <= hi+stepV/2; v += stepV {
		y := yOf(v)
		r.line(x0, y, x1, y, t.Grid, 1, v != 0)
		r.text(x0-8, y+4, FormatCompact(v), "end", t.Muted, 11, "")
	}
	r.line(x0, yOf(0), x1, yOf(0), t.Muted, 1, false)
	if c.Unit != "" {
		r.text(x0-8, y0-6, "单位："+c.Unit, "end", t.Muted, 11, "")
	}

	// X 轴标签
	for i, cat := range c.Categories {
		if i%step != 0 {
			continue
		}
		cx := x0 + band*(float64(i)+0.5)
		label := TruncateRunes(cat, maxRunes)
		if rotate {
			r.text(cx, y1+14, label, "end", t.Muted, 11, fmt.Sprintf(` transform="rotate(-35 %.1f %.1f)"`, cx, y1+14))
		} else {
			r.text(cx, y1+18, label, "middle", t.Muted, 11, "")
		}
	}

	showValues := n*len(c.Series) <= 16
	switch c.Type {
	case "line", "area":
		for j, s := range c.Series {
			color := t.SeriesColor(j)
			var pts []string
			for i, v := range s.Data {
				pts = append(pts, fmt.Sprintf("%.1f,%.1f", x0+band*(float64(i)+0.5), yOf(v)))
			}
			if c.Type == "area" {
				fmt.Fprintf(&r.sb, `<polygon points="%.1f,%.1f %s %.1f,%.1f" fill="#%s" fill-opacity="0.16"/>`,
					x0+band*0.5, yOf(0), strings.Join(pts, " "), x0+band*(float64(n)-0.5), yOf(0), color)
			}
			fmt.Fprintf(&r.sb, `<polyline points="%s" fill="none" stroke="#%s" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round"/>`, strings.Join(pts, " "), color)
			if n <= 40 {
				for i, v := range s.Data {
					cx, cy := x0+band*(float64(i)+0.5), yOf(v)
					fmt.Fprintf(&r.sb, `<circle cx="%.1f" cy="%.1f" r="3.5" fill="#%s" stroke="#%s" stroke-width="1.5"/>`, cx, cy, t.Background, color)
					if showValues && len(c.Series) == 1 {
						r.text(cx, cy-9, FormatCompact(v), "middle", t.Text, 11, "")
					}
				}
			}
		}
	case "stacked":
		barW := band * 0.62
		for i := 0; i < n; i++ {
			pos, neg := 0.0, 0.0
			bx := x0 + band*float64(i) + (band-barW)/2
			for j, s := range c.Series {
				v := s.Data[i]
				var ya, yb float64
				if v >= 0 {
					ya, yb = yOf(pos+v), yOf(pos)
					pos += v
				} else {
					ya, yb = yOf(neg), yOf(neg+v)
					neg += v
				}
				fmt.Fprintf(&r.sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#%s"><title>%s %s: %s</title></rect>`,
					bx, ya, barW, math.Max(yb-ya, 0), t.SeriesColor(j), xmlEscape(c.Categories[i]), xmlEscape(s.Name), FormatCompact(v))
			}
			if n <= 16 {
				r.text(bx+barW/2, yOf(pos)-6, FormatCompact(pos+neg), "middle", t.Text, 11, "")
			}
		}
	default: // bar
		groupW := band * 0.72
		barW := groupW / float64(len(c.Series))
		for j, s := range c.Series {
			for i, v := range s.Data {
				bx := x0 + band*float64(i) + (band-groupW)/2 + barW*float64(j)
				ya, yb := yOf(math.Max(v, 0)), yOf(math.Min(v, 0))
				fmt.Fprintf(&r.sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="2" fill="#%s"><title>%s %s: %s</title></rect>`,
					bx+1, ya, math.Max(barW-2, 1), math.Max(yb-ya, 0.5), t.SeriesColor(j), xmlEscape(c.Categories[i]), xmlEscape(s.Name), FormatCompact(v))
				if showValues {
					r.text(bx+barW/2, ya-5, FormatCompact(v), "middle", t.Text, 11, "")
				}
			}
		}
	}
}

func (r *svgCanvas) hbar(c *ChartSpec) {
	t := r.theme
	top := 12.0 + r.legend(seriesNames(c), 22)
	maxLabel := 0
	for _, cat := range c.Categories {
		if w := DisplayWidth(TruncateRunes(cat, 12)); w > maxLabel {
			maxLabel = w
		}
	}
	left := math.Min(math.Max(float64(maxLabel)*6.5+16, 60), 180)
	x0, x1, y0, y1 := left, r.W-56, top+8, r.H-32
	n := len(c.Categories)
	band := (y1 - y0) / float64(n)
	minV, maxV := 0.0, 0.0
	for _, s := range c.Series {
		for _, v := range s.Data {
			maxV, minV = math.Max(maxV, v), math.Min(minV, v)
		}
	}
	lo, hi, stepV := NiceScale(minV, maxV, 6)
	xOf := func(v float64) float64 { return x0 + (v-lo)/(hi-lo)*(x1-x0) }
	for v := lo; v <= hi+stepV/2; v += stepV {
		x := xOf(v)
		r.line(x, y0, x, y1, t.Grid, 1, v != 0)
		r.text(x, y1+16, FormatCompact(v), "middle", t.Muted, 11, "")
	}
	groupH := band * 0.72
	barH := groupH / float64(len(c.Series))
	for i, cat := range c.Categories {
		r.text(x0-8, y0+band*(float64(i)+0.5)+4, TruncateRunes(cat, 12), "end", t.Muted, 11, "")
	}
	for j, s := range c.Series {
		for i, v := range s.Data {
			by := y0 + band*float64(i) + (band-groupH)/2 + barH*float64(j)
			xa, xb := xOf(math.Min(v, 0)), xOf(math.Max(v, 0))
			fmt.Fprintf(&r.sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="2" fill="#%s"><title>%s: %s</title></rect>`,
				xa, by+1, math.Max(xb-xa, 0.5), math.Max(barH-2, 1), t.SeriesColor(j), xmlEscape(cat(c, i)), FormatCompact(v))
			if n*len(c.Series) <= 24 {
				r.text(xb+5, by+barH/2+4, FormatCompact(v), "start", t.Text, 11, "")
			}
		}
	}
}

func cat(c *ChartSpec, i int) string {
	if i < len(c.Categories) {
		return c.Categories[i]
	}
	return ""
}

func (r *svgCanvas) scatter(c *ChartSpec) {
	t := r.theme
	top := 16.0 + r.legend(seriesNames(c), 22)
	x0, x1, y0, y1 := 64.0, r.W-24, top+8, r.H-44
	xs := make([]float64, len(c.Categories))
	numericX := true
	for i, cs := range c.Categories {
		v, ok := ParseNumber(cs)
		if !ok {
			numericX = false
			break
		}
		xs[i] = v
	}
	if !numericX {
		for i := range xs {
			xs[i] = float64(i + 1)
		}
	}
	minX, maxX := xs[0], xs[0]
	for _, v := range xs {
		minX, maxX = math.Min(minX, v), math.Max(maxX, v)
	}
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, s := range c.Series {
		for _, v := range s.Data {
			minY, maxY = math.Min(minY, v), math.Max(maxY, v)
		}
	}
	lx, hx, sx := NiceScale(math.Min(minX, 0), maxX, 6)
	ly, hy, sy := NiceScale(math.Min(minY, 0), maxY, 6)
	xOf := func(v float64) float64 { return x0 + (v-lx)/(hx-lx)*(x1-x0) }
	yOf := func(v float64) float64 { return y1 - (v-ly)/(hy-ly)*(y1-y0) }
	for v := ly; v <= hy+sy/2; v += sy {
		r.line(x0, yOf(v), x1, yOf(v), t.Grid, 1, true)
		r.text(x0-8, yOf(v)+4, FormatCompact(v), "end", t.Muted, 11, "")
	}
	for v := lx; v <= hx+sx/2; v += sx {
		r.text(xOf(v), y1+18, FormatCompact(v), "middle", t.Muted, 11, "")
	}
	r.line(x0, y1, x1, y1, t.Muted, 1, false)
	if c.XLabel != "" {
		r.text((x0+x1)/2, r.H-6, c.XLabel, "middle", t.Muted, 11, "")
	}
	for j, s := range c.Series {
		for i, v := range s.Data {
			fmt.Fprintf(&r.sb, `<circle cx="%.1f" cy="%.1f" r="5" fill="#%s" fill-opacity="0.75"><title>%s: %s</title></circle>`,
				xOf(xs[i]), yOf(v), t.SeriesColor(j), xmlEscape(cat(c, i)), FormatCompact(v))
		}
	}
}

func (r *svgCanvas) radar(c *ChartSpec) {
	t := r.theme
	top := 8.0 + r.legend(seriesNames(c), 22)
	cx, cy := r.W/2, top+(r.H-top)/2+4
	radius := math.Min(r.W, r.H-top)/2 - 48
	n := len(c.Categories)
	maxV := 0.0
	for _, s := range c.Series {
		for _, v := range s.Data {
			maxV = math.Max(maxV, v)
		}
	}
	_, hi, _ := NiceScale(0, maxV, 5)
	angle := func(i int) float64 { return -math.Pi/2 + 2*math.Pi*float64(i)/float64(n) }
	for ring := 1; ring <= 4; ring++ {
		rr := radius * float64(ring) / 4
		var pts []string
		for i := 0; i < n; i++ {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", cx+rr*math.Cos(angle(i)), cy+rr*math.Sin(angle(i))))
		}
		fmt.Fprintf(&r.sb, `<polygon points="%s" fill="none" stroke="#%s"/>`, strings.Join(pts, " "), t.Grid)
	}
	for i, catName := range c.Categories {
		ex, ey := cx+radius*math.Cos(angle(i)), cy+radius*math.Sin(angle(i))
		r.line(cx, cy, ex, ey, t.Grid, 1, false)
		anchor := "middle"
		if math.Cos(angle(i)) > 0.2 {
			anchor = "start"
		} else if math.Cos(angle(i)) < -0.2 {
			anchor = "end"
		}
		r.text(cx+(radius+14)*math.Cos(angle(i)), cy+(radius+14)*math.Sin(angle(i))+4, TruncateRunes(catName, 10), anchor, t.Muted, 11, "")
	}
	for j, s := range c.Series {
		var pts []string
		for i, v := range s.Data {
			rr := 0.0
			if hi > 0 {
				rr = radius * math.Max(v, 0) / hi
			}
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", cx+rr*math.Cos(angle(i)), cy+rr*math.Sin(angle(i))))
		}
		color := t.SeriesColor(j)
		fmt.Fprintf(&r.sb, `<polygon points="%s" fill="#%s" fill-opacity="0.18" stroke="#%s" stroke-width="2"/>`, strings.Join(pts, " "), color, color)
	}
}

func (r *svgCanvas) pie(c *ChartSpec) {
	t := r.theme
	data := c.Series[0].Data
	total := 0.0
	for _, v := range data {
		if v > 0 {
			total += v
		}
	}
	if total <= 0 {
		r.text(r.W/2, r.H/2, "无有效数据", "middle", t.Muted, 14, "")
		return
	}
	cx, cy := r.W*0.36, r.H/2
	radius := math.Min(r.W*0.3, r.H/2-24)
	inner := 0.0
	if c.Type == "donut" {
		inner = radius * 0.58
	}
	a0 := -math.Pi / 2
	for i, v := range data {
		if v <= 0 {
			continue
		}
		frac := v / total
		a1 := a0 + frac*2*math.Pi
		color := t.SeriesColor(i)
		large := 0
		if frac > 0.5 {
			large = 1
		}
		if frac >= 0.9999 {
			fmt.Fprintf(&r.sb, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="#%s"/>`, cx, cy, radius, color)
		} else {
			ox0, oy0 := cx+radius*math.Cos(a0), cy+radius*math.Sin(a0)
			ox1, oy1 := cx+radius*math.Cos(a1), cy+radius*math.Sin(a1)
			if inner > 0 {
				ix0, iy0 := cx+inner*math.Cos(a0), cy+inner*math.Sin(a0)
				ix1, iy1 := cx+inner*math.Cos(a1), cy+inner*math.Sin(a1)
				fmt.Fprintf(&r.sb, `<path d="M%.1f,%.1f A%.1f,%.1f 0 %d 1 %.1f,%.1f L%.1f,%.1f A%.1f,%.1f 0 %d 0 %.1f,%.1f Z" fill="#%s" stroke="#%s" stroke-width="1.5"><title>%s: %s (%.1f%%)</title></path>`,
					ox0, oy0, radius, radius, large, ox1, oy1, ix1, iy1, inner, inner, large, ix0, iy0, color, t.Background, xmlEscape(cat(c, i)), FormatCompact(v), frac*100)
			} else {
				fmt.Fprintf(&r.sb, `<path d="M%.1f,%.1f L%.1f,%.1f A%.1f,%.1f 0 %d 1 %.1f,%.1f Z" fill="#%s" stroke="#%s" stroke-width="1.5"><title>%s: %s (%.1f%%)</title></path>`,
					cx, cy, ox0, oy0, radius, radius, large, ox1, oy1, color, t.Background, xmlEscape(cat(c, i)), FormatCompact(v), frac*100)
			}
		}
		if frac >= 0.05 {
			mid := (a0 + a1) / 2
			lr := radius * 0.72
			if inner > 0 {
				lr = (radius + inner) / 2
			}
			r.text(cx+lr*math.Cos(mid), cy+lr*math.Sin(mid)+4, fmt.Sprintf("%.1f%%", frac*100), "middle", "FFFFFF", 12, ` font-weight="600"`)
		}
		a0 = a1
	}
	if inner > 0 {
		r.text(cx, cy-2, FormatCompact(total), "middle", t.Text, 22, ` font-weight="700"`)
		r.text(cx, cy+18, "合计", "middle", t.Muted, 12, "")
	}
	// 右侧图例（名称 + 数值 + 占比）
	lx := r.W * 0.66
	ly := cy - float64(len(data))*12
	if ly < 24 {
		ly = 24
	}
	for i, v := range data {
		if i >= 14 {
			r.text(lx, ly+float64(i)*24+4, fmt.Sprintf("… 共 %d 项", len(data)), "start", t.Muted, 12, "")
			break
		}
		y := ly + float64(i)*24
		fmt.Fprintf(&r.sb, `<rect x="%.1f" y="%.1f" width="12" height="12" rx="2" fill="#%s"/>`, lx, y-6, t.SeriesColor(i))
		pct := 0.0
		if v > 0 {
			pct = v / total * 100
		}
		r.text(lx+18, y+5, fmt.Sprintf("%s  %s（%.1f%%）", TruncateRunes(cat(c, i), 10), FormatCompact(v), pct), "start", t.Text, 12, "")
	}
}

// xmlEscape XML/HTML 文本转义
func xmlEscape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#39;")
		default:
			// 过滤 XML 1.0 非法控制字符
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			if r == 0xFFFE || r == 0xFFFF {
				continue
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
