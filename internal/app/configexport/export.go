// Package configexport 提供「用户配置导出」能力：把管理库中的系统设置、提示词、
// 连接、审计配置与收藏导出为可直接回灌管理库的 SQL 文件。
//
// 导出内容为全量配置（管理员操作），连接密码（pwd）与 AI apiKey 按原值（密文/原样）导出，
// 因此更换 AES 密钥后连接密码需要重新配置。生成的文件同时兼容 SQLite 与 MySQL/MariaDB：
// 建表语句使用两者均可接受的类型，INSERT 的冲突忽略语义按当前管理库方言选择。
package configexport

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	admin "websql/internal/app/admin"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// dialect 生成 SQL 时的目标方言。管理库仅可能为 SQLite 或 MySQL/MariaDB。
type dialect int

const (
	dialectSQLite dialect = iota
	dialectMySQL
)

func (d dialect) name() string {
	if d == dialectMySQL {
		return "mysql"
	}
	return "sqlite"
}

// detectDialect 依据管理库驱动名判定导出目标方言。
func detectDialect(db *sqlx.DB) dialect {
	driver := strings.ToLower(db.DriverName())
	if strings.Contains(driver, "mysql") || strings.Contains(driver, "maria") {
		return dialectMySQL
	}
	return dialectSQLite
}

// exportBlock 描述一个导出块：分组标题、目标表、列顺序、取数 SQL 与是否输出建表语句。
type exportBlock struct {
	section string
	table   string
	columns []string
	query   string
	emitDDL bool
}

// 导出顺序即文件中的展示顺序；t_system_config 分「系统设置」与「审计配置」两组。
var exportBlocks = []exportBlock{
	{
		section: "系统设置",
		table:   "t_system_config",
		columns: []string{"id", "config_key", "config_value", "config_type", "remark", "create_time", "update_time"},
		query:   "select `id`,`config_key`,`config_value`,`config_type`,`remark`,`create_time`,`update_time` from t_system_config where config_type is null or config_type <> 'audit' order by config_type, config_key",
		emitDDL: true,
	},
	{
		section: "审计配置",
		table:   "t_system_config",
		columns: []string{"id", "config_key", "config_value", "config_type", "remark", "create_time", "update_time"},
		query:   "select `id`,`config_key`,`config_value`,`config_type`,`remark`,`create_time`,`update_time` from t_system_config where config_type = 'audit' order by config_key",
	},
	{
		section: "提示词",
		table:   "t_prompt",
		columns: []string{"id", "title", "content", "created_by", "role_id", "schemas", "tables", "created_at", "updated_at"},
		query:   "select `id`,`title`,`content`,`created_by`,`role_id`,`schemas`,`tables`,`created_at`,`updated_at` from t_prompt order by id",
		emitDDL: true,
	},
	{
		section: "提示词",
		table:   "t_prompt_share",
		columns: []string{"id", "prompt_id", "shared_by", "shared_to"},
		query:   "select `id`,`prompt_id`,`shared_by`,`shared_to` from t_prompt_share order by id",
		emitDDL: true,
	},
	{
		section: "连接",
		table:   "t_conn",
		columns: []string{"id", "db_type", "parent_id", "name", "user", "pwd", "url", "db_schema", "db_version"},
		query:   "select `id`,`db_type`,`parent_id`,`name`,`user`,`pwd`,`url`,`db_schema`,`db_version` from t_conn order by id",
		emitDDL: true,
	},
	{
		section: "收藏",
		table:   "t_sql_snippet",
		columns: []string{"id", "user_id", "title", "description", "sql_content", "category", "tags", "db_type", "conn_id", "schema_name", "created_at", "updated_at"},
		query:   "select `id`,`user_id`,`title`,`description`,`sql_content`,`category`,`tags`,`db_type`,`conn_id`,`schema_name`,`created_at`,`updated_at` from t_sql_snippet order by id",
		emitDDL: true,
	},
}

// ddlByTable 为可直接在 SQLite 与 MySQL 上执行的建表语句（类型均为两者兼容）。
// 说明：主键列必须使用带长度的 VARCHAR（MySQL 不允许 TEXT 作为主键）；不使用方言特有的
// AUTO_INCREMENT/AUTOINCREMENT 与内联 INDEX，二级索引由目标库既有 schema 提供。
var ddlByTable = map[string]string{
	"t_system_config": "CREATE TABLE IF NOT EXISTS `t_system_config` (\n" +
		"  `id` VARCHAR(64) PRIMARY KEY,\n" +
		"  `config_key` VARCHAR(64) NOT NULL,\n" +
		"  `config_value` TEXT,\n" +
		"  `config_type` VARCHAR(32),\n" +
		"  `remark` TEXT,\n" +
		"  `create_time` DATETIME,\n" +
		"  `update_time` DATETIME,\n" +
		"  UNIQUE (`config_key`)\n" +
		");",
	"t_prompt": "CREATE TABLE IF NOT EXISTS `t_prompt` (\n" +
		"  `id` VARCHAR(64) PRIMARY KEY,\n" +
		"  `title` VARCHAR(256) NOT NULL,\n" +
		"  `content` TEXT NOT NULL,\n" +
		"  `created_by` VARCHAR(64),\n" +
		"  `role_id` VARCHAR(64),\n" +
		"  `schemas` TEXT,\n" +
		"  `tables` TEXT,\n" +
		"  `created_at` DATETIME,\n" +
		"  `updated_at` DATETIME\n" +
		");",
	"t_prompt_share": "CREATE TABLE IF NOT EXISTS `t_prompt_share` (\n" +
		"  `id` VARCHAR(64) PRIMARY KEY,\n" +
		"  `prompt_id` VARCHAR(64) NOT NULL,\n" +
		"  `shared_by` VARCHAR(64) NOT NULL,\n" +
		"  `shared_to` VARCHAR(64) NOT NULL\n" +
		");",
	"t_conn": "CREATE TABLE IF NOT EXISTS `t_conn` (\n" +
		"  `id` VARCHAR(64) PRIMARY KEY,\n" +
		"  `db_type` VARCHAR(64),\n" +
		"  `parent_id` VARCHAR(64),\n" +
		"  `name` VARCHAR(256),\n" +
		"  `user` VARCHAR(128),\n" +
		"  `pwd` VARCHAR(512),\n" +
		"  `url` VARCHAR(1024),\n" +
		"  `db_schema` VARCHAR(256),\n" +
		"  `db_version` VARCHAR(64)\n" +
		");",
	"t_sql_snippet": "CREATE TABLE IF NOT EXISTS `t_sql_snippet` (\n" +
		"  `id` VARCHAR(64) PRIMARY KEY,\n" +
		"  `user_id` VARCHAR(64),\n" +
		"  `title` VARCHAR(256) NOT NULL,\n" +
		"  `description` TEXT,\n" +
		"  `sql_content` TEXT NOT NULL,\n" +
		"  `category` VARCHAR(128),\n" +
		"  `tags` VARCHAR(512),\n" +
		"  `db_type` VARCHAR(64),\n" +
		"  `conn_id` VARCHAR(64),\n" +
		"  `schema_name` VARCHAR(128),\n" +
		"  `created_at` DATETIME,\n" +
		"  `updated_at` DATETIME\n" +
		");",
}

