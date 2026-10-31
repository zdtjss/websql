package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	database "websql/internal/database"

	"github.com/cloudwego/eino/schema"
	"github.com/jmoiron/sqlx"
)

func testMaskerSec() *RunSecurityContext {
	return &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": {UserID: "u1", ConnID: "c1", IsRemote: true}},
		Masker: NewNoopMasker(), ScopeDigest: "digest-current",
	}
}

// ─── F-08 sanitizedMessagesLocked 不得写穿内存态 Security ───

func TestSanitizedMessagesLocked_DoesNotMutateInMemorySecurity(t *testing.T) {
	sess := &Session{ID: "s1", messages: []SessionMessage{{
		Role:    "assistant",
		Content: "联系电话 13800138000",
		dirty:   true,
		Security: &MessageSecurityMeta{
			ScopeDigest: "digest-old",
			Untraceable: true,
		},
	}}}
	sec := testMaskerSec()

	original := sess.messages[0]
	out, err := sess.sanitizedMessagesLocked(sec)
	if err != nil {
		t.Fatalf("净化失败：%v", err)
	}
	if strings.Contains(out[0].Content, "13800138000") {
		t.Fatalf("落库副本应已脱敏：%q", out[0].Content)
	}
	if strings.Contains(sess.messages[0].Content, "13800138000") != true {
		t.Fatalf("内存态正文不应被改写：%q", sess.messages[0].Content)
	}
	if sess.messages[0].Security.ScopeDigest != original.Security.ScopeDigest {
		t.Fatal("内存态 ScopeDigest 被写穿")
	}
	if sess.messages[0].Security.Sanitized {
		t.Fatal("内存态 Sanitized 标记被写穿")
	}
	// 摘要类不可溯源消息不能被补上当前 digest「盖章」
	if out[0].Security.ScopeDigest != "digest-old" {
		t.Fatalf("Untraceable 消息不应被补当前 digest：%q", out[0].Security.ScopeDigest)
	}
}

// ─── F-14 ScopeDigest 不含全局 revision ───

func TestScopeDigest_ExcludesGlobalRevision(t *testing.T) {
	base := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		AllowedTables:     map[string]bool{"s1.orders": true},
		AllowedColumns:    map[string]map[string]bool{},
		FullAccessSchemas: map[string]bool{},
	}
	withOld := *base
	withOld.Revision = 7
	withNew := *base
	withNew.Revision = 8
	if withOld.ScopeDigest() != withNew.ScopeDigest() {
		t.Fatal("权限投影未变时，全局 revision 不应改变 ScopeDigest")
	}
	if len := len(withNew.ScopeDigest()); len != 32 {
		t.Fatalf("digest 长度应为 32 hex，实际 %d", len)
	}

	changed := *base
	changed.AllowedTables = map[string]bool{"s1.orders": true, "s1.users": true}
	if changed.ScopeDigest() == withNew.ScopeDigest() {
		t.Fatal("权限投影变化时 digest 必须变化")
	}
}

// ─── F-15 取消/报错不得删除已配对的工具结果 ───

func cleanupTrailing(messages []SessionMessage) []SessionMessage {
	sess := &Session{ID: "s1", messages: messages}
	sess.mu.Lock()
	sess.removeTrailingIncompleteToolCallsLocked()
	sess.mu.Unlock()
	return sess.messages
}

func TestRemoveTrailingIncompleteToolCalls_KeepsPairedToolResult(t *testing.T) {
	in := []SessionMessage{
		{Role: "user", Content: "删除过期数据"},
		{Role: "assistant", ToolCalls: []SessionToolCall{{ID: "call-1", Function: SessionFunctionCall{Name: "exec_sql"}}}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "exec_sql", Content: `{"affectedRows":10}`},
	}
	out := cleanupTrailing(in)
	if len(out) != 3 {
		t.Fatalf("已执行的写操作结果必须保留，实际剩余 %d 条", len(out))
	}
	if out[2].Role != "tool" || !strings.Contains(out[2].Content, "affectedRows") {
		t.Fatalf("tool 结果被删除：%+v", out[2])
	}
}

