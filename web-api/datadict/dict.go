package datadict

import (
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type DictTable struct {
	Name    string       `json:"name"`
	Comment string       `json:"comment"`
	Engine  string       `json:"engine"`
	Rows    int64        `json:"rows"`
	Columns []DictColumn `json:"columns"`
	Indexes []DictIndex  `json:"indexes"`
}

type DictColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Nullable    bool   `json:"nullable"`
	PrimaryKey  bool   `json:"primaryKey"`
	DefaultVal  string `json:"defaultValue"`
	Comment     string `json:"comment"`
	Position    int    `json:"position"`
	MaxLength   int    `json:"maxLength"`
}

type DictIndex struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Type    string   `json:"type"`
}

type DictExport struct {
	Title       string      `json:"title"`
	Schema      string      `json:"schema"`
	GeneratedAt string      `json:"generatedAt"`
	Tables      []DictTable `json:"tables"`
}

func GenerateDict(c *gin.Context) {
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tablesStr := c.PostForm("tables")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getDictTables(conn, dbType, schema)
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
		dt := buildDictTable(conn, dbType, schema, table)
		dict.Tables = append(dict.Tables, dt)
	}

	utils.WriteJson(c.Writer, dict)
}

func buildDictTable(conn *sqlx.DB, dbType, schema, table string) DictTable {
	dt := DictTable{
		Name:    table,
		Columns: make([]DictColumn, 0),
		Indexes: make([]DictIndex, 0),
	}

	pks := getDictPKs(conn, dbType, schema, table)

	colSQL, ok := dbutils.SQL_DIALECT[dbType]["listTableColumns"]
	if !ok {
		colSQL = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_COMMENT, COLUMN_KEY, ORDINAL_POSITION, CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
	}
	switch dbType {
	case "oracle":
		rows, err := conn.Queryx(colSQL, "notexists", table)
		if err != nil {
			logutils.PrintErrf("获取列信息失败: %v", err)
			return dt
		}
		defer rows.Close()
		i := 0
		for rows.Next() {
			var columnName, dataType, isNullable, comments string
			rows.Scan(&columnName, &dataType, &isNullable, &comments)
			isPK := sliceContainsStr(pks, columnName)
			dt.Columns = append(dt.Columns, DictColumn{
				Name:       columnName,
				Type:       dataType,
				Nullable:   isNullable == "YES" || isNullable == "Y",
				PrimaryKey: isPK,
				Comment:    comments,
				Position:   i + 1,
			})
			i++
		}
	default:
		type MCol struct {
			ColumnName    string  `db:"COLUMN_NAME"`
			ColumnType    string  `db:"COLUMN_TYPE"`
			IsNullable    string  `db:"IS_NULLABLE"`
			ColumnDefault *string `db:"COLUMN_DEFAULT"`
			ColumnComment string  `db:"COLUMN_COMMENT"`
			ColumnKey     string  `db:"COLUMN_KEY"`
			OrdinalPos    int     `db:"ORDINAL_POSITION"`
			CharMaxLen    *int    `db:"CHARACTER_MAXIMUM_LENGTH"`
		}
		var cols []MCol
		err := conn.Select(&cols, colSQL, schema, table)
		if err != nil {
			logutils.PrintErrf("获取列信息失败: %v", err)
			return dt
		}
		for _, c := range cols {
			defaultVal := ""
			if c.ColumnDefault != nil {
				defaultVal = *c.ColumnDefault
			}
			maxLen := 0
			if c.CharMaxLen != nil {
				maxLen = *c.CharMaxLen
			}
			dt.Columns = append(dt.Columns, DictColumn{
				Name:       c.ColumnName,
				Type:       c.ColumnType,
				Nullable:   c.IsNullable == "YES",
				PrimaryKey: c.ColumnKey == "PRI",
				DefaultVal: defaultVal,
				Comment:    c.ColumnComment,
				Position:   c.OrdinalPos,
				MaxLength:  maxLen,
			})
		}
	}

	idxSQL, ok := dbutils.SQL_DIALECT[dbType]["listIndexes"]
	if ok {
		type IdxRow struct {
			IndexName string `db:"INDEX_NAME"`
			ColName   string `db:"COLUMN_NAME"`
			NonUnique int    `db:"NON_UNIQUE"`
			IndexType string `db:"INDEX_TYPE"`
		}
		var idxRows []IdxRow
		switch dbType {
		case "oracle":
			conn.Select(&idxRows, idxSQL, table)
		default:
			conn.Select(&idxRows, idxSQL, schema, table)
		}
		idxMap := make(map[string]*DictIndex)
		var order []string
		for _, r := range idxRows {
			name := strings.TrimSpace(r.IndexName)
			if _, ok := idxMap[name]; !ok {
				idxMap[name] = &DictIndex{
					Name:    name,
					Unique:  r.NonUnique == 0,
					Type:    strings.TrimSpace(r.IndexType),
					Columns: make([]string, 0),
				}
				order = append(order, name)
			}
			idxMap[name].Columns = append(idxMap[name].Columns, strings.TrimSpace(r.ColName))
		}
		for _, name := range order {
			dt.Indexes = append(dt.Indexes, *idxMap[name])
		}
	}

	var comment string
	conn.Get(&comment, fmt.Sprintf("SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s'", schema, table))
	dt.Comment = comment

	var eng string
	conn.Get(&eng, fmt.Sprintf("SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s'", schema, table))
	dt.Engine = eng

	var rowCount int64
	conn.Get(&rowCount, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
	dt.Rows = rowCount

	return dt
}

func ExportDictHTML(c *gin.Context) {
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tablesStr := c.PostForm("tables")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getDictTables(conn, dbType, schema)
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
		dt := buildDictTable(conn, dbType, schema, table)
		dict.Tables = append(dict.Tables, dt)
	}

	html := generateDictHTML(dict, false)
	c.Header("Content-Type", "text/html;charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=datadict_%s.html", schema))
	c.Writer.WriteString(html)
}

