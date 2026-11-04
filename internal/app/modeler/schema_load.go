// Package modeler: schema_load.go — 各方言 ER 元数据加载实现。
//
// 统一以「表」为主键组织数据：每张表包含字段与唯一键组；外键关系单独返回。
// 所有查询均使用参数化语句或只读数据字典视图，不使用字符串拼接标识符，
// 以避免中文/特殊字符标识符带来的注入风险。
package modeler

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// ==================== MySQL / MariaDB ====================

func loadMySQLSchema(db *sqlx.DB, schema string) ([]ERSchemaTable, []ERSchemaRelation, error) {
	type tableRow struct {
		TableName    string         `db:"TABLE_NAME"`
		TableType    string         `db:"TABLE_TYPE"`
		TableComment sql.NullString `db:"TABLE_COMMENT"`
	}
	var tableRows []tableRow
	if err := db.Select(&tableRows, `SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		ORDER BY TABLE_NAME`, schema); err != nil {
		return nil, nil, fmt.Errorf("查询表列表失败: %w", err)
	}

	type columnRow struct {
		TableName     string         `db:"TABLE_NAME"`
		ColumnName    string         `db:"COLUMN_NAME"`
		ColumnType    string         `db:"COLUMN_TYPE"`
		ColumnComment sql.NullString `db:"COLUMN_COMMENT"`
		ColumnKey     string         `db:"COLUMN_KEY"`
	}
	var columnRows []columnRow
	if err := db.Select(&columnRows, `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT, COLUMN_KEY
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME, ORDINAL_POSITION`, schema); err != nil {
		return nil, nil, fmt.Errorf("查询字段信息失败: %w", err)
	}

	type indexRow struct {
		TableName  string         `db:"TABLE_NAME"`
		IndexName  string         `db:"INDEX_NAME"`
		ColumnName sql.NullString `db:"COLUMN_NAME"`
		NonUnique  int            `db:"NON_UNIQUE"`
	}
	var indexRows []indexRow
	if err := db.Select(&indexRows, `SELECT TABLE_NAME, INDEX_NAME, COLUMN_NAME, NON_UNIQUE
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND NON_UNIQUE = 0
		ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`, schema); err != nil {
		return nil, nil, fmt.Errorf("查询唯一键失败: %w", err)
	}

	type fkRow struct {
		ConstraintName string `db:"CONSTRAINT_NAME"`
		TableName      string `db:"TABLE_NAME"`
		ColumnName     string `db:"COLUMN_NAME"`
		RefTable       string `db:"REFERENCED_TABLE_NAME"`
		RefColumn      string `db:"REFERENCED_COLUMN_NAME"`
	}
	var fkRows []fkRow
	// 仅保留同 schema 外键，避免关系指向当前图中不存在的节点。
	if err := db.Select(&fkRows, `SELECT CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME,
			REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND REFERENCED_TABLE_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION`, schema, schema); err != nil {
		return nil, nil, fmt.Errorf("查询外键关系失败: %w", err)
	}

	tableIndex := make(map[string]*ERSchemaTable, len(tableRows))
	tables := make([]ERSchemaTable, 0, len(tableRows))
	for _, r := range tableRows {
		t := ERSchemaTable{
			Name:       r.TableName,
			Type:       normalizeTableType(r.TableType),
			Comment:    r.TableComment.String,
			Columns:    make([]ERSchemaColumn, 0),
			UniqueKeys: make([][]string, 0),
		}
		tables = append(tables, t)
	}
	for i := range tables {
		tableIndex[tables[i].Name] = &tables[i]
	}

	pkCols := make(map[string][]string)
	for _, r := range columnRows {
		t, ok := tableIndex[r.TableName]
		if !ok {
			continue
		}
		isPK := strings.EqualFold(r.ColumnKey, "PRI")
		t.Columns = append(t.Columns, ERSchemaColumn{
			Name:       r.ColumnName,
			Type:       r.ColumnType,
			Comment:    r.ColumnComment.String,
			PrimaryKey: isPK,
		})
		if isPK {
			pkCols[r.TableName] = append(pkCols[r.TableName], r.ColumnName)
		}
	}

	// 按索引名聚合唯一键组（STATISTICS 已按 SEQ_IN_INDEX 排序）。
	type indexKey struct {
		Table string
		Index string
	}
	uniqueGroups := make(map[indexKey][]string)
	indexOrder := make([]indexKey, 0)
	for _, r := range indexRows {
		if _, ok := tableIndex[r.TableName]; !ok {
			continue
		}
		k := indexKey{Table: r.TableName, Index: r.IndexName}
		if _, exists := uniqueGroups[k]; !exists {
			indexOrder = append(indexOrder, k)
		}
		if !r.ColumnName.Valid || strings.TrimSpace(r.ColumnName.String) == "" {
			// 函数索引/表达式索引：整组作废（appendUniqueKey 会因空列忽略）。
			uniqueGroups[k] = append(uniqueGroups[k], "")
			continue
		}
		uniqueGroups[k] = append(uniqueGroups[k], r.ColumnName.String)
	}
	for _, k := range indexOrder {
		t := tableIndex[k.Table]
		if t == nil {
			continue
		}
		appendUniqueKey(&t.UniqueKeys, uniqueGroups[k])
	}
	for name, pk := range pkCols {
		if t := tableIndex[name]; t != nil {
			appendUniqueKey(&t.UniqueKeys, pk)
		}
	}

	relations := make([]ERSchemaRelation, 0, len(fkRows))
	for _, r := range fkRows {
		if tableIndex[r.TableName] == nil || tableIndex[r.RefTable] == nil {
			continue
		}
		relations = append(relations, ERSchemaRelation{
			Source:         r.TableName,
			SourceCol:      r.ColumnName,
			Target:         r.RefTable,
			TargetCol:      r.RefColumn,
			ConstraintName: r.ConstraintName,
		})
	}
	return tables, relations, nil
}

