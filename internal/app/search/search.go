package search

import (
	"fmt"
	"log"
	"strings"
	"sync"

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
			var tableName, tableType, tableComment string
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
			var tableName, tableType, tableComment string
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

// BatchObjectResult 批量搜索时每条结果额外携带 connId 信息
type BatchObjectResult struct {
	ObjectSearchResult
	ConnId string `json:"connId"`
}

// SearchObjectsBatch 批量搜索数据库对象，支持同时搜索多个连接。
// 使用 goroutine 并发搜索所有连接/schema，提高全局搜索性能。
// 参数：
//   - connIds: 逗号分隔的连接 ID 列表
//   - schema: 可选，指定 schema；为空时搜索连接的默认 schema
//   - keyword: 搜索关键词
//   - searchType: 搜索类型 (table/view/column/index/all)
func SearchObjectsBatch(c *gin.Context) {
	connIdsStr := c.Query("connIds")
	schema := c.Query("schema")
	keyword := c.Query("keyword")
	searchType := c.DefaultQuery("searchType", "all")

	if strings.TrimSpace(keyword) == "" {
		response.WriteOK(c, map[string]any{"results": []BatchObjectResult{}, "totalResults": 0, "query": keyword, "searchType": searchType})
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
		response.WriteOK(c, map[string]any{"results": []BatchObjectResult{}, "totalResults": 0, "query": keyword, "searchType": searchType})
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	allResults := make([]BatchObjectResult, 0)

	// 并发度限制，避免同时打开过多数据库连接
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
				return // 连接不可用，跳过
			}

			targetSchema := schema
			if targetSchema == "" {
				targetSchema = conn.GetConnDefaultSchema(connId)
			}

			results := searchObjectsForConn(db, connId, targetSchema, keyword, searchType)
			if len(results) > 0 {
				mu.Lock()
				allResults = append(allResults, results...)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	response.WriteOK(c, map[string]any{
		"results":      allResults,
		"totalResults": len(allResults),
		"query":        keyword,
		"searchType":   searchType,
	})
}

// searchObjectsForConn 在单个连接的指定 schema 下搜索数据库对象
func searchObjectsForConn(db *sqlx.DB, connId, schema, keyword, searchType string) []BatchObjectResult {
	dbType := db.DriverName()
	lowerKeyword := strings.ToLower(keyword)
	likeKeyword := "%" + keyword + "%"
	results := make([]BatchObjectResult, 0)

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
						continue
					}
					if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, BatchObjectResult{
							ObjectSearchResult: ObjectSearchResult{
								Type: "table", Schema: schema, Name: tableName,
								Comment: comment, MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
							},
							ConnId: connId,
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
						continue
					}
					if matchName(tableName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, BatchObjectResult{
							ObjectSearchResult: ObjectSearchResult{
								Type: "table", Schema: schema, Name: tableName,
								Comment: comment, MatchField: getMatchField(tableName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
							},
							ConnId: connId,
						})
					}
				}
			}
		}
	}

	if searchType == "all" || searchType == "view" {
		switch dbType {
		case "oracle":
			// Oracle 视图在 user_views 中
			sql := `SELECT VIEW_NAME FROM USER_VIEWS WHERE LOWER(VIEW_NAME) LIKE :1`
			rows, _ := db.Queryx(sql, likeKeyword)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var viewName string
					if err := rows.Scan(&viewName); err != nil {
						continue
					}
					results = append(results, BatchObjectResult{
						ObjectSearchResult: ObjectSearchResult{
							Type: "view", Schema: schema, Name: viewName,
							MatchField: "name", MatchText: keyword,
						},
						ConnId: connId,
					})
				}
			}
		default:
			sql := fmt.Sprintf(`SELECT TABLE_NAME FROM information_schema.VIEWS WHERE TABLE_SCHEMA='%s' AND LOWER(TABLE_NAME) LIKE ?`, schema)
			rows, err := db.Queryx(sql, likeKeyword)
			if err == nil && rows != nil {
				defer rows.Close()
				for rows.Next() {
					var viewName string
					if err := rows.Scan(&viewName); err != nil {
						continue
					}
					results = append(results, BatchObjectResult{
						ObjectSearchResult: ObjectSearchResult{
							Type: "view", Schema: schema, Name: viewName,
							MatchField: "name", MatchText: keyword,
						},
						ConnId: connId,
					})
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
						continue
					}
					if matchName(colName, lowerKeyword) || matchStr(comment, lowerKeyword) {
						results = append(results, BatchObjectResult{
							ObjectSearchResult: ObjectSearchResult{
								Type: "column", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, colName),
								Comment: comment, MatchField: getMatchField(colName, lowerKeyword, comment, lowerKeyword), MatchText: keyword,
							},
							ConnId: connId,
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
						continue
					}
					results = append(results, BatchObjectResult{
						ObjectSearchResult: ObjectSearchResult{
							Type: "column", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, colName),
							Comment: comment, MatchField: "name", MatchText: keyword,
						},
						ConnId: connId,
					})
				}
			}
		}
	}

	if searchType == "all" || searchType == "index" {
		switch dbType {
		case "oracle":
			sql := `SELECT TABLE_NAME, INDEX_NAME FROM USER_INDEXES WHERE LOWER(INDEX_NAME) LIKE :1`
			rows, _ := db.Queryx(sql, likeKeyword)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, idxName string
					if err := rows.Scan(&tableName, &idxName); err != nil {
						continue
					}
					results = append(results, BatchObjectResult{
						ObjectSearchResult: ObjectSearchResult{
							Type: "index", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, idxName),
							MatchField: "name", MatchText: keyword,
						},
						ConnId: connId,
					})
				}
			}
		default:
			sql := fmt.Sprintf(`SELECT TABLE_NAME, INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA='%s' AND LOWER(INDEX_NAME) LIKE ?`, schema)
			rows, err := db.Queryx(sql, likeKeyword)
			if err == nil && rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, idxName string
					if err := rows.Scan(&tableName, &idxName); err != nil {
						continue
					}
					if matchName(idxName, lowerKeyword) {
						results = append(results, BatchObjectResult{
							ObjectSearchResult: ObjectSearchResult{
								Type: "index", Schema: schema, Name: fmt.Sprintf("%s.%s", tableName, idxName),
								MatchField: "name", MatchText: keyword,
							},
							ConnId: connId,
						})
					}
				}
			}
		}
	}

	return results
}
