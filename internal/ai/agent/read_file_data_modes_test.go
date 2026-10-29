package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/uploadstore"
)

func TestReadTableWindowSheetSelection(t *testing.T) {
	res := &fileparse.Result{
		Kind:      fileparse.KindTable,
		Columns:   []string{"a"},
		Rows:      [][]string{{"1"}},
		TotalRows: 1,
		Sheets: []fileparse.Sheet{
			{Name: "Sheet1", Columns: []string{"a"}, Rows: [][]string{{"1"}}, TotalRows: 1},
			{Name: "明细", Columns: []string{"b", "c"}, Rows: [][]string{{"2", "3"}}, TotalRows: 1},
		},
	}
	out := readTableWindow(res, &ReadFileDataInput{FileID: "x", Sheet: "明细"})
	if len(out.Columns) != 2 || out.Columns[0] != "b" {
		t.Fatalf("应按 sheet 选择列，实际 %+v", out.Columns)
	}
	if len(out.Rows) != 1 || len(out.Rows[0]) != 2 {
		t.Fatalf("应按 sheet 选择行，实际 %+v", out.Rows)
	}
	if len(out.Sheets) != 2 {
		t.Fatalf("应返回全部工作表摘要，实际 %+v", out.Sheets)
	}
}

func TestReadTextWindowPageAndSectionUnits(t *testing.T) {
	res := &fileparse.Result{
		Kind:       fileparse.KindDoc,
		Text:       "# A\n内容A\n## B\n内容B\n",
		TotalChars: 17,
		Sections: []fileparse.Section{
			{Title: "A", Level: 1, StartChar: 0, EndChar: 8},
			{Title: "B", Level: 2, StartChar: 8, EndChar: 17},
		},
		PageList: []fileparse.Page{
			{Number: 1, StartChar: 0, EndChar: 8},
			{Number: 2, StartChar: 8, EndChar: 17},
		},
	}
	section := readTextWindow(res, &ReadFileDataInput{Unit: "section", Offset: 2, Limit: 1})
	if section.SectionTitle != "B" || !strings.Contains(section.Text, "内容B") {
		t.Fatalf("section 读取不正确：%+v", section)
	}
	page := readTextWindow(res, &ReadFileDataInput{Unit: "page", Offset: 2, Limit: 1})
	if page.PageStart != 2 || page.PageEnd != 2 || !strings.Contains(page.Text, "内容B") {
		t.Fatalf("page 读取不正确：%+v", page)
	}
}

func TestReadTableWindowReportsActualTotalRows(t *testing.T) {
	rows := make([][]string, 10)
	for i := range rows {
		rows[i] = []string{"x"}
	}
	res := &fileparse.Result{Kind: fileparse.KindTable, Columns: []string{"a"}, Rows: rows, TotalRows: 1000}
	out := readTableWindow(res, &ReadFileDataInput{FileID: "x", Offset: 5, Limit: 10})
	if out.TotalRows != 1000 {
		t.Fatalf("TotalRows 应保留截断前总数，实际 %d", out.TotalRows)
	}
	if !strings.Contains(out.Notice, "上限") {
		t.Fatalf("应提示解析上限截断，实际 %q", out.Notice)
	}
}

func TestReadFileDataModes(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "doc.md")
	textContent := "# 标题一\n内容 A\n\n## 标题二\n需要查找的关键词\n更多内容\n"
	if err := os.WriteFile(textPath, []byte(textContent), 0o600); err != nil {
		t.Fatalf("写入文本失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-modes-text", UserID: "u1", DiskPath: textPath, FileName: "doc.md",
		Ext: ".md", Size: int64(len(textContent)), Kind: "text", FileType: "markdown",
	})
	defer RemoveUploadedFile("f-modes-text")

	csvPath := filepath.Join(dir, "table.csv")
	csvContent := "name,amount\na,10\nb,20\nc,30\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0o600); err != nil {
		t.Fatalf("写入 CSV 失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-modes-table", UserID: "u1", DiskPath: csvPath, FileName: "table.csv",
		Ext: ".csv", Size: int64(len(csvContent)), Kind: "table", FileType: "csv",
	})
	defer RemoveUploadedFile("f-modes-table")

	fn := NewReadFileDataFunc("u1")
	ctx := context.Background()

	outline, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-text", Mode: "outline"})
	if err != nil {
		t.Fatalf("文本 outline 失败：%v", err)
	}
	if len(outline.Sections) < 2 {
		t.Fatalf("期望至少 2 个章节，实际 %+v", outline.Sections)
	}

	search, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-text", Mode: "search", Query: "关键词"})
	if err != nil {
		t.Fatalf("文本 search 失败：%v", err)
	}
	if len(search.Matches) == 0 || search.Matches[0].Line == 0 {
		t.Fatalf("文本 search 未返回有效命中：%+v", search.Matches)
	}

	lines, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-text", Unit: "lines", Offset: 1, Limit: 2})
	if err != nil {
		t.Fatalf("文本按行读取失败：%v", err)
	}
	if lines.LineStart != 1 || lines.LineEnd != 2 {
		t.Fatalf("行号范围不正确：%+v", lines)
	}

	textProfile, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-text", Mode: "profile"})
	if err != nil {
		t.Fatalf("文本 profile 失败：%v", err)
	}
	if textProfile.Profile == nil {
		t.Fatal("文本 profile 不应为空")
	}

	tableOutline, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-table", Mode: "outline"})
	if err != nil {
		t.Fatalf("表格 outline 失败：%v", err)
	}
	if len(tableOutline.Sheets) == 0 || len(tableOutline.Sheets[0].Columns) != 2 {
		t.Fatalf("表格 outline 结构不正确：%+v", tableOutline.Sheets)
	}

	tableSearch, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-table", Mode: "search", Query: "b"})
	if err != nil {
		t.Fatalf("表格 search 失败：%v", err)
	}
	if len(tableSearch.Matches) == 0 || tableSearch.Matches[0].Row == 0 {
		t.Fatalf("表格 search 未返回有效命中：%+v", tableSearch.Matches)
	}

	tableProfile, err := fn(ctx, &ReadFileDataInput{FileID: "f-modes-table", Mode: "profile"})
	if err != nil {
		t.Fatalf("表格 profile 失败：%v", err)
	}
	if tableProfile.Profile == nil || len(tableProfile.Profile.Columns) != 2 {
		t.Fatalf("表格 profile 结构不正确：%+v", tableProfile.Profile)
	}
}
