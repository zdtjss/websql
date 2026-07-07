package datadict

import (
	"fmt"
	"strings"
	"time"

	"websql/internal/app/conn"
	"websql/internal/pkg/sanitize"
)

// GenerateDictByService 生成数据字典结构。
// 业务来自 GenerateDict handler。
// tablesStr 为空时取该 schema 下全部表。
func GenerateDictByService(connId, schema, tablesStr, authorization string) *DictExport {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getDictTables(db, dbType, schema)
	}

	dict := &DictExport{
		Title:       fmt.Sprintf("数据字典 - %s", schema),
		Schema:      schema,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Tables:      make([]DictTable, 0),
	}

	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		dt := buildDictTable(db, dbType, schema, table)
		dict.Tables = append(dict.Tables, dt)
	}

	return dict
}

// ExportDictHTMLByService 生成数据字典 HTML 字符串。
// 业务来自 ExportDictHTML handler。binding 模式下,调用方负责写入临时文件并返回 BlobResult。
func ExportDictHTMLByService(connId, schema, tablesStr, authorization string) (string, string) {
	dict := GenerateDictByService(connId, schema, tablesStr, authorization)
	html := generateDictHTML(dict, false)
	filename := fmt.Sprintf("datadict_%s.html", schema)
	return html, filename
}

// ExportDictPDFByService 生成数据字典 PDF(实际为可打印 HTML) 字符串。
// 业务来自 ExportDictPDF handler。
func ExportDictPDFByService(connId, schema, tablesStr, authorization string) (string, string) {
	dict := GenerateDictByService(connId, schema, tablesStr, authorization)
	html := generateDictHTML(dict, true)
	filename := fmt.Sprintf("datadict_%s.html", schema)
	return html, filename
}

// GetDictTablesByService 返回数据字典表简表列表。
// 业务来自 GetDictTables handler。
func GetDictTablesByService(connId, schema, authorization string) map[string]any {
	db := conn.GetConn(connId, authorization)
	dbType := db.DriverName()

	tables := getDictTables(db, dbType, schema)

	type TableBrief struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
		Rows    int64  `json:"rows"`
	}

	briefs := make([]TableBrief, 0)
	for _, table := range tables {
		if !sanitize.IsValidIdentifier(schema) || !sanitize.IsValidIdentifier(table) {
			continue
		}
		var comment string
		var rows int64
		db.Get(&comment, "SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", schema, table)
		db.Get(&rows, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
		briefs = append(briefs, TableBrief{Name: table, Comment: comment, Rows: rows})
	}

	return map[string]any{"tables": briefs}
}
