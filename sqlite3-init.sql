
CREATE TABLE IF NOT EXISTS t_conn (
	id BIGINT PRIMARY KEY,
	db_type VARCHAR(64) NULL,
	parent_id BIGINT,
	name VARCHAR(64) NULL,
	user VARCHAR(64) NULL,
	pwd VARCHAR(128) NULL,
	url VARCHAR(512) NULL
);
		
CREATE TABLE IF NOT EXISTS t_power (
	id BIGINT PRIMARY KEY,
	role_id BIGINT,
	conn_id BIGINT
);

CREATE TABLE IF NOT EXISTS t_role (
	id BIGINT PRIMARY KEY,
	name VARCHAR(64)
);

insert into t_role (id, name) values (1, 'admin');

CREATE TABLE IF NOT EXISTS t_tree (
	id BIGINT PRIMARY KEY,
	label TEXT,
	parent BIGINT
);

CREATE TABLE IF NOT EXISTS t_user (
	id BIGINT PRIMARY KEY,
	login_name VARCHAR(64),
	name VARCHAR(64),
	pwd VARCHAR(64)
);

-- 管理员id一定是 1 
INSERT INTO "t_user" ("id", "login_name", "name", "pwd") VALUES (1, 'admin', '管理员', '7e2e1f2e1eb71a6f7915a96201237ff0');

CREATE TABLE IF NOT EXISTS t_user_role (
	id BIGINT PRIMARY KEY,
	user_id BIGINT,
	role_id BIGINT
);

INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES (1, 1, 1);
