package conn

import (
	"websql/internal/pkg/dbtype"
)

// NullStr 可读 NULL 的字符串列（实现见 internal/pkg/dbtype，conn 包保留别名兼容既有用法）。
//
// 用于 t_conn.parent_id 这类历史上可能为 NULL 的列：直接扫进 string 会整行报错
// （`converting NULL to string is unsupported`），一条脏数据就能拖垮
// 连接列表（ListConn2）、连接基础树（ConnBaseTree）与角色权限树。
type NullStr = dbtype.NullStr

type ConnCfg struct {
	Id         string  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   NullStr `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       *string `json:"name"`
	User       *string `json:"user"`
	Pwd        *string `json:"pwd"`
	Url        *string `json:"url"`
	DbSchema   *string `json:"dbSchema" db:"db_schema"`
	DbVersion  *string `json:"dbVersion" db:"db_version"`
	Charset    *string `json:"charset" db:"charset"`
	// ReadOnly 只读连接：开启后写语句会被拒绝，需临时解锁才能写。
	// 不落在 t_conn 上（避免加列迁移），存于 t_system_config（见 readonly.go）。
	ReadOnly bool `json:"readOnly" db:"-"`
	// SSH 跳板机配置：非空表示该连接经 SSH 隧道访问（存于 t_system_config，见 ssh_tunnel.go）。
	SSH *SSHTunnelConfig `json:"ssh,omitempty" db:"-"`
}

type ConnCfgBase struct {
	Id       string  `json:"id"`
	Name     *string `json:"name"`
	ParentId NullStr `json:"parentId" db:"parent_id"`
}

type Tree struct {
	Id       string         `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
	Parent   string         `json:"parent"`
	Children []*Tree        `json:"children"`
}

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

type Column struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

type ColumnsQuery struct {
	ConnId    string `json:"connId"`
	Schema    string `json:"schema"`
	TableName string `json:"tableName"`
}

const (
	TREE_NODE_TYPE_DIR       = "dir"
	TREE_NODE_TYPE_CONN      = "conn"
	TREE_NODE_TYPE_SCHEMA    = "schema"
	TREE_NODE_TYPE_TABLE     = "table"
	TREE_NODE_TYPE_COLUMN    = "column"
	TREE_NODE_TYPE_ALLCOLUMN = "all_column"
)
