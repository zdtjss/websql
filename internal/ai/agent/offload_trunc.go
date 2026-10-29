// offload_trunc.go — 行式 offload 的自定义截断处理器。
//
// 背景：eino reduction 默认把工具结果原样落盘，再将首尾片段 + 文件路径返回给模型。
// 对 query_data/read_file_data 这类 JSON 结果，原样落盘是“单行 JSON”，
// 而 read_offload 按行分页，模型无法从中定位任意一行/一条记录。
//
// 本文件把大结果转换为“元数据行 + 每行一条记录”的 JSONL（文本类保留换行），
// 并返回紧凑的统计摘要与读取方式，使截断真正可恢复。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/schema"
)

// reductionMaxLengthForTrunc 单条工具结果触发截断的字节上限。
// 与 reduction.Config.MaxLengthForTrunc 保持一致；仅作为代码层安全阈值，
// 不写入提示词，也不作为“建议取数数量”。
const reductionMaxLengthForTrunc = 30_000

// buildReductionToolConfig 为 query_data/read_file_data 注册行式截断处理器。
func buildReductionToolConfig(backend reduction.Backend) map[string]*reduction.ToolReductionConfig {
	return map[string]*reduction.ToolReductionConfig{
		"query_data": {
			Backend:      backend,
			TruncHandler: queryTruncHandler,
		},
		"read_file_data": {
			Backend:      backend,
			TruncHandler: readFileTruncHandler,
		},
		"dataset_query": {
			Backend:      backend,
			TruncHandler: queryTruncHandler,
		},
		"get_table_schema": {
			Backend:      backend,
			TruncHandler: schemaTruncHandler,
		},
	}
}

// queryTruncHandler 把 query_data 的 JSON 结果转换为 JSONL + 元数据行。
func queryTruncHandler(ctx context.Context, detail *reduction.ToolDetail) (*reduction.TruncResult, error) {
	text, ok := toolResultText(detail)
	if !ok || len(text) <= reductionMaxLengthForTrunc {
		return &reduction.TruncResult{NeedTrunc: false}, nil
	}
	recordAgentMetric("ai_truncated_result_total", "query_data")

	var out QueryOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return fallbackTruncResult(ctx, detail, text, "query_data"), nil
	}
	// 已物化为数据集：结果本身是可查询句柄，不需要再 offload。
	if out.DatasetID != "" {
		return &reduction.TruncResult{NeedTrunc: false}, nil
	}
	if len(out.Columns) == 0 || len(out.Data) == 0 {
		return fallbackTruncResult(ctx, detail, text, "query_data"), nil
	}

	filePath, err := reductionOffloadPath(ctx, "trunc", toolCallID(detail))
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	meta := map[string]any{
		"columns":   out.Columns,
		"rowCount":  len(out.Data),
		"truncated": true,
		"source":    "query_data",
		"format":    "jsonl",
	}
	metaLine, _ := json.Marshal(meta)
	b.Write(metaLine)
	b.WriteByte('\n')
	for _, row := range out.Data {
		values := make([]any, len(out.Columns))
		for i, col := range out.Columns {
			values[i] = row[col]
		}
		line, marshalErr := json.Marshal(values)
		if marshalErr != nil {
			line = []byte(`null`)
		}
		b.Write(line)
		b.WriteByte('\n')
	}

	notice := buildQueryTruncNotice(out, filePath)
	return &reduction.TruncResult{
		NeedTrunc:       true,
		NeedOffload:     true,
		OffloadFilePath: filePath,
		OffloadContent:  b.String(),
		ToolResult:      textToolResult(notice),
	}, nil
}

