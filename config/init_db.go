package config

import (
	"go-web/logutils"
	"go-web/utils"
	"log"
	"strings"
)

func InitDB(scriptFile string) {
	sql := ReadSql(scriptFile)
	sqlArr := strings.Split(*sql, ";")
	tx, err := Mngtdb.DB.Begin()
	defer tx.Rollback()
	logutils.Panicf("事务开启失败， %s", err)
	resultData := []map[string]any{}
	for _, s := range sqlArr {
		relSql := utils.ExtractSql(s)
		if relSql == "" {
			continue
		}
		rs, err2 := tx.Exec(relSql)
		logutils.Panicln(err2)
		affected, err := rs.RowsAffected()
		logutils.Panicln(err)
		resultData = append(resultData, map[string]any{"受影响行数": affected})
	}
	err = tx.Commit()
	logutils.Panicln(err)
	log.Println(resultData)
}
