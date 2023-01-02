package webapi

import "net/http"

func saveTree(w http.ResponseWriter, r *http.Request) {

}

func initTreeTable() {
	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_tree (
			"id" TEXT NOT NULL,
			"name" TEXT,
			"parent" TEXT,
			PRIMARY KEY ("id")
		  );
		`
	db.Exec(sql_table)
}
