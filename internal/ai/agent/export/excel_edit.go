package export

// Excel 工作簿级编辑（单元格/行列/工作表）。
//
// 设计：不引入 Workbook IR、不重跑 SQL——直接打开当前版本的 xlsx，用 excelize
// 在副本上做最小修改后另存新版本，未修改的样式/公式/图表保持原样。
// 需要重跑数据的参数级修改仍走原有 SQL 重渲染链路（但已手工编辑的文件会被拒绝）。

import (
	"archive/zip"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// SheetOutline 工作表概要。
type SheetOutline struct {
	Name    string `json:"name"`
	Rows    int    `json:"rows"`
	Columns int    `json:"columns"`
	Charts  int    `json:"charts,omitempty"`
	Merges  int    `json:"merges,omitempty"`
}

// WorkbookOutline Excel 产物结构视图。
type WorkbookOutline struct {
	Sheets []SheetOutline `json:"sheets"`
	Charts int            `json:"charts"`
}

var sheetDimensionRe = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

// buildWorkbookOutline 读取 xlsx 产物，返回 Sheet/行列/图表概要（只读）。
func buildWorkbookOutline(fileName string) (*WorkbookOutline, error) {
	path := Path(fileName)
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Excel 产物失败：%w", err)
	}
	defer f.Close()
	out := &WorkbookOutline{}
	for _, sheet := range f.GetSheetList() {
		so := SheetOutline{Name: sheet}
		if dim, err := f.GetSheetDimension(sheet); err == nil && dim != "" {
			so.Rows, so.Columns = parseSheetDimension(dim)
		}
		if merges, err := f.GetMergeCells(sheet); err == nil {
			so.Merges = len(merges)
		}
		out.Sheets = append(out.Sheets, so)
	}
	out.Charts = countWorkbookChartParts(path)
	return out, nil
}

// countWorkbookChartParts 统计 xlsx 内的图表部件（excelize 未提供公开的 GetCharts）。
func countWorkbookChartParts(path string) int {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "xl/charts/chart") && strings.HasSuffix(name, ".xml") {
			n++
		}
	}
	return n
}

// parseSheetDimension 解析 "A1:H101" 形式的工作表维度。
func parseSheetDimension(dim string) (rows, cols int) {
	parts := strings.Split(dim, ":")
	if len(parts) != 2 {
		return 0, 0
	}
	colLetters := ""
	if m := sheetDimensionRe.FindStringSubmatch(strings.ToUpper(parts[1])); m != nil {
		colLetters = m[1]
		if n, err := strconv.Atoi(m[2]); err == nil {
			rows = n
		}
	}
	cols = 0
	for _, r := range colLetters {
		cols = cols*26 + int(r-'A'+1)
	}
	return rows, cols
}

