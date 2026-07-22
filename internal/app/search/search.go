package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/app/conn"
	"websql/internal/dialect"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type ObjectSearchResult struct {
	Type       string `json:"type"`
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Comment    string `json:"comment"`
	MatchField string `json:"matchField"`
	MatchText  string `json:"matchText"`
}

type DataSearchResult struct {
	Schema    string `json:"schema"`
	Table     string `json:"table"`
	Column    string `json:"column"`
	Value     string `json:"value"`
	MatchText string `json:"matchText"`
	RowCount  int    `json:"rowCount"`
}

type SearchResponse struct {
	Query          string               `json:"query"`
	SearchType     string               `json:"searchType"`
	TotalResults   int                  `json:"totalResults"`
	ObjectResults  []ObjectSearchResult `json:"objectResults"`
	DataResults    []DataSearchResult   `json:"dataResults"`
	SearchedTables int                  `json:"searchedTables"`
	Duration       string               `json:"duration"`
}

func SearchObjects(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	keyword := c.Query("keyword")
	searchType := c.DefaultQuery("searchType", "all")

	if connId == "" {
		response.WriteOK(c, map[string]any{"results": []ObjectSearchResult{}, "totalResults": 0, "query": keyword, "searchType": searchType})
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db := conn.GetConn(connId, authorization)
	if db == nil {
		response.WriteOK(c, map[string]any{"results": []ObjectSearchResult{}, "totalResults": 0, "query": keyword, "searchType": searchType, "error": "连接不可用"})
		return
	}
	dbType := db.DriverName()

	// 如果 schema 为空，尝试从连接配置中获取默认 schema
	if schema == "" {
		schema = conn.GetConnDefaultSchema(connId)
	}

	if strings.TrimSpace(keyword) == "" {
		response.WriteOK(c, map[string]any{"results": []ObjectSearchResult{}, "totalResults": 0})
		return
	}

	keyword = strings.TrimSpace(keyword)
	lowerKeyword := strings.ToLower(keyword)
	likeKeyword := "%" + keyword + "%"

	results := make([]ObjectSearchResult, 0)

	if searchType == "all" || searchType == "table" {
		sqlTmpl, _ := dialect.SQL_DIALECT[dbType]["listTable"]
		switch dbType {
		case "oracle":
			rows, _ := db.Queryx(sqlTmpl, "notexists")
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, tableType, comment string
					if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
						log.Printf("扫描行失败: %v", err)
						continue
					}
					if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, ObjectSearchResult{
							Type:       "table",
							Schema:     schema,
							Name:       tableName,
							Comment:    comment,
							MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword),
							MatchText:  keyword,
						})
					}
				}
			}
		default:
			rows, _ := db.Queryx(sqlTmpl, schema)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, tableType, comment string
					if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
						log.Printf("扫描行失败: %v", err)
						continue
					}
					if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, ObjectSearchResult{
							Type:       "table",
							Schema:     schema,
							Name:       tableName,
							Comment:    comment,
							MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword),
							MatchText:  keyword,
						})
					}
				}
			}
		}
	}

	if searchType == "all" || searchType == "column" {
		switch dbType {
		case "oracle":
			sql := `SELECT B.TABLE_NAME,B.COLUMN_NAME,A.COMMENTS FROM USER_COL_COMMENTS A LEFT JOIN USER_TAB_COLUMNS B ON A.TABLE_NAME = B.TABLE_NAME AND a.COLUMN_NAME = b.COLUMN_NAME WHERE 'notexists' <> :1`
			rows, _ := db.Queryx(sql, "notexists")
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, colName, comment string
					if err := rows.Scan(&tableName, &colName, &comment); err != nil {
						log.Printf("扫描行失败: %v", err)
						continue
					}
					if matchName(colName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, ObjectSearchResult{
							Type:       "column",
							Schema:     schema,
							Name:       fmt.Sprintf("%s.%s", tableName, colName),
							Comment:    comment,
							MatchField: getMatchField(colName, lowerKeyword, comment, lowerKeyword),
							MatchText:  keyword,
						})
					}
				}
			}
		default:
			sql := `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND (LOWER(COLUMN_NAME) LIKE ? OR LOWER(COLUMN_COMMENT) LIKE ?)`
			rows, err := db.Queryx(sql, schema, likeKeyword, likeKeyword)
			if err == nil && rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, colName, comment string
					if err := rows.Scan(&tableName, &colName, &comment); err != nil {
						log.Printf("扫描行失败: %v", err)
						continue
					}
					results = append(results, ObjectSearchResult{
						Type:       "column",
						Schema:     schema,
						Name:       fmt.Sprintf("%s.%s", tableName, colName),
						Comment:    comment,
						MatchField: "name",
						MatchText:  keyword,
					})
				}
			}
		}
	}

	if searchType == "all" || searchType == "index" {
		sql := fmt.Sprintf(`SELECT TABLE_NAME, INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA='%s' AND LOWER(INDEX_NAME) LIKE ?`, schema)
		rows, err := db.Queryx(sql, likeKeyword)
		if err == nil && rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, idxName string
				if err := rows.Scan(&tableName, &idxName); err != nil {
					log.Printf("扫描行失败: %v", err)
					continue
				}
				if matchName(idxName, lowerKeyword) {
					results = append(results, ObjectSearchResult{
						Type:       "index",
						Schema:     schema,
						Name:       fmt.Sprintf("%s.%s", tableName, idxName),
						Comment:    "",
						MatchField: "name",
						MatchText:  keyword,
					})
				}
			}
		}
	}

	if searchType == "all" || searchType == "view" {
		sql := fmt.Sprintf(`SELECT TABLE_NAME FROM information_schema.VIEWS WHERE TABLE_SCHEMA='%s' AND LOWER(TABLE_NAME) LIKE ?`, schema)
		rows, err := db.Queryx(sql, likeKeyword)
		if err == nil && rows != nil {
			defer rows.Close()
			for rows.Next() {
				var viewName string
				if err := rows.Scan(&viewName); err != nil {
					log.Printf("扫描行失败: %v", err)
					continue
				}
				results = append(results, ObjectSearchResult{
					Type:       "view",
					Schema:     schema,
					Name:       viewName,
					Comment:    "",
					MatchField: "name",
					MatchText:  keyword,
				})
			}
		}
	}

	response.WriteOK(c, map[string]any{
		"results":      results,
		"totalResults": len(results),
		"query":        keyword,
		"searchType":   searchType,
	})
}

