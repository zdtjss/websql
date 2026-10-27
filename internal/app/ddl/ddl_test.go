package ddl

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }
func strPtr(s string) *string { return &s }

// TestBuildStatements_MySQLModifyColumn 回归缺陷 D2：
// 改类型必须保留 NOT NULL，且表达式默认值不能加引号（CURRENT_TIMESTAMP）。
func TestBuildStatements_MySQLModifyColumn(t *testing.T) {
	stmts, warnings, unsupported, err := buildStatements("mysql", "t_user", []Change{{
		Kind: KindModifyColumn,
		Column: &ColumnDef{
			Name:     "created_at",
			Type:     "datetime",
			Nullable: boolPtr(false),
			Default:  strPtr("CURRENT_TIMESTAMP"),
			Comment:  "创建时间",
		},
	}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(unsupported) != 0 {
		t.Fatalf("MySQL 不应有 unsupported: %v", unsupported)
	}
	want := "ALTER TABLE `t_user` MODIFY `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'"
	if len(stmts) != 1 || stmts[0].SQL != want {
		t.Fatalf("SQL 不符\n got: %+v\nwant: %s", stmts, want)
	}
	if stmts[0].Risk != RiskMedium {
		t.Errorf("改类型风险应为 medium，实际 %s", stmts[0].Risk)
	}
	_ = warnings
}

// TestBuildStatements_DefaultValueKinds 默认值按表达式/数字/字面量分类。
func TestBuildStatements_DefaultValueKinds(t *testing.T) {
	cases := []struct {
		def  *string
		want string
	}{
		{nil, "DEFAULT NULL"},
		{strPtr(""), "DEFAULT NULL"},
		{strPtr("0"), "DEFAULT 0"},
		{strPtr("1.5"), "DEFAULT 1.5"},
		{strPtr("now()"), "DEFAULT now()"},
		{strPtr("CURRENT_TIMESTAMP(6)"), "DEFAULT CURRENT_TIMESTAMP(6)"},
		{strPtr("it's"), "DEFAULT 'it''s'"},
		{strPtr("'already-quoted'"), "DEFAULT 'already-quoted'"},
		{strPtr("b'01'"), "DEFAULT b'01'"},
	}
	for _, tc := range cases {
		stmts, _, _, err := buildStatements("mysql", "t", []Change{{
			Kind:   KindModifyColumn,
			Column: &ColumnDef{Name: "c", Type: "int", Default: tc.def},
		}})
		if err != nil {
			t.Fatalf("生成失败(%v): %v", tc.def, err)
		}
		if !strings.Contains(stmts[0].SQL, tc.want) {
			t.Errorf("默认值 %v 期望包含 %q，实际 %q", tc.def, tc.want, stmts[0].SQL)
		}
	}
}

// TestBuildStatements_MySQLRenameKeepsDefinition MySQL CHANGE 必须重述完整列定义。
func TestBuildStatements_MySQLRenameKeepsDefinition(t *testing.T) {
	stmts, _, _, err := buildStatements("mysql", "t", []Change{{
		Kind:    KindRenameColumn,
		OldName: "old",
		Name:    "new",
		Column: &ColumnDef{
			Name:     "old",
			Type:     "int",
			Nullable: boolPtr(false),
			Default:  strPtr("0"),
		},
	}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	want := "ALTER TABLE `t` CHANGE `old` `new` int NOT NULL DEFAULT 0"
	if stmts[0].SQL != want {
		t.Fatalf("SQL 不符\n got: %s\nwant: %s", stmts[0].SQL, want)
	}
}

// TestBuildStatements_OracleOrder Oracle 的 DEFAULT 必须在 NOT NULL 之前，注释单独一条。
func TestBuildStatements_OracleOrder(t *testing.T) {
	stmts, _, _, err := buildStatements("oracle", "t", []Change{
		{
			Kind: KindModifyColumn,
			Column: &ColumnDef{
				Name:     "c",
				Type:     "number(10)",
				Nullable: boolPtr(false),
				Default:  strPtr("0"),
				Comment:  "备注",
			},
		},
		{
			Kind:   KindAddColumn,
			Column: &ColumnDef{Name: "d", Type: "int", Comment: "新增"},
		},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(stmts) != 4 {
		t.Fatalf("期望 4 条语句（MODIFY+COMMENT+ADD+COMMENT），实际 %d: %+v", len(stmts), stmts)
	}
	if stmts[0].SQL != `ALTER TABLE "t" MODIFY ("c" number(10) DEFAULT 0 NOT NULL)` {
		t.Errorf("MODIFY 语句不符: %s", stmts[0].SQL)
	}
	if stmts[1].SQL != `COMMENT ON COLUMN "t"."c" IS '备注'` {
		t.Errorf("COMMENT 语句不符: %s", stmts[1].SQL)
	}
	if stmts[2].SQL != `ALTER TABLE "t" ADD ("d" int DEFAULT NULL)` {
		t.Errorf("ADD 语句不符: %s", stmts[2].SQL)
	}
}

// TestBuildStatements_SQLiteUnsupported SQLite 不支持改列，必须显式标记而不是发错误 SQL。
func TestBuildStatements_SQLiteUnsupported(t *testing.T) {
	stmts, _, unsupported, err := buildStatements("sqlite", "t", []Change{{
		Kind:   KindModifyColumn,
		Column: &ColumnDef{Name: "c", Type: "int"},
	}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(stmts) != 0 {
		t.Errorf("SQLite 改列不应生成语句: %+v", stmts)
	}
	if len(unsupported) != 1 || unsupported[0] != string(KindModifyColumn) {
		t.Errorf("应标记 modify_column 为不支持，实际 %v", unsupported)
	}

	// 新增列在 SQLite 是支持的
	stmts, _, _, err = buildStatements("sqlite", "t", []Change{{
		Kind:   KindAddColumn,
		Column: &ColumnDef{Name: "c", Type: "int", Default: strPtr("0")},
	}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(stmts) != 1 || stmts[0].SQL != "ALTER TABLE `t` ADD COLUMN `c` int DEFAULT 0" {
		t.Fatalf("SQLite ADD COLUMN 生成异常: %+v", stmts)
	}
}

// TestBuildStatements_DropColumnWarns 删除列必须是 high 风险并带提示。
func TestBuildStatements_DropColumnWarns(t *testing.T) {
	stmts, _, _, err := buildStatements("mysql", "t", []Change{{Kind: KindDropColumn, Name: "c"}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if stmts[0].Risk != RiskHigh {
		t.Errorf("删列风险应为 high，实际 %s", stmts[0].Risk)
	}
	if stmts[0].Message == "" {
		t.Error("删列应带风险提示")
	}
	if stmts[0].SQL != "ALTER TABLE `t` DROP COLUMN `c`" {
		t.Errorf("SQL 不符: %s", stmts[0].SQL)
	}
}

// TestBuildStatements_Index 索引创建/删除按方言生成。
func TestBuildStatements_Index(t *testing.T) {
	stmts, _, _, err := buildStatements("mysql", "t", []Change{{
		Kind:  KindCreateIndex,
		Index: &IndexDef{Name: "idx_a", Unique: true, Columns: []string{"a", "b"}},
	}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if stmts[0].SQL != "CREATE UNIQUE INDEX `idx_a` ON `t` (`a`, `b`)" {
		t.Errorf("建索引 SQL 不符: %s", stmts[0].SQL)
	}

	stmts, _, _, err = buildStatements("mysql", "t", []Change{{Kind: KindDropIndex, Name: "idx_a"}})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if stmts[0].SQL != "DROP INDEX `idx_a` ON `t`" {
		t.Errorf("删索引 SQL 不符: %s", stmts[0].SQL)
	}
}

// TestQuoteIdent 标识符转义：中文可用，引号转义，控制字符拒绝。
func TestQuoteIdent(t *testing.T) {
	if got, err := quoteIdent("订单号", "mysql"); err != nil || got != "`订单号`" {
		t.Errorf("中文列名应被支持，got=%q err=%v", got, err)
	}
	if got, err := quoteIdent("a`b", "mysql"); err != nil || got != "`a``b`" {
		t.Errorf("反引号应转义，got=%q err=%v", got, err)
	}
	if got, err := quoteIdent(`a"b`, "oracle"); err != nil || got != `"a""b"` {
		t.Errorf("双引号应转义，got=%q err=%v", got, err)
	}
	if _, err := quoteIdent("  ", "mysql"); err == nil {
		t.Error("空标识符应报错")
	}
	if _, err := quoteIdent("a\nb", "mysql"); err == nil {
		t.Error("控制字符应报错")
	}
}

// TestPlanStore_OneShot 计划只能被执行一次，且连接不匹配时拒绝。
func TestPlanStore_OneShot(t *testing.T) {
	plan := &Plan{PlanId: "plan-1", DbType: "mysql", Table: "t"}
	storePlan(plan, "conn-1")

	if _, err := takePlan("plan-1", "conn-2"); err == nil {
		t.Fatal("连接不匹配时应拒绝")
	}
	// 上一步已消费，第二次取应失败
	if _, err := takePlan("plan-1", "conn-1"); err == nil {
		t.Fatal("计划应只能消费一次")
	}

	storePlan(&Plan{PlanId: "plan-2", DbType: "mysql", Table: "t"}, "conn-1")
	if got, err := takePlan("plan-2", "conn-1"); err != nil || got == nil {
		t.Fatalf("同连接取计划应成功: err=%v", err)
	}
}
