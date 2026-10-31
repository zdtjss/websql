package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/jmoiron/sqlx"
)

// ─── F-09 流式期间按时间节流复核修订号 ───

func TestRevisionCheckDue(t *testing.T) {
	now := time.Now()
	if revisionCheckDue(time.Time{}, now) {
		t.Fatal("未初始化时间不应触发复核")
	}
	if revisionCheckDue(now.Add(-100*time.Millisecond), now) {
		t.Fatal("间隔不足不应触发复核（避免每 chunk 查库）")
	}
	if !revisionCheckDue(now.Add(-600*time.Millisecond), now) {
		t.Fatal("超过间隔必须触发一次复核")
	}
}

// ─── F-23 未变更消息不重复 hash/脱敏 ───

func TestSanitizedMessagesLocked_TrustsCleanMessages(t *testing.T) {
	sess := &Session{ID: "s1", messages: []SessionMessage{{
		Role:    "assistant",
		Content: "13800138000",
		dirty:   false,
		Security: &MessageSecurityMeta{
			Sanitized: true, ContentSHA: "stale-but-trusted", MaskVersion: "",
			ScopeDigest: "digest-current",
		},
	}}}
	sec := testMaskerSec()
	out, err := sess.sanitizedMessagesLocked(sec)
	if err != nil {
		t.Fatalf("净化失败：%v", err)
	}
	if out[0].Content != "13800138000" {
		t.Fatal("非脏消息不应重新脱敏（应直接复用落库副本）")
	}
	if out[0].Security.ContentSHA != "stale-but-trusted" {
		t.Fatal("非脏消息不应重算 contentSHA")
	}

	sess.messages[0].dirty = true
	out2, err := sess.sanitizedMessagesLocked(sec)
	if err != nil {
		t.Fatalf("净化失败：%v", err)
	}
	if out2[0].Security.ContentSHA == "stale-but-trusted" {
		t.Fatal("脏消息必须重算 contentSHA（同一脱敏版本下正文未变时可跳过正则）")
	}
}

func TestReplaceMessages_MarksDirty(t *testing.T) {
	sess := &Session{ID: "s1"}
	sess.ReplaceMessages([]SessionMessage{{Role: "user", Content: "Q"}, {Role: "assistant", Content: "A"}})
	for _, msg := range sess.messages {
		if !msg.dirty {
			t.Fatalf("ReplaceMessages 后消息应标记为脏：%+v", msg)
		}
	}
	sess.clearDirtyLocked()
	for _, msg := range sess.messages {
		if msg.dirty {
			t.Fatal("保存成功后应清除脏标记")
		}
	}
}

// ─── F-30 工具注册失败可观测 ───

func TestFailedToolNames(t *testing.T) {
	got := failedToolNames([]namedToolError{
		{"export_excel", nil},
		{"export_ppt", fmt.Errorf("schema 生成失败")},
		{"repair_export", nil},
	})
	if len(got) != 1 || got[0] != "export_ppt" {
		t.Fatalf("应只返回失败工具：%v", got)
	}
	if len(failedToolNames(nil)) != 0 {
		t.Fatal("空输入应返回空列表")
	}
}

// ─── F-34 AGENTS.md 段裁剪与加载 ───

func TestApplyAgentsMDConditions(t *testing.T) {
	raw := "保留段\n<!-- @if conn -->\n有连接才注入\n<!-- @endif -->\n" +
		"<!-- @if skill -->\n有 Skill 才注入\n<!-- @endif -->\n" +
		"<!-- @if conn -->\n<!-- @if skill -->\n两者都要\n<!-- @endif -->\n<!-- @endif -->\n"

	got := applyAgentsMDConditions(raw, false, false)
	if strings.Contains(got, "有连接才注入") || strings.Contains(got, "有 Skill 才注入") || strings.Contains(got, "两者都要") {
		t.Fatalf("条件段未被裁剪：%q", got)
	}
	if !strings.Contains(got, "保留段") {
		t.Fatalf("无条件内容必须保留：%q", got)
	}

	got2 := applyAgentsMDConditions(raw, true, true)
	for _, want := range []string{"有连接才注入", "有 Skill 才注入", "两者都要"} {
		if !strings.Contains(got2, want) {
			t.Fatalf("应包含 %q，实际：%q", want, got2)
		}
	}

	got3 := applyAgentsMDConditions(raw, true, false)
	if strings.Contains(got3, "两者都要") || !strings.Contains(got3, "有连接才注入") {
		t.Fatalf("嵌套条件判定错误：%q", got3)
	}
}

