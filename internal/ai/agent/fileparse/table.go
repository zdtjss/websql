package fileparse

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

func init() {
	Register("go-csv", 0, nil, []string{".csv"}, ParserFunc(parseCSV))
	Register("go-xlsx", 0, nil, []string{".xlsx", ".xls"}, ParserFunc(parseXLSX))
}

// parseCSV 解析 CSV（先做编码探测，兼容 Excel 导出的 GBK CSV）。
func parseCSV(_ context.Context, src Source) (*Result, error) {
	raw, err := os.ReadFile(src.Path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，请重试")
	}
	text, _ := decodeText(raw)

	reader := csv.NewReader(strings.NewReader(text))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允许行列数不一致
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析 CSV 失败，请检查文件格式")
	}
	return buildTabularResult(rows)
}

// parseXLSX 解析 Excel（取第一个工作表）。
//
// 说明：excelize 仅支持 OOXML（.xlsx/.xlsm）。旧版二进制 .xls 虽在扩展名白名单内
// （保持既有兼容），但解析时给出明确指引而不是含混的失败。
func parseXLSX(_ context.Context, src Source) (*Result, error) {
	if NormalizeExt(src.Ext) == ".xls" {
		return nil, errors.New("暂不支持旧版 .xls 二进制格式，请在 Excel 中另存为 .xlsx 后重新上传")
	}

	f, err := excelize.OpenFile(src.Path)
	if err != nil {
		return nil, fmt.Errorf("读取 Excel 文件失败，请检查文件格式")
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("Excel 文件中没有工作表")
	}
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败，请检查文件内容")
	}
	return buildTabularResult(rows)
}

// buildTabularResult 把二维行数据（首行表头）转为解析结果。
//
// 与既有实现保持一致的语义：跳过空行、按列数补齐、至少需要表头 + 一行数据。
func buildTabularResult(allRows [][]string) (*Result, error) {
	if len(allRows) < 2 {
		return nil, errors.New("文件数据不足：至少需要表头行和一行数据")
	}

	columns := allRows[0]
	data := make([][]string, 0, len(allRows)-1)
	truncated := false
	for _, row := range allRows[1:] {
		if !rowHasValue(row) {
			continue
		}
		if len(data) >= MaxTableRows {
			truncated = true
			break
		}
		padded := make([]string, len(columns))
		copy(padded, row)
		data = append(data, padded)
	}

	var notice string
	if truncated {
		notice = fmt.Sprintf("数据行数超过上限，仅保留前 %d 行", MaxTableRows)
	}

	return &Result{
		Kind:      KindTable,
		Columns:   columns,
		Rows:      data,
		TotalRows: len(data),
		Notice:    notice,
	}, nil
}

// rowHasValue 判断一行中是否存在非空单元格
func rowHasValue(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return true
		}
	}
	return false
}
