package audit

import (
	"fmt"
	"runtime"
	"strings"
	"time"
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
	ExecTime     *time.Time `json:"execTime" db:"exec_time"`
	ErrorMsg     string     `json:"errorMsg" db:"error_msg"`
	ClientIP     string     `json:"clientIp" db:"client_ip"`
}

type AuditConfig struct {
	Enabled          bool   `json:"enabled"`
	RecordQuery      bool   `json:"recordQuery"`
	RecordWrite      bool   `json:"recordWrite"`
	RecordDangerous  bool   `json:"recordDangerous"`
	RecordAgentTools bool   `json:"recordAgentTools"`
	RecordSQLEditor  bool   `json:"recordSQLEditor"`
	RetentionDays    int    `json:"retentionDays"`
	MinRiskLevel     string `json:"minRiskLevel"`
}

type AuditEntry struct {
	ConnID       string
	ConnName     string
	SchemaName   string
	SessionID    string
	SQLText      string
	SQLType      string
	RiskLevel    string
	Status       string
	Source       string
	ToolName     string
	AffectedRows int
	ExecTimeMs   int
	ErrorMsg     string
	UserID       string
	UserName     string
	ClientIP     string
}

func FormatErrorWithStack(err error) string {
	if err == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(err.Error())
	sb.WriteString("\n\nStack:\n")
	pcs := make([]uintptr, 64)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&sb, "%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
	if sb.Len() > 8192 {
		return sb.String()[:8192] + "\n... (truncated)"
	}
	return sb.String()
}