package agentv2

import (
	"log"
	"strings"
	"time"

	"go-web/config"
)

// SQLAuditLog SQL 审计日志
type SQLAuditLog struct {
	ID           string     `json:"id" db:"id"`
	UserID       string     `json:"userId" db:"user_id"`
	UserName     string     `json:"userName" db:"user_name"`
	ConnID       string     `json:"connId" db:"conn_id"`
	SessionID    string     `json:"sessionId" db:"session_id"`
	SQLText      string     `json:"sqlText" db:"sql_text"`
	SQLType      string     `json:"sqlType" db:"sql_type"`
	RiskLevel    string     `json:"riskLevel" db:"risk_level"`
	Status       string     `json:"status" db:"status"`
	AffectedRows int        `json:"affectedRows" db:"affected_rows"`
	ExecTime     *time.Time `json:"execTime" db:"exec_time"`
	ConfirmTime  *time.Time `json:"confirmTime" db:"confirm_time"`
	ErrorMsg     string     `json:"errorMsg" db:"error_msg"`
}

// InsertSQLAudit 插入审计日志
func InsertSQLAudit(id, userID, userName, connID, sessionID, sqlText, sqlType, riskLevel, status string, affectedRows int, errorMsg string) {
	now := time.Now()
	_, err := config.Mngtdb.Exec(`
		INSERT INTO t_sql_audit (id, user_id, user_name, conn_id, session_id, sql_text, sql_type, risk_level, status, affected_rows, exec_time, confirm_time, error_msg)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, userID, userName, connID, sessionID, sqlText, sqlType, riskLevel, status, affectedRows, now, now, errorMsg)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "doesn't exist") {
			log.Printf("[SQLAudit] 审计表不存在，跳过记录\n")
			return
		}
		log.Printf("[SQLAudit] 插入审计日志失败 - err=%v\n", err)
	}
}

// ListSQLAuditLogs 查询审计日志
func ListSQLAuditLogs(userID string, limit int) ([]SQLAuditLog, error) {
	var logs []SQLAuditLog
	var err error

	if userID == "" {
		err = config.Mngtdb.Select(&logs, `
			SELECT id, user_id, user_name, conn_id, session_id, sql_text, sql_type, risk_level, status, affected_rows, exec_time, confirm_time, error_msg
			FROM t_sql_audit ORDER BY exec_time DESC LIMIT ?
		`, limit)
	} else {
		err = config.Mngtdb.Select(&logs, `
			SELECT id, user_id, user_name, conn_id, session_id, sql_text, sql_type, risk_level, status, affected_rows, exec_time, confirm_time, error_msg
			FROM t_sql_audit WHERE user_id = ? ORDER BY exec_time DESC LIMIT ?
		`, userID, limit)
	}

	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "doesn't exist") {
			return []SQLAuditLog{}, nil
		}
		return nil, err
	}
	return logs, nil
}