func generateDictHTML(dict *DictExport, forPrint bool) string {
	var buf strings.Builder
	buf.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">`)
	buf.WriteString(fmt.Sprintf(`<title>%s</title>`, dict.Title))
	buf.WriteString(`<style>
		*{margin:0;padding:0;box-sizing:border-box}
		body{font-family:'Microsoft YaHei','PingFang SC','Helvetica Neue',Arial,sans-serif;color:#2c3e50;background:#f8f9fa;padding:30px 40px;line-height:1.6}
		.cover{text-align:center;padding:50px 0 30px;border-bottom:3px solid #3498db;margin-bottom:30px}
		.cover h1{font-size:28px;color:#2c3e50;margin-bottom:10px;letter-spacing:2px}
		.cover .subtitle{font-size:14px;color:#7f8c8d;margin-top:8px}
		.meta-bar{display:flex;gap:30px;padding:12px 20px;background:#fff;border-radius:8px;margin-bottom:25px;box-shadow:0 1px 4px rgba(0,0,0,0.06);font-size:13px;color:#7f8c8d}
		.meta-bar span{display:flex;align-items:center;gap:4px}
		.meta-bar strong{color:#2c3e50}
		.toc{background:#fff;border-radius:8px;padding:20px 25px;margin-bottom:30px;box-shadow:0 1px 4px rgba(0,0,0,0.06)}
		.toc h2{font-size:16px;color:#2c3e50;margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid #ecf0f1}
		.toc ol{column-count:2;column-gap:30px;padding-left:20px}
		.toc li{padding:3px 0;font-size:13px;break-inside:avoid}
		.toc a{color:#3498db;text-decoration:none}
		.toc a:hover{text-decoration:underline}
		.toc .toc-comment{color:#95a5a6;font-size:12px}
		.table-section{background:#fff;border-radius:8px;margin-bottom:25px;box-shadow:0 1px 4px rgba(0,0,0,0.06);overflow:hidden;break-inside:avoid}
		.table-header{display:flex;align-items:center;gap:10px;padding:14px 20px;background:linear-gradient(135deg,#3498db,#2980b9);color:#fff}
		.table-header h2{font-size:16px;font-weight:600}
		.table-header .badge{background:rgba(255,255,255,0.2);padding:2px 10px;border-radius:12px;font-size:11px}
		.table-header .comment{font-size:12px;opacity:0.85;margin-left:auto}
		.table-meta{padding:8px 20px;background:#f0f7ff;font-size:12px;color:#7f8c8d;border-bottom:1px solid #e8f0fe}
		table{width:100%;border-collapse:collapse;font-size:13px}
		table thead{background:#f7f9fc}
		table th{padding:10px 12px;text-align:left;font-weight:600;color:#5a6c7d;border-bottom:2px solid #dce4ec;font-size:12px;text-transform:uppercase;letter-spacing:0.5px}
		table td{padding:9px 12px;border-bottom:1px solid #f0f3f5;color:#34495e}
		table tbody tr:nth-child(even){background:#fafbfc}
		table tbody tr:hover{background:#f0f7ff}
		.col-name{font-weight:600;color:#2c3e50}
		.col-type{font-family:'SF Mono',Consolas,'Liberation Mono',Menlo,monospace;font-size:12px;color:#e67e22;background:#fef9e7;padding:1px 6px;border-radius:3px}
		.tag{display:inline-block;padding:1px 7px;border-radius:3px;font-size:10px;font-weight:600;letter-spacing:0.5px;vertical-align:middle}
		.tag-pk{background:#fef3cd;color:#856404;border:1px solid #f0d86e}
		.tag-idx{background:#d1ecf1;color:#0c5460;border:1px solid #97d4da}
		.tag-unique{background:#d4edda;color:#155724;border:1px solid #8fd9a8}
		.yes{color:#27ae60;font-weight:600}
		.no{color:#e74c3c;font-weight:600}
		.default-val{font-family:Consolas,monospace;font-size:12px;color:#8e44ad}
		.idx-section{padding:10px 20px 15px;border-top:1px solid #f0f3f5}
		.idx-section h4{font-size:12px;color:#7f8c8d;margin-bottom:8px}
		.idx-tag{display:inline-block;padding:3px 10px;margin:2px 4px;border-radius:4px;font-size:11px;background:#ecf0f1;color:#555}
		.idx-tag.unique{background:#d4edda;color:#155724}
		.footer{text-align:center;padding:20px 0;margin-top:30px;border-top:1px solid #ecf0f1;color:#bdc3c7;font-size:11px}
	`)
	if forPrint {
		buf.WriteString(`
		@media print{
			body{background:#fff;padding:15px 20px}
			.cover{padding:20px 0 15px;border-bottom-width:2px}
			.table-section{box-shadow:none;border:1px solid #e0e0e0;break-inside:avoid;margin-bottom:15px}
			.table-header{background:#3498db!important;-webkit-print-color-adjust:exact;print-color-adjust:exact}
			table thead{background:#f0f0f0!important;-webkit-print-color-adjust:exact;print-color-adjust:exact}
			table tbody tr:nth-child(even){background:#f9f9f9!important;-webkit-print-color-adjust:exact;print-color-adjust:exact}
			.toc{box-shadow:none;border:1px solid #e0e0e0}
			.meta-bar{box-shadow:none;border:1px solid #e0e0e0}
			.col-type,.tag,.idx-tag{-webkit-print-color-adjust:exact;print-color-adjust:exact}
			.footer{display:none}
		}
		@page{margin:15mm;size:A4}
		`)
	}
	buf.WriteString(`</style></head><body>`)
	buf.WriteString(`<div class="cover">`)
	buf.WriteString(fmt.Sprintf(`<h1>%s</h1>`, dict.Title))
	buf.WriteString(fmt.Sprintf(`<div class="subtitle">Schema: %s</div>`, dict.Schema))
	buf.WriteString(fmt.Sprintf(`<div class="subtitle">生成时间: %s | 共 %d 张表</div>`, dict.GeneratedAt, len(dict.Tables)))
	buf.WriteString(`</div>`)
	buf.WriteString(`<div class="meta-bar">`)
	buf.WriteString(fmt.Sprintf(`<span>📦 Schema: <strong>%s</strong></span>`, dict.Schema))
	buf.WriteString(fmt.Sprintf(`<span>🕐 生成时间: <strong>%s</strong></span>`, dict.GeneratedAt))
	buf.WriteString(fmt.Sprintf(`<span>📊 表数量: <strong>%d</strong></span>`, len(dict.Tables)))
	buf.WriteString(`</div>`)
	buf.WriteString(`<div class="toc"><h2>目录</h2><ol>`)
	for _, table := range dict.Tables {
		comment := ""
		if table.Comment != "" {
			comment = fmt.Sprintf(` <span class="toc-comment">- %s</span>`, table.Comment)
		}
		buf.WriteString(fmt.Sprintf(`<li><a href="#t_%s">%s</a>%s</li>`, table.Name, table.Name, comment))
	}
	buf.WriteString(`</ol></div>`)
	for _, table := range dict.Tables {
		buf.WriteString(fmt.Sprintf(`<div class="table-section" id="t_%s">`, table.Name))
		buf.WriteString(`<div class="table-header">`)
		buf.WriteString(fmt.Sprintf(`<h2>%s</h2>`, table.Name))
		if table.Comment != "" {
			buf.WriteString(fmt.Sprintf(`<span class="comment">%s</span>`, table.Comment))
		}
		buf.WriteString(fmt.Sprintf(`<span class="badge">%d 行</span>`, table.Rows))
		buf.WriteString(`</div>`)
		buf.WriteString(`<div class="table-meta">`)
		metaParts := make([]string, 0)
		if table.Engine != "" {
			metaParts = append(metaParts, fmt.Sprintf("引擎: %s", table.Engine))
		}
		metaParts = append(metaParts, fmt.Sprintf("行数: %d", table.Rows))
		if table.Comment != "" {
			metaParts = append(metaParts, fmt.Sprintf("注释: %s", table.Comment))
		}
		buf.WriteString(strings.Join(metaParts, " | "))
		buf.WriteString(`</div>`)
		buf.WriteString(`<table><thead><tr><th style="width:40px">#</th><th style="width:150px">列名</th><th style="width:140px">类型</th><th style="width:50px">可空</th><th style="width:60px">主键</th><th style="width:100px">默认值</th><th>注释</th></tr></thead><tbody>`)
		for _, col := range table.Columns {
			tags := ""
			if col.PrimaryKey {
				tags += ` <span class="tag tag-pk">PK</span>`
			}
			nullable := `<span class="no">NO</span>`
			if col.Nullable {
				nullable = `<span class="yes">YES</span>`
			}
			pk := ""
			if col.PrimaryKey {
				pk = `<span class="yes">✓</span>`
			}
			defVal := col.DefaultVal
			if defVal == "" {
				defVal = `<span style="color:#bdc3c7">-</span>`
			} else {
				defVal = fmt.Sprintf(`<span class="default-val">%s</span>`, defVal)
			}
			comment := col.Comment
			if comment == "" {
				comment = `<span style="color:#bdc3c7">-</span>`
			}
			buf.WriteString(fmt.Sprintf(`<tr><td>%d</td><td class="col-name">%s%s</td><td><span class="col-type">%s</span></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				col.Position, col.Name, tags, col.Type, nullable, pk, defVal, comment))
		}
		buf.WriteString(`</tbody></table>`)
		if len(table.Indexes) > 0 {
			buf.WriteString(`<div class="idx-section"><h4>索引信息</h4>`)
			for _, idx := range table.Indexes {
				cls := "idx-tag"
				if idx.Unique {
					cls += " unique"
				}
				buf.WriteString(fmt.Sprintf(`<span class="%s">%s (%s) %s</span>`, cls, idx.Name, strings.Join(idx.Columns, ", "), idx.Type))
			}
			buf.WriteString(`</div>`)
		}
		buf.WriteString(`</div>`)
	}
	buf.WriteString(`<div class="footer">Generated by WebSQL Data Dictionary</div>`)
	if forPrint {
		buf.WriteString(`<script>window.onload=function(){setTimeout(function(){window.print()},500)}</script>`)
	}
	buf.WriteString(`</body></html>`)
	return buf.String()
}

func ExportDictPDF(c *gin.Context) {
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tablesStr := c.PostForm("tables")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getDictTables(conn, dbType, schema)
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
		dt := buildDictTable(conn, dbType, schema, table)
		dict.Tables = append(dict.Tables, dt)
	}

	html := generateDictHTML(dict, true)
	c.Header("Content-Type", "text/html;charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=datadict_%s.html", schema))
	c.Writer.WriteString(html)
}

func GetDictTables(c *gin.Context) {
	connId := c.Query("connId")
	schema := c.Query("schema")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	tables := getDictTables(conn, dbType, schema)

	type TableBrief struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
		Rows    int64  `json:"rows"`
	}

	briefs := make([]TableBrief, 0)
	for _, table := range tables {
		var comment string
		var rows int64
		conn.Get(&comment, fmt.Sprintf("SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s'", schema, table))
		conn.Get(&rows, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
		briefs = append(briefs, TableBrief{Name: table, Comment: comment, Rows: rows})
	}

	utils.WriteJson(c.Writer, map[string]any{"tables": briefs})
}

func getDictTables(conn *sqlx.DB, dbType, schema string) []string {
	sqlTmpl, ok := dbutils.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
	switch dbType {
	case "oracle":
		rows, err := conn.Query(sqlTmpl, "notexists")
		if err != nil {
			logutils.PrintErrf("获取表列表失败: %v", err)
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
			logutils.PrintErrf("获取表列表失败: %v", err)
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

func getDictPKs(conn *sqlx.DB, dbType, schema, table string) []string {
	switch dbType {
	case "oracle":
		sql := "SELECT b.COLUMN_NAME FROM user_constraints a LEFT JOIN user_cons_columns b ON a.TABLE_NAME = b.TABLE_NAME WHERE a.TABLE_NAME = '" + table + "' AND CONSTRAINT_TYPE = 'P'"
		pks := make([]string, 0)
		conn.Select(&pks, sql)
		return pks
	default:
		sql := fmt.Sprintf("SELECT column_name FROM information_schema.columns WHERE TABLE_SCHEMA = '%s' AND table_name = '%s' AND column_key = 'PRI'", schema, table)
		pks := make([]string, 0)
		conn.Select(&pks, sql)
		return pks
	}
}

func sliceContainsStr(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

func init() {
	_ = config.Cfg
}