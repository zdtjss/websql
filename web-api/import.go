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
)

func ImportCsv(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(30 * 1024 * 1024)
	table := r.Form.Get("table")
	start, _ := strconv.Atoi(r.Form.Get("start"))
	file, _, err := r.FormFile("file")
	utils.Panicln(err)

	reader := csv.NewReader(file)

	dbParam := &config.DBParam{Env: r.Form.Get("env"), Db: r.Form.Get("db")}

	tx, _ := config.GetConn(dbParam).Begin()

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
			insertToDb(table, columns, totalValues, tx)
			count = -1
		}
	}

	if count != -1 {
		insertToDb(table, columns, totalValues[:count+1], tx)
	}

	tx.Commit()
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
