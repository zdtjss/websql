package sync

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"websql/internal/app/conn"
	"websql/internal/config"
	"websql/internal/dialect"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type SchemaDiffItem struct {
	TableName   string           `json:"tableName"`
	DiffType    string           `json:"diffType"`
	SourceDDL   string           `json:"sourceDDL"`
	TargetDDL   string           `json:"targetDDL"`
	ColumnDiffs []ColumnDiffItem `json:"columnDiffs"`
	IndexDiffs  []IndexDiffItem  `json:"indexDiffs"`
}

type ColumnDiffItem struct {
	ColumnName     string `json:"columnName"`
	DiffType       string `json:"diffType"`
	SourceDef      string `json:"sourceDef"`
	TargetDef      string `json:"targetDef"`
	AlterStatement string `json:"alterStatement"`
}

type IndexDiffItem struct {
	IndexName      string `json:"indexName"`
	DiffType       string `json:"diffType"`
	SourceDef      string `json:"sourceDef"`
	TargetDef      string `json:"targetDef"`
	AlterStatement string `json:"alterStatement"`
}

type ColumnInfo struct {
	Name       string
	Type       string
	Nullable   string
	DefaultVal string
	Comment    string
	Extra      string
	Position   int
	CharMaxLen int
}

type IndexInfo struct {
	Name    string
	Columns []string
	Unique  bool
	Type    string
	Comment string
}

type TableSchema struct {
	Name    string
	Columns []ColumnInfo
	Indexes []IndexInfo
	Comment string
	Engine  string
	DDL     string
}

func CompareSchema(c *gin.Context) {
	connId1 := c.PostForm("sourceConnId")
	connId2 := c.PostForm("targetConnId")
	schema1 := c.PostForm("sourceSchema")
	schema2 := c.PostForm("targetSchema")
	tableFilter := c.PostForm("tables")

	authorization := c.GetHeader("Authorization")
	conn1 := conn.GetConn(connId1, authorization)
	conn2 := conn.GetConn(connId2, authorization)

	dbType1 := conn1.DriverName()
	dbType2 := conn2.DriverName()

	if dbType1 != dbType2 {
		jsonutil.WriteJson(c.Writer, map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": fmt.Sprintf("不允许跨数据库类型比较: 源=%s, 目标=%s", dbType1, dbType2),
		})
		return
	}

	tables1, err1 := getTableList(conn1, dbType1, schema1)
	tables2, err2 := getTableList(conn2, dbType2, schema2)

	if err1 != nil || err2 != nil {
		errMsg := ""
		if err1 != nil {
			errMsg += fmt.Sprintf("源库: %v", err1)
		}
		if err2 != nil {
			if errMsg != "" {
				errMsg += "; "
			}
			errMsg += fmt.Sprintf("目标库: %v", err2)
		}
		jsonutil.WriteJson(c.Writer, map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": errMsg,
		})
		return
	}

	filterSet := make(map[string]bool)
	if tableFilter != "" {
		for _, t := range strings.Split(tableFilter, ",") {
			filterSet[strings.TrimSpace(t)] = true
		}
	}

	diffs := make([]SchemaDiffItem, 0)

	tableMap1 := make(map[string]bool)
	for _, t := range tables1 {
		tableMap1[t] = true
	}
	tableMap2 := make(map[string]bool)
	for _, t := range tables2 {
		tableMap2[t] = true
	}

	for _, table := range tables1 {
		if len(filterSet) > 0 && !filterSet[table] {
			continue
		}
		if !tableMap2[table] {
			schema, err := getTableSchema(conn1, dbType1, schema1, table)
			if err != nil {
				diffs = append(diffs, SchemaDiffItem{TableName: table, DiffType: "ADD"})
				continue
			}
			diffs = append(diffs, SchemaDiffItem{
				TableName: table,
				DiffType:  "ADD",
				SourceDDL: schema.DDL,
				TargetDDL: "",
			})
		}
	}

	for _, table := range tables2 {
		if len(filterSet) > 0 && !filterSet[table] {
			continue
		}
		if !tableMap1[table] {
			schema, err := getTableSchema(conn2, dbType2, schema2, table)
			if err != nil {
				diffs = append(diffs, SchemaDiffItem{TableName: table, DiffType: "DROP"})
				continue
			}
			diffs = append(diffs, SchemaDiffItem{
				TableName: table,
				DiffType:  "DROP",
				SourceDDL: "",
				TargetDDL: schema.DDL,
			})
		} else {
			diff := compareTable(conn1, conn2, dbType1, schema1, schema2, table)
			if diff != nil {
				diffs = append(diffs, *diff)
			}
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].TableName < diffs[j].TableName
	})

	jsonutil.WriteJson(c.Writer, map[string]any{
		"diffs":       diffs,
		"totalCount":  len(diffs),
		"addCount":    countByType(diffs, "ADD"),
		"dropCount":   countByType(diffs, "DROP"),
		"modifyCount": countByType(diffs, "MODIFY"),
	})
}

