package agent

import (
	"testing"
)

// TestMaskRulesFromManagementDB_RedactUnknownOriginExportColumn 端到端验证导出脱敏链路：
// 规则来自管理库 t_data_mask_rule（经 MaskerProvider 刷新），
// 列来源经生产解析函数 resolveColumnOriginsForNames 判定，
// 最终由 DataMasker.MaskTable 决定落盘内容。
//
// 回归目标：
//   - F-11：聚合/表达式列（LENGTH(id) AS salary，库内并无 salary 输出列来源），
//     只要存在同名生效规则就必须整列 redact，不得退化为文本保护；
//   - 已知来源列（phone）按规则 redact。
func TestMaskRulesFromManagementDB_RedactUnknownOriginExportColumn(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`create table if not exists t_data_mask_rule (
		id text primary key, conn_id text not null, schema_name text not null default '',
		table_name text not null, column_name text not null, action text not null,
		enabled integer not null default 1, updated_at datetime default current_timestamp)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	seed := func(id, column string) {
		t.Helper()
		if _, err := db.Exec(`insert or replace into t_data_mask_rule
			(id, conn_id, schema_name, table_name, column_name, action, enabled)
			values (?, 'c1', 's1', 'users', ?, 'redact', 1)`, id, column); err != nil {
			t.Fatalf("插入脱敏规则失败：%v", err)
		}
	}
	seed("rule-phone", "phone")
	seed("rule-salary", "salary")
	defer func() {
		if _, err := db.Exec(`delete from t_data_mask_rule where id in ('rule-phone','rule-salary')`); err != nil {
			t.Fatalf("清理规则失败：%v", err)
		}
		if err := GetMaskerProvider().Refresh(); err != nil {
			t.Fatalf("恢复脱敏快照失败：%v", err)
		}
	}()

	if err := GetMaskerProvider().Refresh(); err != nil {
		t.Fatalf("加载脱敏规则失败：%v", err)
	}
	masker := GetMaskerProvider().Get()
	if masker == nil {
		t.Fatal("脱敏器不可用")
	}
	if !masker.HasRules() {
		t.Fatal("管理库规则应已加载")
	}

	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		HasFullConnAccess: true, FullAccessSchemas: map[string]bool{"s1": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sql := "SELECT LENGTH(id) AS salary, phone FROM users"
	headers := []string{"salary", "phone"}
	origins := resolveColumnOriginsForNames(sql, headers, scope)
	if origin, known := origins["salary"]; known {
		t.Fatalf("用例前提变化：salary 现在可解析来源（%+v），需要重写以覆盖未知来源分支", origin)
	}

	rows := [][]string{{"6", "13800138000"}}
	out := masker.MaskTable(headers, rows, origins, []string{"users"})
	if out[0][0] == "6" || out[0][0] == "" {
		t.Fatalf("来源不明且存在规则的列必须整列 redact，实际 %q", out[0][0])
	}
	if out[0][1] == "13800138000" {
		t.Fatalf("已知来源命中规则的列必须整列 redact，实际 %q", out[0][1])
	}
	// 原始行不被就地修改（导出侧多处复用同一 rows 切片）
	if rows[0][1] != "13800138000" {
		t.Fatal("MaskTable 不应就地修改输入行")
	}
}

// TestMaskRulesFromManagementDB_KnownOriginKeepsGranularity 已知来源列仍按列规则处理，
// 与未知来源的整列 redact 不互相串台。
func TestMaskRulesFromManagementDB_KnownOriginKeepsGranularity(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`create table if not exists t_data_mask_rule (
		id text primary key, conn_id text not null, schema_name text not null default '',
		table_name text not null, column_name text not null, action text not null,
		enabled integer not null default 1, updated_at datetime default current_timestamp)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	if _, err := db.Exec(`insert or replace into t_data_mask_rule
		(id, conn_id, schema_name, table_name, column_name, action, enabled)
		values ('rule-nickname', 'c1', 's1', 'users', 'nickname', 'redact', 1)`); err != nil {
		t.Fatalf("插入规则失败：%v", err)
	}
	defer func() {
		if _, err := db.Exec(`delete from t_data_mask_rule where id = 'rule-nickname'`); err != nil {
			t.Fatalf("清理规则失败：%v", err)
		}
		if err := GetMaskerProvider().Refresh(); err != nil {
			t.Fatalf("恢复脱敏快照失败：%v", err)
		}
	}()
	if err := GetMaskerProvider().Refresh(); err != nil {
		t.Fatalf("加载脱敏规则失败：%v", err)
	}
	masker := GetMaskerProvider().Get()
	if masker == nil {
		t.Fatal("脱敏器不可用")
	}

	scope := &PermissionScope{UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true, HasFullConnAccess: true}
	sql := "SELECT nickname, remark FROM users"
	headers := []string{"nickname", "remark"}
	origins := resolveColumnOriginsForNames(sql, headers, scope)
	out := masker.MaskTable(headers, [][]string{{"张三", "普通备注"}}, origins, []string{"users"})
	if out[0][0] == "张三" {
		t.Fatalf("命中规则的已知列应被 redact，实际 %q", out[0][0])
	}
	// 未命中规则列名的普通列（SELECT * 中的 cid 类列）不应被抹除
	keep := masker.MaskTable([]string{"昵称", "备注"}, [][]string{{"小张", "普通备注"}}, map[string]ResourceRef{}, []string{"users"})
	if keep[0][0] != "小张" || keep[0][1] != "普通备注" {
		t.Fatalf("与规则列无关的输出列不应被改写：%v", keep[0])
	}
}

// TestMaskTable_IgnoreRulesOnUnrelatedTables 规则只在无关表上时，来源不明的聚合列不得被错抹。
//
// 这修正了“只要系统里任意连接配过一条规则，所有表的 COUNT/SUM 结果全变 ***”的误伤，
// 同时保留对已配规则表上来源不明列的 fail-closed。
func TestMaskTable_IgnoreRulesOnUnrelatedTables(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "customers", Column: "salary", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		t.Fatalf("构建脱敏器失败：%v", err)
	}
	if !m.ruleCoversColumn([]string{"customers"}, "salary") {
		t.Fatal("列名命中参查表规则时应返回 true")
	}
	if m.ruleCoversColumn([]string{"orders"}, "salary") {
		t.Fatal("规则只在无关表上时应返回 false")
	}
	if m.ruleCoversColumn([]string{"orders"}, "cnt") {
		t.Fatal("与规则列无关时应返回 false")
	}
	if !m.ruleCoversColumn(nil, "salary") {
		t.Fatal("调用方未提供表名时应退回全局列名匹配")
	}
	got := m.MaskTable([]string{"订单数"}, [][]string{{"240"}}, map[string]ResourceRef{}, []string{"orders"})
	if got[0][0] != "240" {
		t.Fatalf("无关表规则不应影响本表聚合列：%q", got[0][0])
	}
	redacted := m.MaskTable([]string{"salary"}, [][]string{{"88000"}}, map[string]ResourceRef{}, []string{"s1.customers"})
	if redacted[0][0] == "88000" {
		t.Fatalf("限定表名命中规则时仍应整列 redact：%q", redacted[0][0])
	}
}
