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
// 不走 fullScriptContent 快速建库路径：这里需要按 0001 → 0005
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

	// 4) 提示词分享表索引就绪
	var indexCount int
	if err := db.Get(&indexCount, `select count(1) from sqlite_master
		where type = 'index' and name in ('idx_prompt_share_prompt', 'idx_prompt_share_to')`); err != nil {
		t.Fatalf("读取分享表索引失败：%v", err)
	}
	if indexCount != 2 {
		t.Fatalf("t_prompt_share 应有 2 个索引，实际 %d", indexCount)
	}

	// 5) 迁移版本记录完整
	var applied int
	if err := db.Get(&applied, "select count(1) from t_schema_migration"); err != nil {
		t.Fatalf("读取迁移记录失败：%v", err)
	}
	if applied < 5 {
		t.Fatalf("应记录至少 5 个迁移版本，实际 %d", applied)
	}
}

// TestRunMigrations_PackagedFreshDB 复现发行包首次启动场景：
// 包内 nway.sqlite3.db 由全量脚本直接创建（schema 已最新但没有迁移版本记录），
// 迁移器会将其识别为存量库并重放增量脚本；重复列等「已存在」错误应视为已应用，
// 不得中断启动，且脚本中的后续数据回填仍需执行。
func TestRunMigrations_PackagedFreshDB(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败：%v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	full, err := os.ReadFile("../../migrations/full/sqlite_full.sql")
	if err != nil {
		t.Fatalf("读取全量脚本失败：%v", err)
	}
	for _, stmt := range splitStatements(string(full)) {
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("执行全量脚本语句失败：%v\n语句: %s", err, truncate(stmt, 200))
		}
	}

	// 模拟旧打包脚本产出的库：有全量 schema，但无任何迁移版本记录
	if _, err := db.Exec("DELETE FROM t_schema_migration"); err != nil {
		t.Fatalf("清空迁移记录失败：%v", err)
	}
	// 构造待回填数据，验证跳过重复列后 UPDATE 仍会执行
	if _, err := db.Exec("UPDATE t_role SET allow_modify = 0, ai_allow_modify = 1 WHERE id = '825683877266722816'"); err != nil {
		t.Fatalf("准备回填数据失败：%v", err)
	}

	if err := RunMigrations(db, "sqlite", os.DirFS("../../migrations/sqlite")); err != nil {
		t.Fatalf("存量库重放迁移应成功，实际失败：%v", err)
	}

	var applied int
	if err := db.Get(&applied, "select count(1) from t_schema_migration"); err != nil {
		t.Fatalf("读取迁移记录失败：%v", err)
	}
	// 0001..0009：baseline + 8 个增量脚本
	if applied != 9 {
		t.Fatalf("应登记 9 个迁移版本，实际 %d", applied)
	}

	var row struct {
		AllowModify   int `db:"allow_modify"`
		AIAllowModify int `db:"ai_allow_modify"`
	}
	if err := db.Get(&row, "select allow_modify, ai_allow_modify from t_role where id = '825683877266722816'"); err != nil {
		t.Fatalf("读取角色数据失败：%v", err)
	}
	if row.AIAllowModify != row.AllowModify {
		t.Fatalf("跳过重复列后仍应执行回填：ai_allow_modify(%d) 应等于 allow_modify(%d)", row.AIAllowModify, row.AllowModify)
	}
}