func TestRemoveTrailingIncompleteToolCalls_DropsDanglingAndOrphans(t *testing.T) {
	in := []SessionMessage{
		{Role: "user", Content: "查一下"},
		{Role: "assistant", ToolCalls: []SessionToolCall{{ID: "call-x", Function: SessionFunctionCall{Name: "query_data"}}}},
	}
	out := cleanupTrailing(in)
	if len(out) != 1 {
		t.Fatalf("无结果的 assistant 空消息应被删除，实际 %d 条", len(out))
	}

	mixed := []SessionMessage{
		{Role: "user", Content: "查一下"},
		{Role: "assistant", Content: "先查 A", ToolCalls: []SessionToolCall{
			{ID: "call-a", Function: SessionFunctionCall{Name: "query_data"}},
			{ID: "call-b", Function: SessionFunctionCall{Name: "query_data"}},
		}},
		{Role: "tool", ToolCallID: "call-a", Content: "A 结果"},
	}
	got := cleanupTrailing(mixed)
	if len(got) != 3 {
		t.Fatalf("应保留 assistant + 已配对结果，实际 %+v", got)
	}
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call-a" {
		t.Fatalf("只应保留已配对的 tool_call：%+v", got[1].ToolCalls)
	}
	if got[1].Content != "先查 A" {
		t.Fatalf("正文应保留：%q", got[1].Content)
	}
}

func TestRemoveTrailingIncompleteToolCalls_DropsOrphanToolMessage(t *testing.T) {
	in := []SessionMessage{
		{Role: "user", Content: "查一下"},
		{Role: "tool", ToolCallID: "call-z", Content: "孤儿结果"},
	}
	out := cleanupTrailing(in)
	if len(out) != 1 {
		t.Fatalf("无 assistant 声明的孤儿 tool 消息应被删除，实际 %+v", out)
	}
}

// ─── F-17 Oracle schema 限定 DDL 的列过滤 ───

func TestFilterDDLByScope_OracleQualifiedTable(t *testing.T) {
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "HR", IsRemote: true,
		AllowedTables:  map[string]bool{},
		AllowedColumns: map[string]map[string]bool{"HR.EMPLOYEES": {"id": true, "name": true}},
	}
	ddl := "CREATE TABLE \"HR\".\"EMPLOYEES\" (\n  \"ID\" NUMBER,\n  \"NAME\" VARCHAR2(50),\n  \"SALARY\" NUMBER,\n)\n"
	got := filterDDLByScope(ddl, []string{"HR.EMPLOYEES"}, scope)
	if !strings.Contains(got, "\"ID\"") || !strings.Contains(got, "\"NAME\"") {
		t.Fatalf("已授权列应保留：%q", got)
	}
	if strings.Contains(got, "SALARY") {
		t.Fatalf("未授权列必须被过滤：%q", got)
	}
}

func TestFilterDDLByScope_MySQLUnqualifiedStillWorks(t *testing.T) {
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		AllowedColumns: map[string]map[string]bool{"orders": {"id": true}},
	}
	ddl := "CREATE TABLE orders (\n  id int,\n  secret varchar(20)\n)\n"
	got := filterDDLByScope(ddl, []string{"orders"}, scope)
	if !strings.Contains(got, "id int") {
		t.Fatalf("MySQL 已授权列应保留：%q", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("MySQL 未授权列应被过滤：%q", got)
	}
}

// ─── F-24 输出窗口降档后仍拦截跨 chunk 敏感串 ───

func collectStream(t *testing.T, guard *OutputGuard, chunks []string) string {
	t.Helper()
	var out strings.Builder
	for _, chunk := range chunks {
		safe, err := guard.Push(chunk)
		if err != nil {
			t.Fatalf("guard 错误：%v", err)
		}
		out.WriteString(safe)
	}
	tail, err := guard.Close()
	if err != nil {
		t.Fatalf("close 错误：%v", err)
	}
	out.WriteString(tail)
	return out.String()
}

func TestOutputGuard_FirstChunkEmittedEarly(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	// 70 个普通字符（<1024 单 token）：应能在超过 64 rune 后立即出窗
	long := strings.Repeat("字", 70)
	safe, err := guard.Push(long)
	if err != nil {
		t.Fatalf("push 错误：%v", err)
	}
	if safe == "" {
		t.Fatal("首 token 延迟未改善：64 rune 窗口内应可推送")
	}
}

func TestOutputGuard_CrossChunkSecretStillBlocked(t *testing.T) {
	got := collectStream(t, NewOutputGuard(NewNoopMasker(), 0), []string{"password=", "hunter2-secret"})
	if strings.Contains(got, "hunter2-secret") {
		t.Fatalf("跨 chunk 敏感赋值仍泄露：%q", got)
	}
	got2 := collectStream(t, NewOutputGuard(NewNoopMasker(), 0), []string{"手机号 138", "0013", "8000 完"})
	if strings.Contains(got2, "13800138000") {
		t.Fatalf("跨 chunk 手机号仍泄露：%q", got2)
	}
}

