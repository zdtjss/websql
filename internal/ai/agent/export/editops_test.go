package export

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/export/report"

	"github.com/xuri/excelize/v2"
)

func findBlockID(t *testing.T, blocks []report.Block, kind report.BlockKind, contains string) string {
	t.Helper()
	for _, b := range blocks {
		if b.Kind != kind {
			continue
		}
		text := b.Text + b.Title
		if b.Table != nil {
			text += strings.Join(b.Table.Headers, ",")
		}
		if contains == "" || strings.Contains(text, contains) {
			return b.ID
		}
	}
	t.Fatalf("未找到 %s 块（包含 %q）", kind, contains)
	return ""
}

// TestApplyReportOperations_BlockSectionAndTable 覆盖块替换/插入、章节删除、表格行删除。
func TestApplyReportOperations_BlockSectionAndTable(t *testing.T) {
	content := "# 报告\n\n" +
		"## 第一节\n\n正文A\n\n" +
		"| 列A | 列B |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n\n" +
		"## 第二节\n\n正文B\n"
	doc := report.BuildDocument("报告", "", content)
	paraID := findBlockID(t, doc.Blocks, report.KindParagraph, "正文A")
	tableID := findBlockID(t, doc.Blocks, report.KindTable, "列A")
	sectionID := findBlockID(t, doc.Blocks, report.KindHeading, "第二节")

	out, diff, recs, err := applyReportOperations(content, doc, []EditOperation{
		{Op: "replace", Kind: "block", ID: paraID, Value: "正文A-修改"},
		{Op: "insert", Kind: "block", ID: paraID, Position: "after", Value: "补充分析"},
		{Op: "delete", Kind: "table_row", ID: tableID, Index: 1},
		{Op: "delete", Kind: "section", ID: sectionID},
	}, nil, false)
	if err != nil {
		t.Fatalf("应用操作失败：%v", err)
	}
	if !strings.Contains(out, "正文A-修改") || !strings.Contains(out, "补充分析") {
		t.Errorf("替换/插入未生效：\n%s", out)
	}
	if strings.Contains(out, "第二节") || strings.Contains(out, "正文B") {
		t.Errorf("章节删除未生效：\n%s", out)
	}
	if strings.Contains(out, "| 1 | 2 |") {
		t.Errorf("表格行删除未生效：\n%s", out)
	}
	if !strings.Contains(out, "| 3 | 4 |") {
		t.Errorf("表格其余行应保留：\n%s", out)
	}
	if diff == nil || diff.HashSame || diff.Added+diff.Removed+diff.Changed == 0 {
		t.Errorf("diff 异常：%+v", diff)
	}
	if len(recs) < 4 {
		t.Errorf("操作记录不完整：%+v", recs)
	}
	if len(report.ParseMarkdown(out)) == 0 {
		t.Error("编辑后的 Markdown 无法解析")
	}
}

// TestApplyReportOperations_NoChange 同值替换应判定为无变更。
func TestApplyReportOperations_NoChange(t *testing.T) {
	content := "# 报告\n\n## 一\n\n正文\n"
	doc := report.BuildDocument("报告", "", content)
	id := findBlockID(t, doc.Blocks, report.KindParagraph, "正文")
	_, diff, _, err := applyReportOperations(content, doc, []EditOperation{
		{Op: "replace", Kind: "block", ID: id, Value: "正文"},
	}, nil, false)
	if err != nil {
		t.Fatalf("应用操作失败：%v", err)
	}
	if !diff.HashSame {
		t.Errorf("同值替换应判定为无变更：%+v", diff)
	}
}

// TestApplyReportOperations_MasksValues 编辑值必须经过脱敏函数。
func TestApplyReportOperations_MasksValues(t *testing.T) {
	content := "# 报告\n\n## 一\n\n正文\n"
	doc := report.BuildDocument("报告", "", content)
	id := findBlockID(t, doc.Blocks, report.KindParagraph, "正文")
	out, _, _, err := applyReportOperations(content, doc, []EditOperation{
		{Op: "replace", Kind: "block", ID: id, Value: "手机号 13812345678"},
	}, func(s string) string { return strings.ReplaceAll(s, "13812345678", "138****5678") }, false)
	if err != nil {
		t.Fatalf("应用操作失败：%v", err)
	}
	if strings.Contains(out, "13812345678") || !strings.Contains(out, "138****5678") {
		t.Errorf("编辑值未脱敏：%s", out)
	}
}