func SearchData(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	keyword := c.Query("keyword")
	maxTables := c.DefaultQuery("maxTables", "50")
	timeout := c.DefaultQuery("timeout", "30")

	if connId == "" {
		response.WriteOK(c, map[string]any{"results": []DataSearchResult{}, "totalResults": 0, "searchedTables": 0})
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db := conn.GetConn(connId, authorization)
	if db == nil {
		response.WriteOK(c, map[string]any{"results": []DataSearchResult{}, "totalResults": 0, "searchedTables": 0, "error": "连接不可用"})
		return
	}
	dbType := db.DriverName()

	// 如果 schema 为空，尝试从连接配置中获取默认 schema
	if schema == "" {
		schema = conn.GetConnDefaultSchema(connId)
	}

	if strings.TrimSpace(keyword) == "" {
		response.WriteOK(c, map[string]any{"results": []DataSearchResult{}, "totalResults": 0, "searchedTables": 0})
		return
	}

	keyword = strings.TrimSpace(keyword)
	likeKeyword := "%" + keyword + "%"

	tableList := getAllSearchTables(db, dbType, schema)
	searchCount := 0
	results := make([]DataSearchResult, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 5)

	for _, table := range tableList {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		if searchCount >= parseSearchInt(maxTables) {
			break
		}
		searchCount++

		wg.Add(1)
		safego.GoWithName("search-table", func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			colResults := searchTableData(db, schema, table, likeKeyword, keyword)
			if len(colResults) > 0 {
				mu.Lock()
				results = append(results, colResults...)
				mu.Unlock()
			}
		})
	}

	done := make(chan struct{})
	safego.GoWithName("search-wait", func() {
		wg.Wait()
		close(done)
	})

	_ = timeout
	<-done

	response.WriteOK(c, map[string]any{
		"results":        results,
		"totalResults":   len(results),
		"searchedTables": searchCount,
		"query":          keyword,
	})
}