// readFileTruncHandler 把 read_file_data 的结果转换为 JSONL/文本行。
func readFileTruncHandler(ctx context.Context, detail *reduction.ToolDetail) (*reduction.TruncResult, error) {
	text, ok := toolResultText(detail)
	if !ok || len(text) <= reductionMaxLengthForTrunc {
		return &reduction.TruncResult{NeedTrunc: false}, nil
	}
	recordAgentMetric("ai_truncated_result_total", "read_file_data")

	var out ReadFileDataOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return fallbackTruncResult(ctx, detail, text, "read_file_data"), nil
	}
	// 已物化为数据集：结果本身是可查询句柄，不需要再 offload。
	if out.DatasetID != "" {
		return &reduction.TruncResult{NeedTrunc: false}, nil
	}

	// outline/search/profile 模式没有 Text/Rows，只有结构元数据；
	// 用格式化 JSON 落盘，避免把 Matches/Profile 丢失。
	if out.Text == "" && len(out.Rows) == 0 {
		return fallbackTruncResult(ctx, detail, text, "read_file_data"), nil
	}

	filePath, err := reductionOffloadPath(ctx, "trunc", toolCallID(detail))
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	notice := ""
	switch out.Type {
	case "table":
		meta := map[string]any{
			"type":       "table",
			"columns":    out.Columns,
			"totalRows":  out.TotalRows,
			"returned":   out.ReturnedRows,
			"truncated":  true,
			"source":     "read_file_data",
			"format":     "jsonl",
			"sheetCount": len(out.Sheets),
		}
		metaLine, _ := json.Marshal(meta)
		b.Write(metaLine)
		b.WriteByte('\n')
		for _, row := range out.Rows {
			line, marshalErr := json.Marshal(row)
			if marshalErr != nil {
				line = []byte(`null`)
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		notice = buildTableTruncNotice(out, filePath)
	case "text":
		// 文本/文档：保留原始换行与页码标记；单行超长文本按字符窗口插入换行，
		// 仅用于分页阅读，不改变语义。
		content := out.Text
		if !strings.Contains(content, "\n") && len(content) > reductionMaxLengthForTrunc {
			content = wrapLongText(content, 4_000)
		}
		b.WriteString(content)
		notice = fmt.Sprintf(
			"文档内容已落盘（共 %d 字符）。可用 read_offload(path=%q, offset=1, limit=<按需>) 按行读取；"+
				"若已知关键词，可传 pattern 过滤。",
			out.CharCount, filePath,
		)
	default:
		return fallbackTruncResult(ctx, detail, text, "read_file_data"), nil
	}

	return &reduction.TruncResult{
		NeedTrunc:       true,
		NeedOffload:     true,
		OffloadFilePath: filePath,
		OffloadContent:  b.String(),
		ToolResult:      textToolResult(notice),
	}, nil
}

// schemaTruncHandler 把过长的建表语句按行落盘，便于 read_offload 分段查看。
func schemaTruncHandler(ctx context.Context, detail *reduction.ToolDetail) (*reduction.TruncResult, error) {
	text, ok := toolResultText(detail)
	if !ok || len(text) <= reductionMaxLengthForTrunc {
		return &reduction.TruncResult{NeedTrunc: false}, nil
	}
	recordAgentMetric("ai_truncated_result_total", "get_table_schema")
	var out SchemaOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Schema == "" {
		return fallbackTruncResult(ctx, detail, text, "get_table_schema"), nil
	}
	filePath, err := reductionOffloadPath(ctx, "trunc", toolCallID(detail))
	if err != nil {
		return nil, err
	}
	notice := fmt.Sprintf(
		"表结构较长（%d 字符），已按行落盘到 %q。可用 read_offload(path=%q, offset=1, limit=按需) 分段查看，或分批传入表名获取完整结构。",
		len(out.Schema), filePath, filePath,
	)
	return &reduction.TruncResult{
		NeedTrunc:       true,
		NeedOffload:     true,
		OffloadFilePath: filePath,
		OffloadContent:  out.Schema,
		ToolResult:      textToolResult(notice),
	}, nil
}

// fallbackTruncResult 对无法解析的结果做安全降级：
// 合法 JSON 格式化后落盘；否则保留原文并给出首尾预览，确保不返回无提示的全量内容。
func fallbackTruncResult(ctx context.Context, detail *reduction.ToolDetail, text, source string) *reduction.TruncResult {
	filePath, err := reductionOffloadPath(ctx, "trunc", toolCallID(detail))
	if err != nil {
		// 身份缺失等情况下不再 offload，返回带截断提示的预览，避免工具调用整体失败。
		preview := previewHeadTail(text, 2_000)
		return &reduction.TruncResult{
			NeedTrunc: true,
			ToolResult: textToolResult(fmt.Sprintf(
				"结果较大（%d 字节）且无法写入 offload 文件（%v）。以下是首尾预览：\n%s",
				len(text), err, preview,
			)),
		}
	}

	content := text
	if pretty, formatErr := prettyJSON(text); formatErr == nil {
		content = pretty
	}
	notice := fmt.Sprintf(
		"%s 结果较大（%d 字节），已落盘到 %q。可用 read_offload(path=%q, offset=1, limit=<按需>) 按行读取。"+
			"若内容不是行式结构，建议改用聚合/过滤或 read_file_data 的结构化模式。",
		source, len(text), filePath, filePath,
	)
	return &reduction.TruncResult{
		NeedTrunc:       true,
		NeedOffload:     true,
		OffloadFilePath: filePath,
		OffloadContent:  content,
		ToolResult:      textToolResult(notice),
	}
}

func buildQueryTruncNotice(out QueryOutput, filePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "查询结果较大（%d 行），已按行落盘到 %q。\n", len(out.Data), filePath)
	fmt.Fprintf(&b, "列：%s\n", strings.Join(out.Columns, ", "))
	// 前 3 行预览，帮助模型判断后续读取范围。
	preview := out.Data
	if len(preview) > 3 {
		preview = preview[:3]
	}
	if len(preview) > 0 {
		if sample, err := json.Marshal(preview); err == nil {
			fmt.Fprintf(&b, "前 %d 行预览：%s\n", len(preview), clampUTF8Prefix(string(sample), 2_000))
		}
	}
	if stats := summarizeQueryColumns(out); stats != "" {
		fmt.Fprintf(&b, "列统计：%s\n", stats)
	}
	fmt.Fprintf(&b, "读取方式：read_offload(path=%q, offset=2, limit=按需)；第 1 行是元数据，第 2 行起每行一条记录。\n", filePath)
	fmt.Fprintf(&b, "若需要统计/过滤，优先基于原 SQL 聚合/过滤，或按行读取后自行计算。\n")
	return b.String()
}