func countByType(diffs []SchemaDiffItem, diffType string) int {
	count := 0
	for _, d := range diffs {
		if d.DiffType == diffType {
			count++
		}
	}
	return count
}

func compareTable(conn1, conn2 *sqlx.DB, dbType, schema1, schema2, table string) *SchemaDiffItem {
	sourceSchema, err1 := getTableSchema(conn1, dbType, schema1, table)
	targetSchema, err2 := getTableSchema(conn2, dbType, schema2, table)
	if err1 != nil || err2 != nil {
		return nil
	}

	columnDiffs := compareColumns(sourceSchema.Columns, targetSchema.Columns, table, dbType)
	indexDiffs := compareIndexes(sourceSchema.Indexes, targetSchema.Indexes, table, dbType)

	if len(columnDiffs) == 0 && len(indexDiffs) == 0 {
		return nil
	}

	return &SchemaDiffItem{
		TableName:   table,
		DiffType:    "MODIFY",
		SourceDDL:   sourceSchema.DDL,
		TargetDDL:   targetSchema.DDL,
		ColumnDiffs: columnDiffs,
		IndexDiffs:  indexDiffs,
	}
}

func compareColumns(source, target []ColumnInfo, table, dbType string) []ColumnDiffItem {
	diffs := make([]ColumnDiffItem, 0)
	sourceMap := make(map[string]ColumnInfo)
	targetMap := make(map[string]ColumnInfo)

	for _, c := range source {
		lowerName := strings.ToLower(c.Name)
		sourceMap[lowerName] = c
	}
	for _, c := range target {
		lowerName := strings.ToLower(c.Name)
		targetMap[lowerName] = c
	}

	for _, sc := range source {
		lowerName := strings.ToLower(sc.Name)
		if tc, ok := targetMap[lowerName]; ok {
			if sc.Type != tc.Type || sc.Nullable != tc.Nullable || sc.Comment != tc.Comment || sc.DefaultVal != tc.DefaultVal || sc.Extra != tc.Extra {
				sourceDef := fmt.Sprintf("%s %s %s", sc.Name, sc.Type, nullableStr(sc.Nullable))
				targetDef := fmt.Sprintf("%s %s %s", tc.Name, tc.Type, nullableStr(tc.Nullable))
				if sc.Comment != "" {
					sourceDef += fmt.Sprintf(" COMMENT '%s'", sc.Comment)
				}
				alter := generateAlterColumn(dbType, table, tc, sc)
				diffs = append(diffs, ColumnDiffItem{
					ColumnName:     sc.Name,
					DiffType:       "MODIFY",
					SourceDef:      sourceDef,
					TargetDef:      targetDef,
					AlterStatement: alter,
				})
			}
		} else {
			addDDL := generateAddColumn(dbType, table, sc)
			diffs = append(diffs, ColumnDiffItem{
				ColumnName:     sc.Name,
				DiffType:       "ADD",
				SourceDef:      fmt.Sprintf("%s %s %s", sc.Name, sc.Type, nullableStr(sc.Nullable)),
				TargetDef:      "",
				AlterStatement: addDDL,
			})
		}
	}
	for _, tc := range target {
		lowerName := strings.ToLower(tc.Name)
		if _, ok := sourceMap[lowerName]; !ok {
			var dropDDL string
			switch dbType {
			case "oracle":
				dropDDL = fmt.Sprintf("ALTER TABLE \"%s\" DROP COLUMN \"%s\";", table, tc.Name)
			default:
				dropDDL = fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`;", table, tc.Name)
			}
			diffs = append(diffs, ColumnDiffItem{
				ColumnName:     tc.Name,
				DiffType:       "DROP",
				SourceDef:      "",
				TargetDef:      fmt.Sprintf("%s %s %s", tc.Name, tc.Type, nullableStr(tc.Nullable)),
				AlterStatement: dropDDL,
			})
		}
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].DiffType != diffs[j].DiffType {
			typeOrder := map[string]int{"ADD": 0, "MODIFY": 1, "DROP": 2}
			return typeOrder[diffs[i].DiffType] < typeOrder[diffs[j].DiffType]
		}
		return diffs[i].ColumnName < diffs[j].ColumnName
	})
	return diffs
}

func compareIndexes(source, target []IndexInfo, table, dbType string) []IndexDiffItem {
	diffs := make([]IndexDiffItem, 0)
	sourceMap := make(map[string]IndexInfo)
	targetMap := make(map[string]IndexInfo)

	for _, idx := range source {
		sourceMap[strings.ToLower(idx.Name)] = idx
	}
	for _, idx := range target {
		targetMap[strings.ToLower(idx.Name)] = idx
	}

	for _, si := range source {
		lowerName := strings.ToLower(si.Name)
		if ti, ok := targetMap[lowerName]; ok {
			if indexDefStr(si) != indexDefStr(ti) {
				alter := generateAlterIndex(dbType, table, ti, si)
				diffs = append(diffs, IndexDiffItem{
					IndexName:      si.Name,
					DiffType:       "MODIFY",
					SourceDef:      indexDefStr(si),
					TargetDef:      indexDefStr(ti),
					AlterStatement: alter,
				})
			}
		} else {
			addDDL := generateAddIndex(dbType, table, si)
			diffs = append(diffs, IndexDiffItem{
				IndexName:      si.Name,
				DiffType:       "ADD",
				SourceDef:      indexDefStr(si),
				TargetDef:      "",
				AlterStatement: addDDL,
			})
		}
	}
	for _, ti := range target {
		lowerName := strings.ToLower(ti.Name)
		if _, ok := sourceMap[lowerName]; !ok {
			var dropDDL string
			switch dbType {
			case "oracle":
				dropDDL = fmt.Sprintf("DROP INDEX \"%s\";", ti.Name)
			default:
				dropDDL = fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`;", table, ti.Name)
			}
			diffs = append(diffs, IndexDiffItem{
				IndexName:      ti.Name,
				DiffType:       "DROP",
				SourceDef:      "",
				TargetDef:      indexDefStr(ti),
				AlterStatement: dropDDL,
			})
		}
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].DiffType != diffs[j].DiffType {
			typeOrder := map[string]int{"ADD": 0, "MODIFY": 1, "DROP": 2}
			return typeOrder[diffs[i].DiffType] < typeOrder[diffs[j].DiffType]
		}
		return diffs[i].IndexName < diffs[j].IndexName
	})
	return diffs
}

