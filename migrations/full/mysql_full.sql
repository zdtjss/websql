-- WebSQL 管理库初始化脚本（MySQL 方言）
-- 用于生产环境首次部署，由系统管理员手动执行。
-- 后续 schema 变更通过增量迁移脚本由管理员手动执行，不自动升级。

create table if not exists t_conn (
	id varchar(36) primary key,
	db_type varchar(64),
	parent_id varchar(36),
	name varchar(64),
	user varchar(64),
	pwd varchar(256),
	url varchar(512),
	db_schema varchar(128),
	db_version varchar(64)
);

create table if not exists t_power (
	id varchar(36) primary key,
	role_id varchar(36),
	conn_id varchar(36),
	schema_name varchar(128),
	table_name varchar(128),
	column_name varchar(128),
	power_level varchar(32)
);

create table if not exists t_role (
	id varchar(36) primary key,
	name varchar(64),
	view_classic integer default 0,
	allow_modify integer default 1
);

insert ignore into t_role (id, name, view_classic, allow_modify) values ('825683877266722816', 'admin', 1, 1);

create table if not exists t_tree (
	id varchar(36) primary key,
	label varchar(64),
	parent varchar(36)
);

create table if not exists t_user (
	id varchar(36) primary key,
	login_name varchar(64),
	name varchar(64),
	pwd varchar(256),
	bio varchar(512)
);

-- 管理员id一定是 825683877312860160 密码是 1
INSERT IGNORE INTO "t_user" ("id", "login_name", "name", "pwd","bio") VALUES ('825683877312860160', 'admin', '管理员', '7e2e1f2e1eb71a6f7915a96201237ff0','');

-- local 用户，用于本地/桌面模式自动登录，密码是 1，属于 admin 角色
INSERT IGNORE INTO "t_user" ("id", "login_name", "name", "pwd","bio") VALUES ('825683877312860161', 'local', 'local', '7e2e1f2e1eb71a6f7915a96201237ff0','');

create table if not exists t_user_role (
	id varchar(36) primary key,
	user_id varchar(36),
	role_id varchar(36)
);

INSERT IGNORE INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386112', '825683877312860160', '825683877266722816');

-- local 用户绑定 admin 角色
INSERT IGNORE INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386113', '825683877312860161', '825683877266722816');

create index if not exists idx_t_user_role_user_id on t_user_role(user_id);
create index if not exists idx_t_user_role_role_id on t_user_role(role_id);
create index if not exists idx_t_power_role_id on t_power(role_id);
create index if not exists idx_t_conn_parent_id on t_conn(parent_id);

create table if not exists t_backup (
	id varchar(36) primary key,
	name varchar(128),
	conn_id varchar(36),
	schema_name varchar(128),
	db_type varchar(32),
	size_bytes bigint default 0,
	backup_type varchar(32) default 'full',
	encrypted tinyint default 0,
	created_at datetime,
	description text,
	status varchar(32) default 'completed',
	file_path varchar(512)
);

create table if not exists t_history (
	id varchar(36) primary key,
	user varchar(30),
	conn_id varchar(36),
	operation_type varchar(36),
	exec_time datetime,
	exec_sql text,
	data json
);

