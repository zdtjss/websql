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

// NewExportAnalysisDocxFunc 创建 Word 导出工具（双路径，保证导出不失败）。
//   - 路径 1：Python 模板驱动渲染（专业版：封面/目录/样式集/自动图表）
//   - 路径 2：Python 不可用或渲染失败时，自动降级 Go 基础版渲染器
//     （手拼 OOXML 基础实现，无 Python 依赖，保证任何环境均可导出）
//
// 若需更细粒度的自定义（自定义 sections/blocks 结构），Agent 可改用
// skill 工具加载 export-word 技能自行编排。
func NewExportAnalysisDocxFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据分析报告"
		}

		fileName := SanitizeFileName(input.FileName, "report")
		EnsureExportsDir()
		url := "/exports/" + fileName + ".docx"

		// 路径 1：Python 模板驱动渲染（专业版）
		if input.Content != "" {
			req := buildDocxContentJSON(title, input.Content, url)
			if result, err := runPythonRenderer(ctx,
				filepath.Join("export-word", "scripts", "word_generator.py"), req); err == nil {
				log.Printf("[Tool:export_docx] 成功（模板版）- url=%s\n", result.OutputPath)
				return &ExportAnalysisDocxOutput{
					Message:     fmt.Sprintf("已生成 Word 分析报告，[点击下载](%s)", result.OutputPath),
					DownloadURL: result.OutputPath,
					FileType:    "docx",
				}, nil
			} else {
				log.Printf("[Tool:export_docx] Python 渲染不可用，降级 Go 基础版 - err=%v\n", err)
			}
			// 路径 2：Go 基础版兜底（无 Python 依赖，保证导出不失败）
			docxPath := filepath.Join("exports", fileName+".docx")
			if err := GenerateDocxFromContent(input.Content, title, docxPath); err != nil {
				return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
			}
			return &ExportAnalysisDocxOutput{
				Message:     fmt.Sprintf("已生成 Word 分析报告（基础版），[点击下载](%s)", url),
				DownloadURL: url,
				FileType:    "docx",
			}, nil
		}

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		req := buildDocxDataJSON(qr, title, url)
		if result, err := runPythonRenderer(ctx,
			filepath.Join("export-word", "scripts", "word_generator.py"), req); err == nil {
			log.Printf("[Tool:export_docx] 成功（模板版）- url=%s\n", result.OutputPath)
			return &ExportAnalysisDocxOutput{
				Message:     fmt.Sprintf("已生成 Word 分析报告，[点击下载](%s)", result.OutputPath),
				DownloadURL: result.OutputPath,
				FileType:    "docx",
			}, nil
		} else {
			log.Printf("[Tool:export_docx] Python 渲染不可用，降级 Go 基础版 - err=%v\n", err)
		}

		var chartImagePaths []string
		if input.IncludeChart && len(qr.Columns) >= 2 && len(qr.Data) > 0 {
			chartImagePaths = GenerateDocxCharts(qr, title, fileName)
		}
		docxPath := filepath.Join("exports", fileName+".docx")
		if err := GenerateDocx(qr, title, chartImagePaths, docxPath); err != nil {
			cleanupFiles(chartImagePaths)
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}
		cleanupFiles(chartImagePaths)

		return &ExportAnalysisDocxOutput{
			Message:     fmt.Sprintf("已生成 Word 分析报告（%d 条数据，基础版），[点击下载](%s)", len(qr.Data), url),
			DownloadURL: url,
			FileType:    "docx",
		}, nil
	}
}

// NewExportPPTFunc 创建 PPT 导出工具（双路径，保证导出不失败）。
//   - 路径 1：Python 模板驱动渲染（专业版：母版/版式/深色主题）
//   - 路径 2：Python 不可用或渲染失败时，自动降级 Go 基础版渲染器
//     （手拼 OOXML 基础实现，无 Python 依赖，保证任何环境均可导出）
//
// 若需更细粒度的自定义（自定义 sections/blocks 结构），Agent 可改用
// skill 工具加载 export-ppt 技能自行编排。
func NewExportPPTFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据报告"
		}

		fileName := SanitizeFileName(input.FileName, "slides")
		EnsureExportsDir()
		url := "/exports/" + fileName + ".pptx"

		// 路径 1：Python 模板驱动渲染（专业版）
		if input.Content != "" {
			req := buildPPTContentJSON(title, input.Content, url)
			if result, err := runPythonRenderer(ctx,
				filepath.Join("export-ppt", "scripts", "export_ppt.py"), req); err == nil {
				log.Printf("[Tool:export_ppt] 成功（模板版）- slides=%d, url=%s\n", result.SlideCount, result.OutputPath)
				return &ExportPPTOutput{
					Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", result.SlideCount, result.OutputPath),
					SlideCount:  result.SlideCount,
					DownloadURL: result.OutputPath,
					FileType:    "ppt",
				}, nil
			} else {
				log.Printf("[Tool:export_ppt] Python 渲染不可用，降级 Go 基础版 - err=%v\n", err)
			}
			// 路径 2：Go 基础版兜底（无 Python 依赖，保证导出不失败）
			pptxPath := filepath.Join("exports", fileName+".pptx")
			slideCount, err := GeneratePptxFromContent(input.Content, title, pptxPath)
			if err != nil {
				return nil, fmt.Errorf("生成 PPT 失败：%w", err)
			}
			return &ExportPPTOutput{
				Message:     fmt.Sprintf("已生成 PPT（%d 页，基础版），[点击下载](%s)", slideCount, url),
				SlideCount:  slideCount,
				DownloadURL: url,
				FileType:    "ppt",
			}, nil
		}

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		req := buildPPTDataJSON(qr, title, url)
		if result, err := runPythonRenderer(ctx,
			filepath.Join("export-ppt", "scripts", "export_ppt.py"), req); err == nil {
			log.Printf("[Tool:export_ppt] 成功（模板版）- slides=%d, url=%s\n", result.SlideCount, result.OutputPath)
			return &ExportPPTOutput{
				Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", result.SlideCount, result.OutputPath),
				SlideCount:  result.SlideCount,
				DownloadURL: result.OutputPath,
				FileType:    "ppt",
			}, nil
		} else {
			log.Printf("[Tool:export_ppt] Python 渲染不可用，降级 Go 基础版 - err=%v\n", err)
		}

		chartPaths := GeneratePptCharts(qr, title, fileName)
		pptxPath := filepath.Join("exports", fileName+".pptx")
		slideCount, err := GeneratePptx(qr, title, chartPaths, pptxPath)
		cleanupFiles(chartPaths)
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}

		return &ExportPPTOutput{
			Message:     fmt.Sprintf("已生成 PPT（%d 页，基础版），[点击下载](%s)", slideCount, url),
			SlideCount:  slideCount,
			DownloadURL: url,
			FileType:    "ppt",
		}, nil
	}
}
