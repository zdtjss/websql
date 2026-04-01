package db

import (
	"go-web/logutils"
	"reflect"

	"github.com/jmoiron/sqlx"
	"golang.org/x/exp/slices"
)

// dereferenceValue 如果 v 是指针类型，则解引用返回实际值
func dereferenceValue(v interface{}) interface{} {
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
	logutils.PanicErrf("获取字段类型失败", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	// 1. 查询到的数据列名、返回值
	columns, _ := rows.Columns() //列名
	count := len(columns)

	// 2. 遍历 Rows 读取每一行
	for rows.Next() {
		values, valuesPoints := make([]any, count), make([]any, count)

		// for i, v := range values { // 这种写法获取不到地址
		// 	valuesPoints[i] = &v
		// }
		for i := 0; i < count; i++ {
			valuesPoints[i] = &values[i]
		}

		// 2.1 数据库中读取出每一行数据
		rows.Scan(valuesPoints...) //将所有内容读取进 values

		// 2.2 相当于准备接收数据的结构体 Product
		row := make(map[string]any)

		// 2.3 将读取到的数据填充到 product
		for i := range values { // val 是每个列对应的值
			key := columns[i] //列名
			colType := colTypeMap[key]
			// 如果 values[i] 是指针类型，需要解引用
			actualVal := dereferenceValue(values[i])
			// 列名与值对应，注意要对 ConvertColHandler 的返回值解引用
			row[key] = *ConvertColHandler[dbtype](&colType, &actualVal, true)
		}
		// 将 product 归到集合中
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
}

// GetResultRowsForExport 导出 Excel 时使用，overSign=false 避免大数字被转换为 s:格式
func GetResultRowsForExport(dbtype string, rows *sqlx.Rows) []map[string]any {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	logutils.PanicErrf("获取字段类型失败", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	// 1. 查询到的数据列名、返回值
	columns, _ := rows.Columns() //列名
	count := len(columns)

	// 2. 遍历 Rows 读取每一行
	for rows.Next() {
		values, valuesPoints := make([]any, count), make([]any, count)

		for i := 0; i < count; i++ {
			valuesPoints[i] = &values[i]
		}

		// 2.1 数据库中读取出每一行数据
		rows.Scan(valuesPoints...) //将所有内容读取进 values

		// 2.2 相当于准备接收数据的结构体 Product
		row := make(map[string]any)

		// 2.3 将读取到的数据填充到 product
		for i := range values { // val 是每个列对应的值
			key := columns[i] //列名
			colType := colTypeMap[key]
			// 导出时 overSign=false，避免大数字被转换为 s:格式
			// 如果 values[i] 是指针类型，需要解引用
			actualVal := dereferenceValue(values[i])
			// 注意要对 ConvertColHandler 的返回值解引用
			row[key] = *ConvertColHandler[dbtype](&colType, &actualVal, false)
		}
		// 将 product 归到集合中
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
}

func KeyIdx(keys, columns []string) []int {
	keyIdx := make([]int, 0)
	for i := 0; i < len(columns); i++ {
		if slices.Contains(keys, columns[i]) {
			keyIdx = append(keyIdx, i)
		}
	}
	return keyIdx
}
