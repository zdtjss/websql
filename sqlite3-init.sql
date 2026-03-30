
CREATE TABLE IF NOT EXISTS t_conn (
	id BIGINT PRIMARY KEY,
	db_type TEXT,
	parent_id BIGINT,
	name TEXT,
	user TEXT,
	pwd TEXT,
	url TEXT
);
		
CREATE TABLE IF NOT EXISTS t_power (
	id TEXT PRIMARY KEY,
	role_id TEXT,
	conn_id TEXT
);

CREATE TABLE IF NOT EXISTS t_role (
	id TEXT PRIMARY KEY,
	name TEXT
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

create table if not exists t_backup (
	id text  primary key,
	user text,
	exec_time timestamp,
	exec_sql text,
	data JSON
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

CREATE TABLE IF NOT EXISTS t_ai_config (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    base_url    TEXT NOT NULL,
    model       TEXT NOT NULL,
    api_key     TEXT,
    updated_at  DATETIME NOT NULL
);


create table if not exists t_system_config (
	id varchar(36) primary key,
	config_key varchar(64) not null unique,
	config_value text,
	config_type varchar(32),
	remark text,
	create_time datetime default current_timestamp,
	update_time datetime default current_timestamp
);

insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000001', 'ai.provider', 'ollama', 'ai', 'AI 服务提供商：ollama, openai 等');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000002', 'ai.baseUrl', 'https://ollama.com', 'ai', 'AI 服务基础 URL');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000003', 'ai.model', 'deepseek-v3.2', 'ai', 'AI 模型名称');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000004', 'ai.apiKey', '41bb5b5119d6429e963994921a238d30.IdjfzCmn3goAL8VNT34XITiq', 'ai', 'AI API 密钥');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000005', 'system.outterUser', 'http://localhost:8081/nway-system/login/getLoginUser', 'system', '外部用户认证接口 URL');
insert or ignore into t_system_config (id, config_key, config_value, config_type, remark) values ('825683877400000006', 'system.allowedIP', '["[::1]","127.0.0.1"]', 'system', '允许的 IP 地址列表（JSON 格式）');
