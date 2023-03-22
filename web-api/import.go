package webapi

import (
	"bytes"
	"errors"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"log"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
	"golang.org/x/exp/slices"
)

func ImportXlsx(w http.ResponseWriter, r *http.Request) {
	log.Println("收到新增/更新请求")
	r.ParseMultipartForm(30 * 1024 * 1024)

	authorization := r.Header.Get("Authorization")

	connId := r.Form.Get("connId")
	schema := r.Form.Get("schema")
	table := r.Form.Get("table")
	operType := r.Form.Get("optType")
	// start, _ := strconv.Atoi(r.Form.Get("start"))

	file, fileHeader, err := r.FormFile("file")
	logutils.PanicErr(err)
	defer file.Close()

	if fileHeader.Filename[strings.Index(fileHeader.Filename, "-")+1:len(fileHeader.Filename)-5] != table {
		logutils.PanicErr(errors.New("表名不匹配（文件名中横线后为表名）"))
	}

	excel, err := excelize.OpenReader(file)
	logutils.PanicErr(err)

	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	tx, _ := admin.GetConn(connId, authorization).Beginx()
	defer tx.Rollback()

	rows, err := excel.Rows("Sheet1")
	logutils.PanicErr(err)
	defer rows.Close()

	header := make([]string, 0)
	if rows.Next() {
		row, err := rows.Columns()
		logutils.PanicErr(err)
		header = append(header, row...)
	}

	// 忽略第二行
	rows.Next()

	count := -1
	maxLines := 100

	//存所有行的内容totalValues
	totalValues := make([][]string, maxLines)

	for rows.Next() {
		count++
		logutils.PanicErr(err)
		columns := []string{}
		row, err := rows.Columns()
		logutils.PanicErr(err)
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
		logutils.PanicErrf("导入失败,", err)
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

func insertToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) {

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
	if tx.DriverName() == "oracle" {
		plc := make([]string, len(columns))
		for idx := 0; idx < len(columns); idx++ {
			plc[idx] = ":" + fmt.Sprint(idx+1)
		}
		sql.WriteString(strings.Join(plc, ","))
	} else {
		plc := strings.Repeat("?,", len(columns))
		sql.Write([]byte(plc[:len(plc)-1]))
	}
	sql.WriteString(" )")

	log.Println(sql.String())

	stmt, err := tx.Tx.Prepare(sql.String())
	logutils.PanicErr(err)

	colTypeMap := admin.QueryColType(schema, table, tx)
	driverName := tx.DriverName()
	anyVal := make([]interface{}, len(columns))
	for _, val := range data {
		for i := range val {
			colType := colTypeMap[columns[i]]
			anyVal[i] = *admin.ParseVal(&driverName, &colType, &val[i])
		}
		_, err = stmt.Exec(anyVal...)
		logutils.PanicErr(err)
	}

}

func updateToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) {

	if len(data) == 0 {
		return
	}

	keys := admin.QueryPrimaryKey(schema, table, tx)
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

	stmt, err := tx.Tx.Prepare(realSql)
	logutils.PanicErr(err)

	valCount := -1
	paramCount := -1

	colTypeMap := admin.QueryColType(schema, table, tx)

	driverName := tx.DriverName()
	anyVal := make([]any, len(columns))
	for _, val := range data {
		for i := range val {
			colType := colTypeMap[columns[i]]
			if !slices.Contains(keyIdx, i) {
				valCount++
				anyVal[valCount] = *admin.ParseVal(&driverName, &colType, &val[i])
			} else {
				paramCount++
				anyVal[len(columns)-len(keys)+paramCount] = *admin.ParseVal(&driverName, &colType, &val[i])
			}
		}

		valCount = -1
		paramCount = -1
		log.Println(anyVal...)
		_, err = stmt.Exec(anyVal...)
		logutils.PanicErr(err)
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