func TestOutputGuard_KeepsLongURL(t *testing.T) {
	url := "https://example.com/" + strings.Repeat("a", 1200)
	got := collectStream(t, NewOutputGuard(NewNoopMasker(), 0), []string{"链接：" + url})
	if !strings.Contains(got, strings.Repeat("a", 1200)) {
		t.Fatalf("普通长 URL 不应被整段删除：%q", firstRunes(got, 80))
	}
	secretToken := "sk-" + strings.Repeat("b", 1200)
	got2 := collectStream(t, NewOutputGuard(NewNoopMasker(), 0), []string{"令牌 " + secretToken})
	if strings.Contains(got2, secretToken) {
		t.Fatal("超长敏感前缀 token 应被整段替换")
	}
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ─── F-33 checkpoint 淘汰不得删除在途恢复 ───

func TestCheckPointEvict_SkipsInflight(t *testing.T) {
	store := NewInMemoryCheckPointStore()
	store.items["busy"] = &cpEntry{inflight: true, updatedAt: time.Now().Add(-time.Hour), binding: CheckpointBinding{UserID: "u1"}}
	store.items["idle"] = &cpEntry{updatedAt: time.Now(), binding: CheckpointBinding{UserID: "u1"}}
	store.items["extra"] = &cpEntry{updatedAt: time.Now().Add(time.Minute), binding: CheckpointBinding{UserID: "u1"}}
	store.maxEntries = 1
	store.evictOverflowLocked()
	if _, ok := store.items["busy"]; !ok {
		t.Fatal("在途 checkpoint 不得被淘汰")
	}
}

func TestCheckPointRevisionStrictCompare(t *testing.T) {
	store := NewInMemoryCheckPointStore()
	store.Bind("cp1", CheckpointBinding{UserID: "u1", SessionID: "s1", Revision: 0, RevisionLoaded: true, ScopeDigest: "d1"})
	if err := store.Set(context.Background(), "cp1", []byte("state")); err != nil {
		t.Fatalf("写入 checkpoint 失败：%v", err)
	}
	if _, err := store.BeginResume("cp1", CheckpointBinding{UserID: "u1", SessionID: "s1", Revision: 9, RevisionLoaded: true, ScopeDigest: "d1"}); !errors.Is(err, ErrCheckpointStale) {
		t.Fatalf("Revision 变化应拒绝恢复，实际：%v", err)
	}
	version, err := store.BeginResume("cp1", CheckpointBinding{UserID: "u1", SessionID: "s1", Revision: 0, RevisionLoaded: true, ScopeDigest: "d1"})
	if err != nil || version == 0 {
		t.Fatalf("Revision 一致时应可恢复：%v", err)
	}
}

// ─── F-07 流式 tool_calls 必须登记溯源并标记不可溯源 ───

func TestPersistAttempt_RegistersPendingResources(t *testing.T) {
	sess := &Session{ID: "s1"}
	sec := testMaskerSec()
	att := &streamAttempt{}
	att.content.WriteString("先查一下")
	att.safeContent.WriteString("先查一下")
	att.toolCalls = []schema.ToolCall{{ID: "call-1", Function: schema.FunctionCall{Name: "query_data", Arguments: `{"sql":"SELECT id FROM orders"}`}}}

	pending := map[string]pendingResourceInfo{}
	(&SQLAgent{}).persistAttempt(att, sess, sec, pending)

	info, ok := pending["call-1"]
	if !ok || len(info.refs) == 0 {
		t.Fatalf("流式 tool_call 未登记资源溯源：%+v", pending)
	}
	if len(sess.messages) != 1 {
		t.Fatalf("应落一条 assistant 消息：%+v", sess.messages)
	}
	if !sess.messages[0].Security.Untraceable {
		t.Fatal("带 tool_calls 的 assistant 必须标记为不可溯源")
	}
	if len(sess.messages[0].Security.Resources) == 0 {
		t.Fatal("assistant 安全元数据应携带资源溯源")
	}
}

// ─── F-37 连接 panic 不得冒泡到 SSE 流 ───

func TestSafeGetConn_RecoversPanic(t *testing.T) {
	db := setupGoldenDB(t)
	seedRole(t, db, "r_conn_panic", 1, 1)
	seedPower(t, db, "r_conn_panic", "c9", "", "", "", "conn")
	seedUserRole(t, db, "u_panic", "r_conn_panic")
	if _, err := db.Exec(`insert or replace into t_conn (id, name, db_type) values ('c9', 'c9', 'mysql')`); err != nil {
		t.Fatalf("插入连接失败：%v", err)
	}

	original := openManagedConn
	openManagedConn = func(*database.DBParam) *sqlx.DB { panic("connection refused") }
	defer func() { openManagedConn = original }()

	conn, _, err := safeGetConn("c9", "u_panic")
	if err == nil {
		t.Fatal("连接 panic 应转为错误")
	}
	if conn != nil {
		t.Fatal("失败时不应返回连接")
	}
	if got := func() *sqlx.DB { c, _ := GetConn("c9", "u_panic"); return c }(); got != nil {
		t.Fatal("GetConn 兼容包装不应返回连接")
	}
}
