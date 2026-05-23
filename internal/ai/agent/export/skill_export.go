package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

const (
	exportWordSkill = "export-word"
	exportPPTSkill  = "export-ppt"
)

func mustGetSkillEnv() (*SkillEnv, error) {
	env := GetSkillEnv()
	if env == nil {
		return nil, errors.New("Skill 环境未初始化")
	}
	return env, nil
}

func SkillExportWord(ctx context.Context, qr *QueryResult, title, fileName string, includeChart bool, chartImagePaths []string) (string, error) {
	if !IsPythonAvailable() {
		return "", errors.New("Python 不可用，请使用 Go 原生实现")
	}

	env, err := mustGetSkillEnv()
	if err != nil {
		return "", err
	}

	scriptPath, err := env.ResolveScriptPath(ctx, exportWordSkill, "word_generator.py")
	if err != nil {
		return "", err
	}

	if err := env.CheckAndInstallDeps(ctx, exportWordSkill); err != nil {
		log.Printf("[SkillExport] 依赖安装失败，回退 Go 实现: %v", err)
		return "", err
	}

	EnsureExportsDir()
	outputPath := filepath.Join("exports", fileName+".docx")

	numericCols := DetectNumericCols(qr)
	numericStats := buildNumericStats(qr, numericCols)
	findings := buildFindings(qr, numericCols)
	filteredCharts := filterExistingFiles(chartImagePaths)

	input := map[string]any{
		"title":          title,
		"columns":        qr.Columns,
		"data":           qr.Data,
		"chartPaths":     filteredCharts,
		"findings":       findings,
		"numericColumns": numericCols,
		"numericStats":   numericStats,
		"outputPath":     outputPath,
		"includeCharts":  includeChart && len(filteredCharts) > 0,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("序列化输入数据失败: %w", err)
	}

	output, err := RunPythonScript(ctx, scriptPath, string(inputJSON))
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", fmt.Errorf("解析 Python 脚本输出失败: %w", err)
	}

	if success, ok := result["success"].(bool); !ok || !success {
		errMsg := "未知错误"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return "", fmt.Errorf("Skill 执行失败: %s", errMsg)
	}

	return outputPath, nil
}

func SkillExportPPT(ctx context.Context, qr *QueryResult, title, fileName string, chartPaths []string) (string, int, error) {
	if !IsPythonAvailable() {
		return "", 0, errors.New("Python 不可用，请使用 Go 原生实现")
	}

	env, err := mustGetSkillEnv()
	if err != nil {
		return "", 0, err
	}

	scriptPath, err := env.ResolveScriptPath(ctx, exportPPTSkill, "export_ppt.py")
	if err != nil {
		return "", 0, err
	}

	if err := env.CheckAndInstallDeps(ctx, exportPPTSkill); err != nil {
		log.Printf("[SkillExport] PPT 依赖安装失败，回退 Go 实现: %v", err)
		return "", 0, err
	}

	EnsureExportsDir()
	outputPath := filepath.Join("exports", fileName+".pptx")

	numericCols := DetectNumericCols(qr)
	summary := buildSummary(qr)
	highlights := buildHighlights(qr, numericCols)
	filteredCharts := filterExistingFiles(chartPaths)

	input := map[string]any{
		"mode":           "data",
		"title":          title,
		"columns":        qr.Columns,
		"data":           qr.Data,
		"summary":        summary,
		"numericColumns": numericCols,
		"chartPaths":     filteredCharts,
		"highlights":     highlights,
		"outputPath":     outputPath,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", 0, fmt.Errorf("序列化输入数据失败: %w", err)
	}

	output, err := RunPythonScript(ctx, scriptPath, string(inputJSON))
	if err != nil {
		return "", 0, err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", 0, fmt.Errorf("解析 Python 脚本输出失败: %w", err)
	}

	if success, ok := result["success"].(bool); !ok || !success {
		errMsg := "未知错误"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return "", 0, fmt.Errorf("Skill 执行失败: %s", errMsg)
	}

	slideCount := 0
	if sc, ok := result["slideCount"].(float64); ok {
		slideCount = int(sc)
	}

	return outputPath, slideCount, nil
}

