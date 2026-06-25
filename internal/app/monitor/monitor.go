package monitor

import (
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"time"

	"websql/internal/app/conn"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/dberr"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// 监控指标名常量，对应 t_monitor_metric.metric_name 字段
const (
	MetricQPS               = "qps"
	MetricTPS               = "tps"
	MetricConnections       = "connections"
	MetricBufferPoolHitRate = "buffer_pool_hit_rate"
	MetricSlowQueries       = "slow_queries"
	MetricLockWaits         = "lock_waits"
)

// allowedMetrics 指标名白名单，用于校验查询参数，防止通过 metric 名注入
var allowedMetrics = map[string]bool{
	MetricQPS:               true,
	MetricTPS:               true,
	MetricConnections:       true,
	MetricBufferPoolHitRate: true,
	MetricSlowQueries:       true,
	MetricLockWaits:         true,
}

type MetricsSnapshot struct {
	Timestamp         string  `json:"timestamp"`
	Connections       int     `json:"connections"`
	ActiveConnections int     `json:"activeConnections"`
	QPS               float64 `json:"qps"`
	TPS               float64 `json:"tps"`
	SlowQueries       int     `json:"slowQueries"`
	LockWaits         int     `json:"lockWaits"`
	ThreadsRunning    int     `json:"threadsRunning"`
	ThreadsConnected  int     `json:"threadsConnected"`
	BytesReceived     int64   `json:"bytesReceived"`
	BytesSent         int64   `json:"bytesSent"`
	Uptime            int64   `json:"uptime"`
	Questions         int64   `json:"questions"`
	ComSelect         int64   `json:"comSelect"`
	ComInsert         int64   `json:"comInsert"`
	ComUpdate         int64   `json:"comUpdate"`
	ComDelete         int64   `json:"comDelete"`
}

type ResourceSnapshot struct {
	Timestamp          string  `json:"timestamp"`
	DBName             string  `json:"dbName"`
	DataSize           int64   `json:"dataSize"`
	IndexSize          int64   `json:"indexSize"`
	TableCount         int     `json:"tableCount"`
	TotalRows          int64   `json:"totalRows"`
	BufferPoolSize     int64   `json:"bufferPoolSize"`
	BufferPoolUsed     int64   `json:"bufferPoolUsed"`
	BufferPoolHitRate  float64 `json:"bufferPoolHitRate"`
	DiskUsage          int64   `json:"diskUsage"`
	QCacheHitRate      float64 `json:"qCacheHitRate"`
	InnodbRowsRead     int64   `json:"innodbRowsRead"`
	InnodbRowsInserted int64   `json:"innodbRowsInserted"`
	InnodbRowsUpdated  int64   `json:"innodbRowsUpdated"`
	InnodbRowsDeleted  int64   `json:"innodbRowsDeleted"`
}

type ProcessInfo struct {
	Id      int    `json:"id"`
	User    string `json:"user"`
	Host    string `json:"host"`
	Db      string `json:"db"`
	Command string `json:"command"`
	Time    int    `json:"time"`
	State   string `json:"state"`
	Info    string `json:"info"`
}

// 注：原 metricsCache（内存环形缓冲）已移除。
// 历史趋势查询统一走 GetMetricHistory（从 t_monitor_metric 表读取），
// 内存版本与 DB 版本功能重叠且不被前端调用，属于冗余设计。

// collectMetricsSnapshot 采集核心监控指标快照（QPS/TPS/连接数/慢查询/锁等待等）。
// 抽取自 GetMetrics，供 HTTP 接口与后台采集器复用；查询失败仅记录日志、返回部分快照。
func collectMetricsSnapshot(conn *sqlx.DB, dbType string) MetricsSnapshot {
	snapshot := MetricsSnapshot{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	}

	if dbType == "mysql" || dbType == "mariadb" {

		type statusRow struct {
			Name  string `db:"Variable_name"`
			Value int64  `db:"Value"`
		}
		var statusList []statusRow
		err := conn.Select(&statusList, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Questions','Uptime','Com_select','Com_insert','Com_update','Com_delete','Bytes_received','Bytes_sent','Slow_queries','Threads_connected','Threads_running')")
		if err != nil {
			logger.PrintErrf("获取状态变量失败", err)
		}

		for _, s := range statusList {
			switch s.Name {
			case "Questions":
				snapshot.Questions = s.Value
			case "Uptime":
				snapshot.Uptime = s.Value
			case "Com_select":
				snapshot.ComSelect = s.Value
			case "Com_insert":
				snapshot.ComInsert = s.Value
			case "Com_update":
				snapshot.ComUpdate = s.Value
			case "Com_delete":
				snapshot.ComDelete = s.Value
			case "Bytes_received":
				snapshot.BytesReceived = s.Value
			case "Bytes_sent":
				snapshot.BytesSent = s.Value
			case "Slow_queries":
				snapshot.SlowQueries = int(s.Value)
			case "Threads_connected":
				snapshot.ThreadsConnected = int(s.Value)
			case "Threads_running":
				snapshot.ThreadsRunning = int(s.Value)
			}
		}

		var threadCount int
		conn.Get(&threadCount, "SELECT COUNT(*) FROM information_schema.PROCESSLIST")
		snapshot.Connections = threadCount

		var activeCount int
		conn.Get(&activeCount, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE Command != 'Sleep'")
		snapshot.ActiveConnections = activeCount

		if snapshot.Uptime > 0 {
			snapshot.QPS = float64(snapshot.Questions) / float64(snapshot.Uptime)
		}

		var innodbRows []statusRow
		conn.Select(&innodbRows, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Com_commit','Com_rollback')")
		var commits, rollbacks int64
		for _, s := range innodbRows {
			if s.Name == "Com_commit" {
				commits = s.Value
			}
			if s.Name == "Com_rollback" {
				rollbacks = s.Value
			}
		}
		if snapshot.Uptime > 0 {
			snapshot.TPS = float64(commits+rollbacks) / float64(snapshot.Uptime)
		}

		snapshot.LockWaits = countLockWaits(conn)
	} else if dbType == "oracle" {
		var sessions int
		conn.Get(&sessions, "SELECT COUNT(*) FROM v$session")
		snapshot.Connections = sessions

		var activeSessions int
		conn.Get(&activeSessions, "SELECT COUNT(*) FROM v$session WHERE status='ACTIVE'")
		snapshot.ActiveConnections = activeSessions
	}

	return snapshot
}

// bufferPoolStats 缓冲池统计结果，供 GetResources 与后台采集器复用。
type bufferPoolStats struct {
	Size         int64
	Used         int64
	HitRate      float64
	RowsRead     int64
	RowsInserted int64
	RowsUpdated  int64
	RowsDeleted  int64
}

// collectBufferPoolStats 采集 InnoDB 缓冲池大小/使用/命中率与行操作计数（仅 MySQL/MariaDB）。
// 抽取自 GetResources，供 HTTP 接口与后台采集器复用；查询失败返回零值结构并记录日志。
func collectBufferPoolStats(conn *sqlx.DB, dbType string) bufferPoolStats {
	var s bufferPoolStats
	if dbType != "mysql" && dbType != "mariadb" {
		return s
	}
	type bufferRow struct {
		Name  string `db:"Variable_name"`
		Value int64  `db:"Value"`
	}
	var bufferVars []bufferRow
	if err := conn.Select(&bufferVars, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_buffer_pool_pages_total','Innodb_buffer_pool_pages_free','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_rows_read','Innodb_rows_inserted','Innodb_rows_updated','Innodb_rows_deleted')"); err != nil {
		logger.PrintErrf("获取缓冲池状态失败", err)
		return s
	}
	var totalPages, freePages, readReqs, reads int64
	for _, v := range bufferVars {
		switch v.Name {
		case "Innodb_buffer_pool_pages_total":
			totalPages = v.Value
		case "Innodb_buffer_pool_pages_free":
			freePages = v.Value
		case "Innodb_buffer_pool_read_requests":
			readReqs = v.Value
		case "Innodb_buffer_pool_reads":
			reads = v.Value
		case "Innodb_rows_read":
			s.RowsRead = v.Value
		case "Innodb_rows_inserted":
			s.RowsInserted = v.Value
		case "Innodb_rows_updated":
			s.RowsUpdated = v.Value
		case "Innodb_rows_deleted":
			s.RowsDeleted = v.Value
		}
	}
	pageSize := int64(16384)
	s.Size = totalPages * pageSize
	s.Used = (totalPages - freePages) * pageSize
	if readReqs > 0 {
		s.HitRate = float64(readReqs-reads) / float64(readReqs) * 100
	}
	return s
}

// GetMetrics 返回当前数据库的核心监控指标快照，并异步持久化到 t_monitor_metric。
func GetMetrics(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	snapshot := collectMetricsSnapshot(conn, dbType)

	// 异步将指标持久化到 t_monitor_metric 表，不阻塞 API 响应
	// 历史趋势查询走 GetMetricHistory（从 DB 读取），无需再维护内存缓存
	persistMetrics(connId, snapshot)

	response.WriteOK(c, snapshot)
}

func countLockWaits(conn *sqlx.DB) int {
	var count int
	err := conn.Get(&count, "SELECT COUNT(*) FROM information_schema.INNODB_LOCK_WAITS")
	if err != nil {
		return 0
	}
	return count
}

// 注：原 GetMetricsHistory（内存版历史）已移除，前端统一使用
// GET /monitor/history（GetMetricHistory，DB 持久化版本）查询历史趋势。

func GetResources(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	snapshot := ResourceSnapshot{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		DBName:    schema,
	}

	if dbType == "mysql" || dbType == "mariadb" {
		if schema != "" {
			var dataSize, indexSize float64
			err := conn.Get(&dataSize, "SELECT COALESCE(SUM(DATA_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			if err == nil {
				snapshot.DataSize = int64(dataSize)
			}
			err = conn.Get(&indexSize, "SELECT COALESCE(SUM(INDEX_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			if err == nil {
				snapshot.IndexSize = int64(indexSize)
			}

			var tableCount int
			conn.Get(&tableCount, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			snapshot.TableCount = tableCount
		}

		bp := collectBufferPoolStats(conn, dbType)
		snapshot.BufferPoolSize = bp.Size
		snapshot.BufferPoolUsed = bp.Used
		snapshot.BufferPoolHitRate = bp.HitRate
		snapshot.InnodbRowsRead = bp.RowsRead
		snapshot.InnodbRowsInserted = bp.RowsInserted
		snapshot.InnodbRowsUpdated = bp.RowsUpdated
		snapshot.InnodbRowsDeleted = bp.RowsDeleted

		var qCacheHits, qCacheInserts int64
		conn.Get(&qCacheHits, "SHOW GLOBAL STATUS LIKE 'Qcache_hits'")
		conn.Get(&qCacheInserts, "SHOW GLOBAL STATUS LIKE 'Qcache_inserts'")
		if qCacheHits+qCacheInserts > 0 {
			snapshot.QCacheHitRate = float64(qCacheHits) / float64(qCacheHits+qCacheInserts) * 100
		}

		var totalRows int64
		conn.Get(&totalRows, "SELECT COALESCE(SUM(TABLE_ROWS),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
		snapshot.TotalRows = totalRows
	} else if dbType == "oracle" {
		if schema != "" {
			var tables int
			conn.Get(&tables, "SELECT COUNT(*) FROM user_tables")
			snapshot.TableCount = tables
		}
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 异步持久化缓冲池命中率指标（仅 MySQL/MariaDB 有此指标）
	if dbType == "mysql" || dbType == "mariadb" {
		persistSingleMetric(connId, MetricBufferPoolHitRate, snapshot.BufferPoolHitRate)
	}

	response.WriteOK(c, map[string]any{
		"dbResources":    snapshot,
		"appMemoryAlloc": memStats.Alloc,
		"appMemoryTotal": memStats.Sys,
		"appGoroutines":  runtime.NumGoroutine(),
		"appHeapObjects": memStats.HeapObjects,
	})
}

func GetProcesses(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	processes := make([]ProcessInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		rows, err := conn.Queryx("SHOW FULL PROCESSLIST")
		if err != nil {
			logger.PrintErrf("获取进程列表失败", err)
			response.WriteOK(c, map[string]any{"processes": processes, "count": 0})
			return
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			valPtrs := make([]any, len(cols))
			for i := range vals {
				valPtrs[i] = &vals[i]
			}
			rows.Scan(valPtrs...)

			p := ProcessInfo{}
			for i, col := range cols {
				val := ""
				if vals[i] != nil {
					val = fmt.Sprintf("%v", vals[i])
				}
				switch strings.ToLower(col) {
				case "id":
					fmt.Sscanf(val, "%d", &p.Id)
				case "user":
					p.User = val
				case "host":
					p.Host = val
				case "db":
					p.Db = val
				case "command":
					p.Command = val
				case "time":
					fmt.Sscanf(val, "%d", &p.Time)
				case "state":
					p.State = val
				case "info":
					p.Info = val
				}
			}
			processes = append(processes, p)
		}
	} else if dbType == "oracle" {
		rows, err := conn.Queryx("SELECT sid, serial#, username, status, machine, program FROM v$session WHERE type!='BACKGROUND'")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sid, serial, username, status, machine, program string
				rows.Scan(&sid, &serial, &username, &status, &machine, &program)
				processes = append(processes, ProcessInfo{
					Id:      parseStrToInt(sid),
					User:    username,
					Host:    machine,
					State:   status,
					Command: program,
				})
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"processes": processes,
		"count":     len(processes),
	})
}

func GetServerVariables(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	variables := make(map[string]string)

	if dbType == "mysql" || dbType == "mariadb" {
		type varRow struct {
			Name  string `db:"Variable_name"`
			Value string `db:"Value"`
		}
		keyVars := []string{"max_connections", "innodb_buffer_pool_size", "version", "character_set_server", "collation_server"}

		for _, key := range keyVars {
			var v varRow
			err := conn.Get(&v, "SHOW GLOBAL VARIABLES WHERE Variable_name=?", key)
			if err == nil {
				variables[key] = v.Value
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"variables": variables,
	})
}

// ===== 增强监控：InnoDB 状态 / 锁 / 慢查询 / 表统计 =====

// InnoDBStatusResult InnoDB 引擎状态返回结构
type InnoDBStatusResult struct {
	Status    string `json:"status"`
	Supported bool   `json:"supported"`
}

// GetInnodbStatus 返回 InnoDB 引擎状态文本（SHOW ENGINE INNODB STATUS），仅 MySQL/MariaDB 支持。
func GetInnodbStatus(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	result := InnoDBStatusResult{}
	if dbType == "mysql" || dbType == "mariadb" {
		result.Supported = true
		type engineRow struct {
			Engine string `db:"Engine"`
			Status string `db:"Status"`
		}
		var er engineRow
		// SHOW ENGINE INNODB STATUS 需要 PROCESS 权限；失败仅记录日志，返回空文本
		if err := conn.Get(&er, "SHOW ENGINE INNODB STATUS"); err != nil {
			logger.PrintErrf("获取 InnoDB 引擎状态失败（可能缺少 PROCESS 权限）", err)
		} else {
			result.Status = er.Status
		}
	}
	response.WriteOK(c, result)
}

// LockInfo 锁与事务等待信息（统一字段，跨数据库通用）
type LockInfo struct {
	WaitingID   string `json:"waitingId"`   // 等待事务/会话标识（MySQL: trx_id；Oracle: sid）
	BlockingID  string `json:"blockingId"`  // 阻塞会话标识（MySQL: 阻塞 trx_id；Oracle: blocking_session）
	LockType    string `json:"lockType"`    // 锁类型/等待事件
	WaitSeconds int64  `json:"waitSeconds"` // 等待时长（秒）
	TableName   string `json:"tableName"`   // 涉及表名
	Query       string `json:"query"`       // 当前 SQL
}

// GetLocks 返回当前锁等待与阻塞事务列表。
// MySQL: information_schema.INNODB_TRX（LOCK WAIT）+ INNODB_LOCK_WAITS 阻塞映射（best-effort）；
// Oracle: v$session WHERE blocking_session IS NOT NULL；SQLite: 不支持，返回空。
func GetLocks(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	locks := make([]LockInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		// 阻塞映射：requesting_trx_id -> blocking_trx_id（best-effort，表缺失则忽略）
		blockerMap := map[string]string{}
		type waitRow struct {
			Requesting string `db:"requesting_trx_id"`
			Blocking   string `db:"blocking_trx_id"`
		}
		var waits []waitRow
		_ = conn.Select(&waits, "SELECT requesting_trx_id, blocking_trx_id FROM information_schema.INNODB_LOCK_WAITS")
		for _, w := range waits {
			blockerMap[w.Requesting] = w.Blocking
		}

		type trxRow struct {
			TrxID      string  `db:"trx_id"`
			ThreadID   int64   `db:"trx_mysql_thread_id"`
			Query      *string `db:"trx_query"`
			WaitSeconds int64  `db:"wait_seconds"`
		}
		var rows []trxRow
		// 仅取 LOCK WAIT 状态事务，等待时长由 TIMESTAMPDIFF 计算
		err := conn.Select(&rows, `SELECT trx_id, trx_mysql_thread_id, trx_query,
			TIMESTAMPDIFF(SECOND, trx_wait_started, NOW()) AS wait_seconds
			FROM information_schema.INNODB_TRX
			WHERE trx_state = 'LOCK WAIT' AND trx_wait_started IS NOT NULL
			ORDER BY trx_wait_started`)
		if err != nil {
			logger.PrintErrf("获取锁等待事务失败", err)
		} else {
			for _, r := range rows {
				q := ""
				if r.Query != nil {
					q = *r.Query
				}
				locks = append(locks, LockInfo{
					WaitingID:   r.TrxID,
					BlockingID:  blockerMap[r.TrxID],
					LockType:    "InnoDB Record Lock",
					WaitSeconds: r.WaitSeconds,
					Query:       q,
				})
			}
		}
	} else if dbType == "oracle" {
		type sessRow struct {
			SID            int64   `db:"sid"`
			Username        *string `db:"username"`
			Event           *string `db:"event"`
			SecondsInWait   int64   `db:"seconds_in_wait"`
			BlockingSession *int64  `db:"blocking_session"`
			SQLID           *string `db:"sql_id"`
		}
		var rows []sessRow
		err := conn.Select(&rows, `SELECT sid, username, event, seconds_in_wait, blocking_session, sql_id
			FROM v$session
			WHERE blocking_session IS NOT NULL
			ORDER BY seconds_in_wait DESC`)
		if err != nil {
			logger.PrintErrf("获取 Oracle 阻塞会话失败", err)
		} else {
			for _, r := range rows {
				evt := ""
				if r.Event != nil {
					evt = *r.Event
				}
				blk := ""
				if r.BlockingSession != nil {
					blk = strconv.FormatInt(*r.BlockingSession, 10)
				}
				sqlID := ""
				if r.SQLID != nil {
					sqlID = *r.SQLID
				}
				locks = append(locks, LockInfo{
					WaitingID:   strconv.FormatInt(r.SID, 10),
					BlockingID:  blk,
					LockType:    evt,
					WaitSeconds: r.SecondsInWait,
					Query:       sqlID,
				})
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"locks":     locks,
		"count":     len(locks),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	})
}

// SlowQueryInfo 慢查询统计信息
type SlowQueryInfo struct {
	DigestText   string  `json:"digestText"`
	AvgMs        float64 `json:"avgMs"`
	ExecCount    int64   `json:"execCount"`
	RowsExamined int64   `json:"rowsExamined"`
	LastSeen     string  `json:"lastSeen"`
}

// GetSlowQueries 返回按平均耗时排序的慢查询列表。
// MySQL: performance_schema.events_statements_summary_by_digest（AVG_TIMER_WAIT 单位皮秒，转 ms）；
// Oracle: v$sql（elapsed_time 单位微秒，转 ms）；SQLite: 不支持，返回空。
func GetSlowQueries(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	limit := parseLimit(c.Query("limit"), 20)
	queries := make([]SlowQueryInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		type slowRow struct {
			DigestText      string  `db:"DIGEST_TEXT"`
			AvgTimerWait    int64   `db:"AVG_TIMER_WAIT"`
			CountStar       int64   `db:"COUNT_STAR"`
			SumRowsExamined int64   `db:"SUM_ROWS_EXAMINED"`
			LastSeen        *string `db:"LAST_SEEN"`
		}
		var rows []slowRow
		// performance_schema 可能被禁用，查询失败仅记录日志返回空
		err := conn.Select(&rows, `SELECT DIGEST_TEXT, AVG_TIMER_WAIT, COUNT_STAR, SUM_ROWS_EXAMINED, LAST_SEEN
			FROM performance_schema.events_statements_summary_by_digest
			WHERE DIGEST_TEXT IS NOT NULL
			ORDER BY AVG_TIMER_WAIT DESC
			LIMIT ?`, limit)
		if err != nil {
			logger.PrintErrf("获取慢查询摘要失败（performance_schema 可能未启用）", err)
		} else {
			for _, r := range rows {
				last := ""
				if r.LastSeen != nil {
					last = *r.LastSeen
				}
				queries = append(queries, SlowQueryInfo{
					DigestText:   r.DigestText,
					AvgMs:        float64(r.AvgTimerWait) / 1e9, // 皮秒 → 毫秒
					ExecCount:    r.CountStar,
					RowsExamined: r.SumRowsExamined,
					LastSeen:     last,
				})
			}
		}
	} else if dbType == "oracle" {
		type sqlRow struct {
			SQLID       string  `db:"sql_id"`
			SQLText     *string `db:"sql_text"`
			ElapsedTime int64   `db:"elapsed_time"`
			Executions  int64   `db:"executions"`
		}
		var rows []sqlRow
		// limit 已在 parseLimit 钳制为 [1,100] 整数，内联安全；go-ora 占位符约定不一，避免 bind 参数
		err := conn.Select(&rows, fmt.Sprintf(`SELECT * FROM (
			SELECT sql_id, sql_text, elapsed_time, executions
			FROM v$sql ORDER BY elapsed_time DESC
		) WHERE ROWNUM <= %d`, limit))
		if err != nil {
			logger.PrintErrf("获取 Oracle 慢查询失败", err)
		} else {
			for _, r := range rows {
				txt := ""
				if r.SQLText != nil {
					txt = *r.SQLText
				}
				queries = append(queries, SlowQueryInfo{
					DigestText: txt,
					AvgMs:      float64(r.ElapsedTime) / 1000, // 微秒 → 毫秒
					ExecCount:  r.Executions,
					LastSeen:   r.SQLID,
				})
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"queries":   queries,
		"count":     len(queries),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	})
}

// TopTableInfo 表统计信息
type TopTableInfo struct {
	TableName  string `json:"tableName"`
	Engine     string `json:"engine"`
	TableRows  int64  `json:"tableRows"`
	DataSize   int64  `json:"dataSize"`
	IndexSize  int64  `json:"indexSize"`
	DataFree   int64  `json:"dataFree"`
}

// GetTopTables 返回按总大小排序的 TOP N 表统计。
// MySQL/MariaDB: information_schema.TABLES（按 DATA_LENGTH+INDEX_LENGTH DESC）；
// Oracle: user_segments（按 bytes DESC）；SQLite: 不支持，返回空。
func GetTopTables(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	schema := c.Query("schema")
	limit := parseLimit(c.Query("limit"), 20)
	tables := make([]TopTableInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		type tblRow struct {
			TableName   string  `db:"TABLE_NAME"`
			Engine      *string `db:"ENGINE"`
			TableRows   int64   `db:"TABLE_ROWS"`
			DataLength  int64   `db:"DATA_LENGTH"`
			IndexLength int64   `db:"INDEX_LENGTH"`
			DataFree    int64   `db:"DATA_FREE"`
		}
		var rows []tblRow
		var err error
		if schema != "" {
			err = conn.Select(&rows, `SELECT TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, DATA_FREE
				FROM information_schema.TABLES
				WHERE TABLE_SCHEMA = ?
				ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC
				LIMIT ?`, schema, limit)
		} else {
			// 未指定 schema：排除系统库
			err = conn.Select(&rows, `SELECT TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, DATA_FREE
				FROM information_schema.TABLES
				WHERE TABLE_SCHEMA NOT IN ('information_schema','mysql','performance_schema','sys')
				ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC
				LIMIT ?`, limit)
		}
		if err != nil {
			logger.PrintErrf("获取表统计失败", err)
		} else {
			for _, r := range rows {
				eng := ""
				if r.Engine != nil {
					eng = *r.Engine
				}
				tables = append(tables, TopTableInfo{
					TableName:  r.TableName,
					Engine:     eng,
					TableRows:  r.TableRows,
					DataSize:   r.DataLength,
					IndexSize:  r.IndexLength,
					DataFree:   r.DataFree,
				})
			}
		}
	} else if dbType == "oracle" {
		type segRow struct {
			SegmentName string `db:"segment_name"`
			Bytes       int64  `db:"bytes"`
		}
		var rows []segRow
		// limit 已钳制为 [1,100] 整数，内联安全
		err := conn.Select(&rows, fmt.Sprintf(`SELECT * FROM (
			SELECT segment_name, bytes FROM user_segments ORDER BY bytes DESC
		) WHERE ROWNUM <= %d`, limit))
		if err != nil {
			logger.PrintErrf("获取 Oracle 表段统计失败", err)
		} else {
			for _, r := range rows {
				tables = append(tables, TopTableInfo{
					TableName: r.SegmentName,
					Engine:    "ORACLE",
					DataSize:  r.Bytes,
				})
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"tables":    tables,
		"count":     len(tables),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	})
}

// parseLimit 解析 limit 查询参数，默认 def，钳制到 [1, 100]。
func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	if n > 100 {
		return 100
	}
	return n
}

func parseStrToInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ===== 监控指标持久化 =====

// persistMetrics 异步将指标快照批量写入 t_monitor_metric 表。
// 使用 safego.GoWithName 异步执行，写入失败不影响监控 API 的正常响应。
// connId 为空或管理库未初始化时直接跳过。
func persistMetrics(connId string, snapshot MetricsSnapshot) {
	if database.Mngtdb == nil || connId == "" {
		return
	}
	// 采集时间统一取当前时刻，保证一次快照的多个指标时间一致
	collectedAt := time.Now()
	metrics := []struct {
		name  string
		value float64
	}{
		{MetricQPS, snapshot.QPS},
		{MetricTPS, snapshot.TPS},
		{MetricConnections, float64(snapshot.Connections)},
		{MetricSlowQueries, float64(snapshot.SlowQueries)},
		{MetricLockWaits, float64(snapshot.LockWaits)},
	}
	safego.GoWithName("monitor-metric-write", func() {
		insertSQL := `INSERT INTO t_monitor_metric (conn_id, metric_name, metric_value, collected_at) VALUES (?, ?, ?, ?)`
		for _, m := range metrics {
			if err := database.MngtdbExec(insertSQL, connId, m.name, m.value, collectedAt); err != nil {
				// 表不存在时静默跳过（尚未执行初始化脚本），其他错误记录日志
				if !dberr.IsTableNotExist(err) {
					logger.PrintErrf("监控指标写入失败: metric=%s", err, m.name)
				}
				return
			}
		}
	})
}

// persistSingleMetric 异步写入单个指标，用于在 GetResources 中持久化缓冲池命中率等
// 不属于 MetricsSnapshot 的指标。
func persistSingleMetric(connId, metricName string, value float64) {
	if database.Mngtdb == nil || connId == "" {
		return
	}
	collectedAt := time.Now()
	safego.GoWithName("monitor-metric-write", func() {
		insertSQL := `INSERT INTO t_monitor_metric (conn_id, metric_name, metric_value, collected_at) VALUES (?, ?, ?, ?)`
		if err := database.MngtdbExec(insertSQL, connId, metricName, value, collectedAt); err != nil {
			if !dberr.IsTableNotExist(err) {
				logger.PrintErrf("监控指标写入失败: metric=%s", err, metricName)
			}
		}
	})
}

// ===== 历史趋势查询 =====

// 历史时间点：单条记录
type metricPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// GetMetricHistory 查询指标历史趋势，支持按时间范围和降采样粒度查询。
// GET /api/monitor/history?connId=xxx&metric=qps&from=2024-01-01&to=2024-01-02&interval=5min
// interval 取值：raw（原始）/ 5min（5分钟均值）/ 1hour（1小时均值）
// 返回格式：{ points: [{ timestamp, value }] }
func GetMetricHistory(c *gin.Context) {
	connId := c.Query("connId")
	metric := c.Query("metric")
	fromStr := c.Query("from")
	toStr := c.Query("to")
	interval := c.DefaultQuery("interval", "raw")

	// 参数校验
	if connId == "" || metric == "" {
		response.WriteErr(c, 200, 400, "缺少必要参数 connId 或 metric")
		return
	}
	// 指标名白名单校验，防止通过 metric 参数注入
	if !allowedMetrics[metric] {
		response.WriteErr(c, 200, 400, "非法的指标名: "+metric)
		return
	}
	// 指标名标识符校验（双重防护：白名单 + sanitize 标识符规则）
	if !sanitize.IsValidIdentifier(metric) {
		response.WriteErr(c, 200, 400, "非法的指标名格式: "+metric)
		return
	}

	// 解析时间范围，默认最近 1 小时
	now := time.Now()
	var ok bool
	from, ok := parseHistoryTime(fromStr, now.Add(-1*time.Hour))
	if !ok {
		response.WriteErr(c, 200, 400, "非法的 from 时间格式，支持 2006-01-02 或 2006-01-02 15:04:05")
		return
	}
	var to time.Time
	to, ok = parseHistoryTime(toStr, now)
	if !ok {
		response.WriteErr(c, 200, 400, "非法的 to 时间格式，支持 2006-01-02 或 2006-01-02 15:04:05")
		return
	}

	if database.Mngtdb == nil {
		response.WriteOK(c, map[string]any{"points": []metricPoint{}})
		return
	}

	points, err := queryMetricHistory(connId, metric, from, to, interval)
	if err != nil {
		// 表不存在时返回空结果（尚未执行初始化脚本）
		if dberr.IsTableNotExist(err) {
			response.WriteOK(c, map[string]any{"points": []metricPoint{}})
			return
		}
		logger.PrintErrf("查询监控指标历史失败: metric=%s", err, metric)
		response.WriteErr(c, 200, 500, "查询历史指标失败")
		return
	}

	response.WriteOK(c, map[string]any{"points": points})
}

// queryMetricHistory 根据降采样粒度查询历史指标。
// raw: 返回原始数据点；5min/1hour: 按时间桶聚合取平均值。
func queryMetricHistory(connId, metric string, from, to time.Time, interval string) ([]metricPoint, error) {
	dbType := database.Mngtdb.DriverName()
	var query string
	args := []any{connId, metric, from, to}

	switch interval {
	case "5min", "1hour":
		bucketSeconds := 300
		if interval == "1hour" {
			bucketSeconds = 3600
		}
		query = buildDownsampleSQL(dbType, bucketSeconds)
	default:
		// raw：返回原始数据点
		query = `SELECT collected_at, metric_value FROM t_monitor_metric WHERE conn_id = ? AND metric_name = ? AND collected_at >= ? AND collected_at <= ? ORDER BY collected_at`
	}

	rows, err := database.Mngtdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]metricPoint, 0)
	for rows.Next() {
		var ts time.Time
		var value float64
		if err := rows.Scan(&ts, &value); err != nil {
			return nil, err
		}
		points = append(points, metricPoint{
			Timestamp: ts.Format("2006-01-02 15:04:05"),
			Value:     value,
		})
	}
	return points, rows.Err()
}

// buildDownsampleSQL 构建降采样聚合 SQL，按时间桶分组取平均值。
// 根据管理库类型（MySQL/SQLite）使用不同的时间桶表达式。
func buildDownsampleSQL(dbType string, bucketSeconds int) string {
	switch dbType {
	case "sqlite", "sqlite3":
		// SQLite：用 strftime 将时间转 epoch 秒，整除桶大小后乘回，再转回 datetime
		return fmt.Sprintf(`SELECT datetime((strftime('%%s', collected_at) / %d) * %d, 'unixepoch') AS bucket, AVG(metric_value) FROM t_monitor_metric WHERE conn_id = ? AND metric_name = ? AND collected_at >= ? AND collected_at <= ? GROUP BY bucket ORDER BY bucket`, bucketSeconds, bucketSeconds)
	default:
		// MySQL/MariaDB：用 UNIX_TIMESTAMP 整除桶大小再 FROM_UNIXTIME 还原
		return fmt.Sprintf(`SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(collected_at) / %d) * %d) AS bucket, AVG(metric_value) FROM t_monitor_metric WHERE conn_id = ? AND metric_name = ? AND collected_at >= ? AND collected_at <= ? GROUP BY bucket ORDER BY bucket`, bucketSeconds, bucketSeconds)
	}
}

// parseHistoryTime 解析历史查询的时间参数，支持 "2006-01-02" 和 "2006-01-02 15:04:05" 两种格式
func parseHistoryTime(s string, defaultTime time.Time) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return defaultTime, true
	}
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ===== 自动清理 =====

// metricRetentionDays 监控指标保留天数，超过此天数的数据自动删除
const metricRetentionDays = 30

// StartMetricCleaner 启动监控指标自动清理后台任务。
// 每 24 小时清理一次，删除 collected_at 早于 30 天前的数据。
// 启动后立即执行一次清理，之后按周期执行。
func StartMetricCleaner() {
	safego.GoWithName("monitor-metric-cleaner", func() {
		// 启动后先清理一次
		cleanExpiredMetrics()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanExpiredMetrics()
		}
	})
}

// cleanExpiredMetrics 删除 30 天前的监控指标数据
func cleanExpiredMetrics() {
	if database.Mngtdb == nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -metricRetentionDays)
	result, err := database.Mngtdb.Exec("DELETE FROM t_monitor_metric WHERE collected_at < ?", cutoff)
	if err != nil {
		// 表不存在时静默跳过
		if !dberr.IsTableNotExist(err) {
			log.Printf("[MonitorCleaner] 清理过期监控指标失败 - err=%v\n", err)
		}
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		log.Printf("[MonitorCleaner] 已清理 %d 条过期监控指标（保留 %d 天）\n", n, metricRetentionDays)
	}
}

// ===== 后台指标采集器 =====

// metricCollectorInterval 后台采集周期；粒度与典型监控一致，如需更密可调此常量。
const metricCollectorInterval = 60 * time.Second

// StartMetricCollector 启动后台指标采集任务。
// 每 60 秒遍历所有数据库连接，采集核心指标并持久化到 t_monitor_metric，
// 供"性能趋势 → 历史趋势"查询。启动后立即采集一次。
func StartMetricCollector() {
	safego.GoWithName("monitor-metric-collector", func() {
		collectAllMetrics()
		ticker := time.NewTicker(metricCollectorInterval)
		defer ticker.Stop()
		for range ticker.C {
			collectAllMetrics()
		}
	})
}

// collectAllMetrics 枚举所有连接并采集指标。
// SQLite 无 QPS/TPS/缓冲池指标，跳过；连接失败（GetConnNoCheck 返回 nil）跳过。
func collectAllMetrics() {
	if database.Mngtdb == nil {
		return
	}
	type connRow struct {
		Id     string `db:"id"`
		DbType string `db:"db_type"`
	}
	var list []connRow
	if err := database.Mngtdb.Select(&list, "SELECT id, db_type FROM t_conn"); err != nil {
		// 表不存在时静默跳过（与 cleanExpiredMetrics 一致）
		if !dberr.IsTableNotExist(err) {
			logger.PrintErrf("采集器查询连接列表失败", err)
		}
		return
	}
	for _, r := range list {
		if r.DbType == "sqlite" {
			continue // SQLite 无 QPS/TPS/缓冲池指标
		}
		db := conn.GetConnNoCheck(r.Id)
		if db == nil {
			continue
		}
		persistMetrics(r.Id, collectMetricsSnapshot(db, r.DbType))
		if r.DbType == "mysql" || r.DbType == "mariadb" {
			bp := collectBufferPoolStats(db, r.DbType)
			persistSingleMetric(r.Id, MetricBufferPoolHitRate, bp.HitRate)
		}
	}
}