package sql

import (
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/logger"
)

type historyRecord struct {
	Id            string
	User          string
	ConnId        string
	OperationType string
	ExecTime      time.Time
	ExecSql       string
	Data          string
}

type asyncHistoryWriter struct {
	ch     chan *historyRecord
	once   sync.Once
	closed bool
	mu     sync.Mutex
}

var historyWriter = &asyncHistoryWriter{
	ch: make(chan *historyRecord, 4096),
}

func init() {
	go historyWriter.consume()
}

func (w *asyncHistoryWriter) enqueue(record *historyRecord) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	select {
	case w.ch <- record:
	default:
		logger.PrintErrf("历史记录队列已满，丢弃记录: %s", nil, record.ExecSql[:min(100, len(record.ExecSql))])
	}
}

func (w *asyncHistoryWriter) consume() {
	batch := make([]*historyRecord, 0, 64)
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

func (w *asyncHistoryWriter) flushBatch(batch []*historyRecord) {
	if len(batch) == 0 || database.Mngtdb == nil {
		return
	}

	tx, err := database.Mngtdb.Beginx()
	if err != nil {
		logger.PrintErrf("历史记录事务创建失败", err)
		return
	}

	insertSQL := "insert into t_history (id,user,conn_id,operation_type,exec_time,exec_sql,data) values(?,?,?,?,?,?,?)"
	stmt, err := tx.Preparex(insertSQL)
	if err != nil {
		tx.Rollback()
		logger.PrintErrf("历史记录 Prepare 失败", err)
		return
	}
	defer stmt.Close()

	for _, r := range batch {
		dataVal := r.Data
		if dataVal == "" {
			dataVal = "NULL"
		}
		_, err := stmt.Exec(r.Id, r.User, r.ConnId, r.OperationType, r.ExecTime, r.ExecSql, dataVal)
		if err != nil {
			logger.PrintErrf("历史记录写入失败: %s", err, r.ExecSql[:min(100, len(r.ExecSql))])
		}
	}

	if err := tx.Commit(); err != nil {
		logger.PrintErrf("历史记录事务提交失败", err)
	}
}

func ShutdownHistoryWriter() {
	historyWriter.mu.Lock()
	historyWriter.closed = true
	historyWriter.mu.Unlock()
	close(historyWriter.ch)
}