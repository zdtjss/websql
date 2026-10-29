package fileparse

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseXLSXAllSheets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.xlsx")
	f := excelize.NewFile()
	defer f.Close()
	first := f.GetSheetName(0)
	_ = f.SetSheetRow(first, "A1", &[]any{"name", "amount"})
	_ = f.SetSheetRow(first, "A2", &[]any{"a", 10})
	second, err := f.NewSheet("明细")
	if err != nil {
		t.Fatalf("新建 sheet 失败：%v", err)
	}
	_ = f.SetSheetRow("明细", "A1", &[]any{"id", "value"})
	_ = f.SetSheetRow("明细", "A2", &[]any{1, "x"})
	_ = f.SetSheetRow("明细", "A3", &[]any{2, "y"})
	f.SetActiveSheet(second)
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("保存 xlsx 失败：%v", err)
	}

	res, err := Parse(context.Background(), Source{Path: path, FileName: "multi.xlsx", Ext: ".xlsx"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(res.Sheets) != 2 {
		t.Fatalf("期望 2 个工作表，实际 %d：%+v", len(res.Sheets), res.Sheets)
	}
	if len(res.Sheets[1].Columns) != 2 || res.Sheets[1].TotalRows != 2 {
		t.Fatalf("第二个工作表结构不正确：%+v", res.Sheets[1])
	}
	if len(res.Columns) != 2 || res.TotalRows != 1 {
		t.Fatalf("兼容字段应指向第一个工作表：columns=%v total=%d", res.Columns, res.TotalRows)
	}
}
