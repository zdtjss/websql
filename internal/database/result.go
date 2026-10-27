package database

import (
	"fmt"
	"reflect"

	"websql/internal/dialect"
	"websql/internal/logger"

	"slices"

	"github.com/jmoiron/sqlx"
)

func dereferenceValue(v any) any {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		return rv.Elem().Interface()
	}
	return v
}

func GetResultRows(dbtype string, rows *sqlx.Rows) ([]map[string]any, error) {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	if err != nil {
		logger.PrintErrf("获取字段类型失败", err)
		return nil, fmt.Errorf("获取字段类型失败: %w", err)
	}

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	columns, _ := rows.Columns()
	count := len(columns)

	for rows.Next() {
		values, valuesPoints := make([]any, count), make([]any, count)

		for i := range count {
			valuesPoints[i] = &values[i]
		}

		if err := rows.Scan(valuesPoints...); err != nil {
			logger.PrintErrf("数据行扫描失败", err)
			return nil, fmt.Errorf("数据行扫描失败: %w", err)
		}

		row := make(map[string]any)

		for i := range values {
			key := columns[i]
			colType := colTypeMap[key]
			actualVal := dereferenceValue(values[i])
			// 统一走 dialect 注册表：方言未注册时安全降级，不会因 nil 函数 panic
			converted := dialect.ConvertValue(dbtype, &colType, &actualVal, true)
			if converted != nil {
				row[key] = *converted
			} else {
				row[key] = nil
			}
		}
		dataMaps = append(dataMaps, row)
	}
	return dataMaps, nil
}

// GetResultRowsForExport 读取全部结果行并转换为 map 列表。
// limit > 0 时最多读取 limit 行（用于导出场景的行数上限保护，防止大表导出打爆内存）。
func GetResultRowsForExport(dbtype string, rows *sqlx.Rows, limit int) ([]map[string]any, error) {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	if err != nil {
		logger.PrintErrf("获取字段类型失败", err)
		return nil, fmt.Errorf("获取字段类型失败: %w", err)
	}

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	columns, _ := rows.Columns()
	count := len(columns)

	for rows.Next() {
		if limit > 0 && len(dataMaps) >= limit {
			break
		}
		values, valuesPoints := make([]any, count), make([]any, count)

		for i := range count {
			valuesPoints[i] = &values[i]
		}

		if err := rows.Scan(valuesPoints...); err != nil {
			logger.PrintErrf("数据行扫描失败", err)
			return nil, fmt.Errorf("数据行扫描失败: %w", err)
		}

		row := make(map[string]any)

		for i := range values {
			key := columns[i]
			colType := colTypeMap[key]
			actualVal := dereferenceValue(values[i])
			converted := dialect.ConvertValue(dbtype, &colType, &actualVal, false)
			if converted != nil {
				row[key] = *converted
			} else {
				row[key] = nil
			}
		}
		dataMaps = append(dataMaps, row)
	}
	return dataMaps, nil
}

func KeyIdx(keys, columns []string) []int {
	keyIdx := make([]int, 0)
	for i := range columns {
		if slices.Contains(keys, columns[i]) {
			keyIdx = append(keyIdx, i)
		}
	}
	return keyIdx
}
