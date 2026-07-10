// Package migration 提供管理库（SQLite）的版本化迁移能力。
//
// 启动时调用 RunMigrations，按版本号顺序执行未执行的迁移脚本，每个脚本在独立事务中运行，
// 失败则回滚并返回错误。存量库（已有业务表但无 t_schema_migration 记录）首次接入时，
// 自动标记基线版本为已执行，仅运行后续增量迁移，实现零接入升级。
// 全新库（无业务表且无迁移记录）时，若提供了 fullScriptContent，则直接执行全量脚本
// 并标记所有增量版本为已执行，避免逐个执行增量脚本。
package migration

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	"websql/internal/pkg/strutil"

	"github.com/jmoiron/sqlx"
)

const baselineVersion = "0001_baseline"

// RunMigrations 执行未执行的迁移脚本。
// fsys 提供 .sql 文件（Web 版用 os.DirFS，桌面版用 embed.FS）。
// fullScriptContent 为可选的全量初始化脚本内容，用于全新库快速建库。
// 仅对 SQLite 管理库执行自动迁移；MySQL/MariaDB 管理库由系统管理员手动升级，此处跳过。
func RunMigrations(db *sqlx.DB, driverName string, fsys fs.FS, fullScriptContent ...string) error {
	var fullContent string
	if len(fullScriptContent) > 0 {
		fullContent = fullScriptContent[0]
	}
	m := &Migrator{db: db, driverName: driverName, fsys: fsys, fullScriptContent: fullContent}
	return m.run()
}

// GetLatestAppliedVersion 返回已执行的最高迁移版本号，无记录时返回空字符串。
func GetLatestAppliedVersion(db *sqlx.DB) (string, error) {
	var v string
	err := db.QueryRow("SELECT version FROM t_schema_migration ORDER BY version DESC LIMIT 1").Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "no such table") {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

const appVersionKey = "system.appVersion"

// GetPreviousAppVersion 从 t_system_config 读取上次运行的程序版本号。
// 无记录或表不存在时返回空字符串（表示首次运行或全新安装）。
func GetPreviousAppVersion(db *sqlx.DB) (string, error) {
	var v string
	err := db.QueryRow(
		"SELECT config_value FROM t_system_config WHERE config_key = ?", appVersionKey,
	).Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "no such table") {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// RecordAppVersion 将当前程序版本号写入 t_system_config，供下次启动对比。
// 使用 UPSERT 语义：已存在则更新，不存在则插入。
func RecordAppVersion(db *sqlx.DB, ver string) error {
	if ver == "" || ver == "dev" {
		return nil // dev 版本不持久化，避免污染生产库
	}
	_, err := db.Exec(`
		INSERT INTO t_system_config (id, config_key, config_value, config_type, remark)
		VALUES (?, ?, ?, 'system', '上次启动的程序版本')
		ON CONFLICT(config_key) DO UPDATE SET config_value = ?, update_time = CURRENT_TIMESTAMP`,
		fmt.Sprintf("app_version_%s", ver), appVersionKey, ver, ver,
	)
	return err
}

type Migrator struct {
	db                *sqlx.DB
	driverName        string
	fsys              fs.FS
	fullScriptContent string
}

func (m *Migrator) run() error {
	if m.db == nil {
		return fmt.Errorf("migration: db 为空")
	}
	switch m.driverName {
	case "sqlite", "sqlite3":
		// SQLite 管理库：自动迁移
	case "mysql", "mariadb":
		log.Printf("[Migration] MySQL/MariaDB 管理库不执行自动迁移，请由系统管理员手动执行增量脚本")
		return nil
	default:
		return fmt.Errorf("migration: 不支持的数据库驱动 %q", m.driverName)
	}

	if err := m.ensureMigrationTable(); err != nil {
		return fmt.Errorf("创建迁移记录表失败: %w", err)
	}

	scripts, err := m.listScripts()
	if err != nil {
		return fmt.Errorf("读取迁移脚本列表失败: %w", err)
	}
	if len(scripts) == 0 {
		return nil
	}

	applied, err := m.loadAppliedVersions()
	if err != nil {
		return fmt.Errorf("加载已执行迁移版本失败: %w", err)
	}

	// 无任何迁移记录时，判断是存量库还是全新库
	if len(applied) == 0 {
		hasBusinessTable, err := m.hasExistingBusinessTable()
		if err != nil {
			return fmt.Errorf("检测存量业务表失败: %w", err)
		}
		if hasBusinessTable {
			// 存量库首次接入：标记基线已执行，跳过 baseline
			log.Printf("[Migration] 检测到存量库（已有业务表），标记基线 %s 为已执行，仅运行后续增量迁移", baselineVersion)
			if err := m.markApplied(baselineVersion, "baseline (existing database)"); err != nil {
				return fmt.Errorf("标记基线版本失败: %w", err)
			}
			applied[baselineVersion] = true
		} else if m.fullScriptContent != "" {
			// 全新库 + 提供了全量脚本：快速建库并标记所有增量版本为已执行
			if err := m.applyFullScript(scripts); err != nil {
				return fmt.Errorf("执行全量初始化脚本失败: %w", err)
			}
			return nil
		}
		// 无全量脚本时，继续走逐个增量执行
	}

	for _, script := range scripts {
		if applied[script.version] {
			continue
		}
		if err := m.applyScript(script); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", script.version, err)
		}
		log.Printf("[Migration] 已执行迁移: %s", script.version)
	}

	return nil
}

type scriptFile struct {
	version string
	name    string
	content string
}

func (m *Migrator) listScripts() ([]scriptFile, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, err
	}
	var scripts []scriptFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(m.fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", e.Name(), err)
		}
		version := strings.TrimSuffix(e.Name(), ".sql")
		scripts = append(scripts, scriptFile{
			version: version,
			name:    e.Name(),
			content: string(data),
		})
	}
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].version < scripts[j].version })
	return scripts, nil
}