// ExportUserConfig GET /api/system/config/export
// 管理员导出全量用户配置为 SQL 文本，前端取 content 后触发浏览器下载。
func ExportUserConfig(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	sqlText, err := buildExportSQL(getDB())
	if err != nil {
		log.Printf("[ConfigExport] 生成导出 SQL 失败: %v", err)
		response.WriteErr(c, 200, 500, "导出失败")
		return
	}
	filename := "websql-user-config-" + time.Now().Format("20060102-150405") + ".sql"
	response.WriteOK(c, gin.H{"filename": filename, "content": sqlText})
}

// buildExportSQL 组装完整的导出 SQL 文本。
func buildExportSQL(db *sqlx.DB) (string, error) {
	d := detectDialect(db)

	var b strings.Builder
	b.WriteString("-- WebSQL 用户配置导出\n")
	b.WriteString("-- 导出时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("-- 目标方言: " + d.name() + "\n")
	b.WriteString("-- 包含内容: 系统设置、提示词、连接、审计配置、收藏\n")
	b.WriteString("-- 安全说明: 连接密码(pwd)与 AI apiKey 按原值导出；更换 AES 密钥后连接密码需重新配置。\n")
	b.WriteString("-- 执行说明: 采用 IGNORE 语义保证幂等，重复执行不会覆盖既有配置。\n")

	emittedDDL := map[string]bool{}
	lastSection := ""
	for _, blk := range exportBlocks {
		if blk.section != lastSection {
			b.WriteString("\n-- ============================================================\n")
			b.WriteString("-- " + blk.section + "\n")
			b.WriteString("-- ============================================================\n")
			lastSection = blk.section
		}
		if blk.emitDDL && !emittedDDL[blk.table] {
			b.WriteString(ddlByTable[blk.table] + "\n")
			emittedDDL[blk.table] = true
		}

		rows, err := queryRows(db, blk.query)
		if err != nil {
			return "", fmt.Errorf("查询 %s 失败: %w", blk.table, err)
		}
		b.WriteString(fmt.Sprintf("-- %s: %d 条\n", blk.table, len(rows)))
		for _, row := range rows {
			b.WriteString(renderInsert(blk.table, blk.columns, row, d) + "\n")
		}
	}
	return b.String(), nil
}

// queryRows 执行查询并把每行扫描为「小写列名 -> 值」的映射，避免为每张表定义结构体。
func queryRows(db *sqlx.DB, query string) ([]map[string]any, error) {
	rows, err := db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]map[string]any, 0)
	for rows.Next() {
		raw := map[string]any{}
		if err := rows.MapScan(raw); err != nil {
			return nil, err
		}
		// 归一化列名为小写，兼容不同驱动返回的列名大小写差异。
		norm := make(map[string]any, len(raw))
		for k, v := range raw {
			norm[strings.ToLower(k)] = v
		}
		result = append(result, norm)
	}
	return result, rows.Err()
}

// renderInsert 生成单条 INSERT 语句（含方言对应的冲突忽略语义）。
func renderInsert(table string, columns []string, row map[string]any, d dialect) string {
	keyword := "INSERT OR IGNORE INTO"
	if d == dialectMySQL {
		keyword = "INSERT IGNORE INTO"
	}

	var b strings.Builder
	b.WriteString(keyword + " `" + table + "` (")
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("`" + col + "`")
	}
	b.WriteString(") VALUES (")
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(renderValue(row[col], d))
	}
	b.WriteString(");")
	return b.String()
}

// renderValue 将驱动返回的值渲染为 SQL 字面量。
func renderValue(v any, d dialect) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return quoteString(string(t), d)
	case string:
		return quoteString(t, d)
	case time.Time:
		return quoteString(t.Format("2006-01-02 15:04:05"), d)
	case bool:
		if t {
			return "1"
		}
		return "0"
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	default:
		return quoteString(fmt.Sprintf("%v", t), d)
	}
}

// quoteString 按方言转义字符串字面量。
// SQLite 仅需把单引号写成两个单引号；MySQL 还需转义反斜杠（默认开启反斜杠转义）。
func quoteString(s string, d dialect) string {
	if d == dialectMySQL {
		s = strings.ReplaceAll(s, "\\", "\\\\")
	}
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}