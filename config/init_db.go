package config

import (
	"go-web/logutils"
	"go-web/utils"
	"log"
	"strings"
)

func InitDB(scriptFile string) {
	sql := ReadSql(scriptFile)
	sqlArr := strings.Split(sql, ";")
	tx, err := Mngtdb.DB.Begin()
	defer tx.Rollback()
	logutils.PanicErrf("事务开启失败", err)
	for _, s := range sqlArr {
		relSql := utils.ExtractSql(s)
		if relSql == "" {
			continue
		}
		_, err2 := tx.Exec(relSql)
		logutils.PanicErr(err2)
	}
	err = tx.Commit()
	logutils.PanicErr(err)
	log.Println("数据库初始化完毕")
}
