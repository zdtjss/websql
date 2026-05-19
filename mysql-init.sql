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
	view_classic integer default 0
);

insert into t_role (id, name) values ('825683877266722816', 'admin');

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
INSERT INTO "t_user" ("id", "login_name", "name", "pwd","bio") VALUES ('825683877312860160', 'admin', '管理员', '7e2e1f2e1eb71a6f7915a96201237ff0','');

create table if not exists t_user_role (
	id varchar(36) primary key,
	user_id varchar(36),
	role_id varchar(36)
);

INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('825683877367386112', '825683877312860160', '825683877266722816');

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

-- SQL 审计日志表（记录危险 SQL 的执行）
CREATE TABLE IF NOT EXISTS t_sql_audit (
	id varchar(36) primary key,
	user_id varchar(36) not null,
	user_name varchar(64),
	conn_id varchar(36) not null,
	session_id varchar(128),
	sql_text text not null,
	sql_type varchar(32),
	risk_level varchar(16),
	status varchar(32) not null default 'confirmed',
	affected_rows int default 0,
	exec_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	confirm_time TIMESTAMP NULL,
	error_msg text,
	INDEX idx_user_id (user_id),
	INDEX idx_exec_time (exec_time)
);

insert ignore into t_system_config (id, config_key, config_value, config_type, remark) values 
('825683877400000001', 'ai.modelList', '[{"id":"model_default_001","provider":"ollama","baseUrl":"https://ollama.com","model":"deepseek-v3.2","apiKey":"","temperature":0.7,"maxTokens":0,"enableThinking":false,"isDefault":true}]', 'ai', 'AI 模型配置列表'),
('825683877400000002', 'ai.selectedModelId', 'model_default_001', 'ai', '当前选中的模型ID'),
('825683877400000003', 'system.outterUser', 'http://localhost:8081/nway-system/login/getLoginUser', 'system', '外部用户认证接口 URL'),
('825683877400000006', 'system.allowedIP', '["[::1]","127.0.0.1"]', 'system', '允许的 IP 地址列表（JSON 格式）');

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
