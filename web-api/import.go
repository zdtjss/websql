package webapi

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"go-web/config"
	"go-web/utils"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
)

func ImportCsv(w http.ResponseWriter, r *http.Request) {
	log.Println("收到新增/更新请求")
	r.ParseMultipartForm(30 * 1024 * 1024)
	table := r.Form.Get("table")
	operType := r.Form.Get("opt")
	start, _ := strconv.Atoi(r.Form.Get("start"))
	file, _, err := r.FormFile("file")
	utils.Panicln(err)
	defer file.Close()

	reader := csv.NewReader(file)

	dbParam := &config.DBParam{Env: r.Form.Get("env"), Db: r.Form.Get("db")}

	tx, _ := config.GetConn(dbParam).Begin()
	defer tx.Rollback()

	count := -1
	maxLines := 100

	//存所有行的内容totalValues
	totalValues := make([][]string, maxLines)

	columns, err := reader.Read()
	utils.Panicln(err)
	for i := 2; i < start; i++ {
		_, _ = reader.Read()
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		count++
		utils.Panicln(err)
		totalValues[count] = record
		if count+1 >= maxLines {
			if strings.EqualFold(operType, "insert") {
				insertToDb(table, columns, totalValues, tx)
			} else {
				updateToDb(table, columns, totalValues, tx, dbParam)
			}
			count = -1
		}
	}

	if count != -1 {
		if strings.EqualFold(operType, "insert") {
			insertToDb(table, columns, totalValues[:count+1], tx)
		} else {
			updateToDb(table, columns, totalValues[:count+1], tx, dbParam)
		}
	}

	if err = tx.Commit(); err != nil {
		log.Println("导入失败")
	} else {
		if strings.EqualFold(operType, "insert") {
			log.Println("导入完成")
		} else {
			log.Println("更新完成")
		}
	}
}

func insertToDb(table string, columns []string, data [][]string, tx *sql.Tx) {

	if len(data) == 0 {
		return
	}

	sql := bytes.Buffer{}

	sql.WriteString("insert into ")
	sql.WriteString(table)
	sql.WriteString(" (")
	sql.WriteString(strings.Join(columns, ","))
	sql.WriteString(") values (")
	plc := strings.Repeat("?,", len(columns))
	sql.Write([]byte(plc[:len(plc)-1]))
	sql.WriteString(" )")

	log.Println(sql.String())

	stmt, err := tx.Prepare(sql.String())
	utils.Panicln(err)
	anyVal := make([]interface{}, len(columns))
	for _, val := range data {
		for i, v := range val {
			anyVal[i] = v
		}
		_, err = stmt.Exec(anyVal...)
		utils.Panicln(err)
	}

}

func updateToDb(table string, columns []string, data [][]string, tx *sql.Tx, dbParam *config.DBParam) {

	if len(data) == 0 {
		return
	}

	keys := queryKey(dbParam, table, tx)
	keyIdx := keyIdx(keys, columns)

	sql := bytes.Buffer{}
	where := bytes.Buffer{}
	where.WriteString(" where ")

	sql.WriteString("update ")
	sql.WriteString(table)
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
	utils.Panicln(err)

	valCount := -1
	paramCount := -1

	anyVal := make([]interface{}, len(columns))
	for _, val := range data {
		for i, v := range val {
			if !slices.Contains(keyIdx, i) {
				valCount++
				anyVal[valCount] = v
			} else {
				paramCount++
				anyVal[len(columns)-len(keys)+paramCount] = v
			}
		}

		valCount = -1
		paramCount = -1

		_, err = stmt.Exec(anyVal...)
		utils.Panicln(err)
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

func queryKey(dbParam *config.DBParam, table string, tx *sql.Tx) []string {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare("select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'")
	utils.Println(err)
	rs, err2 := stmt.Query(getSchema(dbParam), table)
	utils.Println(err2)
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	return primaryKeys
}