CREATE TABLE IF NOT EXISTS t_system_config (
	id varchar(36) primary key,
	config_key varchar(64) not null unique,
	config_value text,
	config_type varchar(32),
	remark text,
	create_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	update_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS t_ai_session (
	id varchar(64) primary key,
	user_id varchar(64) not null,
	title varchar(256),
	messages MEDIUMTEXT,
	context TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_id (user_id)
);

CREATE TABLE IF NOT EXISTS t_audit_log (
	id varchar(36) primary key,
	user_id varchar(36) not null,
	user_name varchar(64),
	conn_id varchar(36) not null,
	conn_name varchar(128),
	schema_name varchar(128),
	session_id varchar(128),
	sql_text text not null,
	sql_type varchar(32),
	risk_level varchar(16),
	status varchar(32) not null default 'success',
	source varchar(32) not null default 'sqleditor',
	tool_name varchar(64),
	affected_rows int default 0,
	exec_time_ms int default 0,
	exec_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	error_msg text,
	client_ip varchar(64),
	INDEX idx_audit_user_id (user_id),
	INDEX idx_audit_conn_id (conn_id),
	INDEX idx_audit_exec_time (exec_time),
	INDEX idx_audit_source (source),
	INDEX idx_audit_sql_type (sql_type),
	INDEX idx_audit_risk_level (risk_level),
	INDEX idx_audit_session_id (session_id)
);

insert ignore into t_system_config (id, config_key, config_value, config_type, remark) values
('825683877400000001', 'ai.modelList', '[{"id":"model_default_001","provider":"ollama","baseUrl":"https://ollama.com","model":"deepseek-v3.2","apiKey":"","temperature":0.7,"maxTokens":0,"enableThinking":false,"isDefault":true}]', 'ai', 'AI 模型配置列表'),
('825683877400000002', 'ai.selectedModelId', 'model_default_001', 'ai', '当前选中的模型ID'),
('825683877400000003', 'system.outterUser', 'http://localhost:8081/nway-system/login/getLoginUser', 'system', '外部用户认证接口 URL'),
('825683877400000006', 'system.allowedIP', '["[::1]","127.0.0.1"]', 'system', '允许的 IP 地址列表（JSON 格式）'),
('825683877400000007', 'system.defaultHomepage', 'ai', 'system', '默认首页'),
('825683877400000010', 'audit.enabled', 'true', 'audit', '审计日志全局开关'),
('825683877400000011', 'audit.recordQuery', 'false', 'audit', '是否审计只读查询（SELECT/SHOW/DESCRIBE）'),
('825683877400000012', 'audit.recordWrite', 'true', 'audit', '是否审计写操作（INSERT/UPDATE/DELETE）'),
('825683877400000013', 'audit.recordDangerous', 'true', 'audit', '是否审计高风险操作（DROP/TRUNCATE/ALTER）'),
('825683877400000014', 'audit.recordAgentTools', 'true', 'audit', '是否审计 AI Agent 工具调用'),
('825683877400000015', 'audit.recordSQLEditor', 'true', 'audit', '是否审计 SQL 编辑器直接执行'),
('825683877400000016', 'audit.retentionDays', '90', 'audit', '审计日志保留天数'),
('825683877400000017', 'audit.minRiskLevel', 'low', 'audit', '最低记录风险等级（low/medium/high）');

CREATE TABLE IF NOT EXISTS t_prompt (
	id varchar(36) primary key,
	title varchar(256) not null,
	content MEDIUMTEXT not null,
	created_by varchar(36),
	role_id varchar(36),
	schemas TEXT,
	tables TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS t_prompt_share (
	id varchar(36) primary key,
	prompt_id varchar(36) not null,
	shared_by varchar(36) not null,
	shared_to varchar(36) not null,
	INDEX idx_prompt_id (prompt_id),
	INDEX idx_shared_to (shared_to)
);

-- SQL 收藏夹：支持后端同步、分类、标签、关联连接/Schema
CREATE TABLE IF NOT EXISTS t_sql_snippet (
	id varchar(64) primary key,
	user_id varchar(64),
	title varchar(255) not null,
	description text,
	sql_content text not null,
	category varchar(100),
	tags varchar(500),
	db_type varchar(50),
	conn_id varchar(64),
	schema_name varchar(100),
	created_at datetime,
	updated_at datetime,
	INDEX idx_snippet_user_id (user_id),
	INDEX idx_snippet_category (category)
);

-- 监控指标持久化表：存储历史趋势数据，支持按连接/指标/时间范围查询
CREATE TABLE IF NOT EXISTS t_monitor_metric (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	conn_id VARCHAR(64) NOT NULL,
	metric_name VARCHAR(100) NOT NULL,
	metric_value DOUBLE NOT NULL,
	collected_at DATETIME NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	INDEX idx_monitor_metric_conn_time (conn_id, collected_at),
	INDEX idx_monitor_metric_name_time (metric_name, collected_at)
);

-- 用户级 KV 存储：持久化前端 localStorage 数据（主题、侧边栏、SQL 草稿、标签页等），
-- 解决桌面模式动态端口导致 localStorage origin 变化数据丢失的问题。按 user_id 隔离。
CREATE TABLE IF NOT EXISTS t_user_storage (
	id VARCHAR(36) PRIMARY KEY,
	user_id VARCHAR(36) NOT NULL,
	storage_key VARCHAR(128) NOT NULL,
	storage_value TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_user_storage_user_key (user_id, storage_key),
	INDEX idx_user_storage_user_id (user_id)
);

-- 迁移版本记录表：记录已执行的增量迁移脚本版本，供系统管理员确认升级状态
CREATE TABLE IF NOT EXISTS t_schema_migration (
    version      VARCHAR(64) PRIMARY KEY,
    description  TEXT,
    checksum     VARCHAR(64),
    applied_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
