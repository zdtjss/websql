package database

import (
	logutils "websql/internal/logger"
	"websql/internal/config"
	"websql/internal/pkg/strutil"
	"log"
	"strings"
)

func InitDB(scriptFile string) {
	sql := config.ReadSql(scriptFile)
	sqlArr := strings.Split(sql, ";")
	tx, err := Mngtdb.DB.Begin()
	defer tx.Rollback()
	logutils.PanicErrf("事务开启失败", err)
	for _, s := range sqlArr {
		relSql := strutil.ExtractSql(s)
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