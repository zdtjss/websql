package export

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

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

// NewExportAnalysisDocxFunc 创建 Word 导出工具（Go 原生兜底实现）
// 注意：此工具为 Go 原生实现，输出为基础版 Word 文档。
// 若需专业科技感 Word 报告（含封面/目录/KPI/图表），Agent 应优先调用
// skill 工具加载 export-word 技能，由 Python 脚本生成。
func NewExportAnalysisDocxFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据分析报告"
		}

		fileName := SanitizeFileName(input.FileName, "report")
		EnsureExportsDir()

		if input.Content != "" {
			docxPath := filepath.Join("exports", fileName+".docx")
			if err := GenerateDocxFromContent(input.Content, title, docxPath); err != nil {
				return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.docx", fileName)
			log.Printf("[Tool:export_docx] 成功 (content) - url=%s\n", url)
			return &ExportAnalysisDocxOutput{
				Message:     fmt.Sprintf("已生成 Word 分析报告，[点击下载](%s)", url),
				DownloadURL: url,
				FileType:    "docx",
			}, nil
		}

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		var chartImagePaths []string
		if input.IncludeChart && len(qr.Columns) >= 2 && len(qr.Data) > 0 {
			chartImagePaths = GenerateDocxCharts(qr, title, fileName)
		}

		docxPath := filepath.Join("exports", fileName+".docx")
		if err := GenerateDocx(qr, title, chartImagePaths, docxPath); err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}
		cleanupFiles(chartImagePaths)

		url := fmt.Sprintf("/exports/%s.docx", fileName)
		log.Printf("[Tool:export_docx] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportAnalysisDocxOutput{
			Message:     fmt.Sprintf("已生成 Word 报告（%d 条数据），[点击下载](%s)", len(qr.Data), url),
			DownloadURL: url,
			FileType:    "docx",
		}, nil
	}
}

// NewExportPPTFunc 创建 PPT 导出工具（Go 原生兜底实现）
// 注意：此工具为 Go 原生实现，输出为基础版 PPT。
// 若需专业科技感 PPT（含封面/目录/图表页/深色主题），Agent 应优先调用
// skill 工具加载 export-ppt 技能，由 Python 脚本生成。
func NewExportPPTFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据报告"
		}

		fileName := SanitizeFileName(input.FileName, "slides")
		EnsureExportsDir()

		if input.Content != "" {
			pptxPath := filepath.Join("exports", fileName+".pptx")
			slideCount, err := GeneratePptxFromContent(input.Content, title, pptxPath)
			if err != nil {
				return nil, fmt.Errorf("生成 PPT 失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.pptx", fileName)
			log.Printf("[Tool:export_ppt] 成功 (content) - slides=%d, url=%s\n", slideCount, url)
			return &ExportPPTOutput{
				Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
				SlideCount:  slideCount,
				DownloadURL: url,
				FileType:    "ppt",
			}, nil
		}

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		chartPaths := GeneratePptCharts(qr, title, fileName)

		pptxPath := filepath.Join("exports", fileName+".pptx")
		slideCount, err := GeneratePptx(qr, title, chartPaths, pptxPath)
		cleanupFiles(chartPaths)
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.pptx", fileName)
		log.Printf("[Tool:export_ppt] 成功 - slides=%d, url=%s\n", slideCount, url)

		return &ExportPPTOutput{
			Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
			SlideCount:  slideCount,
			DownloadURL: url,
			FileType:    "ppt",
		}, nil
	}
}
