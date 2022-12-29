package webapi

import (
	"go-web/utils"
	"net/http"
)

func ListTable(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tables := queryTableInfo(r.Form.Get("connId"), r.Form.Get("schema"))
	utils.WriteJson(w, tables)
}

func queryTableInfo(key, schema string) []*Table {
	tables := make([]*Table, 0)
	stmt, err := getConn(key).Prepare("SELECT TABLE_NAME,table_comment FROM information_schema.tables WHERE table_schema = ?")
	utils.Println(err)
	rs, err2 := stmt.Query(schema)
	utils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		table := &Table{name, comment}
		tables = append(tables, table)
	}
	return tables
}

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}
