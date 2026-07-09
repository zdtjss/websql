package database

import (
	logutils "websql/internal/logger"
	"websql/internal/config"
	"websql/internal/pkg/strutil"
	"log"
	"strings"
)

// InitDBFromContent 使用 SQL 内容字符串初始化数据库（建表 + 种子数据）。
// 桌面版通过 go:embed 传入内嵌 SQL 内容，Web 版从文件读取后也委托此函数。
func InitDBFromContent(sqlContent string) {
	sqlArr := strings.Split(sqlContent, ";")
	tx, err := Mngtdb.DB.Begin()
	logutils.PanicErrf("事务开启失败", err)

	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

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
	committed = true
	log.Println("数据库初始化完毕")
}

// InitDB 从 SQL 文件初始化数据库。
func InitDB(scriptFile string) {
	sql := config.ReadSql(scriptFile)
	InitDBFromContent(sql)
}