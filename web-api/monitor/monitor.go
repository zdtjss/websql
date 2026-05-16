package monitor

import (
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type MetricsSnapshot struct {
	Timestamp       string  `json:"timestamp"`
	Connections     int     `json:"connections"`
	ActiveConnections int   `json:"activeConnections"`
	QPS             float64 `json:"qps"`
	TPS             float64 `json:"tps"`
	SlowQueries     int     `json:"slowQueries"`
	LockWaits       int     `json:"lockWaits"`
	ThreadsRunning  int     `json:"threadsRunning"`
	ThreadsConnected int    `json:"threadsConnected"`
	BytesReceived   int64   `json:"bytesReceived"`
	BytesSent       int64   `json:"bytesSent"`
	Uptime          int64   `json:"uptime"`
	Questions       int64   `json:"questions"`
	ComSelect       int64   `json:"comSelect"`
	ComInsert       int64   `json:"comInsert"`
	ComUpdate       int64   `json:"comUpdate"`
	ComDelete       int64   `json:"comDelete"`
}

type ResourceSnapshot struct {
	Timestamp       string  `json:"timestamp"`
	DBName          string  `json:"dbName"`
	DataSize        int64   `json:"dataSize"`
	IndexSize       int64   `json:"indexSize"`
	TableCount      int     `json:"tableCount"`
	TotalRows       int64   `json:"totalRows"`
	BufferPoolSize  int64   `json:"bufferPoolSize"`
	BufferPoolUsed  int64   `json:"bufferPoolUsed"`
	BufferPoolHitRate float64 `json:"bufferPoolHitRate"`
	DiskUsage       int64   `json:"diskUsage"`
	QCacheHitRate   float64 `json:"qCacheHitRate"`
	InnodbRowsRead  int64   `json:"innodbRowsRead"`
	InnodbRowsInserted int64 `json:"innodbRowsInserted"`
	InnodbRowsUpdated  int64 `json:"innodbRowsUpdated"`
	InnodbRowsDeleted  int64 `json:"innodbRowsDeleted"`
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

var metricsCache struct {
	sync.RWMutex
	history []MetricsSnapshot
	maxSize int
}

func init() {
	metricsCache.maxSize = 100
	metricsCache.history = make([]MetricsSnapshot, 0)
}

func GetMetrics(c *gin.Context) {
	connId := c.Query("connId")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

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
			logutils.PrintErrf("获取状态变量失败: %v", err)
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

	metricsCache.Lock()
	metricsCache.history = append(metricsCache.history, snapshot)
	if len(metricsCache.history) > metricsCache.maxSize {
		metricsCache.history = metricsCache.history[len(metricsCache.history)-metricsCache.maxSize:]
	}
	metricsCache.Unlock()

	utils.WriteJson(c.Writer, snapshot)
}

func countLockWaits(conn *sqlx.DB) int {
	var count int
	err := conn.Get(&count, "SELECT COUNT(*) FROM information_schema.INNODB_LOCK_WAITS")
	if err != nil {
		return 0
	}
	return count
}

func GetMetricsHistory(c *gin.Context) {
	metricsCache.RLock()
	defer metricsCache.RUnlock()

	history := make([]MetricsSnapshot, len(metricsCache.history))
	copy(history, metricsCache.history)

	utils.WriteJson(c.Writer, map[string]any{
		"history": history,
		"count":   len(history),
	})
}

func GetResources(c *gin.Context) {
	connId := c.Query("connId")
	schema := c.Query("schema")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
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

		type bufferRow struct {
			Name  string `db:"Variable_name"`
			Value int64  `db:"Value"`
		}
		var bufferVars []bufferRow
		conn.Select(&bufferVars, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_buffer_pool_pages_total','Innodb_buffer_pool_pages_free','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_rows_read','Innodb_rows_inserted','Innodb_rows_updated','Innodb_rows_deleted')")

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
				snapshot.InnodbRowsRead = v.Value
			case "Innodb_rows_inserted":
				snapshot.InnodbRowsInserted = v.Value
			case "Innodb_rows_updated":
				snapshot.InnodbRowsUpdated = v.Value
			case "Innodb_rows_deleted":
				snapshot.InnodbRowsDeleted = v.Value
			}
		}

		pageSize := int64(16384)
		snapshot.BufferPoolSize = totalPages * pageSize
		snapshot.BufferPoolUsed = (totalPages - freePages) * pageSize
		if readReqs > 0 {
			snapshot.BufferPoolHitRate = float64(readReqs-reads) / float64(readReqs) * 100
		}

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

	utils.WriteJson(c.Writer, map[string]any{
		"dbResources":     snapshot,
		"appMemoryAlloc":  memStats.Alloc,
		"appMemoryTotal":  memStats.Sys,
		"appGoroutines":   runtime.NumGoroutine(),
		"appHeapObjects":  memStats.HeapObjects,
	})
}

func GetProcesses(c *gin.Context) {
	connId := c.Query("connId")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	processes := make([]ProcessInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		rows, err := conn.Queryx("SHOW FULL PROCESSLIST")
		if err != nil {
			logutils.PrintErrf("获取进程列表失败", err)
			utils.WriteJson(c.Writer, map[string]any{"processes": processes, "count": 0})
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

	utils.WriteJson(c.Writer, map[string]any{
		"processes": processes,
		"count":     len(processes),
	})
}

func GetServerVariables(c *gin.Context) {
	connId := c.Query("connId")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
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

	utils.WriteJson(c.Writer, map[string]any{
		"variables": variables,
	})
}

func parseStrToInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func init() {
	_ = config.Cfg
}