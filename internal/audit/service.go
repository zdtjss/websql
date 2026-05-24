package audit

import (
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
)

type AuditService struct {
	config *AuditConfig
	writer *asyncWriter
	mu     sync.RWMutex
}

var instance *AuditService
var once sync.Once

func GetAuditService() *AuditService {
	once.Do(func() {
		instance = &AuditService{
			writer:  newAsyncWriter(),
			config:  LoadAuditConfig(),
		}
	})
	return instance
}

func (s *AuditService) ReloadConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = LoadAuditConfig()
}

func (s *AuditService) GetConfig() *AuditConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *AuditService) ShouldRecord(entry *AuditEntry) bool {
	cfg := s.GetConfig()

	if !cfg.Enabled {
		return false
	}

	switch entry.Source {
	case "agent":
		if !cfg.RecordAgentTools {
			return false
		}
	case "sqleditor":
		if !cfg.RecordSQLEditor {
			return false
		}
	}

	riskOrder := map[string]int{"low": 0, "medium": 1, "high": 2}
	if riskOrder[entry.RiskLevel] < riskOrder[cfg.MinRiskLevel] {
		return false
	}

	switch entry.SQLType {
	case "SELECT", "SHOW", "DESCRIBE", "EXPLAIN", "WITH":
		if !cfg.RecordQuery {
			return false
		}
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE", "IMPORT":
		if !cfg.RecordWrite {
			return false
		}
	case "DROP", "TRUNCATE", "ALTER", "CREATE":
		if !cfg.RecordDangerous {
			return false
		}
	}

	return true
}

func (s *AuditService) Record(entry *AuditEntry) {
	if !s.ShouldRecord(entry) {
		return
	}

	now := time.Now()
	log := &AuditLog{
		ID:           idgen.RandomStr(),
		UserID:       entry.UserID,
		UserName:     entry.UserName,
		ConnID:       entry.ConnID,
		ConnName:     entry.ConnName,
		SchemaName:   entry.SchemaName,
		SessionID:    entry.SessionID,
		SQLText:      entry.SQLText,
		SQLType:      entry.SQLType,
		RiskLevel:    entry.RiskLevel,
		Status:       entry.Status,
		Source:       entry.Source,
		ToolName:     entry.ToolName,
		AffectedRows: entry.AffectedRows,
		ExecTimeMs:   entry.ExecTimeMs,
		ExecTime:     &now,
		ErrorMsg:     entry.ErrorMsg,
		ClientIP:     entry.ClientIP,
	}

	s.writer.enqueue(log)
}

func (s *AuditService) Shutdown() {
	s.writer.shutdown()
}

// ─── async writer ───

type asyncWriter struct {
	ch     chan *AuditLog
	once   sync.Once
	closed bool
	mu     sync.Mutex
}

func newAsyncWriter() *asyncWriter {
	w := &asyncWriter{
		ch: make(chan *AuditLog, 4096),
	}
	go w.consume()
	return w
}

func (w *asyncWriter) enqueue(record *AuditLog) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	select {
	case w.ch <- record:
	default:
		logger.PrintErrf("审计日志队列已满，丢弃记录: %s", nil, record.SQLText[:min(100, len(record.SQLText))])
	}
}

func (w *asyncWriter) shutdown() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	close(w.ch)
}

func (w *asyncWriter) consume() {
	batch := make([]*AuditLog, 0, 64)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case record, ok := <-w.ch:
			if !ok {
				w.flushBatch(batch)
				return
			}
			batch = append(batch, record)
			if len(batch) >= 64 {
				w.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *asyncWriter) flushBatch(batch []*AuditLog) {
	if len(batch) == 0 || database.Mngtdb == nil {
		return
	}

	tx, err := database.Mngtdb.Beginx()
	if err != nil {
		logger.PrintErrf("审计日志事务创建失败", err)
		return
	}

	insertSQL := `INSERT INTO t_audit_log (id, user_id, user_name, conn_id, conn_name, schema_name, session_id, sql_text, sql_type, risk_level, status, source, tool_name, affected_rows, exec_time_ms, exec_time, error_msg, client_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.Preparex(insertSQL)
	if err != nil {
		tx.Rollback()
		if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "doesn't exist") {
			logger.PrintErrf("审计日志 Prepare 失败", err)
		}
		return
	}
	defer stmt.Close()

	for _, r := range batch {
		_, err := stmt.Exec(r.ID, r.UserID, r.UserName, r.ConnID, r.ConnName, r.SchemaName,
			r.SessionID, r.SQLText, r.SQLType, r.RiskLevel, r.Status, r.Source, r.ToolName,
			r.AffectedRows, r.ExecTimeMs, r.ExecTime, r.ErrorMsg, r.ClientIP)
		if err != nil {
			logger.PrintErrf("审计日志写入失败: %s", err, r.SQLText[:min(100, len(r.SQLText))])
		}
	}

	if err := tx.Commit(); err != nil {
		logger.PrintErrf("审计日志事务提交失败", err)
	}
}

// ─── cleanup scheduler ───

func StartAuditLogCleaner() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			svc := GetAuditService()
			svc.ReloadConfig()
			cfg := svc.GetConfig()
			if cfg.RetentionDays <= 0 {
				continue
			}
			cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays)
			result, err := database.Mngtdb.Exec("DELETE FROM t_audit_log WHERE exec_time < ?", cutoff)
			if err != nil {
				log.Printf("[AuditCleaner] 清理过期日志失败 - err=%v\n", err)
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				log.Printf("[AuditCleaner] 已清理 %d 条过期审计日志（保留 %d 天）\n", n, cfg.RetentionDays)
			}
		}
	}()
}