func TestLoadAgentsMD_CachesAndTruncates(t *testing.T) {
	originalContent, originalMtime := agentsMDLoader.content, agentsMDLoader.mtime
	originalCheck, originalLoaded := agentsMDLoader.lastCheck, agentsMDLoader.loaded
	defer func() {
		agentsMDLoader.mu.Lock()
		defer agentsMDLoader.mu.Unlock()
		agentsMDLoader.content, agentsMDLoader.mtime = originalContent, originalMtime
		agentsMDLoader.lastCheck, agentsMDLoader.loaded = originalCheck, originalLoaded
	}()
	// 本包其它用例会先加载真实 AGENTS.md，必须强制从“未加载”开始才能验证读取/缓存。
	agentsMDLoader.mu.Lock()
	agentsMDLoader.content = ""
	agentsMDLoader.loaded = false
	agentsMDLoader.lastCheck = time.Time{}
	agentsMDLoader.mtime = time.Time{}
	agentsMDLoader.mu.Unlock()

	t.Chdir(t.TempDir())
	if err := os.WriteFile("AGENTS.md", []byte("# 第一版"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadAgentsMD(); got != "# 第一版" {
		t.Fatalf("首次应读取磁盘内容：%q", got)
	}
	// 60s 内即使文件变化也使用缓存（mtime 检查被节流）
	if err := os.WriteFile("AGENTS.md", []byte("# 第二版"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 显式推后 mtime，避免部分文件系统的时间粒度让两次写入落在同一时间戳。
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes("AGENTS.md", future, future); err != nil {
		t.Fatal(err)
	}
	if got := loadAgentsMD(); got != "# 第一版" {
		t.Fatalf("TTL 内不应重读磁盘：%q", got)
	}
	agentsMDLoader.lastCheck = time.Now().Add(-2 * agentsMDCheckInterval)
	if got := loadAgentsMD(); got != "# 第二版" {
		t.Fatalf("超过检查间隔后应热更新：%q", got)
	}

	oversize := strings.Repeat("字", agentsMDMaxBytes)
	if err := os.WriteFile("AGENTS.md", []byte(oversize), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes("AGENTS.md", future.Add(time.Minute), future.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	agentsMDLoader.lastCheck = time.Time{}
	got := loadAgentsMD()
	if len(got) > agentsMDMaxBytes {
		t.Fatalf("超大 AGENTS.md 必须截断：%d > %d", len(got), agentsMDMaxBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("截断必须落在合法 rune 边界")
	}
}

// ─── F-22 安全上下文缓存键与 TTL ───

func TestRunSecurityCacheKeyStableAndScoped(t *testing.T) {
	refs := []SchemaRef{{ConnID: "c1", Schema: "s1"}, {ConnID: "c2", Schema: "s2"}}
	reordered := []SchemaRef{{ConnID: "c2", Schema: "s2"}, {ConnID: "c1", Schema: "s1"}}
	if runSecurityCacheKey(ProfileWebRemote, "u1", "c1", refs, 7) !=
		runSecurityCacheKey(ProfileWebRemote, "u1", "c1", reordered, 7) {
		t.Fatal("同一授权集合的缓存键必须与顺序无关")
	}
	if runSecurityCacheKey(ProfileWebRemote, "u1", "c1", refs, 7) ==
		runSecurityCacheKey(ProfileWebRemote, "u1", "c1", refs, 8) {
		t.Fatal("revision 变化必须换键，不能读到旧权限投影")
	}
	if runSecurityCacheKey(ProfileWebRemote, "u1", "c1", refs, 7) ==
		runSecurityCacheKey(ProfileWebRemote, "u2", "c1", refs, 7) {
		t.Fatal("不同用户不得共享缓存")
	}
}

func TestRunSecurityCacheTTLExpiry(t *testing.T) {
	key := "unit-test-key"
	sec := &RunSecurityContext{UserID: "u1", ScopeDigest: "d1"}
	putCachedRunSecurity(key, sec)
	if got := getCachedRunSecurity(key); got != sec {
		t.Fatal("TTL 内应命中缓存")
	}

	runSecurityCache.mu.Lock()
	entry := runSecurityCache.items[key]
	entry.at = time.Now().Add(-2 * runSecurityCacheTTL)
	runSecurityCache.items[key] = entry
	runSecurityCache.mu.Unlock()

	if got := getCachedRunSecurity(key); got != nil {
		t.Fatal("超过 TTL 必须失效，避免权限调整长期读旧缓存")
	}
}

// ─── F-05 降级路径使用已脱敏参数并走严格列校验 ───

func TestCheckStreamSQLAccessFallback_UsesGivenArgsAndStrictColumnCheck(t *testing.T) {
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		AllowedColumns: map[string]map[string]bool{"s1.users": {"id": true}},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope}, Masker: NewNoopMasker(),
	}
	ctx := WithRunSecurity(context.Background(), sec)
	mw := &PermissionMiddleware{Scope: scope, ConnID: "c1", Schemas: []SchemaRef{{ConnID: "c1", Schema: "s1"}}}

	var received string
	endpoint := func(_ context.Context, args string, _ ...tool.Option) (*schema.StreamReader[string], error) {
		received = args
		return schema.StreamReaderFromArray([]string{"exported"}), nil
	}

	// column 级表上的 SELECT * 必须被拒绝（导出结果直接落盘，没有结果过滤兜底）
	starArgs := `{"sql":"SELECT * FROM users","content":"# 报告\n\n正文"}`
	reader, err := mw.checkStreamSQLAccessFallback(ctx, starArgs, endpoint, "export_html")
	if err != nil {
		t.Fatalf("应以工具结果形式返回拒绝：%v", err)
	}
	chunk, _ := reader.Recv()
	if !strings.Contains(chunk, "SELECT *") && !strings.Contains(chunk, "禁止使用") {
		t.Fatalf("column 级表 SELECT * 应被严格校验拒绝：%q", chunk)
	}
	if received != "" {
		t.Fatal("被拒绝时不应执行 endpoint")
	}

	// 放行时必须使用传入的（已脱敏）参数，而不是重新解析原始参数
	okArgs := `{"sql":"SELECT id FROM users","content":"正文 13800138000"}`
	if _, err := mw.checkStreamSQLAccessFallback(ctx, okArgs, endpoint, "export_html"); err != nil {
		t.Fatalf("合法导出应放行：%v", err)
	}
	if received != okArgs {
		t.Fatalf("fallback 必须透传 checkedArgs：got=%q want=%q", received, okArgs)
	}
}

func TestIsExportToolName(t *testing.T) {
	for _, name := range []string{"export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt", "export_html", "repair_export", "export_skill", "export_by_code", "repair_export_by_code"} {
		if !isExportToolName(name) {
			t.Errorf("%s 应属于导出集合", name)
		}
	}
	for _, name := range []string{"query_data", "list_tables", "get_table_schema"} {
		if isExportToolName(name) {
			t.Errorf("%s 不应属于导出集合", name)
		}
	}
}

// ─── F-06 导出参数脱敏覆盖 payload 与 patches ───

func TestMaskExportArgs_CoversPayloadAndPatches(t *testing.T) {
	scope := &PermissionScope{UserID: "u1", ConnID: "c1", IsRemote: true}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope}, Masker: NewNoopMasker(),
	}
	ctx := WithRunSecurity(context.Background(), sec)
	mw := &PermissionMiddleware{Scope: scope, ConnID: "c1"}

	payload := `{\"markdown\":\"联系 13800138000\",\"data\":{\"rows\":[[\"110101199001011234\"]]}}`
	args := fmt.Sprintf(`{"skill":"export-word","payload":"%s"}`, payload)
	if !hasExportTextArg(args) {
		t.Fatal("payload 必须触发脱敏")
	}
	masked, err := mw.maskExportArgs(ctx, args)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if strings.Contains(masked, "13800138000") || strings.Contains(masked, "110101199001011234") {
		t.Fatalf("payload 内敏感值未脱敏：%s", masked)
	}
	var outer map[string]any
	if err := json.Unmarshal([]byte(masked), &outer); err != nil {
		t.Fatalf("参数不再是合法 JSON：%v", err)
	}

	patchArgs := `{"file":"/exports/x.pptx","patches":[{"find":"a","replace":"手机 13800138000"}]}`
	if !hasExportTextArg(patchArgs) {
		t.Fatal("patches 必须触发脱敏")
	}
	maskedPatch, err := mw.maskExportArgs(ctx, patchArgs)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if strings.Contains(maskedPatch, "13800138000") {
		t.Fatalf("patch.replace 未脱敏：%s", maskedPatch)
	}

	opArgs := `{"file":"/exports/x.pptx","operations":[{"op":"replace","kind":"block","id":"b1","patch":{"replace":"邮箱 a@b.com"}}]}`
	maskedOp, err := mw.maskExportArgs(ctx, opArgs)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if strings.Contains(maskedOp, "a@b.com") {
		t.Fatalf("operations[].patch.replace 未脱敏：%s", maskedOp)
	}
}

// setupRemediationSessionDB 构建包含完整列（含 created_at）的会话测试库。
func setupRemediationSessionDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`create table t_ai_session (
		id text primary key, user_id text, title text, messages text,
		context text, created_at datetime, updated_at datetime)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	Init(db)
	t.Cleanup(func() { Init(nil) })
	return db
}

// seedRemediationSession 写入会话行（必须带 created_at/updated_at，否则 SessionDB 扫描失败）。
func seedRemediationSession(t *testing.T, db *sqlx.DB, id, userID, messages, context string) {
	t.Helper()
	now := time.Now()
	if _, err := db.Exec(`insert or replace into t_ai_session
		(id, user_id, title, messages, context, created_at, updated_at) values (?, ?, '', ?, ?, ?, ?)`,
		id, userID, messages, context, now, now); err != nil {
		t.Fatalf("插入会话失败：%v", err)
	}
}

// ─── F-16 内容损坏不得退化为空会话 ───

func TestLoadSessionFromDB_CorruptedMessagesReturnError(t *testing.T) {
	db := setupRemediationSessionDB(t)
	seedRemediationSession(t, db, "s-broken", "u1", "{not-json", "{}")

	if _, err := loadSessionFromDB("s-broken"); err == nil {
		t.Fatal("消息解析失败必须返回错误，不能按空会话继续")
	}

	// 空消息（合法 []）仍应正常加载
	seedRemediationSession(t, db, "s-empty", "u1", "[]", "{}")
	sess, err := loadSessionFromDB("s-empty")
	if err != nil || sess == nil {
		t.Fatalf("合法空会话应可加载：sess=%+v err=%v", sess, err)
	}
	if len(sess.messages) != 0 {
		t.Fatalf("消息应为空：%+v", sess.messages)
	}
}

func TestGetDetail_CorruptedMessagesReturnError(t *testing.T) {
	db := setupRemediationSessionDB(t)
	seedRemediationSession(t, db, "s-broken-detail", "u1", "{not-json", "{}")
	store, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	if _, err := store.GetDetail("s-broken-detail", "u1"); err == nil {
		t.Fatal("详情接口遇到损坏内容应报错而不是返回空历史")
	}
}

// ─── F-28 会话详情按当前权限过滤 ───

func TestGetDetailFiltered_HidesRevokedAssistantData(t *testing.T) {
	db := setupRemediationSessionDB(t)
	messages := []SessionMessage{
		{Role: "user", Content: "查询手机号"},
		{Role: "assistant", Content: "结果是 13800138000", Security: &MessageSecurityMeta{
			ScopeDigest: "旧的权限快照", Sanitized: true, ContentSHA: "sha",
			// 落库时无法逐列枚举来源的数据消息会被标为不可溯源（见 sanitizedMessagesLocked）
			Untraceable: true,
		}},
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`insert into t_ai_session (id, user_id, title, messages, context, created_at, updated_at)
		values ('s-filter', 'u1', '', ?, ?, ?, ?)`, string(encoded), `{"schemas":[{"connId":"c1","schema":"s1"}]}`, time.Now(), time.Now()); err != nil {
		t.Fatalf("插入会话失败：%v", err)
	}

	store, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	sec := testMaskerSec()
	detail, err := store.GetDetailFiltered("s-filter", "u1", sec)
	if err != nil {
		t.Fatalf("读取详情失败：%v", err)
	}
	var assistantContent string
	for _, msg := range detail.Messages {
		if msg.Role == "assistant" {
			assistantContent = msg.Content
		}
	}
	if strings.Contains(assistantContent, "13800138000") {
		t.Fatalf("权限回收后不得继续展示旧数据：%q", assistantContent)
	}
	if !strings.Contains(assistantContent, "已隐藏") {
		t.Fatalf("应展示隔离占位：%q", assistantContent)
	}
	var userContent string
	for _, msg := range detail.Messages {
		if msg.Role == "user" {
			userContent = msg.Content
		}
	}
	if userContent != "查询手机号" {
		t.Fatalf("用户提问必须保留：%q", userContent)
	}

	// 不带 sec 的旧接口保持原行为（内部/测试使用）
	raw, err := store.GetDetail("s-filter", "u1")
	if err != nil {
		t.Fatalf("读取详情失败：%v", err)
	}
	foundRaw := false
	for _, msg := range raw.Messages {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "13800138000") {
			foundRaw = true
		}
	}
	if !foundRaw {
		t.Fatal("GetDetail 不过滤时应返回落库原文")
	}
}

func TestGetContextParsesSchemas(t *testing.T) {
	db := setupRemediationSessionDB(t)
	seedRemediationSession(t, db, "s-ctx", "u1", "[]", `{"schemas":[{"connId":"c1","schema":"s1"}],"tables":["s1.orders"]}`)
	store, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	ctx, err := store.GetContext("s-ctx", "u1")
	if err != nil {
		t.Fatalf("读取上下文失败：%v", err)
	}
	if len(ctx.Schemas) != 1 || ctx.Schemas[0].ConnID != "c1" {
		t.Fatalf("schemas 解析错误：%+v", ctx.Schemas)
	}
}

// ─── F-22 提示词构建不再建立数据库连接 ───

// TestBuildDynamicPromptPartDoesNotOpenConnections 多 Schema 提示词只需驱动名：
// 必须来于 t_conn 元数据，而不是为每个连接建连接池/探测（旧实现会 Ping 失败而 panic）。
func TestBuildDynamicPromptPartDoesNotOpenConnections(t *testing.T) {
	db := setupGoldenDB(t)
	Init(db)
	t.Cleanup(func() { Init(nil) })
	for _, id := range []string{"prompt-c1", "prompt-c2"} {
		if _, err := db.Exec(`insert or replace into t_conn (id, name, db_type) values (?, ?, 'mysql')`, id, id); err != nil {
			t.Fatalf("插入连接失败：%v", err)
		}
	}
	// 不注册任何 URL/凭据：旧路径会 loadConnCfgCached 失败后返回 nil，
	// 但更重要的是不会尝试 Ping 不可达的数据库。
	scope := &PermissionScope{UserID: "u1", ConnID: "prompt-c1", SchemaName: "s1", IsRemote: true}
	out := buildDynamicPromptPart("prompt-c1", "mysql", "s1", "8.0", nil, scope,
		[]SchemaRef{{ConnID: "prompt-c1", Schema: "s1"}, {ConnID: "prompt-c2", Schema: "s2"}}, false, nil)
	if !strings.Contains(out, "prompt-c2") {
		t.Fatalf("多连接提示词应包含非默认连接：%q", out)
	}
	if !strings.Contains(out, "mysql") {
		t.Fatalf("驱动名应取自 t_conn 元数据：%q", out)
	}
}

// ─── F-23 大会话保存开销基准 ───

func buildLargeSession(totalBytes int, dirty bool) *Session {
	const chunkPerMsg = 4096
	count := totalBytes / chunkPerMsg
	messages := make([]SessionMessage, 0, count)
	for i := 0; i < count; i++ {
		msg := SessionMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("m%d %s", i, strings.Repeat("a", chunkPerMsg)),
			Security: &MessageSecurityMeta{
				Sanitized: true, ContentSHA: "preset-sha", ScopeDigest: "digest-current",
			},
			dirty: dirty,
		}
		messages = append(messages, msg)
	}
	return &Session{ID: "bench", messages: messages}
}

func BenchmarkSanitizedMessagesLockedClean(b *testing.B) {
	sec := testMaskerSec()
	sess := buildLargeSession(8<<20, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sess.sanitizedMessagesLocked(sec); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSanitizedMessagesLockedDirty(b *testing.B) {
	sec := testMaskerSec()
	sess := buildLargeSession(8<<20, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sess.sanitizedMessagesLocked(sec); err != nil {
			b.Fatal(err)
		}
	}
}
