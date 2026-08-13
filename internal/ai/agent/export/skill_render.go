package export

// 薄封装渲染调度：export_ppt / export_analysis_docx 工具的 Python 渲染路径。
//
// 重构说明（模板驱动 + 双路径降级）：
//   - 路径 1（本文件）：Python 模板驱动渲染（专业版）
//     export-ppt/scripts/export_ppt.py       → templates/slides_template.pptx
//     export-word/scripts/word_generator.py  → templates/report_template.docx
//   - 路径 2（tools.go 降级）：Python 不可用或渲染失败时，自动降级
//     Go 基础版渲染器（pptx.go/docx.go，无 Python 依赖），保证导出永不失败
//   - Go 侧只做：取数、统计计算、组装 JSON、fork Python、解析结果
//   - JSON 契约与 SKILL.md 保持一致，skill 路径与工具路径共用同一渲染器

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// renderTimeout Python 渲染子进程执行上限（与 execute 工具的 300s 兜底对齐）。
const renderTimeout = 300 * time.Second

// skillsRootDir 返回 skills 目录：
// 优先 SkillEnv.rootDir（InitSkillEnv 初始化时确定），其次 SKILLS_DIR 环境变量，
// 最后当前工作目录下的 skills/。
func skillsRootDir() string {
	if env := GetSkillEnv(); env != nil && env.rootDir != "" {
		return env.rootDir
	}
	if dir := os.Getenv("SKILLS_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "skills"
	}
	return filepath.Join(cwd, "skills")
}

// pythonRendererOutput Python 渲染器 stdout 的 JSON 结构。
type pythonRendererOutput struct {
	Success    bool   `json:"success"`
	Error      string `json:"error"`
	OutputPath string `json:"outputPath"`
	SlideCount int    `json:"slideCount"`
}

// runPythonRenderer 执行 Python 渲染脚本并解析输出。
//
// scriptRel 为相对 skills 目录的脚本路径（如 export-ppt/scripts/export_ppt.py）；
// input 将被序列化为 JSON 经位置参数文件传入（Eino ExecuteRequest 不支持 stdin，
// 与 skill 路径保持一致）。stdout/stderr 分离收集，避免 matplotlib 警告污染 JSON。
func runPythonRenderer(ctx context.Context, scriptRel string, input any) (*pythonRendererOutput, error) {
	if !IsPythonAvailable() {
		return nil, fmt.Errorf("需要 Python 环境（python-pptx/python-docx/matplotlib/docxtpl），请安装依赖后重试")
	}
	pythonPath := GetPythonPath()
	scriptPath := filepath.Join(skillsRootDir(), scriptRel)

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("组装渲染输入失败：%w", err)
	}
	tmpFile, err := os.CreateTemp("", "websql_render_*.json")
	if err != nil {
		return nil, fmt.Errorf("创建渲染输入临时文件失败：%w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(inputJSON); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("写入渲染输入失败：%w", err)
	}
	tmpFile.Close()

	execCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, pythonPath, scriptPath, tmpFile.Name())
	hideWindow(cmd)
	cmd.Env = append(os.Environ(),
		"PYTHONIOENCODING=utf-8",
		"PYTHONDONTWRITEBYTECODE=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Python 渲染失败：%v，输出：%s", err,
			strings.TrimSpace(stderr.String()))
	}

	var result pythonRendererOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("解析渲染输出失败：%w（stderr: %s）", err,
			strings.TrimSpace(stderr.String()))
	}
	if !result.Success {
		return nil, fmt.Errorf("渲染失败：%s", result.Error)
	}
	return &result, nil
}

// buildNumericStats 对数值列计算统计指标（count/min/max/avg/stddev）。
// 契约与 SKILL.md 的 numericStats 字段一致。
func buildNumericStats(qr *QueryResult) []map[string]any {
	numericCols := DetectNumericCols(qr)
	var stats []map[string]any
	for _, col := range numericCols {
		min, max, avg, count := CalcNumericStats(qr, col)
		var sum, sumSq float64
		for _, row := range qr.Data {
			if v, err := ToFloat64(row[col]); err == nil {
				sum += v
				sumSq += v * v
			}
		}
		stddev := 0.0
		if count > 1 {
			variance := (sumSq - sum*sum/float64(count)) / float64(count-1)
			if variance > 0 {
				stddev = math.Sqrt(variance)
			}
		}
		stats = append(stats, map[string]any{
			"column": col, "count": count, "min": min, "max": max,
			"avg": avg, "stddev": stddev,
		})
	}
	return stats
}

// buildFindings 基于统计结果生成洞察（Word data 模式的 findings 字段）。
func buildFindings(stats []map[string]any) []string {
	var findings []string
	for _, s := range stats {
		findings = append(findings, fmt.Sprintf(
			"%s 平均值 %.2f，峰值 %.2f，波动较大", s["column"], s["avg"], s["max"]))
	}
	return findings
}

// buildHighlights 基于统计结果生成亮点（PPT data 模式的 highlights 字段）。
func buildHighlights(stats []map[string]any) []string {
	var highlights []string
	for _, s := range stats {
		highlights = append(highlights, fmt.Sprintf(
			"%s — 平均: %.2f, 峰值: %.2f", s["column"], s["avg"], s["max"]))
	}
	return highlights
}

// buildPPTDataJSON 组装 PPT data 模式渲染输入（契约与 SKILL.md 一致）。
func buildPPTDataJSON(qr *QueryResult, title, outputPath string) map[string]any {
	stats := buildNumericStats(qr)
	return map[string]any{
		"mode":       "data",
		"title":      title,
		"columns":    qr.Columns,
		"data":       qr.Data,
		"highlights": buildHighlights(stats),
		"outputPath": outputPath,
	}
}

// buildPPTContentJSON 组装 PPT content 模式渲染输入（原始 Markdown 直传，
// Markdown 解析在 Python 侧 md_blocks.py 完成，Go 侧零解析）。
func buildPPTContentJSON(title, markdown, outputPath string) map[string]any {
	return map[string]any{
		"mode":       "content",
		"title":      title,
		"markdown":   markdown,
		"outputPath": outputPath,
	}
}

// buildDocxDataJSON 组装 Word data 模式渲染输入（契约与 SKILL.md 一致）。
func buildDocxDataJSON(qr *QueryResult, title, outputPath string) map[string]any {
	stats := buildNumericStats(qr)
	return map[string]any{
		"mode":           "data",
		"title":          title,
		"columns":        qr.Columns,
		"data":           qr.Data,
		"numericColumns": DetectNumericCols(qr),
		"numericStats":   stats,
		"findings":       buildFindings(stats),
		"includeCharts":  true,
		"outputPath":     outputPath,
	}
}

// buildDocxContentJSON 组装 Word content 模式渲染输入。
func buildDocxContentJSON(title, markdown, outputPath string) map[string]any {
	return map[string]any{
		"mode":       "content",
		"title":      title,
		"markdown":   markdown,
		"outputPath": outputPath,
	}
}
