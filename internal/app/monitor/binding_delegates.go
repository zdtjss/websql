package monitor

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	agentv2 "websql/internal/ai/agent"
	"websql/internal/app/conn"
	"websql/internal/app/system"
	"websql/internal/logger"
	"websql/internal/pkg/sanitize"

	"github.com/cloudwego/eino/schema"
)

// StreamChunk 与 agentv2.StreamChunk 一致，作为 emit 回调的数据类型。
type StreamChunk = agentv2.StreamChunk

// ===== 普通监控指标 =====

// GetMetricsByService 返回当前数据库的核心监控指标快照。
// 业务来自 GetMetrics handler。
func GetMetricsByService(connId, authorization string) (MetricsSnapshot, error) {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()
	snapshot := collectMetricsSnapshot(db, dbType)
	persistMetrics(connId, snapshot, dbType)
	return snapshot, nil
}

// GetResourcesByService 返回数据库资源使用情况。
// 业务来自 GetResources handler。
func GetResourcesByService(connId, schema, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	snapshot := ResourceSnapshot{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		DBName:    schema,
	}

	if dbType == "mysql" || dbType == "mariadb" {
		if schema != "" {
			var dataSize, indexSize float64
			err := db.Get(&dataSize, "SELECT COALESCE(SUM(DATA_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			if err == nil {
				snapshot.DataSize = int64(dataSize)
			}
			err = db.Get(&indexSize, "SELECT COALESCE(SUM(INDEX_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			if err == nil {
				snapshot.IndexSize = int64(indexSize)
			}

			var tableCount int
			db.Get(&tableCount, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
			snapshot.TableCount = tableCount
		}

		bp := collectBufferPoolStats(db, dbType)
		snapshot.BufferPoolSize = bp.Size
		snapshot.BufferPoolUsed = bp.Used
		snapshot.BufferPoolHitRate = bp.HitRate
		snapshot.InnodbRowsRead = bp.RowsRead
		snapshot.InnodbRowsInserted = bp.RowsInserted
		snapshot.InnodbRowsUpdated = bp.RowsUpdated
		snapshot.InnodbRowsDeleted = bp.RowsDeleted

		var qCacheHits, qCacheInserts int64
		db.Get(&qCacheHits, "SHOW GLOBAL STATUS LIKE 'Qcache_hits'")
		db.Get(&qCacheInserts, "SHOW GLOBAL STATUS LIKE 'Qcache_inserts'")
		if qCacheHits+qCacheInserts > 0 {
			snapshot.QCacheHitRate = float64(qCacheHits) / float64(qCacheHits+qCacheInserts) * 100
		}

		var totalRows int64
		db.Get(&totalRows, "SELECT COALESCE(SUM(TABLE_ROWS),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", schema)
		snapshot.TotalRows = totalRows
	}

	if dbType == "mysql" || dbType == "mariadb" {
		persistSingleMetric(connId, MetricBufferPoolHitRate, snapshot.BufferPoolHitRate)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]any{
		"dbResources":    snapshot,
		"appMemoryAlloc": memStats.Alloc,
		"appMemoryTotal": memStats.Sys,
		"appGoroutines":  runtime.NumGoroutine(),
		"appHeapObjects": memStats.HeapObjects,
	}
}

// GetProcessesByService 返回数据库进程列表。
// 业务来自 GetProcesses handler。
func GetProcessesByService(connId, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	processes := make([]ProcessInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		rows, err := db.Queryx("SHOW FULL PROCESSLIST")
		if err != nil {
			logger.PrintErrf("获取进程列表失败", err)
			return map[string]any{"processes": processes, "count": 0}
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
		rows, err := db.Queryx("SELECT sid, serial#, username, status, machine, program FROM v$session WHERE type!='BACKGROUND'")
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

	return map[string]any{
		"processes": processes,
		"count":     len(processes),
	}
}

// GetServerVariablesByService 返回关键服务器变量。
// 业务来自 GetServerVariables handler。
func GetServerVariablesByService(connId, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	variables := make(map[string]string)

	if dbType == "mysql" || dbType == "mariadb" {
		type varRow struct {
			Name  string `db:"Variable_name"`
			Value string `db:"Value"`
		}
		keyVars := []string{"max_connections", "innodb_buffer_pool_size", "version", "character_set_server", "collation_server"}

		for _, key := range keyVars {
			var v varRow
			err := db.Get(&v, "SHOW GLOBAL VARIABLES WHERE Variable_name=?", key)
			if err == nil {
				variables[key] = v.Value
			}
		}
	}

	return map[string]any{
		"variables": variables,
	}
}

// GetAllServerVariablesByService 返回完整服务器变量列表。
// 业务来自 GetAllServerVariables handler。
func GetAllServerVariablesByService(connId, scope, authorization string) VarListResult {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()
	version := getConnDbVersion(connId)

	result := VarListResult{Items: []VarInfo{}, DbType: dbType, Version: version}

	if ok, msg := checkMonitorSupported(dbType, version); !ok {
		result.UnsupportedMessage = msg
		return result
	}

	switch dbType {
	case "mysql", "mariadb":
		result.Supported = true
		type varRow struct {
			Name  string `db:"Variable_name"`
			Value string `db:"Value"`
		}
		var rows []varRow
		var err error
		if scope == "session" {
			err = db.Select(&rows, "SHOW SESSION VARIABLES")
		} else {
			err = db.Select(&rows, "SHOW GLOBAL VARIABLES")
		}
		if err != nil {
			logger.PrintErrf("获取服务器变量失败", err)
			return result
		}
		for _, r := range rows {
			result.Items = append(result.Items, VarInfo{Name: r.Name, Value: r.Value})
		}
	case "oracle":
		result.Supported = true
		type varRow struct {
			Name  string `db:"name"`
			Value string `db:"value"`
		}
		var rows []varRow
		if err := db.Select(&rows, "SELECT name, value FROM v$parameter ORDER BY name"); err != nil {
			logger.PrintErrf("获取 Oracle 参数失败", err)
			return result
		}
		for _, r := range rows {
			result.Items = append(result.Items, VarInfo{Name: r.Name, Value: r.Value})
		}
	default:
		result.Supported = false
		result.UnsupportedMessage = "当前数据库类型不支持查看服务器变量"
	}

	result.Count = len(result.Items)
	return result
}

// GetAllServerStatusByService 返回完整状态指标列表。
// 业务来自 GetAllServerStatus handler。
func GetAllServerStatusByService(connId, authorization string) VarListResult {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()
	version := getConnDbVersion(connId)

	result := VarListResult{Items: []VarInfo{}, DbType: dbType, Version: version}

	if ok, msg := checkMonitorSupported(dbType, version); !ok {
		result.UnsupportedMessage = msg
		return result
	}

	switch dbType {
	case "mysql", "mariadb":
		result.Supported = true
		type varRow struct {
			Name  string `db:"Variable_name"`
			Value string `db:"Value"`
		}
		var rows []varRow
		if err := db.Select(&rows, "SHOW GLOBAL STATUS"); err != nil {
			logger.PrintErrf("获取状态指标失败", err)
			return result
		}
		for _, r := range rows {
			result.Items = append(result.Items, VarInfo{Name: r.Name, Value: r.Value})
		}
	case "oracle":
		result.Supported = true
		type statRow struct {
			Name  string `db:"name"`
			Value string `db:"value"`
		}
		var rows []statRow
		if err := db.Select(&rows, "SELECT name, value FROM v$sysstat ORDER BY name"); err != nil {
			logger.PrintErrf("获取 Oracle 状态指标失败", err)
			return result
		}
		for _, r := range rows {
			result.Items = append(result.Items, VarInfo{Name: r.Name, Value: r.Value})
		}
	default:
		result.Supported = false
		result.UnsupportedMessage = "当前数据库类型不支持查看状态指标"
	}

	result.Count = len(result.Items)
	return result
}

// GetInnodbStatusByService 返回 InnoDB 引擎状态文本。
// 业务来自 GetInnodbStatus handler。
func GetInnodbStatusByService(connId, authorization string) InnoDBStatusResult {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	result := InnoDBStatusResult{}
	if dbType == "mysql" || dbType == "mariadb" {
		result.Supported = true
		type engineRow struct {
			Engine string `db:"Engine"`
			Status string `db:"Status"`
		}
		var er engineRow
		if err := db.Get(&er, "SHOW ENGINE INNODB STATUS"); err != nil {
			logger.PrintErrf("获取 InnoDB 引擎状态失败（可能缺少 PROCESS 权限）", err)
		} else {
			result.Status = er.Status
		}
	}
	return result
}

// GetLocksByService 返回当前锁等待与阻塞事务列表。
// 业务来自 GetLocks handler。
func GetLocksByService(connId, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	locks := make([]LockInfo, 0)

	if dbType == "mysql" || dbType == "mariadb" {
		blockerMap := map[string]string{}
		type waitRow struct {
			Requesting string `db:"requesting_trx_id"`
			Blocking   string `db:"blocking_trx_id"`
		}
		var waits []waitRow
		_ = db.Select(&waits, "SELECT requesting_trx_id, blocking_trx_id FROM information_schema.INNODB_LOCK_WAITS")
		for _, w := range waits {
			blockerMap[w.Requesting] = w.Blocking
		}

		type trxRow struct {
			TrxID       string  `db:"trx_id"`
			ThreadID    int64   `db:"trx_mysql_thread_id"`
			Query       *string `db:"trx_query"`
			WaitSeconds int64   `db:"wait_seconds"`
		}
		var rows []trxRow
		err := db.Select(&rows, `SELECT trx_id, trx_mysql_thread_id, trx_query,
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
			SID             int64   `db:"sid"`
			Username        *string `db:"username"`
			Event           *string `db:"event"`
			SecondsInWait   int64   `db:"seconds_in_wait"`
			BlockingSession *int64  `db:"blocking_session"`
			SQLID           *string `db:"sql_id"`
		}
		var rows []sessRow
		err := db.Select(&rows, `SELECT sid, username, event, seconds_in_wait, blocking_session, sql_id
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

	return map[string]any{
		"locks":     locks,
		"count":     len(locks),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	}
}

// GetSlowQueriesByService 返回慢查询列表。
// 业务来自 GetSlowQueries handler。
func GetSlowQueriesByService(connId, limitStr, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	limit := parseLimit(limitStr, 20)
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
		err := db.Select(&rows, `SELECT DIGEST_TEXT, AVG_TIMER_WAIT, COUNT_STAR, SUM_ROWS_EXAMINED, LAST_SEEN
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
					AvgMs:        float64(r.AvgTimerWait) / 1e9,
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
		err := db.Select(&rows, fmt.Sprintf(`SELECT * FROM (
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
					AvgMs:      float64(r.ElapsedTime) / 1000,
					ExecCount:  r.Executions,
					LastSeen:   r.SQLID,
				})
			}
		}
	}

	return map[string]any{
		"queries":   queries,
		"count":     len(queries),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	}
}

// GetTopTablesByService 返回 TOP N 表统计。
// 业务来自 GetTopTables handler。
func GetTopTablesByService(connId, schema, limitStr, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	limit := parseLimit(limitStr, 20)
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
			err = db.Select(&rows, `SELECT TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, DATA_FREE
				FROM information_schema.TABLES
				WHERE TABLE_SCHEMA = ?
				ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC
				LIMIT ?`, schema, limit)
		} else {
			err = db.Select(&rows, `SELECT TABLE_NAME, ENGINE, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH, DATA_FREE
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
					TableName: r.TableName,
					Engine:    eng,
					TableRows: r.TableRows,
					DataSize:  r.DataLength,
					IndexSize: r.IndexLength,
					DataFree:  r.DataFree,
				})
			}
		}
	} else if dbType == "oracle" {
		type segRow struct {
			SegmentName string `db:"segment_name"`
			Bytes       int64  `db:"bytes"`
		}
		var rows []segRow
		err := db.Select(&rows, fmt.Sprintf(`SELECT * FROM (
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

	return map[string]any{
		"tables":    tables,
		"count":     len(tables),
		"supported": dbType == "mysql" || dbType == "mariadb" || dbType == "oracle",
	}
}

// GetMetricHistoryByService 查询指标历史趋势。
// 业务来自 GetMetricHistory handler。
func GetMetricHistoryByService(connId, metric, fromStr, toStr, interval string) (map[string]any, error) {
	if connId == "" || metric == "" {
		return nil, fmt.Errorf("缺少必要参数 connId 或 metric")
	}
	if !allowedMetrics[metric] {
		return nil, fmt.Errorf("非法的指标名: %s", metric)
	}
	if !sanitize.IsValidIdentifier(metric) {
		return nil, fmt.Errorf("非法的指标名格式: %s", metric)
	}

	now := time.Now()
	from, ok := parseHistoryTime(fromStr, now.Add(-1*time.Hour))
	if !ok {
		return nil, fmt.Errorf("非法的 from 时间格式，支持 2006-01-02 或 2006-01-02 15:04:05")
	}
	to, ok := parseHistoryTime(toStr, now)
	if !ok {
		return nil, fmt.Errorf("非法的 to 时间格式，支持 2006-01-02 或 2006-01-02 15:04:05")
	}

	if getDB() == nil {
		return map[string]any{"points": []metricPoint{}}, nil
	}

	points, err := queryMetricHistory(connId, metric, from, to, interval)
	if err != nil {
		return nil, err
	}
	return map[string]any{"points": points}, nil
}

// ===== AI 分析 (SSE 流式) =====

// AIAnalyzeByService 执行监控变量/状态 AI 分析，通过 emit 回调推送 StreamChunk。
// 业务来自 AIAnalyze handler。
// 错误约定与 sqlopt.OptimizeByService 一致:
//   - 校验类错误返回 error,由调用方决定是否再 emit error
//   - 流式错误直接通过 emit 推送 {Type:"error"}
//   - 完成时调用方负责 emit {Type:"done"} (本 service 内部已 emit done)
func AIAnalyzeByService(ctx context.Context, req *AIAnalyzeRequest, emit func(StreamChunk)) error {
	if req.Kind != "variables" && req.Kind != "status" {
		return fmt.Errorf("非法的分析类型: %s", req.Kind)
	}
	if len(req.Data) == 0 {
		return fmt.Errorf("当前没有可分析的数据")
	}

	if req.DbType == "" || req.Version == "" {
		dbType, _, version := getConnDbTypeAndVersion(req.ConnID)
		if req.DbType == "" {
			req.DbType = dbType
		}
		if req.Version == "" {
			req.Version = version
		}
	}

	truncated := false
	if len(req.Data) > aiAnalyzeMaxItems {
		req.Data = req.Data[:aiAnalyzeMaxItems]
		truncated = true
	}

	aiCfg := system.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		return fmt.Errorf("AI 模型未配置，请联系管理员在系统设置中配置 AI 模型")
	}

	cm, err := agentv2.BuildChatModel(ctx, aiCfg)
	if err != nil {
		logger.PrintErrf("创建监控分析模型失败", err)
		emit(StreamChunk{Type: "error", Content: "AI 模型初始化失败: " + err.Error()})
		return nil
	}

	sysPrompt := buildAIAnalyzeSystemPrompt(req.Kind, req.DbType, req.Version, truncated)
	userPrompt := buildAIAnalyzeUserPrompt(req.Data)

	msgs := []*schema.Message{
		{Role: schema.System, Content: sysPrompt},
		{Role: schema.User, Content: userPrompt},
	}

	sr, err := cm.Stream(ctx, msgs)
	if err != nil {
		logger.PrintErrf("监控分析流式调用失败", err)
		emit(StreamChunk{Type: "error", Content: "AI 调用失败: " + err.Error()})
		return nil
	}

	for {
		chunk, recvErr := sr.Recv()
		if recvErr != nil {
			break
		}
		if chunk.ReasoningContent != "" {
			emit(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
		}
		if chunk.Content != "" {
			emit(StreamChunk{Type: "content", Content: chunk.Content})
		}
	}

	emit(StreamChunk{Type: "done"})
	return nil
}
