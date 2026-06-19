package conn

type ConnCfg struct {
	Id         string  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   string  `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       *string `json:"name"`
	User       *string `json:"user"`
	Pwd        *string `json:"pwd"`
	Url        *string `json:"url"`
	DbSchema   *string `json:"dbSchema" db:"db_schema"`
	DbVersion  *string `json:"dbVersion" db:"db_version"`
	Charset    *string `json:"charset" db:"charset"`
}

type ConnCfgBase struct {
	Id       string  `json:"id"`
	Name     *string `json:"name"`
	ParentId string  `json:"parentId" db:"parent_id"`
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