func searchTableData(db *sqlx.DB, schema, table, likeKeyword, keyword string) []DataSearchResult {
	results := make([]DataSearchResult, 0)

	// 标识符白名单校验，防止 SQL 注入
	if !sanitize.IsValidIdentifier(schema) || !sanitize.IsValidIdentifier(table) {
		return results
	}

	colSQL := "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND DATA_TYPE IN ('varchar','char','text','longtext','mediumtext','tinytext')"
	cols, err := db.Queryx(colSQL, schema, table)
	if err != nil {
		return results
	}
	defer cols.Close()

	var textCols []string
	for cols.Next() {
		var colName string
		if err := cols.Scan(&colName); err != nil {
			log.Printf("扫描行失败: %v", err)
			continue
		}
		if sanitize.IsValidIdentifier(colName) {
			textCols = append(textCols, colName)
		}
	}

	for _, col := range textCols {
		query := fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE `%s` LIKE ?", schema, table, col)
		var count int
		err := db.Get(&count, query, likeKeyword)
		if err != nil {
			continue
		}
		if count > 0 {
			results = append(results, DataSearchResult{
				Schema:    schema,
				Table:     table,
				Column:    col,
				Value:     "",
				MatchText: keyword,
				RowCount:  count,
			})
		}
	}

	return results
}

func SearchAll(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	keyword := c.Query("keyword")
	searchType := c.DefaultQuery("searchType", "all")

	if connId == "" {
		response.WriteOK(c, &SearchResponse{
			Query:         keyword,
			SearchType:    searchType,
			TotalResults:  0,
			ObjectResults: make([]ObjectSearchResult, 0),
			DataResults:   make([]DataSearchResult, 0),
		})
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db := conn.GetConn(connId, authorization)
	if db == nil {
		response.WriteOK(c, &SearchResponse{
			Query:         keyword,
			SearchType:    searchType,
			TotalResults:  0,
			ObjectResults: make([]ObjectSearchResult, 0),
			DataResults:   make([]DataSearchResult, 0),
		})
		return
	}
	dbType := db.DriverName()

	// 如果 schema 为空，尝试从连接配置中获取默认 schema
	if schema == "" {
		schema = conn.GetConnDefaultSchema(connId)
	}

	if strings.TrimSpace(keyword) == "" {
		response.WriteOK(c, &SearchResponse{
			Query:         keyword,
			SearchType:    searchType,
			TotalResults:  0,
			ObjectResults: make([]ObjectSearchResult, 0),
			DataResults:   make([]DataSearchResult, 0),
		})
		return
	}

	keyword = strings.TrimSpace(keyword)
	lowerKeyword := strings.ToLower(keyword)
	likeKeyword := "%" + keyword + "%"

	objectResults := make([]ObjectSearchResult, 0)
	dataResults := make([]DataSearchResult, 0)

	sqlTmpl, _ := dialect.SQL_DIALECT[dbType]["listTable"]
	switch dbType {
	case "oracle":
		rows, _ := db.Queryx(sqlTmpl, "notexists")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, tableType, comment string
				if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
					log.Printf("扫描行失败: %v", err)
					continue
				}
				if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
					objectResults = append(objectResults, ObjectSearchResult{
						Type:       "table",
						Schema:     schema,
						Name:       tableName,
						Comment:    comment,
						MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword),
						MatchText:  keyword,
					})
				}
			}
		}
	default:
		rows, _ := db.Queryx(sqlTmpl, schema)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, tableType, comment string
				if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
					log.Printf("扫描行失败: %v", err)
					continue
				}
				if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
					objectResults = append(objectResults, ObjectSearchResult{
						Type:       "table",
						Schema:     schema,
						Name:       tableName,
						Comment:    comment,
						MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword),
						MatchText:  keyword,
					})
				}
			}
		}
	}

	colSQL := fmt.Sprintf("SELECT TABLE_NAME, COLUMN_NAME, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='%s' AND (LOWER(COLUMN_NAME) LIKE ? OR LOWER(COLUMN_COMMENT) LIKE ?)", schema)
	colRows, err := db.Queryx(colSQL, likeKeyword, likeKeyword)
	if err == nil && colRows != nil {
		defer colRows.Close()
		for colRows.Next() {
			var tableName, colName, comment string
			if err := colRows.Scan(&tableName, &colName, &comment); err != nil {
				log.Printf("扫描行失败: %v", err)
				continue
			}
			objectResults = append(objectResults, ObjectSearchResult{
				Type:       "column",
				Schema:     schema,
				Name:       fmt.Sprintf("%s.%s", tableName, colName),
				Comment:    comment,
				MatchField: "name",
				MatchText:  keyword,
			})
		}
	}

	response.WriteOK(c, &SearchResponse{
		Query:          keyword,
		SearchType:     searchType,
		TotalResults:   len(objectResults) + len(dataResults),
		ObjectResults:  objectResults,
		DataResults:    dataResults,
		SearchedTables: len(objectResults),
	})
}