// ==================== SQLite ====================

func loadSQLiteSchema(db *sqlx.DB) ([]ERSchemaTable, []ERSchemaRelation, error) {
	type tableRow struct {
		Name string `db:"NAME"`
		Type string `db:"TYPE"`
	}
	var tableRows []tableRow
	if err := db.Select(&tableRows, `SELECT name AS NAME, type AS TYPE
		FROM sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY name`); err != nil {
		return nil, nil, fmt.Errorf("查询表列表失败: %w", err)
	}

	tables := make([]ERSchemaTable, 0, len(tableRows))
	for _, r := range tableRows {
		tables = append(tables, ERSchemaTable{
			Name:       r.Name,
			Type:       normalizeTableType(r.Type),
			Comment:    "",
			Columns:    make([]ERSchemaColumn, 0),
			UniqueKeys: make([][]string, 0),
		})
	}

	// pkByTable 记录复合主键的真实顺序（pk 值 1..N），用于回填外键目标列。
	pkByTable := make(map[string][]string)
	type fkGroupKey struct {
		Table string
		ID    int
	}
	fkRowsByGroup := make(map[fkGroupKey][]ERSchemaRelation)
	fkGroupOrder := make([]fkGroupKey, 0)

	for i := range tables {
		t := &tables[i]
		type colRow struct {
			CID  int    `db:"CID"`
			Name string `db:"NAME"`
			Type string `db:"TYPE"`
			Pk   int    `db:"PK"`
		}
		var colRows []colRow
		// 视图同样执行 pragma_table_info，保证 SQLite 视图节点也能展示字段。
		if err := db.Select(&colRows, `SELECT cid AS CID, name AS NAME, type AS TYPE, pk AS PK
			FROM pragma_table_info(?) ORDER BY cid`, t.Name); err != nil {
			return nil, nil, fmt.Errorf("查询表 %s 字段失败: %w", t.Name, err)
		}
		pkPairs := make([]struct {
			Order int
			Name  string
		}, 0)
		for _, r := range colRows {
			t.Columns = append(t.Columns, ERSchemaColumn{
				Name:       r.Name,
				Type:       r.Type,
				Comment:    "",
				PrimaryKey: r.Pk > 0,
			})
			if r.Pk > 0 {
				pkPairs = append(pkPairs, struct {
					Order int
					Name  string
				}{Order: r.Pk, Name: r.Name})
			}
		}
		sort.SliceStable(pkPairs, func(a, b int) bool { return pkPairs[a].Order < pkPairs[b].Order })
		pk := make([]string, 0, len(pkPairs))
		for _, p := range pkPairs {
			pk = append(pk, p.Name)
		}
		if len(pk) > 0 {
			pkByTable[t.Name] = pk
			appendUniqueKey(&t.UniqueKeys, pk)
		}

		type idxRow struct {
			IndexName string `db:"INDEX_NAME"`
		}
		var idxRows []idxRow
		if err := db.Select(&idxRows, `SELECT il.name AS INDEX_NAME
			FROM pragma_index_list(?) AS il
			WHERE il."unique" = 1 AND COALESCE(il.partial, 0) = 0
			ORDER BY il.name`, t.Name); err != nil {
			return nil, nil, fmt.Errorf("查询表 %s 索引失败: %w", t.Name, err)
		}
		for _, idx := range idxRows {
			type idxColRow struct {
				ColumnName sql.NullString `db:"COLUMN_NAME"`
			}
			var cols []idxColRow
			if err := db.Select(&cols, `SELECT name AS COLUMN_NAME
				FROM pragma_index_info(?) ORDER BY seqno`, idx.IndexName); err != nil {
				return nil, nil, fmt.Errorf("查询表 %s 索引列失败: %w", t.Name, err)
			}
			key := make([]string, 0, len(cols))
			for _, c := range cols {
				key = append(key, c.ColumnName.String)
			}
			appendUniqueKey(&t.UniqueKeys, key)
		}

		type fkRow struct {
			ID       int            `db:"ID"`
			Seq      int            `db:"SEQ"`
			RefTable string         `db:"REF_TABLE"`
			FromCol  string         `db:"FROM_COL"`
			ToCol    sql.NullString `db:"TO_COL"`
		}
		var fkRows []fkRow
		if err := db.Select(&fkRows, `SELECT id AS ID, seq AS SEQ, "table" AS REF_TABLE,
			"from" AS FROM_COL, "to" AS TO_COL
			FROM pragma_foreign_key_list(?) ORDER BY id, seq`, t.Name); err != nil {
			return nil, nil, fmt.Errorf("查询表 %s 外键失败: %w", t.Name, err)
		}
		for _, r := range fkRows {
			gk := fkGroupKey{Table: t.Name, ID: r.ID}
			if _, exists := fkRowsByGroup[gk]; !exists {
				fkGroupOrder = append(fkGroupOrder, gk)
			}
			fkRowsByGroup[gk] = append(fkRowsByGroup[gk], ERSchemaRelation{
				Source:       t.Name,
				SourceCol:    r.FromCol,
				Target:       r.RefTable,
				TargetCol:    r.ToCol.String,
				ConstraintID: strconv.Itoa(r.ID),
			})
		}
	}

	// SQLite 的 "to" 可能为空（表示引用父表主键），按 seq 顺序回填父表主键列。
	for _, gk := range fkGroupOrder {
		group := fkRowsByGroup[gk]
		if len(group) == 0 {
			continue
		}
		pk := pkByTable[group[0].Target]
		for i := range group {
			if group[i].TargetCol == "" && i < len(pk) {
				group[i].TargetCol = pk[i]
			}
		}
		fkRowsByGroup[gk] = group
	}

	relations := make([]ERSchemaRelation, 0, len(fkGroupOrder))
	tableNames := make(map[string]bool, len(tables))
	for _, t := range tables {
		tableNames[t.Name] = true
	}
	for _, gk := range fkGroupOrder {
		for _, r := range fkRowsByGroup[gk] {
			if !tableNames[r.Target] {
				continue // 外键指向 sqlite_% 内部表或不存在对象
			}
			relations = append(relations, r)
		}
	}
	return tables, relations, nil
}

