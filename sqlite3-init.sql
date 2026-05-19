
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
	view_classic INTEGER DEFAULT 0
);

insert into t_role (id, name) values ('825683877266722816', 'admin');

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

CREATE TABLE IF NOT EXISTS t_user_role (
	id TEXT PRIMARY KEY,
	user_id TEXT,
	role_id TEXT
);

INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386112', '825683877312860160', '825683877266722816');

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

-- SQL 审计日志表（记录危险 SQL 的执行）
CREATE TABLE IF NOT EXISTS t_sql_audit (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	user_name TEXT,
	conn_id TEXT NOT NULL,
	session_id TEXT,
	sql_text TEXT NOT NULL,
	sql_type TEXT,
	risk_level TEXT,
	status TEXT NOT NULL DEFAULT 'confirmed',
	affected_rows INTEGER DEFAULT 0,
	exec_time DATETIME DEFAULT CURRENT_TIMESTAMP,
	confirm_time DATETIME,
	error_msg TEXT
);

insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000001', 'ai.modelList', '[{"id":"model_default_001","provider":"ollama","baseUrl":"https://ollama.com","model":"deepseek-v3.2","apiKey":"","temperature":0.7,"maxTokens":0,"enableThinking":false,"isDefault":true}]', 'ai', 'AI 模型配置列表');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000002', 'ai.selectedModelId', 'model_default_001', 'ai', '当前选中的模型ID');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000003', 'system.outterUser', 'http://localhost:8081/nway-system/login/getLoginUser', 'system', '外部用户认证接口 URL');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000004', 'system.allowedIP', '["[::1]","127.0.0.1"]', 'system', '允许的 IP 地址列表（JSON 格式）');

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