func GetSearchTables(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

	if connId == "" {
		response.WriteOK(c, map[string]any{"tables": []any{}})
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db := conn.GetConn(connId, authorization)
	if db == nil {
		response.WriteOK(c, map[string]any{"tables": []any{}, "error": "连接不可用"})
		return
	}
	dbType := db.DriverName()

	// 如果 schema 为空，尝试从连接配置中获取默认 schema
	if schema == "" {
		schema = conn.GetConnDefaultSchema(connId)
	}

	tables := getAllSearchTables(db, dbType, schema)

	type TableInfo struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	}

	infos := make([]TableInfo, 0)
	for _, table := range tables {
		var comment string
		db.Get(&comment, fmt.Sprintf("SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s'", schema, table))
		infos = append(infos, TableInfo{Name: table, Comment: comment})
	}

	response.WriteOK(c, map[string]any{"tables": infos})
}

func getAllSearchTables(db *sqlx.DB, dbType, schema string) []string {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
	switch dbType {
	case "oracle":
		rows, err := db.Query(sqlTmpl, "notexists")
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType string
			var tableComment sql.NullString
			if err := rows.Scan(&tableName, &tableType, &tableComment); err != nil {
				log.Printf("扫描行失败: %v", err)
				continue
			}
			result = append(result, strings.TrimSpace(tableName))
		}
	default:
		rows, err := db.Query(sqlTmpl, schema)
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType string
			var tableComment sql.NullString
			if err := rows.Scan(&tableName, &tableType, &tableComment); err != nil {
				log.Printf("扫描行失败: %v", err)
				continue
			}
			result = append(result, strings.TrimSpace(tableName))
		}
	}
	return result
}

func matchName(name, keyword string) bool {
	return strings.Contains(strings.ToLower(name), keyword)
}

func matchStr(s, keyword string) bool {
	if s == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), keyword)
}

func getMatchField(name, nameKw, comment, commentKw string) string {
	if matchName(name, nameKw) {
		return "name"
	}
	if matchStr(comment, commentKw) {
		return "comment"
	}
	return "name"
}

func parseSearchInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	if n <= 0 {
		return 50
	}
	return n
}

// defaultMaxResults 默认最大返回结果数量，避免结果过多导致内存暴涨或前端卡死
const defaultMaxResults = 200

// BatchObjectResult 批量搜索时每条结果额外携带 connId 信息
type BatchObjectResult struct {
	ObjectSearchResult
	ConnId   string `json:"connId"`
	ConnName string `json:"connName,omitempty"`
}