func indexDefStr(idx IndexInfo) string {
	uniqueStr := ""
	if idx.Unique {
		uniqueStr = " UNIQUE"
	}
	return fmt.Sprintf("%s%s (%s)", idx.Type, uniqueStr, strings.Join(idx.Columns, ","))
}

func nullableStr(nullable string) string {
	if nullable == "YES" {
		return "NULL"
	}
	return "NOT NULL"
}

func generateAlterColumn(dbType, table string, target, source ColumnInfo) string {
	switch dbType {
	case "oracle":
		def := fmt.Sprintf("\"%s\" %s", source.Name, source.Type)
		if source.Nullable == "YES" {
			def += " NULL"
		} else {
			def += " NOT NULL"
		}
		if source.DefaultVal != "" {
			def += fmt.Sprintf(" DEFAULT '%s'", source.DefaultVal)
		}
		return fmt.Sprintf("ALTER TABLE \"%s\" MODIFY (%s);", table, def)
	default:
		def := fmt.Sprintf("`%s` `%s` %s", source.Name, source.Name, source.Type)
		if source.Nullable == "YES" {
			def += " NULL"
		} else {
			def += " NOT NULL"
		}
		if source.DefaultVal != "" {
			def += fmt.Sprintf(" DEFAULT '%s'", source.DefaultVal)
		}
		if source.Comment != "" {
			def += fmt.Sprintf(" COMMENT '%s'", source.Comment)
		}
		if source.Extra != "" {
			def += " " + source.Extra
		}
		return fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN %s;", table, def)
	}
}