func (m *Migrator) ensureMigrationTable() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS t_schema_migration (
    version      TEXT PRIMARY KEY,
    description  TEXT,
    checksum     TEXT,
    applied_at   DATETIME DEFAULT CURRENT_TIMESTAMP
)`)
	return err
}

func (m *Migrator) loadAppliedVersions() (map[string]bool, error) {
	rows, err := m.db.Query("SELECT version FROM t_schema_migration")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// hasExistingBusinessTable 检测库中是否已存在业务表（t_conn/t_user），
// 用于判断是否为存量库首次接入迁移系统。
func (m *Migrator) hasExistingBusinessTable() (bool, error) {
	var count int
	err := m.db.QueryRow(
		"SELECT count(1) FROM sqlite_master WHERE type='table' AND name IN ('t_conn','t_user')",
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// applyFullScript 使用全量脚本快速建库，然后将所有增量迁移版本标记为已执行。
func (m *Migrator) applyFullScript(scripts []scriptFile) error {
	tx, err := beginTxWithRetry(m.db, 3, 50*time.Millisecond)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	for _, stmt := range splitStatements(m.fullScriptContent) {
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("执行全量脚本语句失败: %w\n语句: %s", err, truncate(stmt, 200))
		}
	}

	// 标记所有已知增量版本为已执行
	for _, s := range scripts {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO t_schema_migration (version, description, checksum) VALUES (?, ?, ?)",
			s.version, "applied via full init", "",
		); err != nil {
			return fmt.Errorf("标记迁移版本 %s 失败: %w", s.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交全量初始化事务失败: %w", err)
	}
	committed = true
	log.Printf("[Migration] 全量初始化完成，已标记 %d 个增量版本为已执行", len(scripts))
	return nil
}

func (m *Migrator) applyScript(script scriptFile) error {
	tx, err := beginTxWithRetry(m.db, 3, 50*time.Millisecond)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	for _, stmt := range splitStatements(script.content) {
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("执行语句失败: %w\n语句: %s", err, truncate(stmt, 200))
		}
	}

	checksum := md5Hex(script.content)
	if _, err := tx.Exec(
		"INSERT INTO t_schema_migration (version, description, checksum) VALUES (?, ?, ?)",
		script.version, script.version, checksum,
	); err != nil {
		return fmt.Errorf("记录迁移版本失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	committed = true
	return nil
}

func (m *Migrator) markApplied(version, description string) error {
	_, err := m.db.Exec(
		"INSERT INTO t_schema_migration (version, description, checksum) VALUES (?, ?, ?)",
		version, description, "",
	)
	return err
}

// splitStatements 按 ';' 分割脚本并过滤注释/空行，与现有 InitDBFromContent 行为一致。
func splitStatements(content string) []string {
	parts := strings.Split(content, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strutil.ExtractSql(p))
	}
	return out
}

func beginTxWithRetry(db *sqlx.DB, maxRetries int, baseDelay time.Duration) (*sqlx.Tx, error) {
	var tx *sqlx.Tx
	var err error
	for i := 0; i <= maxRetries; i++ {
		tx, err = db.Beginx()
		if err == nil {
			return tx, nil
		}
		if !isBusyErr(err) {
			return nil, err
		}
		if i < maxRetries {
			delay := baseDelay * time.Duration(1<<uint(i))
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			time.Sleep(delay)
		}
	}
	return nil, err
}

func isBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "is locked")
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