// applyWorkbookOperations 在当前 xlsx 产物的副本上应用工作簿操作，返回新文件字节。
func applyWorkbookOperations(fileName string, ops []EditOperation, mask func(string) string) ([]byte, *EditDiff, []OperationRecord, error) {
	if len(ops) == 0 {
		return nil, nil, nil, fmt.Errorf("operations 不能为空")
	}
	path := Path(fileName)
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取 Excel 产物失败：%w", err)
	}
	defer f.Close()

	diff := &EditDiff{}
	records := make([]OperationRecord, 0, len(ops))
	maskValue := func(s string) string {
		if mask == nil {
			return s
		}
		return mask(s)
	}
	requireSheet := func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("Excel 操作必须提供 sheet")
		}
		idx, err := f.GetSheetIndex(name)
		if err != nil || idx < 0 {
			return "", fmt.Errorf("工作表 %q 不存在（可用：%s）", name, strings.Join(f.GetSheetList(), "、"))
		}
		return name, nil
	}

	for _, op := range ops {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		rec := OperationRecord{Op: opName, Kind: kind, Target: op.Sheet}
		switch kind {
		case "cell":
			sheet, err := requireSheet(op.Sheet)
			if err != nil {
				return nil, nil, nil, err
			}
			ref := strings.TrimSpace(op.Ref)
			if ref == "" {
				return nil, nil, nil, fmt.Errorf("cell 操作必须提供 ref（如 B7）")
			}
			if opName != "replace" && opName != "update" && opName != "insert" {
				return nil, nil, nil, fmt.Errorf("cell 仅支持 replace/update，收到 %q", op.Op)
			}
			if err := f.SetCellValue(sheet, ref, maskValue(op.Value)); err != nil {
				return nil, nil, nil, fmt.Errorf("写入单元格 %s!%s 失败：%w", sheet, ref, err)
			}
			rec.Target = sheet + "!" + ref
			diff.Changed++
		case "row":
			sheet, err := requireSheet(op.Sheet)
			if err != nil {
				return nil, nil, nil, err
			}
			if op.Index < 1 {
				return nil, nil, nil, fmt.Errorf("row 操作必须提供 1-based 行号")
			}
			switch opName {
			case "insert":
				if err := f.InsertRows(sheet, op.Index, 1); err != nil {
					return nil, nil, nil, fmt.Errorf("插入行失败：%w", err)
				}
				diff.Added++
			case "delete":
				if err := f.RemoveRow(sheet, op.Index); err != nil {
					return nil, nil, nil, fmt.Errorf("删除行失败：%w", err)
				}
				diff.Removed++
			default:
				return nil, nil, nil, fmt.Errorf("row 仅支持 insert|delete，收到 %q", op.Op)
			}
			rec.Target = fmt.Sprintf("%s 第%d行", sheet, op.Index)
		case "column":
			sheet, err := requireSheet(op.Sheet)
			if err != nil {
				return nil, nil, nil, err
			}
			if op.Index < 1 {
				return nil, nil, nil, fmt.Errorf("column 操作必须提供 1-based 列号")
			}
			switch opName {
			case "insert":
				col, err := excelize.ColumnNumberToName(op.Index)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("列号 %d 非法：%w", op.Index, err)
				}
				if err := f.InsertCols(sheet, col, 1); err != nil {
					return nil, nil, nil, fmt.Errorf("插入列失败：%w", err)
				}
				diff.Added++
			case "delete":
				col, err := excelize.ColumnNumberToName(op.Index)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("列号 %d 非法：%w", op.Index, err)
				}
				if err := f.RemoveCol(sheet, col); err != nil {
					return nil, nil, nil, fmt.Errorf("删除列失败：%w", err)
				}
				diff.Removed++
			default:
				return nil, nil, nil, fmt.Errorf("column 仅支持 insert|delete，收到 %q", op.Op)
			}
			rec.Target = fmt.Sprintf("%s 第%d列", sheet, op.Index)
		case "sheet":
			switch opName {
			case "add", "insert":
				name := strings.TrimSpace(op.Value)
				if name == "" {
					return nil, nil, nil, fmt.Errorf("新增工作表必须提供 value（新名称）")
				}
				if _, err := f.NewSheet(name); err != nil {
					return nil, nil, nil, fmt.Errorf("新增工作表失败：%w", err)
				}
				diff.Added++
				rec.Target = name
			case "delete":
				sheet, err := requireSheet(op.Sheet)
				if err != nil {
					return nil, nil, nil, err
				}
				if len(f.GetSheetList()) <= 1 {
					return nil, nil, nil, fmt.Errorf("不能删除唯一的工作表")
				}
				if err := f.DeleteSheet(sheet); err != nil {
					return nil, nil, nil, fmt.Errorf("删除工作表失败：%w", err)
				}
				diff.Removed++
				rec.Target = sheet
			case "rename", "replace":
				sheet, err := requireSheet(op.Sheet)
				if err != nil {
					return nil, nil, nil, err
				}
				name := strings.TrimSpace(op.Value)
				if name == "" {
					return nil, nil, nil, fmt.Errorf("重命名工作表必须提供 value（新名称）")
				}
				if err := f.SetSheetName(sheet, name); err != nil {
					return nil, nil, nil, fmt.Errorf("重命名工作表失败：%w", err)
				}
				diff.Changed++
				rec.Target = sheet + "→" + name
			default:
				return nil, nil, nil, fmt.Errorf("sheet 仅支持 add|delete|rename，收到 %q", op.Op)
			}
		default:
			return nil, nil, nil, fmt.Errorf("Excel 不支持的操作类型 %q（支持 cell|row|column|sheet）", op.Kind)
		}
		records = append(records, rec)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("保存 Excel 失败：%w", err)
	}
	return buf.Bytes(), diff, records, nil
}
