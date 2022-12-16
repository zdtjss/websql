package webapi

import (
	"go-web/config"
	"go-web/utils"
	"net/http"
	"strings"
)

func ListTable(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	dbParam := &config.DBParam{Env: r.Form.Get("env"), Db: r.Form.Get("db")}
	tables := queryTableInfo(dbParam)
	utils.WriteJson(w, tables)
}

func queryTableInfo(dbParam *config.DBParam) []*Table {
	tables := make([]*Table, 0)
	stmt, err := config.GetConn(dbParam).Prepare("SELECT TABLE_NAME,table_comment FROM information_schema.tables WHERE table_schema = ?")
	utils.Println(err)
	rs, err2 := stmt.Query(getSchema(dbParam))
	utils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		table := &Table{name, comment}
		tables = append(tables, table)
	}
	return tables
}

func getSchema(dbParam *config.DBParam) string {
	dsn := config.Cfg.DB[dbParam.Db][dbParam.Env]
	start := strings.LastIndex(dsn, ")/")
	end := strings.Index(dsn[start:], "?")
	if end != -1 {
		return dsn[start+2 : start+end]
	}
	return dsn[start:]
}

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}
