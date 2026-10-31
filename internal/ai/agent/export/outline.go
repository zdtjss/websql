package export

// 产物结构视图（outline）：只读派生，不落盘。
//
// 报告类由源 Markdown 重新解析（与生成/编辑同一解析器），因此块 ID 与
// repair_export.operations 的定位完全一致；Excel 读取产物本身；PPT 额外
// 返回渲染器计算的页映射。

import (
	"context"
	"fmt"

	"websql/internal/ai/agent/export/report"
)

// artifactSourceKindOf 返回产物的源快照类型（旧记录按工具推断）。
func artifactSourceKindOf(meta *ArtifactMeta) string {
	if meta == nil {
		return ""
	}
	if meta.SourceKind != "" {
		return meta.SourceKind
	}
	switch meta.Tool {
	case "export_analysis_docx", "export_ppt", "export_html":
		return "report_markdown"
	case "export_excel", "export_excel_with_chart":
		return "workbook"
	case "export_by_code":
		return "code"
	case "export_skill":
		return "skill"
	}
	return ""
}

// reportOutlineMode 报告 outline 的补充信息。
type reportOutlineMode struct {
	Mode     string `json:"mode"` // content | sql
	SQL      string `json:"sql,omitempty"`
	Summary  string `json:"summary,omitempty"`
	FileType string `json:"fileType,omitempty"`
}

// buildArtifactOutline 按产物类型构建结构视图。
func buildArtifactOutline(ctx context.Context, auditCtx *AuditCtx, meta *ArtifactMeta) (any, error) {
	if meta == nil {
		return nil, fmt.Errorf("产物不存在")
	}
	switch meta.Tool {
	case "export_analysis_docx", "export_ppt", "export_html":
		content := maskArtifactText(ctx, auditCtx, meta.Content)
		if content == "" {
			// SQL 模式：没有可定位的正文块，返回取数说明，引导模型改用参数级修复。
			return reportOutlineMode{Mode: "sql", SQL: meta.SQL, FileType: meta.FileType}, nil
		}
		doc := report.BuildDocument(meta.Title, meta.Subtitle, content)
		outline := report.BuildOutline(doc)
		outline.Summary.Slides = 0
		if meta.Tool == "export_ppt" {
			outline.Slides = report.PlanSlides(doc)
			outline.Summary.Slides = len(outline.Slides)
		}
		return outline, nil
	case "export_excel", "export_excel_with_chart":
		wo, err := buildWorkbookOutline(meta.FileName)
		if err != nil {
			// outline 是诊断辅助，读取失败不应阻断 get_export_artifact。
			return map[string]any{"mode": "workbook", "warning": err.Error()}, nil
		}
		return wo, nil
	case "export_by_code":
		code := maskArtifactText(ctx, auditCtx, meta.Code)
		return map[string]any{
			"mode":       "code",
			"outputFile": meta.OutputFile,
			"codeChars":  len([]rune(code)),
		}, nil
	case "export_skill":
		return map[string]any{
			"mode":         "skill",
			"skill":        meta.Skill,
			"payloadChars": len([]rune(meta.SkillPayload)),
		}, nil
	}
	return nil, nil
}
