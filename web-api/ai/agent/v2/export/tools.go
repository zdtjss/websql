package export

import (
	"context"
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

func NewExportExcelFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
	return func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		f := excelize.NewFile()
		defer f.Close()

		WriteExcelSheet(f, "Sheet1", qr)

		fileName := SanitizeFileName(input.FileName, "export")
		EnsureExportsDir()
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := f.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportExcelOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据，[点击下载](%s)", len(qr.Data), url),
			RowCount:    len(qr.Data),
			DownloadURL: url,
			FileType:    "excel",
		}, nil
	}
}

func NewExportExcelWithChartFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
	return func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		rowCount := len(qr.Data)
		headerRow := 4
		dataStartRow := 5
		dataEndRow := rowCount + 4

		charts := NormalizeCharts(input)

		f := excelize.NewFile()
		defer f.Close()

		dataSheet := "数据概览"
		f.SetSheetName("Sheet1", dataSheet)

		reportTitle := "数据分析报告"
		if len(charts) > 0 && charts[0].ChartTitle != "" {
			reportTitle = charts[0].ChartTitle
		}
		ExcelSetTitleArea(f, dataSheet, reportTitle, len(qr.Columns), rowCount)
		ExcelWriteStyledTable(f, dataSheet, qr, 4)

		CreateAnalysisSummarySheet(f, "分析概要", qr)

		dashSheet := "仪表盘"
		_, _ = f.NewSheet(dashSheet)
		ExcelDashboardSheet(f, dashSheet, qr)

		chartCount := 0
		for _, chart := range charts {
			xIdx := FindFieldIndex(qr.Columns, chart.XAxisField)
			if xIdx == -1 {
				continue
			}
			xCol := ColLetter(xIdx)

			var chartSeries []excelize.ChartSeries
			for _, series := range chart.Series {
				yIdx := FindFieldIndex(qr.Columns, series.YAxisField)
				if yIdx == -1 {
					continue
				}
				yCol := ColLetter(yIdx)
				name := series.Name
				if name == "" {
					name = series.YAxisField
				}
				chartSeries = append(chartSeries, excelize.ChartSeries{
					Name:       fmt.Sprintf("'%s'!$%s$%d", dataSheet, yCol, headerRow),
					Categories: fmt.Sprintf("'%s'!$%s$%d:$%s$%d", dataSheet, xCol, dataStartRow, xCol, dataEndRow),
					Values:     fmt.Sprintf("'%s'!$%s$%d:$%s$%d", dataSheet, yCol, dataStartRow, yCol, dataEndRow),
				})
			}

			if len(chartSeries) == 0 {
				continue
			}

			chartCount++
			sheetName := chart.SheetName
			if sheetName == "" {
				sheetName = fmt.Sprintf("图表%d", chartCount)
			}
			_, _ = f.NewSheet(sheetName)

			chartTitle := chart.ChartTitle
			if chartTitle == "" {
				chartTitle = chart.XAxisField
			}

			excelChart := CreateExcelChart(sheetName, chart.ChartType, chartTitle, chartSeries, "B2")

			if err := f.AddChart(sheetName, "B2", excelChart); err != nil {
				log.Printf("[Tool:export_excel_chart] 添加图表 [%s] 失败 - err=%v\n", sheetName, err)
			}
		}

		f.SetActiveSheet(0)

		fileName := SanitizeFileName(input.FileName, "chart")
		EnsureExportsDir()
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := f.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel_chart] 成功 - rows=%d, charts=%d, url=%s\n", rowCount, chartCount, url)

		msg := fmt.Sprintf("已生成含 %d 个图表的 Excel（%d 条数据），[点击下载](%s)", chartCount, rowCount, url)
		if chartCount == 1 && len(charts) > 0 && len(charts[0].Series) > 1 {
			msg = fmt.Sprintf("已生成含 %d 条折线/柱状图表的 Excel（%d 条数据），[点击下载](%s)", len(charts[0].Series), rowCount, url)
		}

		return &ExportExcelWithChartOutput{
			Message:     msg,
			RowCount:    rowCount,
			DownloadURL: url,
			FileType:    "excel_with_chart",
		}, nil
	}
}

func NewExportAnalysisDocxFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return NewSkillExportAnalysisDocxFunc(conn)
}

func NewExportPPTFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return NewSkillExportPPTFunc(conn)
}
