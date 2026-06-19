package admin

type User struct {
	Id        string    `json:"id"`
	RoleId    []*string `json:"roleId"`
	RoleName  []*string `json:"roleName"`
	LoginName string    `json:"loginName" db:"login_name"`
	Name      string    `json:"name"`
	Pwd       string    `json:"pwd"`
	Bio       string    `json:"bio"`
}

type UserPower struct {
	UserId string
	Power  []string
}

type PowerCheckParam struct {
	ConnId     string
	SchemaName string
	TableName  string
	ColumnName string
}

type SharedUser struct {
	Id        string `json:"id" db:"id"`
	Name      string `json:"name" db:"name"`
	LoginName string `json:"loginName" db:"login_name"`
}
