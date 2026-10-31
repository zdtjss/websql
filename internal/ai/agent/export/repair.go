package export

// 对话式文件修复
//
// 定位：用户在会话中直接指出“刚才导出的文件有问题”时，AI 先读取该产物的
// 原导出参数 / 报告源内容 / SQL / 图表定义 / Python 脚本，定位根因后做最小修复，
// 以新版本文件返回，原文件保留可追溯。
//
// 边界：
//   - 只能修复当前用户、当前会话内由导出工具生成的产物（ArtifactMeta 校验）；
//   - 内置报告修复复用原渲染链路（content/sql + IR），不引入新的写文件通道；
//   - export_by_code 修复复用同一沙箱执行链路（只读、隔离、队列、凭据拦截不变）；
//   - 修复不是整份重新生成：无法增量修复时明确失败并给出替代方案。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"websql/internal/ai/agent/export/report"

	"github.com/jmoiron/sqlx"
)

// ExportArtifactInput 查询当前会话导出产物的源信息。
type ExportArtifactInput struct {
	File string `json:"file" jsonschema:"required" jsonschema_description:"要检查的导出文件：使用此前导出工具返回的下载链接或完整文件名（仅限当前会话）"`
	// IncludeFile 为 true 时附带文本类产物的渲染结果预览（HTML/MD/CSV/TXT/JSON/XML），用于诊断渲染问题。
	IncludeFile bool `json:"includeFile" jsonschema_description:"是否附带产物本体预览（仅 HTML/Markdown/CSV/TXT/JSON/XML 等文本格式，限长）；诊断 JS/样式/渲染问题时建议 true"`
	// IncludeOutline 为 true 时返回结构视图（章节/块 ID/表格/图表/Sheet/页映射），编辑前必须先取 outline。
	IncludeOutline bool `json:"includeOutline" jsonschema_description:"是否返回结构 outline（章节+块 ID、Excel Sheet、PPT 页映射）；对话式编辑前建议 true"`
	// SourceField 选择要回读的源字段；不传时按产物类型选择主字段。
	SourceField string `json:"sourceField" jsonschema_description:"要回读的源字段：content|sql|code|skillPayload|summary；不传时按产物类型自动选择（报告=content、Excel=sql、高级导出=code、Skill=skillPayload）"`
	// SourceOffset 分段读取的起始字符偏移（rune）。
	SourceOffset int `json:"sourceOffset" jsonschema_description:"源内容分段读取的起始字符偏移（默认 0）；sourceTruncated=true 时用 nextSourceOffset 继续读取"`
	// SourceLimit 本次读取的字符数上限（默认 8000，最大 16000）。
	SourceLimit int `json:"sourceLimit" jsonschema_description:"本次读取的字符数（默认 8000，最大 16000）；长报告请分段读取，不要一次拉全文"`
}