func generateAddColumn(dbType, table string, col ColumnInfo) string {
	switch dbType {
	case "oracle":
		def := fmt.Sprintf("\"%s\" %s", col.Name, col.Type)
		if col.Nullable == "YES" {
			def += " NULL"
		} else {
			def += " NOT NULL"
		}
		if col.DefaultVal != "" {
			def += fmt.Sprintf(" DEFAULT '%s'", col.DefaultVal)
		}
		return fmt.Sprintf("ALTER TABLE \"%s\" ADD (%s);", table, def)
	default:
		def := fmt.Sprintf("`%s` %s", col.Name, col.Type)
		if col.Nullable == "YES" {
			def += " NULL"
		} else {
			def += " NOT NULL"
		}
		if col.DefaultVal != "" {
			def += fmt.Sprintf(" DEFAULT '%s'", col.DefaultVal)
		}
		if col.Comment != "" {
			def += fmt.Sprintf(" COMMENT '%s'", col.Comment)
		}
		if col.Extra != "" {
			def += " " + col.Extra
		}
		return fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN %s;", table, def)
	}
}

func generateAlterIndex(dbType, table string, oldIdx, newIdx IndexInfo) string {
	var drop string
	var add string
	switch dbType {
	case "oracle":
		drop = fmt.Sprintf("DROP INDEX \"%s\";", oldIdx.Name)
		uniqueStr := ""
		if newIdx.Unique {
			uniqueStr = " UNIQUE"
		}
		cols := ""
		for i, c := range newIdx.Columns {
			if i > 0 {
				cols += ", "
			}
			cols += "\"" + c + "\""
		}
		add = fmt.Sprintf("CREATE%s INDEX \"%s\" ON \"%s\" (%s);", uniqueStr, newIdx.Name, table, cols)
	default:
		drop = fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`;", table, oldIdx.Name)
		uniqueStr := ""
		if newIdx.Unique {
			uniqueStr = " UNIQUE"
		}
		cols := ""
		for i, c := range newIdx.Columns {
			if i > 0 {
				cols += ", "
			}
			cols += "`" + c + "`"
		}
		add = fmt.Sprintf("CREATE%s INDEX `%s` ON `%s` (%s);", uniqueStr, newIdx.Name, table, cols)
	}
	return drop + "\n" + add
}

func generateAddIndex(dbType, table string, idx IndexInfo) string {
	uniqueStr := ""
	if idx.Unique {
		uniqueStr = " UNIQUE"
	}
	switch dbType {
	case "oracle":
		cols := ""
		for i, c := range idx.Columns {
			if i > 0 {
				cols += ", "
			}
			cols += "\"" + c + "\""
		}
		return fmt.Sprintf("CREATE%s INDEX \"%s\" ON \"%s\" (%s);", uniqueStr, idx.Name, table, cols)
	default:
		cols := ""
		for i, c := range idx.Columns {
			if i > 0 {
				cols += ", "
			}
			cols += "`" + c + "`"
		}
		return fmt.Sprintf("CREATE%s INDEX `%s` ON `%s` (%s);", uniqueStr, idx.Name, table, cols)
	}
}

func getTableList(conn *sqlx.DB, dbType, schema string) ([]string, error) {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME,TABLE_TYPE,table_comment FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
	switch dbType {
	case "oracle":
		rows, err := conn.Query(sqlTmpl, "notexists")
		if err != nil {
			return result, fmt.Errorf("获取表列表失败: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			if err := rows.Scan(&tableName, &tableType, &tableComment); err != nil {
				return result, fmt.Errorf("扫描表列表失败: %v", err)
			}
			result = append(result, strings.TrimSpace(tableName))
		}
	default:
		rows, err := conn.Query(sqlTmpl, schema)
		if err != nil {
			return result, fmt.Errorf("获取表列表失败 (schema=%s): %v", schema, err)
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			if err := rows.Scan(&tableName, &tableType, &tableComment); err != nil {
				return result, fmt.Errorf("扫描表列表失败: %v", err)
			}
			result = append(result, strings.TrimSpace(tableName))
		}
	}
	return result, nil
}

func getTableSchema(conn *sqlx.DB, dbType, schema, table string) (*TableSchema, error) {
	result := &TableSchema{Name: table}

	sqlTmpl, _ := dialect.SQL_DIALECT[dbType]["listTableColumns"]

	var columns []ColumnInfo
	switch dbType {
	case "oracle":
		type OracleCol struct {
			ColumnName string `db:"COLUMN_NAME"`
			DataType   string `db:"COLUMN_TYPE"`
			Nullable   string `db:"IS_NULLABLE"`
			Comments   string `db:"COLUMN_COMMENT"`
		}
		var oracleCols []OracleCol
		err := conn.Select(&oracleCols, sqlTmpl, "notexists", table)
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败: %v", err)
		}
		for _, oc := range oracleCols {
			columns = append(columns, ColumnInfo{
				Name:     oc.ColumnName,
				Type:     oc.DataType,
				Nullable: oc.Nullable,
				Comment:  oc.Comments,
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
			Extra         string  `db:"EXTRA"`
			OrdinalPos    int     `db:"ORDINAL_POSITION"`
			CharMaxLen    *int    `db:"CHARACTER_MAXIMUM_LENGTH"`
		}
		var mysqlCols []MySQLCol
		err := conn.Select(&mysqlCols, sqlTmpl, schema, table)
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败: %v", err)
		}
		for _, mc := range mysqlCols {
			defaultVal := ""
			if mc.ColumnDefault != nil {
				defaultVal = *mc.ColumnDefault
			}
			charMaxLen := 0
			if mc.CharMaxLen != nil {
				charMaxLen = *mc.CharMaxLen
			}
			columns = append(columns, ColumnInfo{
				Name:       mc.ColumnName,
				Type:       mc.ColumnType,
				Nullable:   mc.IsNullable,
				DefaultVal: defaultVal,
				Comment:    mc.ColumnComment,
				Extra:      mc.Extra,
				Position:   mc.OrdinalPos,
				CharMaxLen: charMaxLen,
			})
		}
	}
	result.Columns = columns

	indexes, err := getTableIndexes(conn, dbType, schema, table)
	if err != nil {
		return nil, err
	}
	result.Indexes = indexes

	ddl := generateShowDDL(conn, dbType, table)
	result.DDL = ddl

	var comment string
	switch dbType {
	case "oracle":
		conn.Get(&comment, "SELECT COMMENTS FROM USER_TAB_COMMENTS WHERE TABLE_NAME = :1", table)
	default:
		conn.Get(&comment, "SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", schema, table)
	}
	result.Comment = comment

	return result, nil
}

func getTableIndexes(conn *sqlx.DB, dbType, schema, table string) ([]IndexInfo, error) {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listIndexes"]
	if !ok {
		return nil, nil
	}

	type IndexRow struct {
		IndexName    string `db:"INDEX_NAME"`
		ColName      string `db:"COLUMN_NAME"`
		NonUnique    int    `db:"NON_UNIQUE"`
		SeqInIndex   int    `db:"SEQ_IN_INDEX"`
		IndexType    string `db:"INDEX_TYPE"`
		Nullable     string `db:"NULLABLE"`
		Comment      string `db:"COMMENT"`
		IndexComment string `db:"INDEX_COMMENT"`
	}

	var rows []IndexRow
	switch dbType {
	case "oracle":
		err := conn.Select(&rows, sqlTmpl, table)
		if err != nil {
			return nil, fmt.Errorf("获取索引失败: %v", err)
		}
	default:
		err := conn.Select(&rows, sqlTmpl, schema, table)
		if err != nil {
			return nil, fmt.Errorf("获取索引失败: %v", err)
		}
	}

	idxMap := make(map[string]*IndexInfo)
	var idxOrder []string
	for _, row := range rows {
		name := strings.TrimSpace(row.IndexName)
		if _, ok := idxMap[name]; !ok {
			idxMap[name] = &IndexInfo{
				Name:    name,
				Unique:  row.NonUnique == 0,
				Type:    strings.TrimSpace(row.IndexType),
				Comment: strings.TrimSpace(row.Comment),
				Columns: make([]string, 0),
			}
			idxOrder = append(idxOrder, name)
		}
		idxMap[name].Columns = append(idxMap[name].Columns, strings.TrimSpace(row.ColName))
	}

	result := make([]IndexInfo, 0)
	for _, name := range idxOrder {
		result = append(result, *idxMap[name])
	}
	return result, nil
}

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '$') {
			return false
		}
	}
	return true
}

func generateShowDDL(conn *sqlx.DB, dbType, table string) string {
	if !isValidIdentifier(table) {
		return ""
	}
	switch dbType {
	case "oracle":
		var ddl string
		err := conn.Get(&ddl, "SELECT DBMS_METADATA.GET_DDL('TABLE', :1) FROM DUAL", table)
		if err != nil {
			return ""
		}
		return ddl
	default:
		type ShowCreateResult struct {
			Table       string `db:"Table"`
			CreateTable string `db:"Create Table"`
		}
		var result []ShowCreateResult
		err := conn.Select(&result, fmt.Sprintf("SHOW CREATE TABLE `%s`", table))
		if err != nil || len(result) == 0 {
			return ""
		}
		return result[0].CreateTable
	}
}

func ApplySchemaDiff(c *gin.Context) {
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)

	if strings.TrimSpace(sqlStr) == "" {
		jsonutil.WriteJson(c.Writer, map[string]any{"success": false, "message": "SQL不能为空"})
		return
	}

	sqlList := strings.Split(sqlStr, ";")
	validatedSQLs := make([]string, 0, len(sqlList))
	for _, s := range sqlList {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if err := validateSchemaSQL(s); err != nil {
			jsonutil.WriteJson(c.Writer, map[string]any{"success": false, "message": err.Error()})
			return
		}
		validatedSQLs = append(validatedSQLs, s)
	}

	tx, err := conn.Beginx()
	if err != nil {
		jsonutil.WriteJson(c.Writer, map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)})
		return
	}

	executedCount := 0
	errors := make([]string, 0)

	for _, s := range validatedSQLs {
		_, err := tx.Exec(s)
		if err != nil {
			if len(s) > 80 {
				s = s[:80]
			}
			errors = append(errors, fmt.Sprintf("执行失败: %s - %s", s, err.Error()))
		} else {
			executedCount++
		}
	}

	if len(errors) > 0 {
		tx.Rollback()
	} else {
		tx.Commit()
	}

	log.Printf("[SyncAudit] ApplySchemaDiff connId=%s schema=%s sqlCount=%d success=%v user=%s",
		connId, schema, len(validatedSQLs), len(errors) == 0, authorization)

	jsonutil.WriteJson(c.Writer, map[string]any{
		"success":       len(errors) == 0,
		"executedCount": executedCount,
		"errorCount":    len(errors),
		"errors":        errors,
	})
}

func validateSchemaSQL(sql string) error {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	allowedPrefixes := []string{
		"ALTER TABLE", "CREATE INDEX", "DROP INDEX",
	}
	matched := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的SQL类型: %s (仅允许 ALTER TABLE / CREATE INDEX / DROP INDEX)", preview)
	}

	dangerousPatterns := []string{
		"DROP TABLE", "DROP DATABASE", "TRUNCATE",
		"GRANT", "REVOKE", "CREATE USER", "ALTER USER", "DROP USER",
		"SHUTDOWN", "LOAD DATA", "INTO OUTFILE", "INTO DUMPFILE",
	}
	for _, d := range dangerousPatterns {
		if strings.Contains(upper, d) {
			return fmt.Errorf("SQL包含危险操作: %s", d)
		}
	}

	return nil
}

func GetSyncTargets(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	connId := c.Query("connId")
	schema := c.Query("schema")

	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	if schema == "" {
		switch dbType {
		case "mysql", "mariadb":
			conn.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			conn.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	tables, _ := getTableList(conn, dbType, schema)
	schemas := getSchemaList(conn, dbType)

	jsonutil.WriteJson(c.Writer, map[string]any{
		"tables":  tables,
		"schemas": schemas,
	})
}

func getSchemaList(conn *sqlx.DB, dbType string) []string {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listSchema"]
	if !ok {
		sqlTmpl = "SELECT schema_name FROM information_schema.schemata ORDER BY schema_name"
	}
	schemas := make([]string, 0)
	err := conn.Select(&schemas, sqlTmpl)
	if err != nil {
		log.Printf("[SyncAudit] 获取Schema列表失败: %v", err)
	}
	return schemas
}

func init() {
	_ = config.Cfg
}