func buildTableTruncNotice(out ReadFileDataOutput, filePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "表格结果较大（总行数 %d，本次 %d 行），已按行落盘到 %q。\n", out.TotalRows, out.ReturnedRows, filePath)
	if len(out.Columns) > 0 {
		fmt.Fprintf(&b, "列：%s\n", strings.Join(out.Columns, ", "))
	}
	if len(out.Sheets) > 0 {
		names := make([]string, 0, len(out.Sheets))
		for _, s := range out.Sheets {
			names = append(names, s.Name)
		}
		fmt.Fprintf(&b, "工作表：%s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(&b, "读取方式：read_offload(path=%q, offset=2, limit=按需)；第 1 行是元数据，第 2 行起每行一条记录。\n", filePath)
	fmt.Fprintf(&b, "若需要统计/过滤，优先使用 read_file_data 的 profile/search 模式或 dataset_query。\n")
	return b.String()
}

// summarizeQueryColumns 生成紧凑列统计（数值 min/max、空值数、distinct 上限）。
func summarizeQueryColumns(out QueryOutput) string {
	if len(out.Data) == 0 {
		return ""
	}
	parts := make([]string, 0, len(out.Columns))
	for _, col := range out.Columns {
		nulls := 0
		var min, max float64
		hasNum := false
		distinct := make(map[string]struct{}, 32)
		distinctCapped := false
		for _, row := range out.Data {
			v, ok := row[col]
			if !ok || v == nil {
				nulls++
				continue
			}
			if f, ok := toFloat(v); ok {
				if !hasNum {
					min, max, hasNum = f, f, true
				} else {
					if f < min {
						min = f
					}
					if f > max {
						max = f
					}
				}
			}
			if !distinctCapped {
				key := fmt.Sprint(v)
				distinct[key] = struct{}{}
				if len(distinct) > 50 {
					distinctCapped = true
				}
			}
		}
		desc := col
		if hasNum {
			desc += fmt.Sprintf("[min=%g,max=%g]", min, max)
		}
		if nulls > 0 {
			desc += fmt.Sprintf("[null=%d]", nulls)
		}
		if distinctCapped {
			desc += fmt.Sprintf("[distinct>%d]", len(distinct))
		} else if len(distinct) > 0 {
			desc += fmt.Sprintf("[distinct=%d]", len(distinct))
		}
		parts = append(parts, desc)
	}
	return strings.Join(parts, "; ")
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

func toolResultText(detail *reduction.ToolDetail) (string, bool) {
	if detail == nil || detail.ToolResult == nil || len(detail.ToolResult.Parts) == 0 {
		return "", false
	}
	for _, part := range detail.ToolResult.Parts {
		if part.Type == schema.ToolPartTypeText {
			return part.Text, true
		}
	}
	return "", false
}

func toolCallID(detail *reduction.ToolDetail) string {
	if detail != nil && detail.ToolContext != nil {
		return detail.ToolContext.CallID
	}
	return ""
}

func textToolResult(text string) *schema.ToolResult {
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}},
	}
}

func prettyJSON(text string) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(text), "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func previewHeadTail(text string, limit int) string {
	if len(text) <= limit*2 {
		return text
	}
	return clampUTF8Prefix(text, limit) + "\n...\n" + clampUTF8Suffix(text, limit)
}

func clampUTF8Suffix(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	start := len(s) - max
	for start < len(s) && !utf8RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func utf8RuneStart(b byte) bool {
	return b&0xC0 != 0x80
}

// wrapLongText 在无换行的超长文本中按固定字符窗口插入换行，仅用于分页。
func wrapLongText(text string, window int) string {
	if window <= 0 || len(text) <= window {
		return text
	}
	var b strings.Builder
	for len(text) > 0 {
		if len(text) <= window {
			b.WriteString(text)
			break
		}
		end := window
		for end > 0 && !utf8RuneStart(text[end]) {
			end--
		}
		if end == 0 {
			end = 1
		}
		b.WriteString(text[:end])
		b.WriteByte('\n')
		text = text[end:]
	}
	return b.String()
}