// TestRepairExport_OperationsEndToEnd 走完整工具链：outline → operations → 新版本 → 操作记录。
func TestRepairExport_OperationsEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: "# 销售分析\n\n## 摘要\n\n上半年稳步增长。\n",
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	got, err := NewGetExportArtifactFunc(audit)(ctx, &ExportArtifactInput{File: out.DownloadURL, IncludeOutline: true})
	if err != nil || !got.Found {
		t.Fatalf("读取 outline 失败：%v", err)
	}
	outline, ok := got.Outline.(*report.Outline)
	if !ok || outline == nil {
		t.Fatalf("outline 类型异常：%T", got.Outline)
	}
	var paraID string
	for _, sec := range outline.Sections {
		for _, b := range sec.Blocks {
			if b.Kind == string(report.KindParagraph) {
				paraID = b.ID
			}
		}
	}
	if paraID == "" {
		t.Fatalf("outline 未返回段落块：%+v", outline)
	}

	repaired, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, ExpectedVersion: 1,
		Operations: []EditOperation{{Op: "replace", Kind: "block", ID: paraID, Value: "上半年稳步增长（修订）。"}},
	})
	if err != nil {
		t.Fatalf("结构化编辑失败：%v", err)
	}
	if repaired.Version != 2 || repaired.Diff == "" {
		t.Errorf("回执异常：%+v", repaired)
	}
	file := strings.TrimPrefix(repaired.DownloadURL, "/exports/")
	meta, err := loadArtifactMetaByFileName(file)
	if err != nil {
		t.Fatalf("读取新版本元数据失败：%v", err)
	}
	if !strings.Contains(meta.Content, "修订") {
		t.Errorf("新版本源内容未包含修改：%s", meta.Content)
	}
	if len(meta.Operations) == 0 || meta.ArtifactID == "" || meta.SourceHash == "" {
		t.Errorf("产物 v2 字段缺失：%+v", meta)
	}

	// 过期版本必须拒绝
	if _, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: repaired.DownloadURL, ExpectedVersion: 1,
		Operations: []EditOperation{{Op: "replace", Kind: "block", ID: paraID, Value: "x"}},
	}); err == nil || !strings.Contains(err.Error(), "版本已变化") {
		t.Fatalf("过期版本应被拒绝：%v", err)
	}
}

// TestApplyBlockMove 移动块后应保持目标顺序且不丢内容。
func TestApplyBlockMove(t *testing.T) {
	content := "# 报告\n\n## 一\n\n第一段\n\n## 二\n\n第二段\n"
	doc := report.BuildDocument("报告", "", content)
	first := findBlockID(t, doc.Blocks, report.KindParagraph, "第一段")
	second := findBlockID(t, doc.Blocks, report.KindParagraph, "第二段")
	out, _, _, err := applyReportOperations(content, doc, []EditOperation{
		{Op: "move", Kind: "block", ID: first, Position: "after", Value: second},
	}, nil, false)
	if err != nil {
		t.Fatalf("移动块失败：%v", err)
	}
	if strings.Index(out, "第二段") > strings.Index(out, "第一段") {
		t.Errorf("移动后顺序不正确：\n%s", out)
	}
	if strings.Count(out, "第一段") != 1 || strings.Count(out, "第二段") != 1 {
		t.Errorf("移动不应丢失或重复内容：\n%s", out)
	}
}

