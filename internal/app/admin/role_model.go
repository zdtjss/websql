package admin

type Role struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	PowerList   []*PowerDetail `json:"powerList"`
	ViewClassic int            `json:"viewClassic" db:"view_classic"`
	AllowModify int            `json:"allowModify" db:"allow_modify"`
}

type UserRole struct {
	Id       string `json:"id"`
	UserId   string `json:"userId" db:"user_id"`
	RoleId   string `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
}

type RoleSave struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	AddPowers   []*PowerDetail `json:"addPowers"`
	DelPowers   []*PowerDetail `json:"delPowers"`
	ViewClassic int            `json:"viewClassic"`
	AllowModify int            `json:"allowModify"`
}

type Power struct {
	Id     string `json:"id"`
	RoleId string `json:"roleId" db:"role_id"`
	ConnId string `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       string  `json:"id"`
	RoleId   string  `json:"roleId" db:"role_id"`
	ConnId   string  `json:"connId" db:"conn_id"`
	ConnName *string `json:"connName" db:"conn_name"`
}

type PowerDetail struct {
	Id         string  `json:"id"`
	RoleId     string  `json:"roleId" db:"role_id"`
	ConnId     string  `json:"connId" db:"conn_id"`
	ConnName   *string `json:"connName" db:"conn_name"`
	SchemaName *string `json:"schemaName,omitempty" db:"schema_name"`
	TableName  *string `json:"tableName,omitempty" db:"table_name"`
	ColumnName *string `json:"columnName,omitempty" db:"column_name"`
	Level      string  `json:"level" db:"power_level"`
}
