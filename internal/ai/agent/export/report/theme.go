package report

import (
	"math"
	"strconv"
	"strings"
)

// Theme 渲染主题（颜色均为不带 # 的 6 位十六进制）
type Theme struct {
	Name       string
	Dark       bool
	Background string
	Surface    string // 卡片/提示框底色
	Text       string
	Muted      string
	Primary    string
	Accent     string
	Border     string
	HeaderBg   string // 表头底色
	HeaderText string
	Zebra      string
	Grid       string // 图表网格线
	Up         string
	Down       string
	Palette    []string
	FontFamily string // 中文字体（Office 渲染用）
	MonoFont   string
}

var businessTheme = Theme{
	Name: "business", Background: "FFFFFF", Surface: "F4F7FB", Text: "1F2937", Muted: "6B7280",
	Primary: "1F4E79", Accent: "2E75B6", Border: "D6DEE8", HeaderBg: "1F4E79", HeaderText: "FFFFFF",
	Zebra: "F6F9FC", Grid: "E5E7EB", Up: "16A34A", Down: "DC2626",
	Palette:    []string{"2E75B6", "F59E0B", "10B981", "EF4444", "8B5CF6", "06B6D4", "EC4899", "84CC16", "64748B", "F97316", "0EA5E9", "A855F7"},
	FontFamily: "Microsoft YaHei", MonoFont: "Consolas",
}

var techTheme = Theme{
	Name: "tech", Dark: true, Background: "0A1628", Surface: "12233D", Text: "F1F5F9", Muted: "B0C4D8",
	Primary: "00A8FF", Accent: "00F5D4", Border: "1E3A5F", HeaderBg: "1A3A6B", HeaderText: "FFFFFF",
	Zebra: "0F1E36", Grid: "1E3A5F", Up: "00E696", Down: "FF5A67",
	Palette:    []string{"00A8FF", "00F5D4", "7B61FF", "FF6B35", "FFD700", "E63946", "00E696", "F472B6", "38BDF8", "A3E635", "FB923C", "C084FC"},
	FontFamily: "Microsoft YaHei", MonoFont: "Consolas",
}

// ResolveTheme 按名称返回主题；fallback 为默认主题名
func ResolveTheme(name, fallback string) Theme {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		n = fallback
	}
	switch n {
	case "tech", "dark", "深色", "科技":
		return techTheme
	}
	return businessTheme
}

// SeriesColor 第 i 个系列颜色
func (t Theme) SeriesColor(i int) string {
	return t.Palette[i%len(t.Palette)]
}

// CalloutColor 提示框主色
func CalloutColor(style string) string {
	switch style {
	case "tip":
		return "10B981"
	case "important":
		return "8B5CF6"
	case "warning":
		return "F59E0B"
	case "caution":
		return "EF4444"
	}
	return "3B82F6"
}

// Tint 把颜色与白色按 ratio（0-1，越大越接近白色）混合，用于浅色底纹
func Tint(hex string, ratio float64) string {
	r, g, b := parseHex(hex)
	mix := func(c int) int { return int(math.Round(float64(c) + (255-float64(c))*ratio)) }
	return toHex(mix(r), mix(g), mix(b))
}

// Shade 把颜色与黑色按 ratio 混合（深色主题提示框底纹）
func Shade(hex string, ratio float64) string {
	r, g, b := parseHex(hex)
	mix := func(c int) int { return int(math.Round(float64(c) * (1 - ratio))) }
	return toHex(mix(r), mix(g), mix(b))
}

func parseHex(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(v >> 16 & 0xFF), int(v >> 8 & 0xFF), int(v & 0xFF)
}

func toHex(r, g, b int) string {
	clamp := func(c int) int {
		if c < 0 {
			return 0
		}
		if c > 255 {
			return 255
		}
		return c
	}
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 6)
	for _, c := range []int{clamp(r), clamp(g), clamp(b)} {
		out = append(out, digits[c>>4], digits[c&0xF])
	}
	return string(out)
}

// ─── 数值格式化 ─────────────────────────────────────────────

// FormatCompact 紧凑数值（中文单位）：12345 → 1.23万，1.5e8 → 1.5亿
func FormatCompact(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e8:
		return trimZeros(strconv.FormatFloat(v/1e8, 'f', 2, 64)) + "亿"
	case a >= 1e4:
		return trimZeros(strconv.FormatFloat(v/1e4, 'f', 2, 64)) + "万"
	case a >= 100 || v == math.Trunc(v):
		return FormatThousands(v, 0)
	case a >= 1:
		return trimZeros(strconv.FormatFloat(v, 'f', 2, 64))
	}
	return trimZeros(strconv.FormatFloat(v, 'f', 4, 64))
}

// FormatThousands 千分位格式
func FormatThousands(v float64, decimals int) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var sb strings.Builder
	if v < 0 {
		sb.WriteByte('-')
	}
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	sb.WriteString(frac)
	return sb.String()
}

func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// NiceScale 计算坐标轴的「好看」刻度：返回 (min, max, step)
func NiceScale(minV, maxV float64, ticks int) (float64, float64, float64) {
	if ticks < 2 {
		ticks = 5
	}
	if minV > 0 {
		minV = 0
	}
	if maxV < 0 {
		maxV = 0
	}
	if maxV == minV {
		maxV = minV + 1
	}
	rng := niceNum(maxV-minV, false)
	step := niceNum(rng/float64(ticks-1), true)
	lo := math.Floor(minV/step) * step
	hi := math.Ceil(maxV/step) * step
	return lo, hi, step
}

func niceNum(x float64, round bool) float64 {
	if x <= 0 {
		return 1
	}
	exp := math.Floor(math.Log10(x))
	f := x / math.Pow(10, exp)
	var nf float64
	if round {
		switch {
		case f < 1.5:
			nf = 1
		case f < 3:
			nf = 2
		case f < 7:
			nf = 5
		default:
			nf = 10
		}
	} else {
		switch {
		case f <= 1:
			nf = 1
		case f <= 2:
			nf = 2
		case f <= 5:
			nf = 5
		default:
			nf = 10
		}
	}
	return nf * math.Pow(10, exp)
}

// IsNumericColumn 判断一列是否为数值列（非空值中 ≥80% 可解析为数字）
func IsNumericColumn(values []string) bool {
	total, num := 0, 0
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		total++
		if _, ok := ParseNumber(v); ok {
			num++
		}
	}
	return total > 0 && num*5 >= total*4
}

// TruncateRunes 按字符截断并追加省略号
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// DisplayWidth 估算显示宽度（中日韩全角字符按 2 计）
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2E80 {
			w += 2
		} else {
			w++
		}
	}
	return w
}
