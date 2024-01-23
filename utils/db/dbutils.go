package db

import (
	"go-web/logutils"

	"github.com/jmoiron/sqlx"
)

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

	// 2. 遍历Rows读取每一行
	for rows.Next() {
		values, valuesPoints := make([]any, count), make([]any, count)

		// for i, v := range values { // 这种写法获取不到地址
		// 	valuesPoints[i] = &v
		// }
		for i := 0; i < count; i++ {
			valuesPoints[i] = &values[i]
		}

		// 2.1 数据库中读取出每一行数据
		rows.Scan(valuesPoints...) //将所有内容读取进values

		// 2.2 相当于准备接收数据的结构体Product
		row := make(map[string]any)

		// 2.3 将读取到的数据填充到product
		for i := range values { // val是每个列对应的值
			key := columns[i] //列名
			colType := colTypeMap[key]
			// 列名与值对应
			row[key] = ConvertColHandler[dbtype](&colType, &values[i], true)
		}
		// 将product归到集合中
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
}
