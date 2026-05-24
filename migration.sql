-- ==========================================
-- SQLite 数据库结构迁移脚本
-- 生成时间: 2026-05-14 14:31:12
-- 源数据库: D:\workspace\opensource\websql\nway.sqlite3.db
-- 目标数据库: D:\workspace\WebSQL2\nway.sqlite3.db
-- ==========================================

BEGIN TRANSACTION;

-- ==========================================
-- 表结构变更
-- ==========================================

-- 表: t_ai_session
-- 新增列: 1, 修改列: 5, 删除列: 0
ALTER TABLE t_ai_session ADD COLUMN context TEXT;

-- 注意: 以下操作需要重建表 t_ai_session
-- SQLite不支持直接删除或修改列
-- 步骤:
-- 1. 创建临时表
-- 2. 复制数据
-- 3. 删除原表
-- 4. 重命名临时表

-- 重建表 t_ai_session
CREATE TABLE t_ai_session_backup AS SELECT * FROM t_ai_session;
DROP TABLE t_ai_session;

-- 新的表结构
CREATE TABLE t_ai_session (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	title TEXT,
	messages TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
, context TEXT);

-- 从备份表恢复数据
INSERT INTO t_ai_session (messages, id, title, user_id, updated_at, created_at)
SELECT messages, id, title, user_id, updated_at, created_at FROM t_ai_session_backup;
DROP TABLE t_ai_session_backup;


-- 表: t_conn
-- 新增列: 0, 修改列: 2, 删除列: 0

-- 注意: 以下操作需要重建表 t_conn
-- SQLite不支持直接删除或修改列
-- 步骤:
-- 1. 创建临时表
-- 2. 复制数据
-- 3. 删除原表
-- 4. 重命名临时表

-- 重建表 t_conn
CREATE TABLE t_conn_backup AS SELECT * FROM t_conn;
DROP TABLE t_conn;

-- 新的表结构
CREATE TABLE t_conn (
	id TEXT PRIMARY KEY,
	db_type TEXT,
	parent_id TEXT,
	name TEXT,
	user TEXT,
	pwd TEXT,
	url TEXT
, db_schema text AFTER db_type, db_version text);

-- 从备份表恢复数据
INSERT INTO t_conn (db_version, id, pwd, parent_id, db_schema, user, url, db_type, name)
SELECT db_version, id, pwd, parent_id, db_schema, user, url, db_type, name FROM t_conn_backup;
DROP TABLE t_conn_backup;


-- 表: t_power
-- 新增列: 1, 修改列: 4, 删除列: 0
ALTER TABLE t_power ADD COLUMN tree_visible INTEGER NULL;

-- 注意: 以下操作需要重建表 t_power
-- SQLite不支持直接删除或修改列
-- 步骤:
-- 1. 创建临时表
-- 2. 复制数据
-- 3. 删除原表
-- 4. 重命名临时表

-- 重建表 t_power
CREATE TABLE t_power_backup AS SELECT * FROM t_power;
DROP TABLE t_power;

-- 新的表结构
CREATE TABLE t_power (
	id TEXT PRIMARY KEY,
	role_id TEXT,
	conn_id TEXT
, schema_name TEXT, table_name TEXT, column_name TEXT, power_level TEXT, "tree_visible" INTEGER NULL);

-- 从备份表恢复数据
INSERT INTO t_power (table_name, conn_id, id, power_level, role_id, schema_name, column_name)
SELECT table_name, conn_id, id, power_level, role_id, schema_name, column_name FROM t_power_backup;
DROP TABLE t_power_backup;


-- 表: t_role
-- 新增列: 1, 修改列: 0, 删除列: 0
ALTER TABLE t_role ADD COLUMN view_classic INTEGER DEFAULT 0;

COMMIT;

-- 迁移脚本生成完成