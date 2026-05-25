package database

import (
	"fmt"
	"reflect"

	"websql/internal/dialect"
	"websql/internal/logger"

	"github.com/jmoiron/sqlx"
	"slices"
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
			converted := dialect.ConvertColHandler[dbtype](&colType, &actualVal, true)
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

func GetResultRowsForExport(dbtype string, rows *sqlx.Rows) ([]map[string]any, error) {
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
			converted := dialect.ConvertColHandler[dbtype](&colType, &actualVal, false)
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
