package database

import (
	"websql/internal/dialect"
	"websql/internal/logger"
	"reflect"

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

func GetResultRows(dbtype string, rows *sqlx.Rows) []map[string]any {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	logger.PanicErrf("获取字段类型失败", err)

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

		rows.Scan(valuesPoints...)

		row := make(map[string]any)

		for i := range values {
			key := columns[i]
			colType := colTypeMap[key]
			actualVal := dereferenceValue(values[i])
			row[key] = *dialect.ConvertColHandler[dbtype](&colType, &actualVal, true)
		}
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
}

func GetResultRowsForExport(dbtype string, rows *sqlx.Rows) []map[string]any {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	logger.PanicErrf("获取字段类型失败", err)

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

		rows.Scan(valuesPoints...)

		row := make(map[string]any)

		for i := range values {
			key := columns[i]
			colType := colTypeMap[key]
			actualVal := dereferenceValue(values[i])
			row[key] = *dialect.ConvertColHandler[dbtype](&colType, &actualVal, false)
		}
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
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