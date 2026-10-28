package admin

type Role struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	PowerList   []*PowerDetail `json:"powerList"`
	ViewClassic int            `json:"viewClassic" db:"view_classic"`
	// AllowModify 全局数据修改能力：同时影响经典 SQL 与 AI。
	AllowModify int `json:"allowModify" db:"allow_modify"`
	// AIAllowModify 仅约束 AI 数据写工具，是 AllowModify 的附加约束（不能提升原权限）。
	AIAllowModify int `json:"aiAllowModify" db:"ai_allow_modify"`
	// PermissionDigest 权限摘要，用于并发编辑的乐观锁；不含权限维度语义。
	PermissionDigest string `json:"permissionDigest" db:"-"`
}

type UserRole struct {
	Id       string `json:"id"`
	UserId   string `json:"userId" db:"user_id"`
	RoleId   string `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
	// 角色能力快照（供用户管理页展示，不参与授权判断）
	AllowModify   int `json:"allowModify" db:"allow_modify"`
	AIAllowModify int `json:"aiAllowModify" db:"ai_allow_modify"`
}

type RoleSave struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	AddPowers   []*PowerDetail `json:"addPowers"`
	DelPowers   []*PowerDetail `json:"delPowers"`
	ViewClassic int            `json:"viewClassic"`
	AllowModify int            `json:"allowModify"`
	// AIAllowModify 使用指针区分「旧客户端未提交」（nil → 保持数据库原值）
	// 与显式配置 0。
	AIAllowModify *int `json:"aiAllowModify"`
	// ExpectedDigest 并发控制字段：客户端加载角色时的权限摘要。
	// 灰度期允许缺省；全面切换后管理端必须携带。
	ExpectedDigest string `json:"expectedDigest"`
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