func SkillExportWordFromContent(ctx context.Context, content, title, fileName string) (string, error) {
	if !IsPythonAvailable() {
		return "", errors.New("Python 不可用，请使用 Go 原生实现")
	}

	env, err := mustGetSkillEnv()
	if err != nil {
		return "", err
	}

	scriptPath, err := env.ResolveScriptPath(ctx, exportWordSkill, "word_generator.py")
	if err != nil {
		return "", err
	}

	if err := env.CheckAndInstallDeps(ctx, exportWordSkill); err != nil {
		return "", err
	}

	EnsureExportsDir()
	outputPath := filepath.Join("exports", fileName+".docx")

	blocks := ParseMarkdownBlocks(content)
	sections := buildSections(blocks)

	input := map[string]any{
		"mode":       "content",
		"title":      title,
		"sections":   sections,
		"outputPath": outputPath,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("序列化输入数据失败: %w", err)
	}

	output, err := RunPythonScript(ctx, scriptPath, string(inputJSON))
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", fmt.Errorf("解析 Python 脚本输出失败: %w", err)
	}

	if success, ok := result["success"].(bool); !ok || !success {
		return "", errors.New("Skill 执行失败")
	}

	return outputPath, nil
}

func SkillExportPPTFromContent(ctx context.Context, content, title, fileName string) (string, int, error) {
	if !IsPythonAvailable() {
		return "", 0, errors.New("Python 不可用，请使用 Go 原生实现")
	}

	env, err := mustGetSkillEnv()
	if err != nil {
		return "", 0, err
	}

	scriptPath, err := env.ResolveScriptPath(ctx, exportPPTSkill, "export_ppt.py")
	if err != nil {
		return "", 0, err
	}

	if err := env.CheckAndInstallDeps(ctx, exportPPTSkill); err != nil {
		return "", 0, err
	}

	EnsureExportsDir()
	outputPath := filepath.Join("exports", fileName+".pptx")

	blocks := ParseMarkdownBlocks(content)
	sections := buildSections(blocks)

	input := map[string]any{
		"mode":       "content",
		"title":      title,
		"sections":   sections,
		"outputPath": outputPath,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", 0, fmt.Errorf("序列化输入数据失败: %w", err)
	}

	output, err := RunPythonScript(ctx, scriptPath, string(inputJSON))
	if err != nil {
		return "", 0, err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", 0, fmt.Errorf("解析 Python 脚本输出失败: %w", err)
	}

	slideCount := 0
	if sc, ok := result["slideCount"].(float64); ok {
		slideCount = int(sc)
	}

	return outputPath, slideCount, nil
}

func SkillGenerateChart(ctx context.Context, seriesList []ChartSeries, chartType, title, filePath string) (string, error) {
	if !IsPythonAvailable() {
		return "", errors.New("Python 不可用")
	}

	env, err := mustGetSkillEnv()
	if err != nil {
		return "", err
	}

	var scriptPath string
	var scriptErr error
	scriptPath, scriptErr = env.ResolveScriptPath(ctx, exportWordSkill, "chart_generator.py")
	if scriptErr != nil {
		scriptPath, scriptErr = env.ResolveScriptPath(ctx, exportPPTSkill, "chart_generator.py")
		if scriptErr != nil {
			return "", fmt.Errorf("图表生成脚本不存在: %v", scriptErr)
		}
	}

	if err := env.CheckAndInstallDeps(ctx, exportWordSkill); err != nil {
		return "", err
	}

	var series []map[string]any
	for _, s := range seriesList {
		series = append(series, map[string]any{
			"name":    s.Name,
			"xLabels": s.XLabels,
			"yValues": s.YValues,
		})
	}

	input := map[string]any{
		"chartType":  chartType,
		"title":      title,
		"outputPath": filePath,
		"series":     series,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("序列化图表数据失败: %w", err)
	}

	output, err := RunPythonScript(ctx, scriptPath, string(inputJSON))
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", fmt.Errorf("解析图表脚本输出失败: %w", err)
	}

	if success, ok := result["success"].(bool); !ok || !success {
		return "", errors.New("图表生成失败")
	}

	return filePath, nil
}

