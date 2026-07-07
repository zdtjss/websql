package search

import (
	"fmt"
	"strings"
	"sync"

	"websql/internal/app/conn"
	"websql/internal/dialect"
	"websql/internal/pkg/safego"
)

// SearchObjectsByService 在数据库对象(表/列/索引/视图)中搜索关键字。
// 业务来自 SearchObjects handler。
func SearchObjectsByService(connId, schema, keyword, searchType, authorization string) map[string]any {
	if searchType == "" {
		searchType = "all"
	}
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	if strings.TrimSpace(keyword) == "" {
		return map[string]any{"results": []ObjectSearchResult{}, "totalResults": 0}
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
			rows, _ := db.Queryx(sqlTmpl, schema)
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
			rows, _ := db.Queryx(sql, "notexists")
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
			rows, err := db.Queryx(sql, schema, likeKeyword, likeKeyword)
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
		rows, err := db.Queryx(sql, likeKeyword)
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
		rows, err := db.Queryx(sql, likeKeyword)
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

	return map[string]any{
		"results":      results,
		"totalResults": len(results),
		"query":        keyword,
		"searchType":   searchType,
	}
}

// SearchDataByService 在表数据中搜索关键字。
// 业务来自 SearchData handler。
func SearchDataByService(connId, schema, keyword, maxTables, timeout, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	if strings.TrimSpace(keyword) == "" {
		return map[string]any{"results": []DataSearchResult{}, "totalResults": 0, "searchedTables": 0}
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

	return map[string]any{
		"results":        results,
		"totalResults":   len(results),
		"searchedTables": searchCount,
		"query":          keyword,
	}
}

// SearchAllByService 综合搜索对象与数据。
// 业务来自 SearchAll handler。
func SearchAllByService(connId, schema, keyword, searchType, authorization string) *SearchResponse {
	if searchType == "" {
		searchType = "all"
	}
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	if strings.TrimSpace(keyword) == "" {
		return &SearchResponse{
			Query:         keyword,
			SearchType:    searchType,
			TotalResults:  0,
			ObjectResults: make([]ObjectSearchResult, 0),
			DataResults:   make([]DataSearchResult, 0),
		}
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
		rows, _ := db.Queryx(sqlTmpl, schema)
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
	colRows, err := db.Queryx(colSQL, likeKeyword, likeKeyword)
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

	return &SearchResponse{
		Query:          keyword,
		SearchType:     searchType,
		TotalResults:   len(objectResults) + len(dataResults),
		ObjectResults:  objectResults,
		DataResults:    dataResults,
		SearchedTables: len(objectResults),
	}
}

// GetSearchTablesByService 返回可搜索的表列表。
// 业务来自 GetSearchTables handler。
func GetSearchTablesByService(connId, schema, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

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

	return map[string]any{"tables": infos}
}


