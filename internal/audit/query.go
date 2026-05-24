package audit

import (
	"strings"

	"websql/internal/database"
	"websql/internal/logger"
)

func queryAuditLogs(userID, connID, sessionID, sqlType, riskLevel, source, startTime, endTime, keyword string, page, pageSize int) ([]AuditLog, int, error) {
	whereClause := " WHERE 1=1"
	args := []any{}

	if userID != "" {
		whereClause += " AND user_id = ?"
		args = append(args, userID)
	}
	if connID != "" {
		whereClause += " AND conn_id = ?"
		args = append(args, connID)
	}
	if sessionID != "" {
		whereClause += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if sqlType != "" {
		whereClause += " AND sql_type = ?"
		args = append(args, sqlType)
	}
	if riskLevel != "" {
		whereClause += " AND risk_level = ?"
		args = append(args, riskLevel)
	}
	if source != "" {
		whereClause += " AND source = ?"
		args = append(args, source)
	}
	if startTime != "" {
		whereClause += " AND exec_time >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		whereClause += " AND exec_time <= ?"
		args = append(args, endTime)
	}
	if keyword != "" {
		whereClause += " AND sql_text LIKE ?"
		args = append(args, "%"+keyword+"%")
	}

	total := 0
	countSQL := "SELECT COUNT(*) FROM t_audit_log" + whereClause
	err := database.Mngtdb.Get(&total, countSQL, args...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "doesn't exist") {
			return []AuditLog{}, 0, nil
		}
		logger.PrintErrf("审计日志 count 查询失败", err)
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	dataSQL := `SELECT id, user_id, user_name, conn_id,
		COALESCE(conn_name, '') as conn_name,
		COALESCE(schema_name, '') as schema_name,
		COALESCE(session_id, '') as session_id,
		sql_text, sql_type, risk_level, status, source,
		COALESCE(tool_name, '') as tool_name,
		affected_rows, exec_time_ms, exec_time, COALESCE(error_msg, '') as error_msg,
		COALESCE(client_ip, '') as client_ip
		FROM t_audit_log` + whereClause + ` ORDER BY exec_time DESC LIMIT ? OFFSET ?`
	dataArgs := append(args, pageSize, offset)

	var logs []AuditLog
	err = database.Mngtdb.Select(&logs, dataSQL, dataArgs...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "doesn't exist") {
			return []AuditLog{}, 0, nil
		}
		logger.PrintErrf("审计日志查询失败", err)
		return nil, 0, err
	}

	return logs, total, nil
}