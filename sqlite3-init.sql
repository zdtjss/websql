
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
