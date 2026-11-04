package modeler

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestPaginateTables(t *testing.T) {
	tables := []ERSchemaTable{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}

	page, next, loaded := paginateTables(tables, "", 2)
	if len(page) != 2 || page[0].Name != "a" || page[1].Name != "b" {
		t.Fatalf("首页结果不符: %+v", page)
	}
	if next != "b" || loaded != 2 {
		t.Fatalf("首页游标不符: next=%q loaded=%d", next, loaded)
	}

	page, next, loaded = paginateTables(tables, "b", 2)
	if len(page) != 2 || page[0].Name != "c" || page[1].Name != "d" {
		t.Fatalf("第二页结果不符: %+v", page)
	}
	if next != "" || loaded != 4 {
		t.Fatalf("末页游标不符: next=%q loaded=%d", next, loaded)
	}

	page, next, loaded = paginateTables(tables, "", 0)
	if len(page) != 4 || next != "" || loaded != 4 {
		t.Fatalf("不分页结果不符: len=%d next=%q loaded=%d", len(page), next, loaded)
	}
}

func TestLoadSQLiteSchemaCompositeAndView(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	stmts := []string{
		// 复合主键父表
		`CREATE TABLE parent (a INTEGER NOT NULL, b INTEGER NOT NULL, name TEXT, PRIMARY KEY (a, b))`,
		// 复合外键且 REFERENCES 未写列名（to 为空，需按 seq 回填父表主键）
		`CREATE TABLE child (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER,
			FOREIGN KEY (a, b) REFERENCES parent)`,
		// 纯连接表：复合主键全部是外键列
		`CREATE TABLE link (a INTEGER NOT NULL, b INTEGER NOT NULL, other_id INTEGER,
			PRIMARY KEY (a, b),
			FOREIGN KEY (a, b) REFERENCES parent(a, b),
			FOREIGN KEY (other_id) REFERENCES child(id))`,
		// 唯一索引
		`CREATE TABLE uq (id INTEGER PRIMARY KEY, code TEXT, region TEXT)`,
		`CREATE UNIQUE INDEX idx_uq_code_region ON uq(code, region)`,
		// 部分唯一索引不应计入唯一键组
		`CREATE UNIQUE INDEX idx_uq_partial ON uq(code) WHERE region IS NOT NULL`,
		`CREATE VIEW v_child AS SELECT id, a, b FROM child`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("初始化测试库失败: %v\nSQL: %s", err, s)
		}
	}

	tables, relations, err := loadSQLiteSchema(db)
	if err != nil {
		t.Fatalf("加载 SQLite schema 失败: %v", err)
	}

	tableMap := make(map[string]ERSchemaTable, len(tables))
	for _, table := range tables {
		tableMap[table.Name] = table
	}

	parent, ok := tableMap["parent"]
	if !ok {
		t.Fatalf("缺少 parent 表: %+v", tables)
	}
	if len(parent.Columns) != 3 || !parent.Columns[0].PrimaryKey || !parent.Columns[1].PrimaryKey {
		t.Fatalf("复合主键识别失败: %+v", parent.Columns)
	}
	if !containsKey(parent.UniqueKeys, []string{"a", "b"}) {
		t.Fatalf("复合主键未进入 uniqueKeys: %+v", parent.UniqueKeys)
	}

	view, ok := tableMap["v_child"]
	if !ok {
		t.Fatalf("缺少视图节点")
	}
	if view.Type != "VIEW" || len(view.Columns) != 3 {
		t.Fatalf("视图字段未加载: type=%s columns=%+v", view.Type, view.Columns)
	}

	uq := tableMap["uq"]
	if !containsKey(uq.UniqueKeys, []string{"code", "region"}) {
		t.Fatalf("复合唯一索引未进入 uniqueKeys: %+v", uq.UniqueKeys)
	}
	for _, key := range uq.UniqueKeys {
		if len(key) == 1 && key[0] == "code" {
			t.Fatalf("部分唯一索引不应进入 uniqueKeys: %+v", uq.UniqueKeys)
		}
	}

	// 复合外键必须按 constraintId 成组，且 to 为空时回填父表主键。
	var childGroup []ERSchemaRelation
	for _, r := range relations {
		if r.Source == "child" {
			childGroup = append(childGroup, r)
		}
	}
	if len(childGroup) != 2 {
		t.Fatalf("复合外键列数不符: %+v", relations)
	}
	if childGroup[0].ConstraintID != childGroup[1].ConstraintID {
		t.Fatalf("复合外键未按 constraintId 成组: %+v", childGroup)
	}
	if childGroup[0].TargetCol != "a" || childGroup[1].TargetCol != "b" {
		t.Fatalf("to 为空时回填父表主键失败: %+v", childGroup)
	}

	// link 表应形成两条独立外键约束（复合 FK 一组 + 单列 FK 一组）。
	linkConstraints := map[string]bool{}
	for _, r := range relations {
		if r.Source == "link" {
			linkConstraints[r.ConstraintID] = true
		}
	}
	if len(linkConstraints) != 2 {
		t.Fatalf("link 外键约束数量不符: %+v", linkConstraints)
	}
}

func containsKey(keys [][]string, want []string) bool {
	for _, key := range keys {
		if len(key) != len(want) {
			continue
		}
		matched := true
		for i := range want {
			if key[i] != want[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestNormalizeRelations(t *testing.T) {
	tables := []AnalyzeTable{
		{
			Name: "orders",
			Columns: []AnalyzeColumn{
				{Name: "id", PrimaryKey: true},
				{Name: "user_id"},
			},
		},
		{
			Name: "order_item",
			Columns: []AnalyzeColumn{
				{Name: "id", PrimaryKey: true},
				{Name: "order_id"},
			},
		},
	}

	in := []AnalyzeRelation{
		{Source: "order_item", SourceCol: "order_id", Target: "orders", TargetCol: "id", RelationType: "1:n"},
		{Source: "order_item", SourceCol: "order_id", Target: "orders", TargetCol: "id", RelationType: "1:N"}, // 重复
		{Source: "order_item", SourceCol: "missing", Target: "orders", TargetCol: "id"},                       // 字段不存在
		{Source: "order_item", SourceCol: "order_id", Target: "ghost", TargetCol: "id"},                       // 表不存在
		{Source: "orders", SourceCol: "id", Target: "orders", TargetCol: "id"},                                // 自环
		{Source: "orders", SourceCol: "id", Target: "order_item", TargetCol: "order_id", RelationType: "1:1", Confidence: "HIGH"},
	}
	out := normalizeRelations(in, tables)
	if len(out) != 2 {
		t.Fatalf("归一化结果数量不符: got=%d want=2 (%+v)", len(out), out)
	}
	if out[0].RelationType != "1:N" || out[0].Confidence != "medium" {
		t.Fatalf("关系类型/置信度归一化失败: %+v", out[0])
	}
	if out[1].RelationType != "1:1" || out[1].Confidence != "high" {
		t.Fatalf("关系类型/置信度归一化失败: %+v", out[1])
	}
}
