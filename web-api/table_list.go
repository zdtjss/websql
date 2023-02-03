package webapi

import (
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"net/http"
)

func ListTable(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	r.ParseForm()
	tables := queryTableInfo(utils.AtoUint64(r.FormValue("connId")), r.Form.Get("schema"), authorization)
	utils.WriteJson(w, tables)
}

func queryTableInfo(key uint64, schema, authorization string) []*Table {
	tables := make([]*Table, 0)
	stmt, err := admin.GetConn(key, authorization).Prepare("SELECT TABLE_NAME,table_comment FROM information_schema.tables WHERE table_schema = ?")
	logutils.Println(err)
	rs, err2 := stmt.Query(schema)
	logutils.Println(err2)
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