// escapeLikeKeyword 转义 SQL LIKE 通配符，防止用户输入的 % 和 _ 影响匹配
func escapeLikeKeyword(keyword string) string {
	keyword = strings.ReplaceAll(keyword, `\`, `\\`)
	keyword = strings.ReplaceAll(keyword, `%`, `\%`)
	keyword = strings.ReplaceAll(keyword, `_`, `\_`)
	return "%" + keyword + "%"
}

// SearchObjectsBatch 批量搜索数据库对象（SSE 流式响应，查到 1 条推送 1 条）。
// 支持同时搜索多个连接，使用 goroutine 并发搜索，结果实时推送给前端。
// 参数：
//   - connIds: 逗号分隔的连接 ID 列表（为空时搜索用户权限内所有连接）
//   - schema: 可选，指定 schema；为空时搜索连接的默认 schema
//   - keyword: 搜索关键词（支持模糊匹配表名/表注释、字段名/字段注释）
//   - searchType: 搜索类型 (table/view/column/index/all)
//
// 注意：当 searchType 为 column 或 index 时，必须指定 connIds 和 schema，否则返回错误。
//
// SSE 事件：
//   - event: result  — 每条搜索结果（实时推送）
//   - event: done    — 搜索完成，携带 {"totalResults":N}
//   - event: error   — 参数校验失败等错误
func SearchObjectsBatch(c *gin.Context) {
	connIdsStr := c.Query("connIds")
	schema := c.Query("schema")
	keyword := c.Query("keyword")
	searchType := c.DefaultQuery("searchType", "all")

	// SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	// 按字段或索引搜索时，必须指定连接和 schema（数据量太大，不能全库扫描）
	if searchType == "column" || searchType == "index" {
		if strings.TrimSpace(connIdsStr) == "" || strings.TrimSpace(schema) == "" {
			fmt.Fprintf(c.Writer, "event: error\ndata: {\"msg\":\"按字段或索引搜索时必须指定连接和Schema\"}\n\n")
			c.Writer.Flush()
			return
		}
	}

	if strings.TrimSpace(keyword) == "" {
		fmt.Fprintf(c.Writer, "event: done\ndata: {\"totalResults\":0}\n\n")
		c.Writer.Flush()
		return
	}

	keyword = strings.TrimSpace(keyword)
	authorization := appctx.Ctx.GetAuthorization(c)

	// 未指定连接时，使用当前用户权限内的所有连接
	var connIds []string
	if strings.TrimSpace(connIdsStr) == "" {
		connIds = conn.GetUserConnIds(authorization)
	} else {
		connIds = strings.Split(connIdsStr, ",")
	}

	if len(connIds) == 0 {
		fmt.Fprintf(c.Writer, "event: done\ndata: {\"totalResults\":0}\n\n")
		c.Writer.Flush()
		return
	}

	// 结果上限（防止无限内存增长）
	maxResults := 2000

	resultCh := make(chan BatchObjectResult, 64)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	// 生产者：并发搜索所有连接
	go func() {
		defer close(resultCh)
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)

		for _, cid := range connIds {
			cid = strings.TrimSpace(cid)
			if cid == "" {
				continue
			}
			wg.Add(1)
			connId := cid
			safego.GoWithName("search-batch-conn", func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				db := conn.GetConn(connId, authorization)
				if db == nil {
					log.Printf("[SearchObjectsBatch] connId=%s 连接不可用，跳过", connId)
					return
				}

				targetSchema := schema
				if targetSchema == "" {
					targetSchema = conn.GetConnDefaultSchema(connId)
				}

				searchObjectsForConnStream(ctx, db, connId, targetSchema, keyword, searchType, maxResults, resultCh)
			})
		}
		wg.Wait()
	}()

	// 消费者：逐条实时推送所有结果
	totalResults := 0
	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				fmt.Fprintf(c.Writer, "event: done\ndata: {\"totalResults\":%d}\n\n", totalResults)
				c.Writer.Flush()
				return
			}
			totalResults++
			data, _ := json.Marshal(result)
			fmt.Fprintf(c.Writer, "event: result\ndata: %s\n\n", string(data))
			c.Writer.Flush()
		case <-ctx.Done():
			fmt.Fprintf(c.Writer, "event: done\ndata: {\"totalResults\":%d,\"timeout\":true}\n\n", totalResults)
			c.Writer.Flush()
			return
		}
	}
}

// searchObjectsForConnStream 在单个连接下搜索数据库对象，结果逐条写入 channel（支持流式推送）。
// 内部根据 dbType 分发到对应数据库的搜索逻辑，修复了 SQL 注入、Oracle 条件恒 false、SQLite 不支持等问题。
func searchObjectsForConnStream(ctx context.Context, db *sqlx.DB, connId, schema, keyword, searchType string, maxResults int, ch chan<- BatchObjectResult) {
	dbType := db.DriverName()

	// schema 为空时，依次尝试从连接配置、数据库连接中获取默认 schema
	if schema == "" {
		schema = conn.GetConnDefaultSchema(connId)
	}
	if schema == "" {
		schema = getCurrentSchema(db, dbType)
	}
	if schema == "" {
		log.Printf("[searchObjectsForConnStream] connId=%s 无法获取 schema，跳过", connId)
		return
	}

	lowerKeyword := strings.ToLower(keyword)
	likeKeyword := escapeLikeKeyword(keyword)

	// sent 用于统计已发送结果数（跨 searchType），达到 maxResults 后停止
	sent := 0

	// emit 尝试将结果写入 channel，返回 false 表示已达上限或 context 取消
	emit := func(r BatchObjectResult) bool {
		if sent >= maxResults {
			return false
		}
		select {
		case ch <- r:
			sent++
			return true
		case <-ctx.Done():
			return false
		}
	}

	// ===== TABLE 搜索 =====
	if searchType == "all" || searchType == "table" {
		searchTables(db, dbType, connId, schema, lowerKeyword, likeKeyword, keyword, emit)
	}

	// ===== VIEW 搜索 =====
	if sent < maxResults && (searchType == "all" || searchType == "view") {
		searchViews(db, dbType, connId, schema, lowerKeyword, likeKeyword, keyword, emit)
	}

	// ===== COLUMN 搜索 =====
	if sent < maxResults && (searchType == "all" || searchType == "column") {
		searchColumns(db, dbType, connId, schema, lowerKeyword, likeKeyword, keyword, emit)
	}

	// ===== INDEX 搜索 =====
	if sent < maxResults && (searchType == "all" || searchType == "index") {
		searchIndexes(db, dbType, connId, schema, lowerKeyword, likeKeyword, keyword, emit)
	}
}

// getCurrentSchema 从数据库连接中查询当前 schema
func getCurrentSchema(db *sqlx.DB, dbType string) string {
	var schema string
	switch dbType {
	case "mysql", "mariadb":
		db.Get(&schema, "SELECT DATABASE()")
	case "oracle":
		db.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		schema = strings.ToUpper(schema)
	case "sqlite":
		schema = "main"
	}
	return schema
}

// searchTables 搜索表（支持 Oracle、MySQL/MariaDB、SQLite）
func searchTables(db *sqlx.DB, dbType, connId, schema, lowerKeyword, likeKeyword, keyword string, emit func(BatchObjectResult) bool) {
	switch dbType {
	case "oracle":
		// Oracle: 在数据库侧过滤，避免全表扫描后应用层过滤
		sql := `SELECT TABLE_NAME, TABLE_TYPE, COMMENTS FROM USER_TAB_COMMENTS WHERE LOWER(TABLE_NAME) LIKE :1 OR LOWER(COMMENTS) LIKE :2 ORDER BY TABLE_NAME`
		rows, err := db.Queryx(sql, likeKeyword, likeKeyword)
		if err != nil {
			log.Printf("[searchTables] connId=%s oracle query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, tableType, comment string
			if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
				log.Printf("[searchTables] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "table", Schema: schema, Name: tableName,
					Comment: comment, MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "sqlite":
		// SQLite: 在数据库侧过滤表名（SQLite 不支持表注释）
		sql := `SELECT name AS TABLE_NAME, type AS TABLE_TYPE, '' AS table_comment FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' AND LOWER(name) LIKE ?1 ORDER BY name`
		rows, err := db.Queryx(sql, likeKeyword)
		if err != nil {
			log.Printf("[searchTables] connId=%s sqlite query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, tableType, comment string
			if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
				log.Printf("[searchTables] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "table", Schema: schema, Name: tableName,
					Comment: comment, MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "mysql", "mariadb":
		// MySQL/MariaDB: 参数化查询，在数据库侧过滤
		sql := `SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND (LOWER(TABLE_NAME) LIKE ? OR LOWER(TABLE_COMMENT) LIKE ?) ORDER BY TABLE_NAME`
		rows, err := db.Queryx(sql, schema, likeKeyword, likeKeyword)
		if err != nil {
			log.Printf("[searchTables] connId=%s mysql query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, tableType, comment string
			if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
				log.Printf("[searchTables] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "table", Schema: schema, Name: tableName,
					Comment: comment, MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	default:
		// 其他数据库：使用 dialect 模板 + 应用层过滤（兜底）
		sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
		if !ok {
			log.Printf("[searchTables] connId=%s unsupported dbType=%s", connId, dbType)
			return
		}
		rows, err := db.Queryx(sqlTmpl, schema)
		if err != nil {
			log.Printf("[searchTables] connId=%s query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, tableType, comment string
			if err := rows.Scan(&tableName, &tableType, &comment); err != nil {
				log.Printf("[searchTables] connId=%s scan failed: %v", connId, err)
				continue
			}
			if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
				if !emit(BatchObjectResult{
					ObjectSearchResult: ObjectSearchResult{
						Type: "table", Schema: schema, Name: tableName,
						Comment: comment, MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
					},
					ConnId: connId,
				}) {
					rows.Close()
					return
				}
			}
		}
		rows.Close()
	}
}

// searchViews 搜索视图（支持 Oracle、MySQL/MariaDB、SQLite）
func searchViews(db *sqlx.DB, dbType, connId, schema, lowerKeyword, likeKeyword, keyword string, emit func(BatchObjectResult) bool) {
	switch dbType {
	case "oracle":
		sql := `SELECT VIEW_NAME FROM USER_VIEWS WHERE LOWER(VIEW_NAME) LIKE :1`
		rows, err := db.Queryx(sql, likeKeyword)
		if err != nil {
			log.Printf("[searchViews] connId=%s oracle query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var viewName string
			if err := rows.Scan(&viewName); err != nil {
				log.Printf("[searchViews] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "view", Schema: schema, Name: viewName,
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "sqlite":
		sql := `SELECT name FROM sqlite_master WHERE type = 'view' AND name NOT LIKE 'sqlite_%' AND LOWER(name) LIKE ?1`
		rows, err := db.Queryx(sql, likeKeyword)
		if err != nil {
			log.Printf("[searchViews] connId=%s sqlite query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var viewName string
			if err := rows.Scan(&viewName); err != nil {
				log.Printf("[searchViews] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "view", Schema: schema, Name: viewName,
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "mysql", "mariadb":
		// 修复：使用参数化查询而非 fmt.Sprintf 拼接 schema，防止 SQL 注入
		sql := `SELECT TABLE_NAME FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ? AND LOWER(TABLE_NAME) LIKE ?`
		rows, err := db.Queryx(sql, schema, likeKeyword)
		if err != nil {
			log.Printf("[searchViews] connId=%s mysql query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var viewName string
			if err := rows.Scan(&viewName); err != nil {
				log.Printf("[searchViews] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "view", Schema: schema, Name: viewName,
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	default:
		log.Printf("[searchViews] connId=%s unsupported dbType=%s for view search", connId, dbType)
	}
}

// searchColumns 搜索字段（支持 Oracle、MySQL/MariaDB、SQLite）
func searchColumns(db *sqlx.DB, dbType, connId, schema, lowerKeyword, likeKeyword, keyword string, emit func(BatchObjectResult) bool) {
	switch dbType {
	case "oracle":
		// Oracle: 在数据库侧过滤列名和注释
		sql := `SELECT B.TABLE_NAME, B.COLUMN_NAME, NVL(A.COMMENTS,'') AS COMMENTS
			FROM USER_COL_COMMENTS A
			JOIN USER_TAB_COLUMNS B ON A.TABLE_NAME = B.TABLE_NAME AND A.COLUMN_NAME = B.COLUMN_NAME
			WHERE LOWER(B.COLUMN_NAME) LIKE :1 OR LOWER(A.COMMENTS) LIKE :2`
		rows, err := db.Queryx(sql, likeKeyword, likeKeyword)
		if err != nil {
			log.Printf("[searchColumns] connId=%s oracle query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, colName, comment string
			if err := rows.Scan(&tableName, &colName, &comment); err != nil {
				log.Printf("[searchColumns] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "column", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, colName),
					Comment: comment, MatchField: getMatchField(colName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "sqlite":
		// SQLite: 先列出所有表，再逐表查询 pragma_table_info 并在应用层过滤
		tableRows, err := db.Queryx(`SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%'`)
		if err != nil {
			log.Printf("[searchColumns] connId=%s sqlite list tables failed: %v", connId, err)
			return
		}
		var tables []string
		for tableRows.Next() {
			var tName string
			if err := tableRows.Scan(&tName); err != nil {
				continue
			}
			tables = append(tables, tName)
		}
		tableRows.Close()

		for _, tbl := range tables {
			colRows, err := db.Queryx(fmt.Sprintf(`SELECT name FROM pragma_table_info('%s')`, strings.ReplaceAll(tbl, "'", "''")))
			if err != nil {
				log.Printf("[searchColumns] connId=%s sqlite pragma_table_info(%s) failed: %v", connId, tbl, err)
				continue
			}
			for colRows.Next() {
				var colName string
				if err := colRows.Scan(&colName); err != nil {
					continue
				}
				if matchName(colName, lowerKeyword) {
					if !emit(BatchObjectResult{
						ObjectSearchResult: ObjectSearchResult{
							Type: "column", Schema: schema, Name: fmt.Sprintf("%s.%s", tbl, colName),
							Comment: "", MatchField: "name", MatchText: keyword,
						},
						ConnId: connId,
					}) {
						colRows.Close()
						return
					}
				}
			}
			colRows.Close()
		}

	case "mysql", "mariadb":
		// MySQL/MariaDB: 参数化查询
		sql := `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND (LOWER(COLUMN_NAME) LIKE ? OR LOWER(COLUMN_COMMENT) LIKE ?)`
		rows, err := db.Queryx(sql, schema, likeKeyword, likeKeyword)
		if err != nil {
			log.Printf("[searchColumns] connId=%s mysql query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, colName, comment string
			if err := rows.Scan(&tableName, &colName, &comment); err != nil {
				log.Printf("[searchColumns] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "column", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, colName),
					Comment: comment, MatchField: getMatchField(colName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	default:
		log.Printf("[searchColumns] connId=%s unsupported dbType=%s for column search", connId, dbType)
	}
}

// searchIndexes 搜索索引（支持 Oracle、MySQL/MariaDB、SQLite）
func searchIndexes(db *sqlx.DB, dbType, connId, schema, lowerKeyword, likeKeyword, keyword string, emit func(BatchObjectResult) bool) {
	switch dbType {
	case "oracle":
		sql := `SELECT TABLE_NAME, INDEX_NAME FROM USER_INDEXES WHERE LOWER(INDEX_NAME) LIKE :1`
		rows, err := db.Queryx(sql, likeKeyword)
		if err != nil {
			log.Printf("[searchIndexes] connId=%s oracle query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, idxName string
			if err := rows.Scan(&tableName, &idxName); err != nil {
				log.Printf("[searchIndexes] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "index", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, idxName),
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "sqlite":
		// SQLite: 通过 sqlite_master 搜索索引
		sql := `SELECT tbl_name, name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%' AND LOWER(name) LIKE ?1`
		rows, err := db.Queryx(sql, likeKeyword)
		if err != nil {
			log.Printf("[searchIndexes] connId=%s sqlite query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, idxName string
			if err := rows.Scan(&tableName, &idxName); err != nil {
				log.Printf("[searchIndexes] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "index", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, idxName),
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	case "mysql", "mariadb":
		// 修复：使用参数化查询而非 fmt.Sprintf 拼接 schema，防止 SQL 注入
		sql := `SELECT TABLE_NAME, INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND LOWER(INDEX_NAME) LIKE ?`
		rows, err := db.Queryx(sql, schema, likeKeyword)
		if err != nil {
			log.Printf("[searchIndexes] connId=%s mysql query failed: %v", connId, err)
			return
		}
		for rows.Next() {
			var tableName, idxName string
			if err := rows.Scan(&tableName, &idxName); err != nil {
				log.Printf("[searchIndexes] connId=%s scan failed: %v", connId, err)
				continue
			}
			if !emit(BatchObjectResult{
				ObjectSearchResult: ObjectSearchResult{
					Type: "index", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, idxName),
					MatchField: "name", MatchText: keyword,
				},
				ConnId: connId,
			}) {
				rows.Close()
				return
			}
		}
		rows.Close()

	default:
		log.Printf("[searchIndexes] connId=%s unsupported dbType=%s for index search", connId, dbType)
	}
}