// ==================== Oracle ====================

func loadOracleSchema(db *sqlx.DB) ([]ERSchemaTable, []ERSchemaRelation, error) {
	type tableRow struct {
		TableName    string `db:"TABLE_NAME"`
		TableType    string `db:"TABLE_TYPE"`
		TableComment string `db:"TABLE_COMMENT"`
	}
	var tableRows []tableRow
	// USER_TAB_COMMENTS 同时覆盖表与视图，且与现有方言口径（USER_*）一致。
	if err := db.Select(&tableRows, `SELECT TABLE_NAME, DECODE(TABLE_TYPE, 'VIEW', 'VIEW', 'TABLE') AS TABLE_TYPE,
		NVL(COMMENTS, '') AS TABLE_COMMENT
		FROM USER_TAB_COMMENTS ORDER BY TABLE_NAME`); err != nil {
		return nil, nil, fmt.Errorf("查询表列表失败: %w", err)
	}

	type columnRow struct {
		TableName     string `db:"TABLE_NAME"`
		ColumnName    string `db:"COLUMN_NAME"`
		ColumnType    string `db:"COLUMN_TYPE"`
		ColumnComment string `db:"COLUMN_COMMENT"`
	}
	var columnRows []columnRow
	if err := db.Select(&columnRows, `SELECT c.TABLE_NAME, c.COLUMN_NAME, c.DATA_TYPE AS COLUMN_TYPE,
		NVL(cc.COMMENTS, '') AS COLUMN_COMMENT
		FROM USER_TAB_COLUMNS c
		LEFT JOIN USER_COL_COMMENTS cc
			ON c.TABLE_NAME = cc.TABLE_NAME AND c.COLUMN_NAME = cc.COLUMN_NAME
		ORDER BY c.TABLE_NAME, c.COLUMN_ID`); err != nil {
		return nil, nil, fmt.Errorf("查询字段信息失败: %w", err)
	}

	type constraintRow struct {
		TableName      string `db:"TABLE_NAME"`
		ConstraintName string `db:"CONSTRAINT_NAME"`
		ConstraintType string `db:"CONSTRAINT_TYPE"`
		ColumnName     string `db:"COLUMN_NAME"`
		Position       int    `db:"POSITION"`
	}
	var consRows []constraintRow
	if err := db.Select(&consRows, `SELECT c.TABLE_NAME, c.CONSTRAINT_NAME, c.CONSTRAINT_TYPE, cc.COLUMN_NAME, cc.POSITION
		FROM USER_CONSTRAINTS c
		JOIN USER_CONS_COLUMNS cc
			ON c.CONSTRAINT_NAME = cc.CONSTRAINT_NAME AND c.OWNER = cc.OWNER
		WHERE c.CONSTRAINT_TYPE IN ('P', 'U')
		ORDER BY c.TABLE_NAME, c.CONSTRAINT_NAME, cc.POSITION`); err != nil {
		return nil, nil, fmt.Errorf("查询主键/唯一键失败: %w", err)
	}

	type fkRow struct {
		TableName      string `db:"TABLE_NAME"`
		ConstraintName string `db:"CONSTRAINT_NAME"`
		ColumnName     string `db:"COLUMN_NAME"`
		RefTable       string `db:"REF_TABLE"`
		RefColumn      string `db:"REF_COLUMN"`
		Position       int    `db:"POSITION"`
	}
	var fkRows []fkRow
	if err := db.Select(&fkRows, `SELECT c.TABLE_NAME, c.CONSTRAINT_NAME, cc.COLUMN_NAME,
		rc.TABLE_NAME AS REF_TABLE, rcc.COLUMN_NAME AS REF_COLUMN, cc.POSITION
		FROM USER_CONSTRAINTS c
		JOIN USER_CONS_COLUMNS cc
			ON c.CONSTRAINT_NAME = cc.CONSTRAINT_NAME AND c.OWNER = cc.OWNER
		JOIN USER_CONSTRAINTS rc
			ON c.R_CONSTRAINT_NAME = rc.CONSTRAINT_NAME AND c.OWNER = rc.OWNER
		JOIN USER_CONS_COLUMNS rcc
			ON rc.CONSTRAINT_NAME = rcc.CONSTRAINT_NAME AND rc.OWNER = rcc.OWNER
			AND cc.POSITION = rcc.POSITION
		WHERE c.CONSTRAINT_TYPE = 'R'
		ORDER BY c.TABLE_NAME, c.CONSTRAINT_NAME, cc.POSITION`); err != nil {
		return nil, nil, fmt.Errorf("查询外键关系失败: %w", err)
	}

	tables := make([]ERSchemaTable, 0, len(tableRows))
	tableIndex := make(map[string]*ERSchemaTable, len(tableRows))
	for _, r := range tableRows {
		tables = append(tables, ERSchemaTable{
			Name:       r.TableName,
			Type:       normalizeTableType(r.TableType),
			Comment:    r.TableComment,
			Columns:    make([]ERSchemaColumn, 0),
			UniqueKeys: make([][]string, 0),
		})
	}
	for i := range tables {
		tableIndex[tables[i].Name] = &tables[i]
	}
	for _, r := range columnRows {
		if t := tableIndex[r.TableName]; t != nil {
			t.Columns = append(t.Columns, ERSchemaColumn{
				Name:    r.ColumnName,
				Type:    r.ColumnType,
				Comment: r.ColumnComment,
			})
		}
	}

	type consKey struct {
		Table string
		Name  string
	}
	consGroups := make(map[consKey][]string)
	consOrder := make([]consKey, 0)
	for _, r := range consRows {
		k := consKey{Table: r.TableName, Name: r.ConstraintName}
		if _, exists := consGroups[k]; !exists {
			consOrder = append(consOrder, k)
		}
		consGroups[k] = append(consGroups[k], r.ColumnName)
		if r.ConstraintType == "P" {
			if t := tableIndex[r.TableName]; t != nil {
				for i := range t.Columns {
					if t.Columns[i].Name == r.ColumnName {
						t.Columns[i].PrimaryKey = true
					}
				}
			}
		}
	}
	for _, k := range consOrder {
		if t := tableIndex[k.Table]; t != nil {
			appendUniqueKey(&t.UniqueKeys, consGroups[k])
		}
	}

	relations := make([]ERSchemaRelation, 0, len(fkRows))
	for _, r := range fkRows {
		if tableIndex[r.TableName] == nil || tableIndex[r.RefTable] == nil {
			continue
		}
		relations = append(relations, ERSchemaRelation{
			Source:         r.TableName,
			SourceCol:      r.ColumnName,
			Target:         r.RefTable,
			TargetCol:      r.RefColumn,
			ConstraintName: r.ConstraintName,
		})
	}
	return tables, relations, nil
}

func normalizeTableType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "VIEW":
		return "VIEW"
	default:
		return "TABLE"
	}
}