// TestApplySlideDelete 页级删除应翻译为对应源块的删除。
func TestApplySlideDelete(t *testing.T) {
	content := "# 报告\n\n" +
		"# 第一部分\n\n第一部分正文\n\n" +
		"# 第二部分\n\n第二部分正文\n"
	doc := report.BuildDocument("报告", "", content)
	slides := report.PlanSlides(doc)
	target := report.SlideRef{}
	for _, s := range slides {
		if s.Kind == "section" && s.StartBlock >= 0 {
			target = s
			break
		}
	}
	if target.Index == 0 {
		t.Fatalf("未找到章节页：%+v", slides)
	}
	sectionTitle := strings.TrimSpace(doc.Blocks[target.StartBlock].Text)
	out, _, _, err := applyReportOperations(content, doc, []EditOperation{
		{Op: "delete", Kind: "slide", Index: target.Index},
	}, nil, true)
	if err != nil {
		t.Fatalf("删除页失败：%v", err)
	}
	if strings.Contains(out, sectionTitle) {
		t.Errorf("页对应章节未删除：\n%s", out)
	}
	if !strings.Contains(out, "第二部分") {
		t.Errorf("其余章节应保留：\n%s", out)
	}
}

// TestWorkbookOperationsAndConflict 单元格编辑应原地生效；已编辑工作簿拒绝参数级重渲染。
func TestWorkbookOperationsAndConflict(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	out, err := NewExportExcelFunc(conn, audit, newTestRunner(conn))(ctx, &ExportExcelInput{
		SQL: "SELECT 月份, 金额 FROM sales ORDER BY 月份", FileName: "工作簿编辑",
	})
	if err != nil {
		t.Fatalf("Excel 导出失败：%v", err)
	}
	repaired, err := NewRepairExportFunc(conn, audit, nil, newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL,
		Operations: []EditOperation{
			{Op: "replace", Kind: "cell", Sheet: "数据明细", Ref: "B2", Value: "999"},
			{Op: "insert", Kind: "sheet", Value: "补充说明"},
		},
	})
	if err != nil {
		t.Fatalf("工作簿编辑失败：%v", err)
	}
	if repaired.Version != 2 || repaired.Diff == "" {
		t.Errorf("工作簿编辑回执异常：%+v", repaired)
	}
	file := strings.TrimPrefix(repaired.DownloadURL, "/exports/")
	f, err := excelize.OpenFile(filepath.Join("exports", file))
	if err != nil {
		t.Fatalf("打开修复后的 xlsx 失败：%v", err)
	}
	defer f.Close()
	if v, _ := f.GetCellValue("数据明细", "B2"); v != "999" {
		t.Errorf("单元格未更新：%q", v)
	}
	if idx, err := f.GetSheetIndex("补充说明"); err != nil || idx < 0 {
		t.Errorf("新增工作表失败：idx=%d err=%v", idx, err)
	}

	if _, err := NewRepairExportFunc(conn, audit, nil, newTestRunner(conn))(ctx, &ExportRepairInput{
		File: repaired.DownloadURL, SQL: "SELECT 月份, 金额 FROM sales WHERE 1=1",
	}); err == nil || !strings.Contains(err.Error(), "手工编辑") {
		t.Fatalf("应拒绝覆盖手工编辑：%v", err)
	}

	if _, err := NewRepairExportFunc(conn, audit, nil, newTestRunner(conn))(ctx, &ExportRepairInput{
		File: repaired.DownloadURL, ExpectedVersion: 1,
		Operations: []EditOperation{{Op: "replace", Kind: "cell", Sheet: "数据明细", Ref: "B2", Value: "1"}},
	}); err == nil || !strings.Contains(err.Error(), "版本已变化") {
		t.Fatalf("过期版本应被拒绝：%v", err)
	}
}

// TestOperationApprovalDescription 删除类操作必须可被审核清单识别。
func TestOperationApprovalDescription(t *testing.T) {
	ops := []EditOperation{
		{Op: "replace", Kind: "cell", Sheet: "S", Ref: "A1", Value: "1"},
		{Op: "delete", Kind: "section", ID: "blk_0003_heading"},
		{Op: "delete", Kind: "slide", Index: 4},
	}
	if !OperationNeedsApproval(ops) {
		t.Fatal("含删除操作应需要确认")
	}
	desc := DescribeDestructiveOperations(ops)
	if !strings.Contains(desc, "section") || !strings.Contains(desc, "第4页") {
		t.Errorf("审核清单不完整：%s", desc)
	}
	if OperationNeedsApproval([]EditOperation{{Op: "replace", Kind: "cell", Sheet: "S", Ref: "A1"}}) {
		t.Error("纯修改不应触发确认")
	}
}
