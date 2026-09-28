package export

import "fmt"

// NormalizeCharts 归一化 Excel 图表入参：优先使用 charts 列表；
// 仅提供顶层单图字段（chartType/xAxisField/yAxisField/chartTitle，历史兼容）时组装为单元素列表。
func NormalizeCharts(input *ExportExcelWithChartInput) []ExcelChart {
	if len(input.Charts) > 0 {
		return input.Charts
	}
	chartType := input.ChartType
	if chartType == "" {
		chartType = "bar"
	}
	chartTitle := input.ChartTitle
	if chartTitle == "" {
		chartTitle = fmt.Sprintf("%s vs %s", input.XAxisField, input.YAxisField)
	}
	return []ExcelChart{{
		ChartType:  chartType,
		XAxisField: input.XAxisField,
		ChartTitle: chartTitle,
		Series: []ExcelChartSeriesDef{{
			YAxisField: input.YAxisField,
			Name:       input.YAxisField,
		}},
	}}
}
