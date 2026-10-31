package admin

import (
	"testing"

	"websql/internal/pkg/dbtype"
)

// TestConnCfgLocalParentIdNullable 锁定 parent_id 的扫描类型。
//
// 审核 M2：connCfgLocal 走的是 `select c.*` / `select * from t_conn`，
// 若 ParentId 退回 string，一行 parent_id IS NULL 就会让 getConnTree 整体失败
// （converting NULL to string is unsupported），远程模式下该连接直接不可用。
func TestConnCfgLocalParentIdNullable(t *testing.T) {
	var row connCfgLocal
	// 编译期断言：字段必须可容纳 NULL
	var field dbtype.NullStr = row.ParentId

	if err := field.Scan(nil); err != nil {
		t.Fatalf("NULL 必须可扫描，实际报错：%v", err)
	}
	if field.String() != "" {
		t.Fatalf("NULL 应归一为空串，实际 %q", field.String())
	}
	if err := field.Scan("dir-1"); err != nil {
		t.Fatalf("字符串必须可扫描，实际报错：%v", err)
	}
	if field.String() != "dir-1" {
		t.Fatalf("值应原样保留，实际 %q", field.String())
	}
}
