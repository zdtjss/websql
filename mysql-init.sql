create table if not exists t_conn (
	id varchar(36) primary key,
	db_type varchar(64),
	parent_id varchar(36),
	name varchar(64),
	user varchar(64),
	pwd varchar(256),
	url varchar(512)
);

create table if not exists t_power (
	id varchar(36) primary key,
	role_id varchar(36),
	conn_id varchar(36)
);

create table if not exists t_role (
	id varchar(36) primary key,
	name varchar(64)
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
	user varchar(30),
	conn_id varchar(36),
	exec_time datetime,
	exec_sql text,
	data json
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
