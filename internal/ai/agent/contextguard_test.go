package agent

import (
	"encoding/json"
	"testing"
)

// contextguard_test.go 对齐整改计划 §9.1 的「重点新增测试文件」清单：
// 覆盖 SanitizeSessionContext 对会话上下文（schemas/tables）的再授权过滤。
// 该函数是历史会话回放的第二道闸门：前端传来的 context 不可信，必须按当前权限重建。

func newContextGuardScope() *PermissionScope {
	return &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		AllowedTables:  map[string]bool{"s1.orders": true},
		AllowedColumns: map[string]map[string]bool{},
	}
}

func newContextGuardSec() *RunSecurityContext {
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": newContextGuardScope()},
		Masker: NewNoopMasker(),
	}
	sec.ScopeDigest = "digest-ctx"
	return sec
}

// TestSanitizeSessionContext_EmptyPassthrough 空上下文不应触发解析或报错。
func TestSanitizeSessionContext_EmptyPassthrough(t *testing.T) {
	for _, in := range []string{"", "  ", "{}"} {
		out, err := SanitizeSessionContext(in, newContextGuardSec())
		if err != nil {
			t.Fatalf("输入 %q 不应报错，实际：%v", in, err)
		}
		if out != "{}" {
			t.Fatalf("输入 %q 应归一为 {}，实际 %q", in, out)
		}
	}
}

// TestSanitizeSessionContext_InvalidJSONFailsClosed 非法 JSON 必须报错，不能回退为未过滤的原文。
func TestSanitizeSessionContext_InvalidJSONFailsClosed(t *testing.T) {
	out, err := SanitizeSessionContext(`{"tables":["s1.orders"`, newContextGuardSec())
	if err == nil {
		t.Fatalf("非法 JSON 必须报错（fail-closed），实际返回 %q", out)
	}
	if out != "" {
		t.Fatalf("报错时不应返回内容，实际 %q", out)
	}
}

// TestSanitizeSessionContext_DropsUnauthorizedTables 客户端传入的无权表必须被剔除。
func TestSanitizeSessionContext_DropsUnauthorizedTables(t *testing.T) {
	in := `{"tables":["s1.orders","s1.secret","s1.orders"]}`
	out, err := SanitizeSessionContext(in, newContextGuardSec())
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	var ctx SessionContext
	if err := json.Unmarshal([]byte(out), &ctx); err != nil {
		t.Fatalf("输出不是合法 JSON：%v，原文 %q", err, out)
	}
	if len(ctx.Tables) != 1 || ctx.Tables[0] != "s1.orders" {
		t.Fatalf("应仅保留有权的 s1.orders（并去重），实际 %v", ctx.Tables)
	}
}

// TestSanitizeSessionContext_DropsUnscopedSchemas 不在授权连接集合内的 Schema 引用必须被剔除。
func TestSanitizeSessionContext_DropsUnscopedSchemas(t *testing.T) {
	in := `{"schemas":[{"connId":"c1","schema":"s1"},{"connId":"c9","schema":"s9"}],"tables":["s1.orders"]}`
	out, err := SanitizeSessionContext(in, newContextGuardSec())
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	var ctx SessionContext
	if err := json.Unmarshal([]byte(out), &ctx); err != nil {
		t.Fatalf("输出不是合法 JSON：%v", err)
	}
	if len(ctx.Schemas) != 1 || ctx.Schemas[0].ConnID != "c1" {
		t.Fatalf("应仅保留 c1 的 Schema 引用，实际 %+v", ctx.Schemas)
	}
}

// TestSanitizeSessionContext_NilSecurityDropsTables 缺少安全上下文时表范围一律丢弃（fail-closed），
// 但 Schema 引用本身不是数据内容，保留以便前端展示选择器。
func TestSanitizeSessionContext_NilSecurityDropsTables(t *testing.T) {
	in := `{"schemas":[{"connId":"c1","schema":"s1"}],"tables":["s1.orders"]}`
	out, err := SanitizeSessionContext(in, nil)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	var ctx SessionContext
	if err := json.Unmarshal([]byte(out), &ctx); err != nil {
		t.Fatalf("输出不是合法 JSON：%v", err)
	}
	if len(ctx.Tables) != 0 {
		t.Fatalf("无安全上下文时不得保留表范围，实际 %v", ctx.Tables)
	}
	if len(ctx.Schemas) != 1 {
		t.Fatalf("Schema 引用应原样保留，实际 %+v", ctx.Schemas)
	}
}

// TestSanitizeSessionContext_RejectsUnsafeIdentifiers 非法标识符（注入/空白/超长）必须丢弃。
func TestSanitizeSessionContext_RejectsUnsafeIdentifiers(t *testing.T) {
	in := `{"tables":["s1.orders; DROP TABLE x","s1.or ders","s1.orders--"]}`
	out, err := SanitizeSessionContext(in, newContextGuardSec())
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	var ctx SessionContext
	if err := json.Unmarshal([]byte(out), &ctx); err != nil {
		t.Fatalf("输出不是合法 JSON：%v", err)
	}
	if len(ctx.Tables) != 0 {
		t.Fatalf("非法标识符必须全部丢弃，实际 %v", ctx.Tables)
	}
}
