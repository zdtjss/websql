package export

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

var ChartBrandColors = []drawing.Color{
	drawing.ColorFromHex("1A237E"),
	drawing.ColorFromHex("00BCD4"),
	drawing.ColorFromHex("4CAF50"),
	drawing.ColorFromHex("FF9800"),
	drawing.ColorFromHex("E91E63"),
	drawing.ColorFromHex("5B9BD5"),
	drawing.ColorFromHex("9C27B0"),
	drawing.ColorFromHex("00897B"),
	drawing.ColorFromHex("F44336"),
	drawing.ColorFromHex("795548"),
}

var ChartLightGrid = chart.Style{
	StrokeColor: drawing.ColorFromHex("E8ECF1"),
	StrokeWidth: 0.5,
}

type ChartSeries struct {
	Name    string
	XValues []float64
	YValues []float64
	XLabels []string
}

func RenderMultiChartPNG(seriesList []ChartSeries, title, chartType, filePath string) error {
	switch strings.ToLower(chartType) {
	case "pie", "doughnut", "donut":
		if len(seriesList) > 0 {
			return renderPieChartFile(filePath, seriesList[0].XLabels, seriesList[0].YValues, title)
		}
		return errors.New("pie chart requires at least one series")
	case "bar", "column":
		return renderBarChartFile(filePath, seriesList, title)
	default:
		return renderLineChartFile(filePath, seriesList, title)
	}
}

func renderLineChartFile(filePath string, seriesList []ChartSeries, title string) error {
	if len(seriesList) == 0 {
		return errors.New("no series data")
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
			StrokeColor: drawing.ColorFromHex("E8ECF1"),
			StrokeWidth: 1.0,
		},
		XAxis: chart.XAxis{
			Ticks: buildXTicks(seriesList[0].XLabels, seriesList[0].XValues),
			Style: chartAxisStyle(),
		},
		YAxis: chart.YAxis{
			Style:          chartAxisStyle(),
			GridMajorStyle: ChartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: NumberFormatter,
		},
	}

	for i, s := range seriesList {
		name := s.Name
		if name == "" {
			if len(seriesList) > 1 {
				name = fmt.Sprintf("系列 %d", i+1)
			}
		}
		color := ChartBrandColors[i%len(ChartBrandColors)]
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

func renderBarChartFile(filePath string, seriesList []ChartSeries, title string) error {
	if len(seriesList) == 0 {
		return errors.New("no series data")
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
				FillColor: ChartBrandColors[i%len(ChartBrandColors)],
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
			GridMajorStyle: ChartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: NumberFormatter,
		},
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

func renderMultiBarChart(filePath string, seriesList []ChartSeries, title string) error {
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
			GridMajorStyle: ChartLightGrid,
			GridMinorStyle: chart.Style{Hidden: true},
			ValueFormatter: NumberFormatter,
		},
	}

	for i, s := range seriesList {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("系列 %d", i+1)
		}
		color := ChartBrandColors[i%len(ChartBrandColors)]
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
				FillColor: ChartBrandColors[i%len(ChartBrandColors)],
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

func GeneratePptCharts(qr *QueryResult, title, fileName string) []string {
	numericCols := DetectNumericCols(qr)
	if len(numericCols) == 0 || len(qr.Data) == 0 || len(qr.Columns) < 2 {
		return nil
	}

	xCol := qr.Columns[0]
	var paths []string

	if len(numericCols) > 1 {
		seriesNames := numericCols
		if len(seriesNames) > 3 {
			seriesNames = seriesNames[:3]
		}

		var seriesList []ChartSeries
		for _, col := range seriesNames {
			xVals, yVals, labels := ExtractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, ChartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		if len(seriesList) > 0 {
			chartPath := fmt.Sprintf("exports/%s_ppt_chart.png", fileName)
			chartType := "line"
			if len(seriesList) == 1 {
				chartType = "bar"
			}
			if err := RenderMultiChartPNG(seriesList, title+" · 关键指标", chartType, chartPath); err != nil {
				fmt.Printf("[export_ppt] 图表生成失败: %v\n", err)
			} else {
				paths = append(paths, chartPath)
			}
		}
	}

	return paths
}

func GenerateDocxCharts(qr *QueryResult, title, fileName string) []string {
	numericCols := DetectNumericCols(qr)
	if len(numericCols) == 0 {
		return nil
	}

	if len(qr.Data) == 0 || len(qr.Columns) < 2 {
		return nil
	}

	xCol := qr.Columns[0]
	var paths []string

	if len(numericCols) > 1 {
		seriesNames := numericCols
		if len(seriesNames) > 4 {
			seriesNames = seriesNames[:4]
		}

		var seriesList []ChartSeries
		for _, col := range seriesNames {
			xVals, yVals, labels := ExtractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, ChartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		if len(seriesList) > 1 {
			linePath := fmt.Sprintf("exports/%s_chart_multi.png", fileName)
			if err := RenderMultiChartPNG(seriesList, title+" · 趋势对比", "line", linePath); err != nil {
				fmt.Printf("[export_docx] 多系列图表生成失败: %v\n", err)
			} else {
				paths = append(paths, linePath)
			}
		}
	}

	if len(numericCols) >= 1 {
		var seriesList []ChartSeries
		for _, col := range numericCols {
			if len(seriesList) >= 3 {
				break
			}
			xVals, yVals, labels := ExtractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, ChartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		barPath := fmt.Sprintf("exports/%s_chart_bar.png", fileName)
		if err := RenderMultiChartPNG(seriesList, title+" · 指标对比", "bar", barPath); err != nil {
			fmt.Printf("[export_docx] 柱状图生成失败: %v\n", err)
		} else {
			paths = append(paths, barPath)
		}
	}

	return paths
}