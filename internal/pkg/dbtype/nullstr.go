// Package dbtype 提供跨层复用的数据库列扫描类型。
//
// 存在的原因：`t_conn.parent_id` 允许为 NULL，而 `database/sql` 对 `NULL → string`
// 会返回 `converting NULL to string is unsupported`，单行脏数据即可拖垮整条查询
// （连接列表、连接基础树、角色权限目录同时失效）。把可空文本列统一用 NullStr 读取。
package dbtype

import (
	"database/sql"
	"fmt"
)

// NullStr 可空文本列：NULL 归一为空串，其余按 string / []byte / sql.NullString 读取。
type NullStr string

// Scan 实现 sql.Scanner。
func (n *NullStr) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*n = ""
	case string:
		*n = NullStr(v)
	case []byte:
		*n = NullStr(string(v))
	case sql.NullString:
		if v.Valid {
			*n = NullStr(v.String)
		} else {
			*n = ""
		}
	default:
		return fmt.Errorf("NullStr: 不支持的数据类型 %T", src)
	}
	return nil
}

// Value 实现 driver.Valuer：空串按空串写回（历史数据里 parent_id 的空串与 NULL 等价）。
func (n NullStr) Value() (any, error) { return string(n), nil }

// String 供赋值给普通 string 字段与驱动参数使用。
func (n NullStr) String() string { return string(n) }
