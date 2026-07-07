//go:build desktop

package main

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// openSQLiteForInit 打开 sqlite 连接，用于首次启动时执行 init SQL。
// driver 名 "sqlite" 由 modernc.org/sqlite 注册。
func openSQLiteForInit(dsn string) (*sql.DB, error) {
	return sql.Open("sqlite", dsn)
}

// executeInitSQL 按分号切分初始化 SQL 并逐条执行。
// 与 build_release.py 中的初始化逻辑保持一致。
func executeInitSQL(db *sql.DB, initSQL string) error {
	stmts := strings.Split(initSQL, ";")
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("执行初始化 SQL 失败: %w", err)
		}
	}
	return nil
}
