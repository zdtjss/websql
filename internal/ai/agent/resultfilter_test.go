package agent

import (
	"encoding/json"
	"testing"
)

// resultfilter_test.go 对齐整改计划 §9.1 的「重点新增测试文件」清单：
// 覆盖 list_tables 结果的按表授权过滤（filterListTablesResult）。
// 该路径是工具结果的最后一道闸门：即使工具实现返回了全量表名，也必须在此处裁剪。

func decodeListTables(t *testing.T, payload string) ListTablesOutput {
	t.Helper()
	var out ListTablesOutput
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("输出不是合法 JSON：%v，原文 %q", err, payload)
	}
	return out
}

// TestFilterListTablesResult_DropsUnauthorizedTables 无权表必须被裁剪，Count 同步重算。
func TestFilterListTablesResult_DropsUnauthorizedTables(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil)}
	raw := `{"tables":[{"tableName":"orders"},{"tableName":"secret"},{"tableName":"ORDERS"}],"count":3}`

	out := decodeListTables(t, mw.filterListTablesResult(raw))
	if out.Count != 2 || len(out.Tables) != 2 {
		t.Fatalf("应保留 orders/ORDERS 两行（忽略大小写），实际 count=%d tables=%+v", out.Count, out.Tables)
	}
	for _, tbl := range out.Tables {
		if tbl.TableName == "secret" {
			t.Fatalf("无权表 secret 不应出现在结果中：%+v", out.Tables)
		}
	}
}

// TestFilterListTablesResult_InvalidJSONFailsClosed 解析失败必须返回空集合，不能回退为原始结果。
func TestFilterListTablesResult_InvalidJSONFailsClosed(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil)}
	out := decodeListTables(t, mw.filterListTablesResult(`{"tables":[{"tableName":"orders"}`))
	if out.Count != 0 || len(out.Tables) != 0 {
		t.Fatalf("解析失败时必须返回空集合（fail-closed），实际 %+v", out)
	}
}

// TestFilterListTablesResult_KeepsAllWhenAuthorized 全部授权时结果原样保留，不改变顺序。
func TestFilterListTablesResult_KeepsAllWhenAuthorized(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders", "customers"}, nil)}
	raw := `{"tables":[{"tableName":"customers"},{"tableName":"orders"}],"count":2}`

	out := decodeListTables(t, mw.filterListTablesResult(raw))
	if out.Count != 2 || len(out.Tables) != 2 {
		t.Fatalf("全部授权时不应裁剪，实际 %+v", out)
	}
	if out.Tables[0].TableName != "customers" || out.Tables[1].TableName != "orders" {
		t.Fatalf("应保持原始顺序，实际 %+v", out.Tables)
	}
}
