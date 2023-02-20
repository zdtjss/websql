
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

insert into t_role (id, name) values ('1', 'admin');

CREATE TABLE IF NOT EXISTS t_tree (
	id TEXT PRIMARY KEY,
	label TEXT,
	parent TEXT
);

CREATE TABLE IF NOT EXISTS t_user (
	id TEXT PRIMARY KEY,
	login_name TEXT,
	name TEXT,
	pwd TEXT
);

-- 管理员id一定是 1 
INSERT INTO "t_user" ("id", "login_name", "name", "pwd") VALUES ('1', 'admin', '管理员', '7e2e1f2e1eb71a6f7915a96201237ff0');

CREATE TABLE IF NOT EXISTS t_user_role (
	id BIGINT PRIMARY KEY,
	user_id BIGINT,
	role_id BIGINT
);

INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES ('1', '1', '1');