// ExportArtifactOutput 导出产物源信息（供模型诊断后修复）。
type ExportArtifactOutput struct {
	Found       bool         `json:"found"`
	Message     string       `json:"message"`
	Tool        string       `json:"tool,omitempty"`
	ConnID      string       `json:"connId,omitempty"`
	FileName    string       `json:"fileName,omitempty"`
	DownloadURL string       `json:"downloadUrl,omitempty"`
	FileType    string       `json:"fileType,omitempty"`
	Version     int          `json:"version,omitempty"`
	CreatedAt   string       `json:"createdAt,omitempty"`
	Title       string       `json:"title,omitempty"`
	Subtitle    string       `json:"subtitle,omitempty"`
	Theme       string       `json:"theme,omitempty"`
	SQL         string       `json:"sql,omitempty"`
	Content     string       `json:"content,omitempty"`
	Charts      []ExcelChart `json:"charts,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	Code        string       `json:"code,omitempty"`
	OutputFile  string       `json:"outputFile,omitempty"`
	// Skill/SkillPayload 仅 export_skill 产物：payload 是 repair_export 的编辑源。
	Skill        string `json:"skill,omitempty"`
	SkillPayload string `json:"skillPayload,omitempty"`
	ParentURL    string `json:"parentUrl,omitempty"`
	// SourceField 本次实际回读的源字段。
	SourceField string `json:"sourceField,omitempty"`
	// SourceOffset 本次回读的起始偏移（rune）。
	SourceOffset int `json:"sourceOffset,omitempty"`
	// SourceTotalChars 该源字段的总字符数。
	SourceTotalChars int `json:"sourceTotalChars,omitempty"`
	// SourceNextOffset 下一页起始偏移（仅 sourceTruncated=true 时有效）。
	SourceNextOffset int `json:"sourceNextOffset,omitempty"`
	// SourceTruncated 表示源内容仍有后续；模型应使用 sourceNextOffset 分段读取。
	SourceTruncated bool `json:"sourceTruncated,omitempty"`
	// ─── 产物 v2 ────────────────────────────────────
	ArtifactID string              `json:"artifactId,omitempty"`
	SourceKind string              `json:"sourceKind,omitempty"`
	SourceHash string              `json:"sourceHash,omitempty"`
	Operations []OperationRecord   `json:"operations,omitempty"`
	Validation *ArtifactValidation `json:"validation,omitempty"`
	// Outline 结构视图（includeOutline=true）：报告章节/块、Excel Sheet、PPT 页映射。
	Outline any `json:"outline,omitempty"`
	// FilePreview 产物本体预览（仅 IncludeFile=true 且为文本格式时返回）。
	FilePreview string `json:"filePreview,omitempty"`
	// FilePreviewTruncated 表示预览被截断。
	FilePreviewTruncated bool `json:"filePreviewTruncated,omitempty"`
}

// ExportPatch 局部替换补丁：在原始源内容上按 find/replace 应用。
// 用于长文档最小修复，避免模型回传整份正文导致丢段/压缩。
type ExportPatch struct {
	Find    string `json:"find" jsonschema:"required" jsonschema_description:"原文中要替换的精确片段（区分大小写，必须能在当前源内容中命中）"`
	Replace string `json:"replace" jsonschema_description:"替换后的内容"`
	All     bool   `json:"all" jsonschema_description:"是否替换所有匹配（默认仅替换第一处）"`
}

// ExportRepairInput 内置报告产物的增量修复入参。
// 未提供的字段沿用原导出参数；提供的字段作为本次修复的替换内容。
type ExportRepairInput struct {
	File    string `json:"file" jsonschema:"required" jsonschema_description:"要修复的导出文件：使用此前返回的下载链接或完整文件名（仅限当前会话）"`
	Issue   string `json:"issue" jsonschema_description:"用户反馈的问题现象（如 HTML JS 报错、图表不显示、PPT 排版错乱、Excel 打不开）"`
	Changes string `json:"changes" jsonschema_description:"诊断结论与本次修改点（用于回执与审计）"`
	// ExpectedVersion 乐观锁：传入 get_export_artifact 返回的版本；不一致时拒绝，避免基于旧结构编辑。
	ExpectedVersion int           `json:"expectedVersion" jsonschema_description:"期望版本号（get_export_artifact 返回的 version）；不一致会拒绝编辑"`
	Title           string        `json:"title" jsonschema_description:"修复后的报告标题（可选，缺省沿用原标题）"`
	Subtitle        string        `json:"subtitle" jsonschema_description:"修复后的副标题（可选，缺省沿用原副标题）"`
	Theme           string        `json:"theme" jsonschema_description:"修复后的主题 business|tech（可选，缺省沿用原主题）"`
	Content         string        `json:"content" jsonschema_description:"修复后的完整扩展 Markdown 正文（可选；缺省沿用原 content；与 patches/operations 互斥）"`
	SQL             string        `json:"sql" jsonschema_description:"修复后的 SELECT SQL（可选；缺省沿用原 SQL；显式传入且未传 content 时按 SQL 模式修复）"`
	Patches         []ExportPatch `json:"patches" jsonschema_description:"局部替换列表：在原始正文上按 find/replace 应用；与 content/operations 互斥"`
	// Operations 结构化编辑（推荐）：块/章节/表格行/页/单元格等增量操作；删除类操作会触发服务端确认。
	Operations []EditOperation `json:"operations" jsonschema_description:"结构化编辑操作（推荐）：块/章节/表格行/页/单元格等；删除类操作会请求用户确认；与 content/patches 互斥"`
	Charts     []ExcelChart    `json:"charts" jsonschema_description:"修复后的图表定义（仅 export_excel_with_chart；缺省沿用原图表定义）"`
	Summary    string          `json:"summary" jsonschema_description:"修复后的结论与建议（仅 export_excel_with_chart；扩展 Markdown；缺省沿用原 summary）"`
}

// ExportRepairOutput 内置报告修复结果。
type ExportRepairOutput struct {
	Message      string `json:"message"`
	DownloadURL  string `json:"downloadUrl,omitempty"`
	FileType     string `json:"fileType,omitempty"`
	Version      int    `json:"version,omitempty"`
	OriginalURL  string `json:"originalUrl,omitempty"`
	RowCount     int    `json:"rowCount,omitempty"`
	SlideCount   int    `json:"slideCount,omitempty"`
	SectionCount int    `json:"sectionCount,omitempty"`
	ChartCount   int    `json:"chartCount,omitempty"`
	TableCount   int    `json:"tableCount,omitempty"`
	// Diff 变更摘要（新增/删除/修改数量与样例）。
	Diff string `json:"diff,omitempty"`
	// Validation 产物校验结果（与元数据一致）。
	Validation *ArtifactValidation `json:"validation,omitempty"`
}

// RepairExportByCodeInput export_by_code 产物的脚本级修复入参。
type RepairExportByCodeInput struct {
	File       string        `json:"file" jsonschema:"required" jsonschema_description:"要修复的导出文件：使用此前返回的下载链接或完整文件名（仅限当前会话）"`
	Issue      string        `json:"issue" jsonschema_description:"用户反馈的问题现象（如编码错误、PDF 截断、CSV 分隔符异常、ZIP 缺文件）"`
	Changes    string        `json:"changes" jsonschema_description:"诊断结论与本次脚本修改点（用于回执与审计）"`
	Code       string        `json:"code" jsonschema_description:"修改后的完整 Python 脚本（基于 get_export_artifact 返回的原始脚本做最小修改；与 patches 二选一）"`
	Patches    []ExportPatch `json:"patches" jsonschema_description:"局部替换列表（推荐）：在原始脚本上按 find/replace 应用；与 code 二选一"`
	OutputFile string        `json:"outputFile" jsonschema_description:"修改后脚本产出的相对路径（缺省沿用原 outputFile）"`
	TimeoutSec int           `json:"timeoutSec" jsonschema_description:"执行超时秒数（可选，默认 300，最大 600）"`
}

// RepairExportByCodeOutput 脚本级修复结果。
type RepairExportByCodeOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	FileType    string `json:"fileType,omitempty"`
	Version     int    `json:"version,omitempty"`
	OriginalURL string `json:"originalUrl,omitempty"`
	Logs        string `json:"logs,omitempty"`
}

// artifactSourceField 返回指定源字段的文本（正文/摘要/Skill payload 在读回时重新脱敏）。
func artifactSourceField(meta *ArtifactMeta, field string, ctx context.Context, auditCtx *AuditCtx) (string, error) {
	if meta == nil {
		return "", nil
	}
	switch field {
	case "content":
		return maskArtifactText(ctx, auditCtx, meta.Content), nil
	case "sql":
		return meta.SQL, nil
	case "code":
		return meta.Code, nil
	case "skillPayload":
		return maskArtifactText(ctx, auditCtx, meta.SkillPayload), nil
	case "summary":
		return maskArtifactText(ctx, auditCtx, meta.Summary), nil
	default:
		return "", fmt.Errorf("不支持的 sourceField=%q（可选：content|sql|code|skillPayload|summary）", field)
	}
}

// clipArtifactCharts 按图表定义序列化后的字符数截断，避免图表数组撑爆回执。
func clipArtifactCharts(charts []ExcelChart, maxChars int) ([]ExcelChart, bool) {
	if len(charts) == 0 {
		return nil, false
	}
	out := make([]ExcelChart, 0, len(charts))
	for _, chart := range charts {
		candidate := append(out, chart)
		data, err := json.Marshal(candidate)
		if err != nil {
			return out, true
		}
		if len([]rune(string(data))) > maxChars {
			return out, true
		}
		out = candidate
	}
	return out, false
}

// NewGetExportArtifactFunc 创建「查询导出产物源信息」只读工具。
func NewGetExportArtifactFunc(auditCtx *AuditCtx) func(ctx context.Context, input *ExportArtifactInput) (*ExportArtifactOutput, error) {
	return func(ctx context.Context, input *ExportArtifactInput) (out *ExportArtifactOutput, err error) {
		start := time.Now()
		var auditErr error
		defer func() {
			recErr := auditErr
			if recErr == nil {
				recErr = err
			}
			recordExportAudit(ctx, auditCtx, "get_export_artifact", start, recErr)
		}()

		if input == nil || strings.TrimSpace(input.File) == "" {
			return nil, fmt.Errorf("请提供要检查的导出文件名或下载链接")
		}
		if err := guardExportSource(ctx, auditCtx); err != nil {
			return nil, err
		}
		meta, resolveErr := resolveArtifact(input.File, ownerOf(auditCtx), sessionOfCtx(ctx, auditCtx))
		if resolveErr != nil {
			auditErr = resolveErr
			return &ExportArtifactOutput{Found: false, Message: resolveErr.Error()}, nil
		}

		field := strings.ToLower(strings.TrimSpace(input.SourceField))
		if field == "" {
			field = defaultArtifactSourceField(meta)
		}
		sourceText, fieldErr := artifactSourceField(meta, field, ctx, auditCtx)
		if fieldErr != nil {
			return nil, fieldErr
		}
		page, total, pageTruncated, nextOffset := pageArtifactText(sourceText, input.SourceOffset, input.SourceLimit)
		charts, chartsTruncated := clipArtifactCharts(meta.Charts, maxArtifactSourceChars)
		out = &ExportArtifactOutput{
			Found:            true,
			Tool:             meta.Tool,
			ConnID:           meta.ConnID,
			FileName:         meta.FileName,
			DownloadURL:      meta.URL,
			FileType:         meta.FileType,
			Version:          meta.Version,
			CreatedAt:        meta.CreatedAt.Format("2006-01-02 15:04:05"),
			Title:            meta.Title,
			Subtitle:         meta.Subtitle,
			Theme:            meta.Theme,
			Charts:           charts,
			OutputFile:       meta.OutputFile,
			Skill:            meta.Skill,
			ParentURL:        meta.ParentURL,
			SourceField:      field,
			SourceOffset:     input.SourceOffset,
			SourceTotalChars: total,
			SourceNextOffset: nextOffset,
			SourceTruncated:  pageTruncated || chartsTruncated,
			ArtifactID:       meta.ArtifactID,
			SourceKind:       artifactSourceKindOf(meta),
			SourceHash:       meta.SourceHash,
			Operations:       meta.Operations,
			Validation:       meta.Validation,
		}
		switch field {
		case "content":
			out.Content = page
		case "sql":
			out.SQL = page
		case "code":
			out.Code = page
		case "skillPayload":
			out.SkillPayload = page
		case "summary":
			out.Summary = page
		}
		repairTool := "repair_export"
		if meta.Tool == "export_by_code" {
			repairTool = "repair_export_by_code"
		}
		if meta.Tool == "export_skill" {
			out.Message = fmt.Sprintf("已定位当前会话 Skill 产物 %s（%s v%d）。请基于 skillPayload 做 patch（或用 operations kind=payload 的 patch）后调用 repair_export 重跑；不要整份重生成。",
				meta.FileName, meta.Skill, meta.Version)
		} else {
			out.Message = fmt.Sprintf("已定位当前会话导出文件 %s（%s，v%d）。请基于原参数/源内容定位根因，调用 %s 做最小修复；不要重新生成整份文件。",
				meta.FileName, meta.Tool, meta.Version, repairTool)
		}
		if meta.Tool == "export_by_code" {
			out.Message += " 若当前环境未提供 repair_export_by_code，请说明原因并建议改用内置报告导出。"
		}
		if pageTruncated {
			out.Message += fmt.Sprintf(" 源字段 %s 共 %d 字符，本次返回 [%d,%d)；请用 sourceOffset=%d 继续读取，不要一次拉全文。",
				field, total, input.SourceOffset, nextOffset, out.SourceNextOffset)
		}
		if chartsTruncated {
			out.Message += " 图表定义过多已按回执上限截断，请结合 outline 定位需要的图表。"
		}
		if input.IncludeOutline {
			outline, outlineErr := buildArtifactOutline(ctx, auditCtx, meta)
			if outlineErr != nil {
				return nil, outlineErr
			}
			out.Outline = outline
			if outline != nil {
				out.Message += " 已附带结构 outline（块 ID 可直接用于 operations）；删除整章/整页/整表需用户确认。"
			}
		}
		if input.IncludeFile {
			preview, truncated, ok, previewErr := artifactTextPreview(meta.FileName)
			if previewErr != nil {
				return nil, previewErr
			}
			if ok {
				out.FilePreview = maskArtifactText(ctx, auditCtx, preview)
				out.FilePreviewTruncated = truncated
				out.Message += " 已附带产物文本预览。"
				if truncated {
					out.Message += " 预览已截断。"
				}
			} else {
				out.Message += " 该格式为二进制产物，不提供文本预览；请结合原参数与报错文本诊断。"
			}
		} else if isTextArtifact(meta.FileName) {
			out.Message += " 需要查看渲染结果时可传 includeFile=true 获取文本预览。"
		}
		return out, nil
	}
}

// NewRepairExportFunc 创建内置报告产物的增量修复工具。
// 修复复用原渲染链路：content/sql + 报告 IR，以新版本文件名写出，原文件保留。
func NewRepairExportFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource, runners ...QueryRunner) func(ctx context.Context, input *ExportRepairInput) (*ExportRepairOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportRepairInput) (out *ExportRepairOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "repair_export", start, err) }()

		if input == nil || strings.TrimSpace(input.File) == "" {
			return nil, fmt.Errorf("请提供要修复的导出文件名或下载链接")
		}
		if err := guardExportSource(ctx, auditCtx); err != nil {
			return nil, err
		}
		meta, err := resolveArtifact(input.File, ownerOf(auditCtx), sessionOfCtx(ctx, auditCtx))
		if err != nil {
			return nil, err
		}
		if err := ensureRepairConn(meta, auditCtx); err != nil {
			return nil, err
		}
		if input.ExpectedVersion > 0 && input.ExpectedVersion != meta.Version {
			return nil, fmt.Errorf("版本已变化（当前 v%d，请求 v%d），请重新调用 get_export_artifact 后再编辑", meta.Version, input.ExpectedVersion)
		}
		if len(input.Operations) > 0 && (strings.TrimSpace(input.Content) != "" || len(input.Patches) > 0) {
			return nil, fmt.Errorf("operations 与 content/patches 只能三选一：结构化编辑请只传 operations；长文局部替换请只传 patches")
		}
		if len(input.Operations) > 0 && strings.TrimSpace(input.SQL) != "" {
			return nil, fmt.Errorf("operations 与 sql 不能同时使用：正文块编辑请只传 operations；切换到 SQL 模式请不传 operations")
		}

		version := nextArtifactVersion(meta)
		newName := artifactVersionNameFor(meta, version)
		// 元数据中的正文/摘要按当前策略重新脱敏；SQL 保持原样（结果集由 runner/DataSource 脱敏）。
		metaContent := maskArtifactText(ctx, auditCtx, meta.Content)
		metaSummary := maskArtifactText(ctx, auditCtx, meta.Summary)
		switch meta.Tool {
		case "export_analysis_docx", "export_ppt", "export_html":
			if strings.TrimSpace(input.Content) != "" && (len(input.Patches) > 0 || len(input.Operations) > 0) {
				return nil, fmt.Errorf("content 与 patches/operations 只能三选一：请传完整正文、局部替换或结构化操作")
			}
			if strings.TrimSpace(input.Content) != "" && len(input.Patches) == 0 && len([]rune(metaContent)) > maxArtifactSourceChars {
				return nil, fmt.Errorf("原正文超过回执上限（%d 字符），无法确认回传正文完整；请改用 patches 局部替换，或确认整份重生成后使用原导出工具", maxArtifactSourceChars)
			}
			// 模型可能把截断预览当完整正文回传：前缀一致且更短时视为截断，强制走 patches。
			if input.Content != "" && len(input.Patches) == 0 && len(input.Content) < len(metaContent) && strings.HasPrefix(metaContent, input.Content) {
				return nil, fmt.Errorf("回传正文是原文的前缀（疑似截断），不能作为完整正文；请改用 patches 局部替换")
			}
			title := firstNonEmptyStr(input.Title, meta.Title)
			subtitle := firstNonEmptyStr(input.Subtitle, meta.Subtitle)
			theme := firstNonEmptyStr(input.Theme, meta.Theme)
			content := firstNonEmptyStr(input.Content, metaContent)
			var diff *EditDiff
			var opRecords []OperationRecord
			if len(input.Operations) > 0 {
				if strings.TrimSpace(metaContent) == "" {
					return nil, fmt.Errorf("原导出没有可编辑的正文（SQL 模式）；请先重新导出 content 模式报告，或改用 sql 参数修复")
				}
				// 块 ID 必须与 get_export_artifact 的 outline 一致：outline 使用 meta.Title
				// 解析（可能提升/移除首个 H1），因此这里也必须用原标题解析，
				// 不能把本次修改后的标题带入，否则块 ID 会错位。
				beforeDoc := report.BuildDocument(meta.Title, meta.Subtitle, metaContent)
				edited, d, recs, opErr := applyReportOperations(metaContent, beforeDoc, input.Operations, func(s string) string {
					return maskArtifactText(ctx, auditCtx, s)
				}, meta.Tool == "export_ppt")
				if opErr != nil {
					return nil, opErr
				}
				content = edited
				diff = d
				opRecords = recs
			}
			if strings.TrimSpace(input.Content) == "" && len(input.Patches) > 0 {
				if strings.TrimSpace(metaContent) == "" {
					return nil, fmt.Errorf("原导出没有可替换的正文，patches 无法应用；请直接传 content 或 sql")
				}
				patched, applied, patchErr := applyExportPatches(metaContent, input.Patches, func(s string) string {
					return maskArtifactText(ctx, auditCtx, s)
				})
				if patchErr != nil {
					return nil, patchErr
				}
				if applied == 0 {
					return nil, fmt.Errorf("patches 未产生任何替换，本次修复不成立")
				}
				content = patched
			}
			sql := firstNonEmptyStr(input.SQL, meta.SQL)
			// 模型显式只传 sql 时，视为切换到 SQL 模式，不能回填旧 content 让 sql 被忽略。
			if strings.TrimSpace(input.SQL) != "" && strings.TrimSpace(input.Content) == "" && len(input.Patches) == 0 && strings.TrimSpace(metaContent) != "" {
				content = ""
			}
			if content == metaContent && sql == meta.SQL && title == meta.Title &&
				subtitle == meta.Subtitle && theme == meta.Theme && metaContent == meta.Content {
				return nil, fmt.Errorf("本次修复参数与原导出完全一致，未包含任何修改；请先定位并修改出错部分")
			}
			if len(input.Operations) > 0 && diff != nil && diff.HashSame {
				return nil, fmt.Errorf("operations 应用后源内容未变化，本次编辑不成立；请核对块 ID 与写入内容")
			}
			reportIn := &ExportReportInput{
				Title: title, Subtitle: subtitle, Theme: theme, Content: content, SQL: sql,
			}
			data, doc, renderErr := renderReportExport(ctx, conn, ds, runner, meta.Tool, reportIn)
			if renderErr != nil {
				return nil, fmt.Errorf("按原生成链路修复失败：%w；可改用其他格式或回退内置导出", renderErr)
			}
			ext := map[string]string{"export_analysis_docx": "docx", "export_ppt": "pptx", "export_html": "html"}[meta.Tool]
			sections, charts, tables, kpis := documentStats(doc)
			if valErr := validateExportArtifact(ext, data, charts); valErr != nil {
				return nil, fmt.Errorf("修复产物校验未通过：%w；可改用其他格式或回退内置报告导出", valErr)
			}
			url, writeErr := writeExportFile(ctx, SanitizeFileName(newName, "report"), ext, data, auditCtx, &ArtifactMeta{
				Tool: meta.Tool, Title: reportIn.Title, Subtitle: reportIn.Subtitle, Theme: reportIn.Theme,
				Content: reportIn.Content, SQL: reportIn.SQL,
				Version: version, ParentURL: meta.URL, Issue: input.Issue, Changes: input.Changes,
				ArtifactID: meta.ArtifactID, SourceKind: "report_markdown", Operations: opRecords,
				Validation: &ArtifactValidation{OK: true, Checks: []string{"artifact_integrity", ext}},
			})
			if writeErr != nil {
				return nil, writeErr
			}
			log.Printf("[Tool:repair_export] 成功 - tool=%s, version=%d, url=%s\n", meta.Tool, version, url)

			msg := fmt.Sprintf("已生成修复版本（.%s，%d 个章节、%d 个图表、%d 张表格），[点击下载](%s)；原文件保留：[原文件](%s)%s",
				ext, sections, charts, tables, url, meta.URL, reportQualityHint(sections, charts, tables, kpis))
			if ext == "pptx" {
				msg = fmt.Sprintf("已生成修复版本（.pptx，%d 页、%d 个章节、%d 个图表），[点击下载](%s)；原文件保留：[原文件](%s)%s",
					slideCountFor(ext, data), sections, charts, url, meta.URL, reportQualityHint(sections, charts, tables, kpis))
			}
			msg = withRepairNotes(msg, input.Issue, input.Changes)
			if diff != nil {
				msg += "。变更：" + diff.String()
			}
			return &ExportRepairOutput{
				Message: msg, DownloadURL: url, FileType: ext,
				Version: version, OriginalURL: meta.URL,
				SectionCount: sections, ChartCount: charts, TableCount: tables,
				SlideCount: slideCountFor(ext, data), Diff: diffString(diff),
				Validation: &ArtifactValidation{OK: true, Checks: []string{"artifact_integrity", ext}},
			}, nil

		case "export_excel", "export_excel_with_chart":
			if len(input.Patches) > 0 {
				return nil, fmt.Errorf("patches 仅用于报告正文或 Python 脚本；Excel 请使用 operations（cell/row/column/sheet）或 sql/summary/charts")
			}
			if len(input.Operations) > 0 {
				if strings.TrimSpace(input.SQL) != "" || strings.TrimSpace(input.Summary) != "" || len(input.Charts) > 0 {
					return nil, fmt.Errorf("Excel 结构化编辑（operations）不能与 sql/summary/charts 同时使用：请只传其中一种修改方式")
				}
				data, diff, recs, opErr := applyWorkbookOperations(meta.FileName, input.Operations, func(s string) string {
					return maskArtifactText(ctx, auditCtx, s)
				})
				if opErr != nil {
					return nil, opErr
				}
				if diff != nil && diff.Added+diff.Removed+diff.Changed == 0 {
					return nil, fmt.Errorf("operations 应用后工作簿未发生变化，本次编辑不成立")
				}
				if valErr := validateExportArtifact("xlsx", data, 0); valErr != nil {
					return nil, fmt.Errorf("修复产物校验未通过：%w", valErr)
				}
				url, writeErr := writeExportFile(ctx, SanitizeFileName(newName, "chart"), "xlsx", data, auditCtx, &ArtifactMeta{
					Tool: meta.Tool, Title: meta.Title, SQL: meta.SQL, Charts: meta.Charts, Summary: meta.Summary,
					Version: version, ParentURL: meta.URL, Issue: input.Issue, Changes: input.Changes,
					ArtifactID: meta.ArtifactID, SourceKind: "workbook", WorkbookEdited: true, Operations: recs,
					Validation: &ArtifactValidation{OK: true, Checks: []string{"ooxml_xml"}},
				})
				if writeErr != nil {
					return nil, writeErr
				}
				msg := fmt.Sprintf("已生成修复版本（.xlsx，%s），[点击下载](%s)；原文件保留：[原文件](%s)", diff.String(), url, meta.URL)
				msg = withRepairNotes(msg, input.Issue, input.Changes)
				return &ExportRepairOutput{
					Message: msg, DownloadURL: url, FileType: "xlsx",
					Version: version, OriginalURL: meta.URL, Diff: diff.String(),
					Validation: &ArtifactValidation{OK: true, Checks: []string{"ooxml_xml"}},
				}, nil
			}
			if meta.WorkbookEdited {
				return nil, fmt.Errorf("该 Excel 已含手工编辑（workbookEdited），参数级重渲染会覆盖人工内容；请改用 cell/row/column/sheet 操作，或重新导出")
			}
			if meta.Tool == "export_excel" && (len(input.Charts) > 0 || strings.TrimSpace(input.Summary) != "") {
				return nil, fmt.Errorf("该文件为纯数据 Excel，不支持 charts/summary；如需图表或结论请改用 export_excel_with_chart 重新导出")
			}
			sql := firstNonEmptyStr(input.SQL, meta.SQL)
			if strings.TrimSpace(sql) == "" {
				return nil, fmt.Errorf("原导出缺少 SQL 且本次未提供修复后的 SQL，无法增量修复 Excel")
			}
			title := firstNonEmptyStr(input.Title, meta.Title)
			if title == "" {
				title = "数据导出"
			}
			summary := firstNonEmptyStr(input.Summary, metaSummary)
			if len(input.Charts) == 0 && summary == metaSummary && sql == meta.SQL && title == meta.Title && metaSummary == meta.Summary {
				return nil, fmt.Errorf("本次修复参数与原导出完全一致，未包含任何修改；请先定位并修改出错部分")
			}
			qr, queryErr := runExportQuery(ctx, conn, runner, sql)
			if queryErr != nil {
				return nil, queryErr
			}
			var (
				data       []byte
				dataCharts []report.DataChart
				charts     []ExcelChart
			)
			if meta.Tool == "export_excel" {
				data, err = renderExcelData(title, maskArtifactText(ctx, auditCtx, sql), qr)
			} else {
				charts = input.Charts
				if len(charts) == 0 {
					charts = meta.Charts
				}
				renderedCharts := DataChartsFromExcelCharts(qr, charts)
				if len(charts) > 0 && len(renderedCharts) == 0 {
					return nil, fmt.Errorf("图表字段与查询结果列不匹配（可用列：%v），修复未生成图表；请修正 charts 后再试", qr.Columns)
				}
				data, err = renderExcelAnalysis(title, maskArtifactText(ctx, auditCtx, sql), qr, renderedCharts, buildExcelSummary(title, qr, summary))
				dataCharts = renderedCharts
			}
			if err != nil {
				return nil, fmt.Errorf("按原生成链路修复 Excel 失败：%w；可改用其他格式或回退内置导出", err)
			}
			ext := "xlsx"
			expectedCharts := len(dataCharts)
			if valErr := validateExportArtifact(ext, data, expectedCharts); valErr != nil {
				return nil, fmt.Errorf("修复产物校验未通过：%w；可改用其他格式或回退内置报告导出", valErr)
			}
			url, writeErr := writeExportFile(ctx, SanitizeFileName(newName, "chart"), ext, data, auditCtx, &ArtifactMeta{
				Tool: meta.Tool, Title: title, SQL: sql, Charts: charts, Summary: summary,
				Version: version, ParentURL: meta.URL, Issue: input.Issue, Changes: input.Changes,
				ArtifactID: meta.ArtifactID, SourceKind: "workbook",
				Validation: &ArtifactValidation{OK: true, Checks: []string{"ooxml_xml"}},
			})
			if writeErr != nil {
				return nil, writeErr
			}
			log.Printf("[Tool:repair_export] 成功 - tool=%s, version=%d, url=%s\n", meta.Tool, version, url)

			msg := fmt.Sprintf("已生成修复版本（.xlsx，%d 行 × %d 列数据），[点击下载](%s)；原文件保留：[原文件](%s)",
				len(qr.Data), len(qr.Columns), url, meta.URL)
			if meta.Tool == "export_excel_with_chart" {
				msg = fmt.Sprintf("已生成修复版本（.xlsx，%d 行 × %d 列数据、%d 个图表），[点击下载](%s)；原文件保留：[原文件](%s)",
					len(qr.Data), len(qr.Columns), expectedCharts, url, meta.URL)
			}
			msg = withRepairNotes(msg, input.Issue, input.Changes)
			return &ExportRepairOutput{
				Message: msg, DownloadURL: url, FileType: ext,
				Version: version, OriginalURL: meta.URL,
				RowCount: len(qr.Data), ChartCount: expectedCharts,
				Validation: &ArtifactValidation{OK: true, Checks: []string{"ooxml_xml"}},
			}, nil

		case "export_skill":
			_, ext, specErr := skillScriptSpec(meta.Skill)
			if specErr != nil {
				return nil, specErr
			}
			if len(input.Operations) > 0 {
				if strings.TrimSpace(input.Content) != "" {
					return nil, fmt.Errorf("Skill 产物编辑不支持 content；请用 operations(kind=payload) 的 patch 或直接传 patches")
				}
				for _, op := range input.Operations {
					if strings.ToLower(strings.TrimSpace(op.Kind)) != "payload" || op.Patch == nil {
						return nil, fmt.Errorf("Skill 产物仅支持 kind=payload 的 patch 操作")
					}
					input.Patches = append(input.Patches, *op.Patch)
				}
			}
			if strings.TrimSpace(input.Content) != "" && len(input.Patches) > 0 {
				return nil, fmt.Errorf("content 与 patches 只能二选一")
			}
			payload := meta.SkillPayload
			if strings.TrimSpace(input.Content) != "" {
				payload = input.Content
			}
			maskFn := func(s string) string {
				return maskArtifactText(ctx, auditCtx, s)
			}
			if len(input.Patches) > 0 {
				patched, applied, patchErr := applyExportPatches(payload, input.Patches, maskFn)
				if patchErr != nil {
					return nil, patchErr
				}
				if applied == 0 {
					return nil, fmt.Errorf("patches 未产生任何替换，本次修复不成立")
				}
				payload = patched
			}
			if strings.TrimSpace(payload) == strings.TrimSpace(meta.SkillPayload) {
				return nil, fmt.Errorf("payload 与原始内容一致，未包含任何修改；请先定位并修改出错部分")
			}
			if !json.Valid([]byte(payload)) {
				return nil, fmt.Errorf("payload 不是合法 JSON，无法重跑技能")
			}
			// 旧产物 payload 可能包含历史敏感数据：重跑前统一递归脱敏所有字符串叶子，
			// 避免通过 patch 之外的原文再次落盘。
			if auditCtx != nil && auditCtx.Mask != nil {
				maskedPayload, maskErr := MaskJSONStrings(payload, maskFn)
				if maskErr != nil {
					return nil, fmt.Errorf("Skill payload 脱敏失败：%w", maskErr)
				}
				payload = maskedPayload
			}
			skillFileName := SanitizeFileName(newName, "skill")
			data, logs, runErr := runSkillExport(ctx, auditCtx, meta.Skill, payload, skillFileName, 0)
			if runErr != nil {
				return nil, fmt.Errorf("按原技能重新执行失败：%w；可改用 Go 原生导出重新生成", runErr)
			}
			if valErr := validateExportArtifact(ext, data, 0); valErr != nil {
				return nil, fmt.Errorf("修复产物校验未通过：%w", valErr)
			}
			diff := &EditDiff{Changed: 1}
			url, writeErr := writeExportFile(ctx, skillFileName, ext, data, auditCtx, &ArtifactMeta{
				Tool: "export_skill", Skill: meta.Skill, SkillPayload: normalizeSkillPayload(payload, skillFileName, ext),
				Version: version, ParentURL: meta.URL, Issue: input.Issue, Changes: input.Changes,
				ArtifactID: meta.ArtifactID, SourceKind: "skill", Operations: []OperationRecord{{Op: "patch", Kind: "payload", Detail: "Skill payload 修改"}},
				Validation: &ArtifactValidation{OK: true, Checks: []string{"artifact_integrity", ext}},
			})
			if writeErr != nil {
				return nil, writeErr
			}
			log.Printf("[Tool:repair_export] 成功 - tool=export_skill, version=%d, url=%s\n", version, url)
			msg := fmt.Sprintf("已生成修复版本（.%s，%s），[点击下载](%s)；原文件保留：[原文件](%s)", ext, diff.String(), url, meta.URL)
			msg = withRepairNotes(msg, input.Issue, input.Changes)
			if strings.TrimSpace(logs) != "" {
				msg += "。技能日志：" + truncateForRecord(logs)
			}
			return &ExportRepairOutput{
				Message: msg, DownloadURL: url, FileType: ext,
				Version: version, OriginalURL: meta.URL, Diff: diff.String(),
				SlideCount: slideCountFor(ext, data),
				Validation: &ArtifactValidation{OK: true, Checks: []string{"artifact_integrity", ext}},
			}, nil

		default:
			return nil, fmt.Errorf("该文件由 %s 生成，请改用 repair_export_by_code 修复自定义格式", meta.Tool)
		}
	}
}

// NewRepairExportByCodeFunc 创建 export_by_code 产物的脚本级修复工具。
// 复用 runCodegen：仍受只读助手、OS 隔离、队列、凭据拦截与输出目录限制。
func NewRepairExportByCodeFunc(conn *sqlx.DB, dbInfo *CodegenDBInfo, auditCtx *AuditCtx) func(ctx context.Context, input *RepairExportByCodeInput) (*RepairExportByCodeOutput, error) {
	return func(ctx context.Context, input *RepairExportByCodeInput) (out *RepairExportByCodeOutput, err error) {
		start := time.Now()
		var auditErr error
		defer func() {
			recErr := auditErr
			if recErr == nil {
				recErr = err
			}
			recordExportAudit(ctx, auditCtx, "repair_export_by_code", start, recErr)
		}()

		if input == nil || strings.TrimSpace(input.File) == "" {
			return nil, fmt.Errorf("请提供要修复的导出文件名或下载链接")
		}
		if err := guardExportSource(ctx, auditCtx); err != nil {
			return nil, err
		}
		meta, resolveErr := resolveArtifact(input.File, ownerOf(auditCtx), sessionOfCtx(ctx, auditCtx))
		if resolveErr != nil {
			return nil, resolveErr
		}
		if err := ensureRepairConn(meta, auditCtx); err != nil {
			return nil, err
		}
		if meta.Tool != "export_by_code" {
			return nil, fmt.Errorf("该文件由 %s 生成，请改用 repair_export 修复内置报告", meta.Tool)
		}
		if strings.TrimSpace(input.Code) != "" && len(input.Patches) > 0 {
			return nil, fmt.Errorf("code 与 patches 只能二选一：请传入完整脚本，或只传局部替换")
		}
		code := strings.TrimSpace(input.Code)
		if code == "" && len(input.Patches) > 0 {
			if strings.TrimSpace(meta.Code) == "" {
				return nil, fmt.Errorf("原产物没有可替换的 Python 脚本，patches 无法应用；请直接传 code")
			}
			patched, applied, patchErr := applyExportPatches(meta.Code, input.Patches, nil)
			if patchErr != nil {
				return nil, patchErr
			}
			if applied == 0 {
				return nil, fmt.Errorf("patches 未产生任何替换，本次修复不成立")
			}
			code = strings.TrimSpace(patched)
		}
		if code == "" {
			return nil, fmt.Errorf("请提供修改后的完整 Python 脚本或 patches（可先用 get_export_artifact 获取原始脚本）")
		}
		if meta.Code != "" && code == strings.TrimSpace(meta.Code) {
			return nil, fmt.Errorf("脚本与原始脚本一致，未包含修复改动；请先定位并修改出错部分")
		}
		outputFile := firstNonEmptyStr(input.OutputFile, meta.OutputFile)
		if outputFile == "" {
			return nil, fmt.Errorf("缺少 outputFile，且原产物没有记录可沿用的产出路径")
		}

		result, runErr := runCodegen(ctx, conn, dbInfo, auditCtx, &ExportByCodeInput{
			Code: code, OutputFile: outputFile, TimeoutSec: input.TimeoutSec,
		})
		if runErr != nil {
			return nil, runErr
		}
		if result.Failure != "" {
			auditErr = fmt.Errorf("%s", result.Failure)
			return &RepairExportByCodeOutput{Message: result.Failure, Logs: result.Logs}, nil
		}
		if valErr := validateExportArtifact(result.Ext, result.Data, 0); valErr != nil {
			return nil, fmt.Errorf("修复产物校验未通过：%w；可改用其他格式或回退内置报告导出", valErr)
		}

		version := nextArtifactVersion(meta)
		newName := artifactVersionNameFor(meta, version)
		url, writeErr := writeExportFile(ctx, SanitizeFileName(newName, "export"), result.Ext, result.Data, auditCtx, &ArtifactMeta{
			Tool: "export_by_code", Code: code, OutputFile: outputFile,
			Version: version, ParentURL: meta.URL, Issue: input.Issue, Changes: input.Changes,
		})
		if writeErr != nil {
			return nil, writeErr
		}
		log.Printf("[Tool:repair_export_by_code] 成功 - version=%d, url=%s\n", version, url)

		msg := fmt.Sprintf("已生成修复版本（.%s，%.1f KB），[点击下载](%s)；原文件保留：[原文件](%s)",
			result.Ext, float64(len(result.Data))/1024, url, meta.URL)
		msg = withRepairNotes(msg, input.Issue, input.Changes)
		return &RepairExportByCodeOutput{
			Message: msg, DownloadURL: url, FileType: result.Ext,
			Version: version, OriginalURL: meta.URL, Logs: result.Logs,
		}, nil
	}
}

// ensureRepairConn 要求在原导出连接下修复，避免切换连接后套用错误的权限/数据源。
// 文件模式（无连接）导出的报告跳过该校验。
func ensureRepairConn(meta *ArtifactMeta, auditCtx *AuditCtx) error {
	if meta == nil || meta.ConnID == "" {
		return nil
	}
	current := ""
	if auditCtx != nil {
		current = auditCtx.ConnID
	}
	if current == "" {
		return fmt.Errorf("该文件由连接 %s 导出，当前未选择数据库连接；请切回原连接后再修复", meta.ConnID)
	}
	if meta.ConnID != current {
		return fmt.Errorf("该文件由连接 %s 导出，当前连接为 %s；修复需切回原连接，以避免套用错误的数据范围", meta.ConnID, current)
	}
	return nil
}

func slideCountFor(ext string, data []byte) int {
	if ext == "pptx" {
		return countPPTXSlides(data)
	}
	return 0
}

// withRepairNotes 把问题现象与修改点补充到修复回执，便于模型直接向用户说明。
func withRepairNotes(msg, issue, changes string) string {
	if s := strings.TrimSpace(issue); s != "" {
		msg += "。问题：" + s
	}
	if s := strings.TrimSpace(changes); s != "" {
		msg += "。修改点：" + s
	}
	return msg
}

// applyExportPatches 在原始源内容上应用局部替换（最小修复入口）。
// mask 为 nil 时不对替换文本脱敏（仅用于代码补丁）；正文补丁应传入当前脱敏函数。
func applyExportPatches(src string, patches []ExportPatch, mask func(string) string) (string, int, error) {
	if len(patches) == 0 {
		return src, 0, nil
	}
	out := src
	total := 0
	for i, p := range patches {
		if p.Find == "" {
			return "", 0, fmt.Errorf("第 %d 个 patch 的 find 不能为空", i+1)
		}
		if !strings.Contains(out, p.Find) {
			return "", 0, fmt.Errorf("第 %d 个 patch 未命中原文，请确认 find 与当前源内容一致（区分大小写）", i+1)
		}
		replace := p.Replace
		if mask != nil {
			replace = mask(replace)
		}
		if p.All {
			n := strings.Count(out, p.Find)
			out = strings.ReplaceAll(out, p.Find, replace)
			total += n
			continue
		}
		out = strings.Replace(out, p.Find, replace, 1)
		total++
	}
	return out, total, nil
}
