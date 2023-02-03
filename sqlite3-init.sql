
-- 导出  表 nway.sqlite3.t_conn 结构
CREATE TABLE IF NOT EXISTS t_conn (
	id BIGINT PRIMARY KEY,
	db_type VARCHAR(64) NULL,
	parent_id BIGINT,
	name VARCHAR(64) NULL,
	user VARCHAR(64) NULL,
	pwd VARCHAR(128) NULL,
	url VARCHAR(512) NULL
);
		
-- 导出  表 nway.sqlite3.t_power 结构
CREATE TABLE IF NOT EXISTS t_power (
	id BIGINT PRIMARY KEY,
	role_id BIGINT,
	conn_id BIGINT
);

-- 导出  表 nway.sqlite3.t_role 结构
CREATE TABLE IF NOT EXISTS t_role (
	id BIGINT PRIMARY KEY,
	name VARCHAR(64)
);

insert into t_role (id, name) values (1, 'admin');

-- 导出  表 nway.sqlite3.t_tree 结构
CREATE TABLE IF NOT EXISTS t_tree (
	id BIGINT PRIMARY KEY,
	label TEXT,
	parent BIGINT
);

-- 导出  表 nway.sqlite3.t_user 结构
CREATE TABLE IF NOT EXISTS t_user (
	id BIGINT PRIMARY KEY,
	login_name VARCHAR(64),
	name VARCHAR(64),
	pwd VARCHAR(64)
);

-- 正在导出表  nway.sqlite3.t_user 的数据：1 rows
/*!40000 ALTER TABLE "t_user" DISABLE KEYS */;
INSERT INTO "t_user" ("id", "login_name", "name", "pwd") VALUES
	(1, 'admin', '管理员', 'admin');
/*!40000 ALTER TABLE "t_user" ENABLE KEYS */;

-- 导出  表 nway.sqlite3.t_user_role 结构
CREATE TABLE IF NOT EXISTS t_user_role (
	id BIGINT PRIMARY KEY,
	user_id BIGINT,
	role_id BIGINT
);

-- 正在导出表  nway.sqlite3.t_user_role 的数据：1 rows
/*!40000 ALTER TABLE "t_user_role" DISABLE KEYS */;
INSERT INTO "t_user_role" ("id", "user_id", "role_id") VALUES
	(1, 1, 1);
/*!40000 ALTER TABLE "t_user_role" ENABLE KEYS */;
