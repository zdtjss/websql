package main

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type AuditLog struct {
	ID           string     `json:"id" db:"id"`
	UserID       string     `json:"userId" db:"user_id"`
	UserName     string     `json:"userName" db:"user_name"`
	ConnID       string     `json:"connId" db:"conn_id"`
	ConnName     string     `json:"connName" db:"conn_name"`
	SchemaName   string     `json:"schemaName" db:"schema_name"`
	SessionID    string     `json:"sessionId" db:"session_id"`
	SQLText      string     `json:"sqlText" db:"sql_text"`
	SQLType      string     `json:"sqlType" db:"sql_type"`
	RiskLevel    string     `json:"riskLevel" db:"risk_level"`
	Status       string     `json:"status" db:"status"`
	Source       string     `json:"source" db:"source"`
	ToolName     string     `json:"toolName" db:"tool_name"`
	AffectedRows int        `json:"affectedRows" db:"affected_rows"`
	ExecTimeMs   int        `json:"execTimeMs" db:"exec_time_ms"`
	ExecTime     *time.Time `json:"-" db:"exec_time"`
	ExecTimeStr  string     `json:"execTime" db:"-"`
	ErrorMsg     string     `json:"errorMsg" db:"error_msg"`
	ClientIP     string     `json:"clientIp" db:"client_ip"`
}

func main() {
	db, err := sqlx.Connect("sqlite", "./nway.sqlite3.db")
	if err != nil {
		fmt.Println("connect err:", err)
		return
	}
	defer db.Close()

	var total int
	if err := db.Get(&total, "SELECT COUNT(*) FROM t_audit_log"); err != nil {
		fmt.Println("count err:", err)
		return
	}
	fmt.Println("total:", total)

	dataSQL := `SELECT id, user_id, user_name, conn_id,
		COALESCE(conn_name, '') as conn_name,
		COALESCE(schema_name, '') as schema_name,
		COALESCE(session_id, '') as session_id,
		sql_text, sql_type, risk_level, status, source,
		COALESCE(tool_name, '') as tool_name,
		affected_rows, exec_time_ms, exec_time, COALESCE(error_msg, '') as error_msg,
		COALESCE(client_ip, '') as client_ip
		FROM t_audit_log ORDER BY exec_time DESC LIMIT 20 OFFSET 0`

	var logs []AuditLog
	if err := db.Select(&logs, dataSQL); err != nil {
		fmt.Println("select err:", err)
		return
	}
	fmt.Println("got rows:", len(logs))
	for i, l := range logs {
		t := ""
		if l.ExecTime != nil {
			t = l.ExecTime.String()
		}
		fmt.Printf("[%d] id=%s sqlType=%s execTime=%q\n", i, l.ID, l.SQLType, t)
	}
}
