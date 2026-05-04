package agentv2

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

var chartBrandColors = []drawing.Color{
	drawing.ColorFromHex("1A237E"),
	drawing.ColorFromHex("00BCD4"),
	drawing.ColorFromHex("4CAF50"),
	drawing.ColorFromHex("FF9800"),
	drawing.ColorFromHex("E91E63"),
	drawing.ColorFromHex("5B9BD5"),
	drawing.ColorFromHex("9C27B0"),
	drawing.ColorFromHex("00897B"),
}

var chartLightGrid = chart.Style{
	StrokeColor: drawing.ColorFromHex("E8ECF1"),
	StrokeWidth: 0.5,
}

type chartSeries struct {
	Name    string
	XValues []float64
	YValues []float64
	XLabels []string
}

func renderMultiChartPNG(seriesList []chartSeries, title, chartType, filePath string) error {
	switch strings.ToLower(chartType) {
	case "pie", "doughnut", "donut":
		if len(seriesList) > 0 {
			return renderPieChartFile(filePath, seriesList[0].XLabels, seriesList[0].YValues, title)
		}
		return fmt.Errorf("pie chart requires at least one series")
	case "bar", "column":
		return renderBarChartFile(filePath, seriesList, title)
	default:
		return renderLineChartFile(filePath, seriesList, title)
	}
}

func renderLineChartFile(filePath string, seriesList []chartSeries, title string) error {
	if len(seriesList) == 0 {
		return fmt.Errorf("no series data")
	}

	var allSeries []chart.Series
	graph := chart.Chart{
		Title:      title,
		TitleStyle: chartTitleStyle(),
		Width:      960,
		Height:     540,
		Background: chartBackStyle(),
		Canvas: chart.Style{
			FillColor: drawing.ColorFromHex("FFFFFF"),
		},
		XAxis: chart.XAxis{
			Ticks: buildXTicks(seriesList[0].XLabels, seriesList[0].XValues),
			Style: chartAxisStyle(),
		},
		YAxis: chart.YAxis{
			Style:          chartAxisStyle(),
			GridMajorStyle: chartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: numberFormatter,
		},
	}

	for i, s := range seriesList {
		name := s.Name
		if name == "" {
			if len(seriesList) > 1 {
				name = fmt.Sprintf("系列 %d", i+1)
			}
		}
		color := chartBrandColors[i%len(chartBrandColors)]
		allSeries = append(allSeries, chart.ContinuousSeries{
			Name:    name,
			XValues: s.XValues,
			YValues: s.YValues,
			Style: chart.Style{
				StrokeColor: color,
				StrokeWidth: 2.5,
				DotColor:    color,
				DotWidth:    3,
			},
		})
	}

	graph.Series = allSeries

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

func renderBarChartFile(filePath string, seriesList []chartSeries, title string) error {
	if len(seriesList) == 0 {
		return fmt.Errorf("no series data")
	}

	if len(seriesList) > 1 {
		return renderMultiBarChart(filePath, seriesList, title)
	}

	s := seriesList[0]
	bars := make([]chart.Value, 0, len(s.YValues))
	for i, v := range s.YValues {
		label := s.XLabels[i]
		if len([]rune(label)) > 12 {
			label = string([]rune(label)[:12]) + "\u2026"
		}
		bars = append(bars, chart.Value{
			Label: label,
			Value: v,
			Style: chart.Style{
				FillColor: chartBrandColors[i%len(chartBrandColors)],
				FontSize:  9,
			},
		})
	}

	graph := chart.BarChart{
		Title:      title,
		TitleStyle: chartTitleStyle(),
		Width:      960,
		Height:     540,
		Background: chartBackStyle(),
		Canvas: chart.Style{
			FillColor: drawing.ColorFromHex("FFFFFF"),
		},
		BarWidth: 40,
		Bars:     bars,
		YAxis: chart.YAxis{
			Style:          chartAxisStyle(),
			GridMajorStyle: chartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: numberFormatter,
		},
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

func renderMultiBarChart(filePath string, seriesList []chartSeries, title string) error {
	var allSeries []chart.Series
	graph := chart.Chart{
		Title:      title,
		TitleStyle: chartTitleStyle(),
		Width:      960,
		Height:     540,
		Background: chartBackStyle(),
		Canvas: chart.Style{
			FillColor: drawing.ColorFromHex("FFFFFF"),
		},
		XAxis: chart.XAxis{
			Ticks: buildXTicks(seriesList[0].XLabels, seriesList[0].XValues),
			Style: chartAxisStyle(),
		},
		YAxis: chart.YAxis{
			Style:          chartAxisStyle(),
			GridMajorStyle: chartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: numberFormatter,
		},
	}

	for i, s := range seriesList {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("系列 %d", i+1)
		}
		color := chartBrandColors[i%len(chartBrandColors)]
		allSeries = append(allSeries, chart.ContinuousSeries{
			Name:    name,
			XValues: s.XValues,
			YValues: s.YValues,
			Style: chart.Style{
				StrokeColor: color,
				StrokeWidth: 35,
				DotColor:    color,
				FillColor:   color,
			},
		})
	}

	graph.Series = allSeries

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

func renderPieChartFile(filePath string, xLabels []string, yValues []float64, title string) error {
	values := make([]chart.Value, 0, len(yValues))
	for i, v := range yValues {
		label := xLabels[i]
		if len([]rune(label)) > 15 {
			label = string([]rune(label)[:15]) + "\u2026"
		}
		values = append(values, chart.Value{
			Label: fmt.Sprintf("%s (%.1f)", label, v),
			Value: v,
			Style: chart.Style{
				FillColor: chartBrandColors[i%len(chartBrandColors)],
				FontSize:  10,
				FontColor: drawing.ColorFromHex("424242"),
			},
		})
	}

	graph := chart.PieChart{
		Title:      title,
		TitleStyle: chartTitleStyle(),
		Width:      720,
		Height:     540,
		Background: chartBackStyle(),
		Canvas: chart.Style{
			FillColor: drawing.ColorFromHex("FFFFFF"),
		},
		Values: values,
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

func buildXTicks(labels []string, xFloats []float64) []chart.Tick {
	n := len(labels)
	step := 1
	if n > 20 {
		step = n / 20
	}

	ticks := make([]chart.Tick, 0)
	for i := 0; i < n && len(ticks) < 20; i += step {
		label := labels[i]
		if len([]rune(label)) > 10 {
			label = string([]rune(label)[:10]) + "\u2026"
		}
		ticks = append(ticks, chart.Tick{Value: xFloats[i], Label: label})
	}
	return ticks
}

func chartTitleStyle() chart.Style {
	return chart.Style{
		FontColor: drawing.ColorFromHex("1A237E"),
		FontSize:  16,
	}
}

func chartBackStyle() chart.Style {
	return chart.Style{
		FillColor: drawing.ColorFromHex("FAFAFA"),
		Padding: chart.Box{
			Top:    40,
			Left:   20,
			Right:  20,
			Bottom: 20,
		},
	}
}

func chartAxisStyle() chart.Style {
	return chart.Style{
		StrokeColor: drawing.ColorFromHex("757575"),
		FontColor:   drawing.ColorFromHex("757575"),
		FontSize:    9,
	}
}

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

func numberFormatter(v any) string {
	if f, ok := v.(float64); ok {
		if f >= 1e7 {
			return fmt.Sprintf("%.1fM", f/1e6)
		}
		if f >= 1e4 {
			return fmt.Sprintf("%.1fK", f/1e3)
		}
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%v", v)
}
