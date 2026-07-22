package modeler

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"websql/internal/app/conn"
	"websql/internal/dialect"
	"websql/internal/logger"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sanitize"
	"websql/internal/pkg/sqlguard"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type ERModel struct {
	Tables    []ERTable    `json:"tables"`
	Relations []ERRelation `json:"relations"`
}

type ERTable struct {
	Name    string     `json:"name"`
	Schema  string     `json:"schema"`
	Comment string     `json:"comment"`
	Columns []ERColumn `json:"columns"`
	Indexes []ERIndex  `json:"indexes"`
}

type ERColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primaryKey"`
	ForeignKey bool   `json:"foreignKey"`
	Comment    string `json:"comment"`
	DefaultVal string `json:"defaultValue"`
}

type ERIndex struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Type    string   `json:"type"`
}

type ERRelation struct {
	SourceTable  string `json:"sourceTable"`
	SourceColumn string `json:"sourceColumn"`
	TargetTable  string `json:"targetTable"`
	TargetColumn string `json:"targetColumn"`
	RelationName string `json:"relationName"`
}

func ReverseEngineer(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	includeRelations := c.DefaultPostForm("includeRelations", "true")

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	model := &ERModel{
		Tables:    make([]ERTable, 0),
		Relations: make([]ERRelation, 0),
	}

	tableNames := getSyncTableList(conn, dbType, schema)
	for _, tableName := range tableNames {
		table := buildERTable(conn, dbType, schema, tableName)
		model.Tables = append(model.Tables, table)
	}

	if includeRelations == "true" {
		model.Relations = extractRelations(conn, dbType, schema)
	}

	response.WriteOK(c, model)
}

func buildERTable(conn *sqlx.DB, dbType, schema, tableName string) ERTable {
	table := ERTable{
		Name:    tableName,
		Schema:  schema,
		Columns: make([]ERColumn, 0),
		Indexes: make([]ERIndex, 0),
	}

	primaryKeys := getPKCols(conn, dbType, schema, tableName)

	sqlTmpl, _ := dialect.SQL_DIALECT[dbType]["listTableColumns"]
	switch dbType {
	case "oracle":
		type OracleCol struct {
			ColumnName string `db:"COLUMN_NAME"`
			DataType   string `db:"COLUMN_TYPE"`
			Nullable   string `db:"IS_NULLABLE"`
			Comments   string `db:"COLUMN_COMMENT"`
		}
		var cols []OracleCol
		err := conn.Select(&cols, sqlTmpl, "notexists", tableName)
		if err != nil {
			log.Printf("[buildERTable] 获取列信息失败: %v", err)
			return table
		}
		for _, c := range cols {
			isPK := false
			for _, pk := range primaryKeys {
				if strings.EqualFold(pk, c.ColumnName) {
					isPK = true
					break
				}
			}
			table.Columns = append(table.Columns, ERColumn{
				Name:       c.ColumnName,
				Type:       c.DataType,
				Nullable:   c.Nullable == "YES" || c.Nullable == "Y",
				PrimaryKey: isPK,
				Comment:    c.Comments,
			})
		}
	default:
		type MySQLCol struct {
			ColumnName    string  `db:"COLUMN_NAME"`
			ColumnType    string  `db:"COLUMN_TYPE"`
			IsNullable    string  `db:"IS_NULLABLE"`
			ColumnDefault *string `db:"COLUMN_DEFAULT"`
			ColumnComment string  `db:"COLUMN_COMMENT"`
			ColumnKey     string  `db:"COLUMN_KEY"`
		}
		var cols []MySQLCol
		err := conn.Select(&cols, sqlTmpl, schema, tableName)
		if err != nil {
			log.Printf("[buildERTable] 获取列信息失败: %v", err)
			return table
		}
		for _, c := range cols {
			isPK := c.ColumnKey == "PRI"
			defaultVal := ""
			if c.ColumnDefault != nil {
				defaultVal = *c.ColumnDefault
			}
			table.Columns = append(table.Columns, ERColumn{
				Name:       c.ColumnName,
				Type:       c.ColumnType,
				Nullable:   c.IsNullable == "YES",
				PrimaryKey: isPK,
				Comment:    c.ColumnComment,
				DefaultVal: defaultVal,
			})
		}
	}

	indexes := getSyncTableIndexes(conn, dbType, schema, tableName)
	table.Indexes = indexes

	var comment string
	conn.Get(&comment, "SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", schema, tableName)
	table.Comment = comment

	return table
}