func SkillGeneratePptCharts(ctx context.Context, qr *QueryResult, title, fileName string) []string {
	if !IsPythonAvailable() {
		return GeneratePptCharts(qr, title, fileName)
	}

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

		seriesList := buildChartSeries(qr, xCol, seriesNames)
		if len(seriesList) > 0 {
			chartPath := filepath.Join("exports", fileName+"_ppt_chart.png")
			chartType := "line"
			if len(seriesList) == 1 {
				chartType = "bar"
			}
			if _, err := SkillGenerateChart(ctx, seriesList, chartType, title+" · Key Metrics", chartPath); err != nil {
				log.Printf("[SkillExport] Skill 图表生成失败，回退 Go: %v", err)
				return GeneratePptCharts(qr, title, fileName)
			}
			paths = append(paths, chartPath)
		}
	}

	return paths
}

func SkillGenerateDocxCharts(ctx context.Context, qr *QueryResult, title, fileName string) []string {
	if !IsPythonAvailable() {
		return GenerateDocxCharts(qr, title, fileName)
	}

	numericCols := DetectNumericCols(qr)
	if len(numericCols) == 0 || len(qr.Data) == 0 || len(qr.Columns) < 2 {
		return nil
	}

	xCol := qr.Columns[0]
	var paths []string

	if len(numericCols) > 1 {
		seriesNames := numericCols
		if len(seriesNames) > 4 {
			seriesNames = seriesNames[:4]
		}

		seriesList := buildChartSeries(qr, xCol, seriesNames)
		if len(seriesList) > 1 {
			linePath := filepath.Join("exports", fileName+"_chart_multi.png")
			if _, err := SkillGenerateChart(ctx, seriesList, "line", title+" · Trend Comparison", linePath); err != nil {
				log.Printf("[SkillExport] Skill 趋势图失败，回退 Go: %v", err)
			} else {
				paths = append(paths, linePath)
			}
		}
	}

	if len(numericCols) >= 1 {
		seriesNames := numericCols
		if len(seriesNames) > 3 {
			seriesNames = seriesNames[:3]
		}

		seriesList := buildChartSeries(qr, xCol, seriesNames)
		barPath := filepath.Join("exports", fileName+"_chart_bar.png")
		if _, err := SkillGenerateChart(ctx, seriesList, "bar", title+" · Metric Comparison", barPath); err != nil {
			log.Printf("[SkillExport] Skill 柱状图失败，回退 Go: %v", err)
		} else {
			paths = append(paths, barPath)
		}
	}

	if len(paths) == 0 {
		return GenerateDocxCharts(qr, title, fileName)
	}

	return paths
}

func NewSkillExportAnalysisDocxFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据分析报告"
		}

		fileName := SanitizeFileName(input.FileName, "report")
		EnsureExportsDir()

		if input.Content != "" {
			if IsPythonAvailable() {
				_, err := SkillExportWordFromContent(ctx, input.Content, title, fileName)
				if err == nil {
					url := fmt.Sprintf("/exports/%s.docx", fileName)
					log.Printf("[Tool:export_docx:Skill] 成功 (content) - url=%s\n", url)
					return &ExportAnalysisDocxOutput{
						Message:     fmt.Sprintf("已生成专业 Word 分析报告（Skill），[点击下载](%s)", url),
						DownloadURL: url,
						FileType:    "docx",
					}, nil
				}
				log.Printf("[Tool:export_docx] Skill 失败，回退 Go: %v\n", err)
			}
			docxPath := filepath.Join("exports", fileName+".docx")
			if err := GenerateDocxFromContent(input.Content, title, docxPath); err != nil {
				return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.docx", fileName)
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
			if IsPythonAvailable() {
				chartImagePaths = SkillGenerateDocxCharts(ctx, qr, title, fileName)
			} else {
				chartImagePaths = GenerateDocxCharts(qr, title, fileName)
			}
		}

		if IsPythonAvailable() {
			_, err := SkillExportWord(ctx, qr, title, fileName, input.IncludeChart, chartImagePaths)
			if err == nil {
				cleanupFiles(chartImagePaths)
				url := fmt.Sprintf("/exports/%s.docx", fileName)
				log.Printf("[Tool:export_docx:Skill] 成功 - rows=%d, url=%s\n", len(qr.Data), url)
				return &ExportAnalysisDocxOutput{
					Message:     fmt.Sprintf("已生成专业 Word 报告（Skill，%d 条数据），[点击下载](%s)", len(qr.Data), url),
					DownloadURL: url,
					FileType:    "docx",
				}, nil
			}
			log.Printf("[Tool:export_docx] Skill 失败，回退 Go: %v\n", err)
		}

		docxPath := filepath.Join("exports", fileName+".docx")
		if err := GenerateDocx(qr, title, chartImagePaths, docxPath); err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}
		cleanupFiles(chartImagePaths)

		url := fmt.Sprintf("/exports/%s.docx", fileName)
		log.Printf("[Tool:export_docx] 成功 (Go fallback) - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportAnalysisDocxOutput{
			Message:     fmt.Sprintf("已生成 Word 报告（%d 条数据），[点击下载](%s)", len(qr.Data), url),
			DownloadURL: url,
			FileType:    "docx",
		}, nil
	}
}

func NewSkillExportPPTFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据报告"
		}

		fileName := SanitizeFileName(input.FileName, "slides")
		EnsureExportsDir()

		if input.Content != "" {
			if IsPythonAvailable() {
				_, slideCount, err := SkillExportPPTFromContent(ctx, input.Content, title, fileName)
				if err == nil {
					url := fmt.Sprintf("/exports/%s.pptx", fileName)
					log.Printf("[Tool:export_ppt:Skill] 成功 (content) - slides=%d, url=%s\n", slideCount, url)
					return &ExportPPTOutput{
						Message:     fmt.Sprintf("已生成专业 PPT（Skill，%d 页），[点击下载](%s)", slideCount, url),
						SlideCount:  slideCount,
						DownloadURL: url,
						FileType:    "ppt",
					}, nil
				}
				log.Printf("[Tool:export_ppt] Skill 失败，回退 Go: %v\n", err)
			}
			pptxPath := filepath.Join("exports", fileName+".pptx")
			slideCount, err := GeneratePptxFromContent(input.Content, title, pptxPath)
			if err != nil {
				return nil, fmt.Errorf("生成 PPT 失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.pptx", fileName)
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

		var chartPaths []string
		if IsPythonAvailable() {
			chartPaths = SkillGeneratePptCharts(ctx, qr, title, fileName)
		} else {
			chartPaths = GeneratePptCharts(qr, title, fileName)
		}

		if IsPythonAvailable() {
			_, slideCount, err := SkillExportPPT(ctx, qr, title, fileName, chartPaths)
			if err == nil {
				cleanupFiles(chartPaths)
				url := fmt.Sprintf("/exports/%s.pptx", fileName)
				log.Printf("[Tool:export_ppt:Skill] 成功 - slides=%d, url=%s\n", slideCount, url)
				return &ExportPPTOutput{
					Message:     fmt.Sprintf("已生成专业 PPT（Skill，%d 页），[点击下载](%s)", slideCount, url),
					SlideCount:  slideCount,
					DownloadURL: url,
					FileType:    "ppt",
				}, nil
			}
			log.Printf("[Tool:export_ppt] Skill 失败，回退 Go: %v\n", err)
		}

		pptxPath := filepath.Join("exports", fileName+".pptx")
		slideCount, err := GeneratePptx(qr, title, chartPaths, pptxPath)
		cleanupFiles(chartPaths)
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.pptx", fileName)
		log.Printf("[Tool:export_ppt] 成功 (Go fallback) - slides=%d, url=%s\n", slideCount, url)

		return &ExportPPTOutput{
			Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
			SlideCount:  slideCount,
			DownloadURL: url,
			FileType:    "ppt",
		}, nil
	}
}

