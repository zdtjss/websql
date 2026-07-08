
CREATE TABLE IF NOT EXISTS t_conn (
	id BIGINT PRIMARY KEY,
	db_type TEXT,
	parent_id BIGINT,
	name TEXT,
	user TEXT,
	pwd TEXT,
	url TEXT,
	db_schema TEXT,
	db_version TEXT
);
		
CREATE TABLE IF NOT EXISTS t_power (
	id TEXT PRIMARY KEY,
	role_id TEXT,
	conn_id TEXT,
	schema_name TEXT,
	table_name TEXT,
	column_name TEXT,
	power_level TEXT
);

CREATE TABLE IF NOT EXISTS t_role (
	id TEXT PRIMARY KEY,
	name TEXT,
	view_classic INTEGER DEFAULT 0,
	allow_modify INTEGER DEFAULT 1
);

insert into t_role (id, name, view_classic, allow_modify) values ('825683877266722816', 'admin', 1, 1);

CREATE TABLE IF NOT EXISTS t_tree (
	id TEXT PRIMARY KEY,
	label TEXT,
	parent TEXT
);

CREATE TABLE IF NOT EXISTS t_user (
	id TEXT PRIMARY KEY,
	login_name TEXT,
	name TEXT,
	pwd TEXT,
	bio TEXT
);

-- 管理员id一定是 825683877312860160 密码是 1
INSERT INTO "t_user" ("id", "login_name", "name", "pwd","bio") VALUES ('825683877312860160', 'admin', '管理员', '7e2e1f2e1eb71a6f7915a96201237ff0','');

-- local 用户，用于本地/桌面模式自动登录，密码是 1，属于 admin 角色
INSERT INTO "t_user" ("id", "login_name", "name", "pwd","bio") VALUES ('825683877312860161', 'local', 'local', '7e2e1f2e1eb71a6f7915a96201237ff0','');

CREATE TABLE IF NOT EXISTS t_user_role (
	id TEXT PRIMARY KEY,
	user_id TEXT,
	role_id TEXT
);

INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386112', '825683877312860160', '825683877266722816');

-- local 用户绑定 admin 角色
INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386113', '825683877312860161', '825683877266722816');

CREATE INDEX IF NOT EXISTS idx_t_user_role_user_id ON t_user_role(user_id);
CREATE INDEX IF NOT EXISTS idx_t_user_role_role_id ON t_user_role(role_id);
CREATE INDEX IF NOT EXISTS idx_t_power_role_id ON t_power(role_id);
CREATE INDEX IF NOT EXISTS idx_t_conn_parent_id ON t_conn(parent_id);

create table if not exists t_backup (
	id text primary key,
	name text,
	conn_id text,
	schema_name text,
	db_type text,
	size_bytes integer default 0,
	backup_type text default 'full',
	encrypted integer default 0,
	created_at text,
	description text,
	status text default 'completed',
	file_path text
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
	create_time datetime default current_timestamp,
	update_time datetime default current_timestamp
);

CREATE TABLE IF NOT EXISTS t_ai_session (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	title TEXT,
	messages TEXT,
	context TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS t_audit_log (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	user_name TEXT,
	conn_id TEXT NOT NULL,
	conn_name TEXT,
	schema_name TEXT,
	session_id TEXT,
	sql_text TEXT NOT NULL,
	sql_type TEXT,
	risk_level TEXT,
	status TEXT NOT NULL DEFAULT 'success',
	source TEXT NOT NULL DEFAULT 'sqleditor',
	tool_name TEXT,
	affected_rows INTEGER DEFAULT 0,
	exec_time_ms INTEGER DEFAULT 0,
	exec_time DATETIME DEFAULT CURRENT_TIMESTAMP,
	error_msg TEXT,
	client_ip TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_user_id ON t_audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_conn_id ON t_audit_log(conn_id);
CREATE INDEX IF NOT EXISTS idx_audit_exec_time ON t_audit_log(exec_time);
CREATE INDEX IF NOT EXISTS idx_audit_source ON t_audit_log(source);
CREATE INDEX IF NOT EXISTS idx_audit_sql_type ON t_audit_log(sql_type);
CREATE INDEX IF NOT EXISTS idx_audit_risk_level ON t_audit_log(risk_level);
CREATE INDEX IF NOT EXISTS idx_audit_session_id ON t_audit_log(session_id);

insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000001', 'ai.modelList', '[{"id":"model_default_001","provider":"ollama","baseUrl":"https://ollama.com","model":"deepseek-v3.2","apiKey":"","temperature":0.7,"maxTokens":0,"enableThinking":false,"isDefault":true}]', 'ai', 'AI 模型配置列表');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000002', 'ai.selectedModelId', 'model_default_001', 'ai', '当前选中的模型ID');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000003', 'system.outterUser', 'http://localhost:8081/nway-system/login/getLoginUser', 'system', '外部用户认证接口 URL');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000004', 'system.allowedIP', '["[::1]","127.0.0.1"]', 'system', '允许的 IP 地址列表（JSON 格式）');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000005', 'system.defaultHomepage', 'ai', 'system', '默认首页');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000010', 'audit.enabled', 'true', 'audit', '审计日志全局开关');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000011', 'audit.recordQuery', 'false', 'audit', '是否审计只读查询（SELECT/SHOW/DESCRIBE）');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000012', 'audit.recordWrite', 'true', 'audit', '是否审计写操作（INSERT/UPDATE/DELETE）');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000013', 'audit.recordDangerous', 'true', 'audit', '是否审计高风险操作（DROP/TRUNCATE/ALTER）');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000014', 'audit.recordAgentTools', 'true', 'audit', '是否审计 AI Agent 工具调用');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000015', 'audit.recordSQLEditor', 'true', 'audit', '是否审计 SQL 编辑器直接执行');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000016', 'audit.retentionDays', '90', 'audit', '审计日志保留天数');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000017', 'audit.minRiskLevel', 'low', 'audit', '最低记录风险等级（low/medium/high）');

CREATE TABLE IF NOT EXISTS t_prompt (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	content TEXT NOT NULL,
	created_by TEXT,
	role_id TEXT,
	schemas TEXT,
	tables TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS t_prompt_share (
	id TEXT PRIMARY KEY,
	prompt_id TEXT NOT NULL,
	shared_by TEXT NOT NULL,
	shared_to TEXT NOT NULL
);

-- SQL 收藏夹：支持后端同步、分类、标签、关联连接/Schema
CREATE TABLE IF NOT EXISTS t_sql_snippet (
	id TEXT PRIMARY KEY,
	user_id TEXT,
	title TEXT NOT NULL,
	description TEXT,
	sql_content TEXT NOT NULL,
	category TEXT,
	tags TEXT,
	db_type TEXT,
	conn_id TEXT,
	schema_name TEXT,
	created_at DATETIME,
	updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_snippet_user_id ON t_sql_snippet(user_id);
CREATE INDEX IF NOT EXISTS idx_snippet_category ON t_sql_snippet(category);

-- 监控指标持久化表：存储历史趋势数据，支持按连接/指标/时间范围查询
CREATE TABLE IF NOT EXISTS t_monitor_metric (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	conn_id TEXT NOT NULL,
	metric_name TEXT NOT NULL,
	metric_value DOUBLE NOT NULL,
	collected_at DATETIME NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_monitor_metric_conn_time ON t_monitor_metric(conn_id, collected_at);
CREATE INDEX IF NOT EXISTS idx_monitor_metric_name_time ON t_monitor_metric(metric_name, collected_at);