func getPKCols(conn *sqlx.DB, dbType, schema, table string) []string {
	if !sanitize.IsValidIdentifier(table) {
		return nil
	}
	switch dbType {
	case "oracle":
		sql := "SELECT b.COLUMN_NAME FROM user_constraints a LEFT JOIN user_cons_columns b ON a.TABLE_NAME = b.TABLE_NAME WHERE a.TABLE_NAME = :1 AND CONSTRAINT_TYPE = 'P'"
		cols := make([]string, 0)
		e := conn.Select(&cols, sql, table)
		if e != nil {
			return nil
		}
		return cols
	default:
		if !sanitize.IsValidIdentifier(schema) {
			return nil
		}
		sql := fmt.Sprintf("SELECT column_name FROM information_schema.columns WHERE TABLE_SCHEMA = '%s' AND table_name = '%s' AND column_key = 'PRI'", schema, table)
		cols := make([]string, 0)
		e := conn.Select(&cols, sql)
		if e != nil {
			return nil
		}
		return cols
	}
}

func getSyncTableIndexes(conn *sqlx.DB, dbType, schema, table string) []ERIndex {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listIndexes"]
	if !ok {
		return nil
	}

	type IdxRow struct {
		IndexName string `db:"INDEX_NAME"`
		ColName   string `db:"COLUMN_NAME"`
		NonUnique int    `db:"NON_UNIQUE"`
		IndexType string `db:"INDEX_TYPE"`
	}

	var rows []IdxRow
	switch dbType {
	case "oracle":
		e := conn.Select(&rows, sqlTmpl, table)
		if e != nil {
			return nil
		}
	default:
		e := conn.Select(&rows, sqlTmpl, schema, table)
		if e != nil {
			return nil
		}
	}

	idxMap := make(map[string]*ERIndex)
	var order []string
	for _, r := range rows {
		name := strings.TrimSpace(r.IndexName)
		if _, ok := idxMap[name]; !ok {
			idxMap[name] = &ERIndex{
				Name:    name,
				Unique:  r.NonUnique == 0,
				Type:    strings.TrimSpace(r.IndexType),
				Columns: make([]string, 0),
			}
			order = append(order, name)
		}
		idxMap[name].Columns = append(idxMap[name].Columns, strings.TrimSpace(r.ColName))
	}

	result := make([]ERIndex, 0)
	for _, name := range order {
		result = append(result, *idxMap[name])
	}
	return result
}

func extractRelations(conn *sqlx.DB, dbType, schema string) []ERRelation {
	relations := make([]ERRelation, 0)
	if !sanitize.IsValidIdentifier(schema) {
		return relations
	}
	sql := `SELECT
		CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME,
		REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND REFERENCED_TABLE_SCHEMA IS NOT NULL`
	rows, err := conn.Queryx(sql, schema)
	if err != nil {
		logger.PrintErrf("获取外键关系失败", err)
		return relations
	}
	defer rows.Close()

	for rows.Next() {
		var constraintName, tableName, columnName, refTable, refColumn string
		if err := rows.Scan(&constraintName, &tableName, &columnName, &refTable, &refColumn); err != nil {
			log.Printf("扫描行失败: %v", err)
			continue
		}
		relations = append(relations, ERRelation{
			SourceTable:  tableName,
			SourceColumn: columnName,
			TargetTable:  refTable,
			TargetColumn: refColumn,
			RelationName: constraintName,
		})
	}
	return relations
}

