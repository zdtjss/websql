package agentv2

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// renderChartPNG 使用 go-chart 渲染 PNG 图表
func renderChartPNG(qr *queryResult, xIdx, yIdx int, title, chartType, filePath string) error {
	if len(qr.Data) == 0 {
		return fmt.Errorf("无数据可绘制")
	}

	// 提取 X/Y 值
	xLabels := make([]string, 0, len(qr.Data))
	yValues := make([]float64, 0, len(qr.Data))

	for _, row := range qr.Data {
		xVal := fmt.Sprintf("%v", row[qr.Columns[xIdx]])
		xLabels = append(xLabels, xVal)

		yRaw := row[qr.Columns[yIdx]]
		yFloat, err := toFloat64(yRaw)
		if err != nil {
			yFloat = 0
		}
		yValues = append(yValues, yFloat)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()

	switch strings.ToLower(chartType) {
	case "pie":
		return renderPieChart(f, xLabels, yValues, title)
	case "bar":
		return renderBarChart(f, xLabels, yValues, title)
	default: // line
		return renderLineChart(f, xLabels, yValues, title)
	}
}

// renderLineChart 折线图
func renderLineChart(f *os.File, xLabels []string, yValues []float64, title string) error {
	xFloats := make([]float64, len(yValues))
	for i := range xFloats {
		xFloats[i] = float64(i)
	}

	// 生成刻度
	ticks := makeTicks(xLabels)

	graph := chart.Chart{
		Title:  title,
		Width:  960,
		Height: 540,
		TitleStyle: chart.Style{
			FontSize: 16,
		},
		XAxis: chart.XAxis{
			Ticks: ticks,
			Style: chart.Style{
				FontSize: 9,
			},
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				FontSize: 10,
			},
		},
		Series: []chart.Series{
			chart.ContinuousSeries{
				XValues: xFloats,
				YValues: yValues,
				Style: chart.Style{
					StrokeColor: drawing.ColorFromHex("1976d2"),
					StrokeWidth: 2.5,
				},
			},
		},
	}

	return graph.Render(chart.PNG, f)
}

// renderBarChart 柱状图
func renderBarChart(f *os.File, xLabels []string, yValues []float64, title string) error {
	bars := make([]chart.Value, 0, len(yValues))
	colors := []drawing.Color{
		drawing.ColorFromHex("1976d2"),
		drawing.ColorFromHex("388e3c"),
		drawing.ColorFromHex("f57c00"),
		drawing.ColorFromHex("d32f2f"),
		drawing.ColorFromHex("7b1fa2"),
		drawing.ColorFromHex("0097a7"),
	}

	for i, v := range yValues {
		label := xLabels[i]
		if len([]rune(label)) > 12 {
			label = string([]rune(label)[:12]) + "…"
		}
		bars = append(bars, chart.Value{
			Label: label,
			Value: v,
			Style: chart.Style{
				FillColor: colors[i%len(colors)],
			},
		})
	}

	graph := chart.BarChart{
		Title:  title,
		Width:  960,
		Height: 540,
		TitleStyle: chart.Style{
			FontSize: 16,
		},
		BarWidth: 40,
		Bars:     bars,
	}

	return graph.Render(chart.PNG, f)
}

// renderPieChart 饼图
func renderPieChart(f *os.File, xLabels []string, yValues []float64, title string) error {
	values := make([]chart.Value, 0, len(yValues))
	colors := []drawing.Color{
		drawing.ColorFromHex("1976d2"),
		drawing.ColorFromHex("388e3c"),
		drawing.ColorFromHex("f57c00"),
		drawing.ColorFromHex("d32f2f"),
		drawing.ColorFromHex("7b1fa2"),
		drawing.ColorFromHex("0097a7"),
		drawing.ColorFromHex("c2185b"),
		drawing.ColorFromHex("455a64"),
	}

	for i, v := range yValues {
		label := xLabels[i]
		if len([]rune(label)) > 15 {
			label = string([]rune(label)[:15]) + "…"
		}
		values = append(values, chart.Value{
			Label: fmt.Sprintf("%s (%.0f)", label, v),
			Value: v,
			Style: chart.Style{
				FillColor: colors[i%len(colors)],
				FontSize:  10,
			},
		})
	}

	graph := chart.PieChart{
		Title:  title,
		Width:  720,
		Height: 540,
		TitleStyle: chart.Style{
			FontSize: 16,
		},
		Values: values,
	}

	return graph.Render(chart.PNG, f)
}

// makeTicks 生成 X 轴刻度（最多显示 20 个标签避免重叠）
func makeTicks(labels []string) []chart.Tick {
	n := len(labels)
	step := 1
	if n > 20 {
		step = n / 20
	}

	ticks := make([]chart.Tick, 0)
	for i := 0; i < n; i += step {
		label := labels[i]
		if len([]rune(label)) > 10 {
			label = string([]rune(label)[:10]) + "…"
		}
		ticks = append(ticks, chart.Tick{Value: float64(i), Label: label})
	}
	return ticks
}

// toFloat64 尝试将 any 转为 float64
func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	case []byte:
		return strconv.ParseFloat(string(val), 64)
	default:
		s := fmt.Sprintf("%v", v)
		return strconv.ParseFloat(s, 64)
	}
}