func buildNumericStats(qr *QueryResult, numericCols []string) []map[string]any {
	var stats []map[string]any
	for _, col := range numericCols {
		min, max, avg, count := CalcNumericStats(qr, col)
		if count == 0 {
			continue
		}

		var stddev float64
		if count > 1 {
			var sumSq float64
			for _, row := range qr.Data {
				if v, err := ToFloat64(row[col]); err == nil {
					diff := v - avg
					sumSq += diff * diff
				}
			}
			stddev = math.Sqrt(sumSq / float64(count-1))
		}

		stats = append(stats, map[string]any{
			"column": col,
			"count":  count,
			"min":    min,
			"max":    max,
			"avg":    avg,
			"stddev": stddev,
		})
	}
	return stats
}

func buildFindings(qr *QueryResult, numericCols []string) []string {
	var findings []string
	for _, col := range numericCols {
		_, max, avg, count := CalcNumericStats(qr, col)
		if count > 1 {
			findings = append(findings,
				fmt.Sprintf("%s: average %.2f, peak %.2f, indicating significant fluctuation", col, avg, max))
			if len(findings) >= 5 {
				break
			}
		}
	}
	if len(findings) == 0 {
		findings = append(findings, "Data quality is satisfactory; consider further dimensional analysis")
	}
	return findings
}

func buildHighlights(qr *QueryResult, numericCols []string) []string {
	var highlights []string
	for _, col := range numericCols {
		_, max, avg, count := CalcNumericStats(qr, col)
		if count > 1 {
			highlights = append(highlights,
				fmt.Sprintf("%s — Average: %.2f, Peak: %.2f", col, avg, max))
			if len(highlights) >= 8 {
				break
			}
		}
	}
	return highlights
}

func buildSummary(qr *QueryResult) map[string]any {
	summary := map[string]any{
		"totalRows": len(qr.Data),
		"totalCols": len(qr.Columns),
		"columns":   qr.Columns,
		"stats":     make(map[string]any),
	}

	stats := summary["stats"].(map[string]any)
	for _, col := range DetectNumericCols(qr) {
		min, max, avg, count := CalcNumericStats(qr, col)
		if count > 0 {
			stats[col] = map[string]any{
				"min": min,
				"max": max,
				"avg": avg,
			}
		}
	}

	return summary
}

func buildSections(blocks []MdBlock) []map[string]any {
	var sections []map[string]any
	var current map[string]any
	var currentBlocks []map[string]any

	ensureBlocks := func() []map[string]any {
		if currentBlocks == nil {
			return []map[string]any{}
		}
		return currentBlocks
	}

	for _, block := range blocks {
		if block.Type == "h1" || block.Type == "h2" {
			if current != nil {
				current["blocks"] = ensureBlocks()
				sections = append(sections, current)
			}
			current = map[string]any{
				"title": StripMarkdownFormatting(block.Content),
			}
			currentBlocks = nil
		} else {
			currentBlocks = append(currentBlocks, map[string]any{
				"type":    block.Type,
				"content": StripMarkdownFormatting(block.Content),
			})
		}
	}
	if current != nil {
		current["blocks"] = ensureBlocks()
		sections = append(sections, current)
	}

	return sections
}

func buildChartSeries(qr *QueryResult, xCol string, seriesNames []string) []ChartSeries {
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
	return seriesList
}

func filterExistingFiles(paths []string) []string {
	var result []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			result = append(result, p)
		}
	}
	return result
}

func cleanupFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}