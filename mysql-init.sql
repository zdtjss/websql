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

insert into t_role (
	id,
	name
) values (
	'1',
	'admin'
);

create table if not exists t_tree (
	id varchar(36) primary key,
	label varchar(64),
	parent varchar(36)
);

create table if not exists t_user (
	id varchar(36) primary key,
	login_name varchar(64),
	name varchar(64),
	pwd varchar(256)
);

-- 管理员id一定是 1
insert into t_user (
	id,
	login_name,
	name,
	pwd
) values (
	'1',
	'admin',
	'管理员',
	'7e2e1f2e1eb71a6f7915a96201237ff0'
);

create table if not exists t_user_role (
	id varchar(36) primary key,
	user_id bigint,
	role_id bigint
);

insert into t_user_role (
	id,
	user_id,
	role_id
) values (
	'1',
	'1',
	'1'
);


create table if not exists t_backup (
	id bigint primary key,
	user varchar(30),
	exec_time timestamp,
	exec_sql text,
	data json
);
