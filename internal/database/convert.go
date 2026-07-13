package database

import (
	"log"

	"websql/internal/dialect"
	"websql/internal/logger"

	"github.com/jmoiron/sqlx"
)

func QueryColType(schema, table string, tx *sqlx.Tx) map[string]string {
	colTypeMap := make(map[string]string, 0)
	stmt, err := tx.Prepare(dialect.SQL_DIALECT[tx.DriverName()]["QueryColType"])
	logger.PrintErr(err)
	rs, err2 := stmt.Query(schema, table)
	logger.PrintErr(err2)
	var colName, colType string
	for rs.Next() {
		colType = ""
		if err := rs.Scan(&colName, &colType); err != nil {
			log.Printf("扫描行失败: %v", err)
			continue
		}
		colTypeMap[colName] = colType
	}
	return colTypeMap
}

func ConvertCol(dbType, colType *string, val *any, overSign bool) *any {
	return dialect.ConvertColHandler[*dbType](colType, val, overSign)
}

func ParseVal(dbType, colType *string, val *string) *any {
	return dialect.ParseValHandler[*dbType](colType, val)
}