package migration
package migration

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// TestRunMigrations_AISecuritySchema 验证增量迁移脚本可在全新 SQLite 库上依次执行，
// 并产出 AI 数据权限改造所需的列与表。
//
// 不走 fullScriptContent 快速建库路径：这里需要按 0001 → 0002 → 0003 → 0004
// 顺序执行增量脚本，确保 ALTER/CREATE 语句本身正确。
func TestRunMigrations_AISecuritySchema(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败：%v", err)
	}
	defer db.Close()
	// modernc sqlite 的内存库按连接隔离，限制单连接保证迁移与断言看到同一库
	db.SetMaxOpenConns(1)

	if err := RunMigrations(db, "sqlite", os.DirFS("../../migrations/sqlite")); err != nil {
		t.Fatalf("执行迁移失败：%v", err)
	}

	// 1) t_role.ai_allow_modify 存在，且存量角色按 allow_modify 回填
	var aiAllow int
	if err := db.Get(&aiAllow, "select ai_allow_modify from t_role where id = '825683877266722816'"); err != nil {
		t.Fatalf("读取 ai_allow_modify 失败：%v", err)
	}
	var allowModify int
	if err := db.Get(&allowModify, "select allow_modify from t_role where id = '825683877266722816'"); err != nil {
		t.Fatalf("读取 allow_modify 失败：%v", err)
	}
	if aiAllow != allowModify {
		t.Fatalf("迁移后 ai_allow_modify(%d) 应等于 allow_modify(%d)", aiAllow, allowModify)
	}

	// 2) 权限修订号单行表已就绪
	var revision uint64
	if err := db.Get(&revision, "select revision from t_permission_revision where id = 1"); err != nil {
		t.Fatalf("读取 t_permission_revision 失败：%v", err)
	}
	if revision != 0 {
		t.Fatalf("初始修订号应为 0，实际 %d", revision)
	}

	// 3) 脱敏规则表与唯一索引就绪
	if _, err := db.Exec(`
		insert into t_data_mask_rule (id, conn_id, schema_name, table_name, column_name, action, enabled)
		values ('r1', 'c1', 's1', 'users', 'phone', 'partial', 1)`); err != nil {
		t.Fatalf("插入脱敏规则失败：%v", err)
	}
	if _, err := db.Exec(`
		insert into t_data_mask_rule (id, conn_id, schema_name, table_name, column_name, action, enabled)
		values ('r2', 'c1', 's1', 'users', 'phone', 'redact', 1)`); err == nil {
		t.Fatal("同连接同表同列应触发唯一索引冲突")
	}

	// 4) 迁移版本记录完整
	var applied int
	if err := db.Get(&applied, "select count(1) from t_schema_migration"); err != nil {
		t.Fatalf("读取迁移记录失败：%v", err)
	}
	if applied < 4 {
		t.Fatalf("应记录至少 4 个迁移版本，实际 %d", applied)
	}
}
