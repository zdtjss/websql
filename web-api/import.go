package webapi

import (
	"bytes"
	"database/sql"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/exp/slices"
)

func ImportXlsx(w http.ResponseWriter, r *http.Request) {
	log.Println("收到新增/更新请求")
	r.ParseMultipartForm(30 * 1024 * 1024)

	connId := utils.AtoUint64(r.Form.Get("connId"))
	schema := r.Form.Get("schema")
	table := r.Form.Get("table")
	operType := r.Form.Get("optType")
	// start, _ := strconv.Atoi(r.Form.Get("start"))

	file, _, err := r.FormFile("file")
	logutils.Panicln(err)
	defer file.Close()

	excel, err := excelize.OpenReader(file)
	logutils.Panicln(err)

	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	tx, _ := admin.GetConn(connId).Begin()
	defer tx.Rollback()

	rows, err := excel.Rows(table)
	logutils.Panicln(err)
	defer rows.Close()

	header := make([]string, 0)
	if rows.Next() {
		row, err := rows.Columns()
		logutils.Panicln(err)
		header = append(header, row...)
	}

	count := -1
	maxLines := 100

	//存所有行的内容totalValues
	totalValues := make([][]string, maxLines)

	for rows.Next() {
		count++
		logutils.Panicln(err)
		columns := []string{}
		row, err := rows.Columns()
		logutils.Panicln(err)
		columns = append(columns, row...)
		totalValues[count] = columns
		if count+1 >= maxLines {
			if strings.EqualFold(operType, "insert") {
				insertToDb(schema, table, header, totalValues, tx)
			} else {
				updateToDb(schema, table, header, totalValues, tx)
			}
			count = -1
		}
	}

	if count != -1 {
		if strings.EqualFold(operType, "insert") {
			insertToDb(schema, table, header, totalValues[:count+1], tx)
		} else {
			updateToDb(schema, table, header, totalValues[:count+1], tx)
		}
	}

	if err = tx.Commit(); err != nil {
		logutils.Panicf("导入失败, %x", err)
	} else {
		if strings.EqualFold(operType, "insert") {
			log.Println("导入完成")
			utils.WriteJson(w, "导入完成")
		} else {
			log.Println("更新完成")
			utils.WriteJson(w, "更新完成")
		}
	}
}

func insertToDb(schema, table string, columns []string, data [][]string, tx *sql.Tx) {

	if len(data) == 0 {
		return
	}

	sql := bytes.Buffer{}

	sql.WriteString("insert into ")
	sql.WriteString(schema)
	sql.WriteString(".")
	sql.WriteString(table)
	sql.WriteString(" (")
	sql.WriteString(strings.Join(columns, ","))
	sql.WriteString(") values (")
	plc := strings.Repeat("?,", len(columns))
	sql.Write([]byte(plc[:len(plc)-1]))
	sql.WriteString(" )")

	log.Println(sql.String())

	stmt, err := tx.Prepare(sql.String())
	logutils.Panicln(err)

	colTypeMap := queryColType(schema, table, tx)

	anyVal := make([]interface{}, len(columns))
	for _, val := range data {
		for i, v := range val {
			anyVal[i] = parseVal(colTypeMap[columns[i]], v)
		}
		_, err = stmt.Exec(anyVal...)
		logutils.Panicln(err)
	}

}

func updateToDb(schema, table string, columns []string, data [][]string, tx *sql.Tx) {

	if len(data) == 0 {
		return
	}

	keys := queryKey(schema, table, tx)
	keyIdx := keyIdx(keys, columns)

	sql := bytes.Buffer{}
	where := bytes.Buffer{}
	where.WriteString(" where ")

	sql.WriteString("update ")
	sql.WriteString(schema + "." + table)
	sql.WriteString(" set ")

	for i, val := range columns {
		if !slices.Contains(keyIdx, i) {
			sql.WriteString(val)
			sql.WriteString(" = ?,")
		} else {
			where.WriteString(val)
			where.WriteString(" = ? and ")
		}
	}

	realSql := strings.TrimRight(sql.String(), ",") + strings.TrimRight(where.String(), " and ")

	log.Println(realSql)

	stmt, err := tx.Prepare(realSql)
	logutils.Panicln(err)

	valCount := -1
	paramCount := -1

	colTypeMap := queryColType(schema, table, tx)

	anyVal := make([]any, len(columns))
	for _, val := range data {
		for i, v := range val {
			if !slices.Contains(keyIdx, i) {
				valCount++
				anyVal[valCount] = parseVal(colTypeMap[columns[i]], v)
			} else {
				paramCount++
				anyVal[len(columns)-len(keys)+paramCount] = parseVal(colTypeMap[columns[i]], v)
			}
		}

		valCount = -1
		paramCount = -1
		log.Println(anyVal...)
		_, err = stmt.Exec(anyVal...)
		logutils.Panicln(err)
	}

}

func keyIdx(keys, columns []string) []int {
	keyIdx := make([]int, 0)
	for i := 0; i < len(columns); i++ {
		if slices.Contains(keys, columns[i]) {
			keyIdx = append(keyIdx, i)
		}
	}
	return keyIdx
}

func queryKey(schema, table string, tx *sql.Tx) []string {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare("select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'")
	logutils.Println(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.Println(err2)
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	if len(primaryKeys) == 0 {
		msg := fmt.Sprintf("%s 没有主键", table)
		log.Println(msg)
		panic(msg)
	}
	return primaryKeys
}

func queryColType(schema, table string, tx *sql.Tx) map[string]string {
	colTypeMap := make(map[string]string, 0)
	stmt, err := tx.Prepare("select column_name,DATA_TYPE from information_schema.columns where TABLE_SCHEMA = ? and table_name = ?")
	logutils.Println(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.Println(err2)
	var colName, colType string
	for rs.Next() {
		rs.Scan(&colName, &colType)
		colTypeMap[colName] = colType
	}
	return colTypeMap
}

func parseVal(colType string, val string) (retVal any) {
	if slices.Contains([]string{"float", "double", "datetime", "decimal", "int", "bigint", "smallint", "tinyint", "bit"}, colType) && val == "" {
		return nil
	}
	switch colType {
	case "float", "double", "decimal":
		f, err := strconv.ParseFloat(val, 64)
		logutils.Panicln(err)
		retVal = f
	case "int", "bigint", "smallint", "tinyint":
		f, err := strconv.ParseInt(val, 10, 64)
		logutils.Panicln(err)
		retVal = f
	case "bit":
		f, err := strconv.ParseBool(val)
		logutils.Panicln(err)
		retVal = f
	default:
		retVal = val
	}
	return retVal
}