func getSyncTableList(conn *sqlx.DB, dbType, schema string) []string {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
	switch dbType {
	case "oracle":
		rows, err := conn.Query(sqlTmpl, "notexists")
		if err != nil {
			logger.PrintErrf("获取表列表失败: %v", err)
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
		rows, err := conn.Query(sqlTmpl, schema)
		if err != nil {
			logger.PrintErrf("获取表列表失败: %v", err)
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

func ForwardEngineer(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	ddlSql := c.PostForm("ddl")

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)

	if strings.TrimSpace(ddlSql) == "" {
		response.WriteErr(c, 200, 400, "DDL不能为空")
		return
	}

	statements := splitDDL(ddlSql)
	results := make([]map[string]any, 0)
	successCount := 0
	failCount := 0

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		// DDL 安全校验
		if err := sqlguard.ValidateDDL(stmt); err != nil {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": false,
				"error":   err.Error(),
			})
			failCount++
			continue
		}
		_, err := conn.Exec(stmt)
		if err != nil {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": false,
				"error":   err.Error(),
			})
			failCount++
		} else {
			results = append(results, map[string]any{
				"sql":     clampStrLen(200, stmt),
				"success": true,
			})
			successCount++
		}
	}

	response.WriteOK(c, map[string]any{
		"successCount": successCount,
		"failCount":    failCount,
		"results":      results,
		"allSuccess":   failCount == 0,
	})
}

func splitDDL(ddl string) []string {
	statements := make([]string, 0)
	current := ""
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(ddl); i++ {
		ch := ddl[i]
		if inString {
			current += string(ch)
			if ch == stringChar {
				inString = false
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			current += string(ch)
			continue
		}
		if ch == ';' {
			if strings.TrimSpace(current) != "" {
				statements = append(statements, current)
			}
			current = ""
			continue
		}
		current += string(ch)
	}
	if strings.TrimSpace(current) != "" {
		statements = append(statements, current)
	}
	return statements
}

func ExportModel(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	format := c.DefaultPostForm("format", "json")

	authorization := appctx.Ctx.GetAuthorization(c)
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	model := &ERModel{
		Tables:    make([]ERTable, 0),
		Relations: make([]ERRelation, 0),
	}

	tableNames := getSyncTableList(conn, dbType, schema)
	for _, tableName := range tableNames {
		table := buildERTable(conn, dbType, schema, tableName)
		model.Tables = append(model.Tables, table)
	}

	model.Relations = extractRelations(conn, dbType, schema)

	switch format {
	case "ddl":
		ddl := generateDDLExport(model)
		response.WriteOK(c, map[string]any{"ddl": ddl, "format": "ddl"})
	case "json":
		response.WriteOK(c, map[string]any{"model": model, "format": "json"})
	default:
		response.WriteOK(c, map[string]any{"model": model, "format": format})
	}
}

func generateDDLExport(model *ERModel) string {
	var buf strings.Builder
	for _, table := range model.Tables {
		buf.WriteString(fmt.Sprintf("-- Table: %s\n", table.Name))
		buf.WriteString(fmt.Sprintf("CREATE TABLE `%s` (\n", table.Name))
		for i, col := range table.Columns {
			nullable := "NOT NULL"
			if col.Nullable {
				nullable = "NULL"
			}
			pk := ""
			if col.PrimaryKey {
				pk = " AUTO_INCREMENT"
			}
			buf.WriteString(fmt.Sprintf("  `%s` %s %s%s", col.Name, col.Type, nullable, pk))
			if col.Comment != "" {
				buf.WriteString(fmt.Sprintf(" COMMENT '%s'", col.Comment))
			}
			if i < len(table.Columns)-1 {
				buf.WriteString(",\n")
			} else {
				buf.WriteString("\n")
			}
		}
		if hasPK(table) {
			pkCols := make([]string, 0)
			for _, col := range table.Columns {
				if col.PrimaryKey {
					pkCols = append(pkCols, fmt.Sprintf("`%s`", col.Name))
				}
			}
			buf.WriteString(fmt.Sprintf("  ,PRIMARY KEY (%s)\n", strings.Join(pkCols, ", ")))
		}
		buf.WriteString(fmt.Sprintf(") ENGINE=InnoDB"))
		if table.Comment != "" {
			buf.WriteString(fmt.Sprintf(" COMMENT='%s'", table.Comment))
		}
		buf.WriteString(";\n\n")
	}
	return buf.String()
}

func hasPK(table ERTable) bool {
	for _, col := range table.Columns {
		if col.PrimaryKey {
			return true
		}
	}
	return false
}

func clampStrLen(maxLen int, s string) int {
	if maxLen < len(s) {
		return maxLen
	}
	return len(s)
}