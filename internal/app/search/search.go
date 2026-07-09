package search

import (
	"fmt"
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

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

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
			rows, _ := conn.Queryx(sqlTmpl, "notexists")
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, tableType, comment string
					rows.Scan(&tableName, &tableType, &comment)
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
			rows, _ := conn.Queryx(sqlTmpl, schema)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, tableType, comment string
					rows.Scan(&tableName, &tableType, &comment)
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
			rows, _ := conn.Queryx(sql, "notexists")
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, colName, comment string
					rows.Scan(&tableName, &colName, &comment)
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
			rows, err := conn.Queryx(sql, schema, likeKeyword, likeKeyword)
			if err == nil && rows != nil {
				defer rows.Close()
				for rows.Next() {
					var tableName, colName, comment string
					rows.Scan(&tableName, &colName, &comment)
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
		rows, err := conn.Queryx(sql, likeKeyword)
		if err == nil && rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, idxName string
				rows.Scan(&tableName, &idxName)
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
		rows, err := conn.Queryx(sql, likeKeyword)
		if err == nil && rows != nil {
			defer rows.Close()
			for rows.Next() {
				var viewName string
				rows.Scan(&viewName)
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

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	if strings.TrimSpace(keyword) == "" {
		response.WriteOK(c, map[string]any{"results": []DataSearchResult{}, "totalResults": 0, "searchedTables": 0})
		return
	}

	keyword = strings.TrimSpace(keyword)
	likeKeyword := "%" + keyword + "%"

	tableList := getAllSearchTables(conn, dbType, schema)
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

			colResults := searchTableData(conn, schema, table, likeKeyword, keyword)
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

func searchTableData(conn *sqlx.DB, schema, table, likeKeyword, keyword string) []DataSearchResult {
	results := make([]DataSearchResult, 0)

	// 标识符白名单校验，防止 SQL 注入
	if !sanitize.IsValidIdentifier(schema) || !sanitize.IsValidIdentifier(table) {
		return results
	}

	colSQL := "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND DATA_TYPE IN ('varchar','char','text','longtext','mediumtext','tinytext')"
	cols, err := conn.Queryx(colSQL, schema, table)
	if err != nil {
		return results
	}
	defer cols.Close()

	var textCols []string
	for cols.Next() {
		var colName string
		cols.Scan(&colName)
		if sanitize.IsValidIdentifier(colName) {
			textCols = append(textCols, colName)
		}
	}

	for _, col := range textCols {
		query := fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE `%s` LIKE ?", schema, table, col)
		var count int
		err := conn.Get(&count, query, likeKeyword)
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

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

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
		rows, _ := conn.Queryx(sqlTmpl, "notexists")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, tableType, comment string
				rows.Scan(&tableName, &tableType, &comment)
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
		rows, _ := conn.Queryx(sqlTmpl, schema)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var tableName, tableType, comment string
				rows.Scan(&tableName, &tableType, &comment)
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
	colRows, err := conn.Queryx(colSQL, likeKeyword, likeKeyword)
	if err == nil && colRows != nil {
		defer colRows.Close()
		for colRows.Next() {
			var tableName, colName, comment string
			colRows.Scan(&tableName, &colName, &comment)
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

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	tables := getAllSearchTables(conn, dbType, schema)

	type TableInfo struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	}

	infos := make([]TableInfo, 0)
	for _, table := range tables {
		var comment string
		conn.Get(&comment, fmt.Sprintf("SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s'", schema, table))
		infos = append(infos, TableInfo{Name: table, Comment: comment})
	}

	response.WriteOK(c, map[string]any{"tables": infos})
}

func getAllSearchTables(conn *sqlx.DB, dbType, schema string) []string {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
	switch dbType {
	case "oracle":
		rows, err := conn.Query(sqlTmpl, "notexists")
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			rows.Scan(&tableName, &tableType, &tableComment)
			result = append(result, strings.TrimSpace(tableName))
		}
	default:
		rows, err := conn.Query(sqlTmpl, schema)
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			rows.Scan(&tableName, &tableType, &tableComment)
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