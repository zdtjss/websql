package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"websql/internal/ai/agent/export"

	"github.com/cloudwego/eino/adk"
	adkfilesystem "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// TestInjectedContextFilterMiddleware 验证 AGENTS/toolsearch 瞬态消息不会进入摘要与持久化链路。
func TestInjectedContextFilterMiddleware(t *testing.T) {
	mw := &InjectedContextFilterMiddleware{}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		{Role: schema.User, Content: "real-question"},
		{Role: schema.User, Content: "agents-md", Extra: map[string]any{agentsMDExtraKey: true}},
		{Role: schema.User, Content: "toolsearch", Extra: map[string]any{toolSearchReminderExtraKey: true}},
		{Role: schema.Assistant, Content: "answer"},
	}}
	_, got, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	for _, msg := range got.Messages {
		if msg.Extra != nil {
			if _, ok := msg.Extra[agentsMDExtraKey]; ok {
				t.Fatal("agentsmd message should be removed")
			}
			if _, ok := msg.Extra[toolSearchReminderExtraKey]; ok {
				t.Fatal("toolsearch reminder should be removed")
			}
		}
	}
}

// TestBuildQueryOutput_TotalKnown 验证截断结果不会把返回行数伪装成总行数。
func TestBuildQueryOutput_TotalKnown(t *testing.T) {
	rows := []map[string]any{{"id": 1}, {"id": 2}}
	full := buildQueryOutput([]string{"id"}, rows, false)
	if !full.TotalKnown || full.RowCount != 2 || full.TotalRows != 0 {
		t.Fatalf("full result should expose known total: %+v", full)
	}
	truncated := buildQueryOutput([]string{"id"}, rows, true)
	if truncated.TotalKnown || truncated.RowCount != 0 || truncated.ReturnedRows != 2 {
		t.Fatalf("truncated result must not expose total: %+v", truncated)
	}
}

// TestCountMessageTokensIncludesToolSchema 验证工具参数 schema 计入 token 预算。
func TestCountMessageTokensIncludesToolSchema(t *testing.T) {
	info := &schema.ToolInfo{
		Name: "demo",
		Desc: "demo tool",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"a": {Type: schema.String, Desc: "param a", Required: true},
			"b": {Type: schema.String, Desc: "param b"},
		}),
	}
	withSchema := countMessageTokens(nil, []*schema.ToolInfo{info})
	descOnly := estimateTextTokens(info.Name) + estimateTextTokens(info.Desc) + 8
	descOnly = descOnly * 115 / 100
	if withSchema <= descOnly {
		t.Fatalf("tool schema should be counted: withSchema=%d descOnly=%d", withSchema, descOnly)
	}
}

// TestPermDecisionCacheRevisionIsolation 验证权限修订号变化后旧决策不复用。
func TestPermDecisionCacheRevisionIsolation(t *testing.T) {
	cache := &permDecisionCache{entries: make(map[string]permDecisionEntry)}
	key1 := permDecisionCacheKey("u1", "c1", 1, "query_data", "select 1")
	key2 := permDecisionCacheKey("u1", "c1", 2, "query_data", "select 1")
	cache.Set(key1, &PermDecisionOutput{Allowed: true, Reason: "ok"})
	if _, ok := cache.Get(key2); ok {
		t.Fatal("revision changed: cached decision must not be reused")
	}
	if d, ok := cache.Get(key1); !ok || !d.Allowed {
		t.Fatal("same revision should hit cache")
	}
	cache.Clear()
	if _, ok := cache.Get(key1); ok {
		t.Fatal("clear should drop all entries")
	}
}

// TestCheckpointPromptRoundTrip 验证 Resume 可复用发起中断时的系统提示词。
func TestCheckpointPromptRoundTrip(t *testing.T) {
	store := &InMemoryCheckPointStore{items: make(map[string]*cpEntry), maxAge: time.Minute, maxEntries: 8}
	store.Bind("cp1", CheckpointBinding{UserID: "u1", SessionID: "s1", Prompt: "SYSTEM-PROMPT"})
	if got := store.Prompt("cp1"); got != "SYSTEM-PROMPT" {
		t.Fatalf("prompt roundtrip failed: %q", got)
	}
	store.Release("cp1", false)
	if got := store.Prompt("cp1"); got != "" {
		t.Fatal("released checkpoint should not expose prompt")
	}
}

// TestDenyByCapability_ExportByCodeRequiresModify 验证只读用户无法执行高级导出代码。
func TestDenyByCapability_ExportByCodeRequiresModify(t *testing.T) {
	readOnly := &PermissionMiddleware{Scope: &PermissionScope{IsRemote: true, AllowModify: false}}
	if err := readOnly.denyByCapability("export_by_code"); err == nil {
		t.Fatal("read-only scope must be denied export_by_code")
	}
	writable := &PermissionMiddleware{Scope: &PermissionScope{IsRemote: true, AllowModify: true}}
	if err := writable.denyByCapability("export_by_code"); err != nil {
		t.Fatalf("writable scope should pass capability check: %v", err)
	}
	// 双门槛生效后：仅 allow_modify、未开 AI 写能力也必须拒绝。
	strict := &PermissionMiddleware{Scope: &PermissionScope{IsRemote: true, AllowModify: true, AIAllowModify: false, AIPolicyEnforce: true}}
	if err := strict.denyByCapability("export_by_code"); err == nil {
		t.Fatal("enforced AI policy without ai_allow_modify must deny export_by_code")
	}
	if exportByCodeAllowed(&PermissionScope{AllowModify: false, HasFullConnAccess: true}) {
		t.Fatal("exportByCodeAllowed must require AllowModify")
	}
	if exportByCodeAllowed(&PermissionScope{AllowModify: true, AIAllowModify: false, AIPolicyEnforce: true, HasFullConnAccess: true}) {
		t.Fatal("exportByCodeAllowed must require CanAIModifyData")
	}
}

// TestUploadFSBackendCrossUserDenied 直接验证文件工具（read_file/ls 委托后端）不能跨用户访问。
func TestUploadFSBackendCrossUserDenied(t *testing.T) {
	owner := "phase2-upload-owner"
	other := "phase2-upload-other"
	dir := filepath.Join("exports", "users", export.SandboxUserSegment(owner))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir owner exports: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("owner-only"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	backend := export.NewOSFilesystemBackend().CloneForUser(other)
	fs := newUploadFSBackend(backend, other)
	if _, err := fs.Read(context.Background(), &adkfilesystem.ReadRequest{FilePath: secret}); err == nil {
		t.Fatal("read_file must not read another user's export")
	}
	if _, err := fs.LsInfo(context.Background(), &adkfilesystem.LsInfoRequest{Path: "exports"}); err == nil {
		t.Fatal("ls must not enumerate the shared exports root")
	}
	if err := fs.Write(context.Background(), &adkfilesystem.WriteRequest{FilePath: "exports/.owners/victim.owner", Content: "u2"}); err == nil {
		t.Fatal("write_file must not overwrite .owners")
	}
}

// TestExportByCodeDispatchDeniedSyncAndStream 覆盖 export_by_code 的同步/流式能力拒绝。
func TestExportByCodeDispatchDeniedSyncAndStream(t *testing.T) {
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		Profile: ProfileWebRemote, AllowModify: false,
		FullAccessSchemas: map[string]bool{}, AllowedTables: map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileDesktopLocal, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope}, Masker: NewNoopMasker(),
	}
	ctx := WithRunSecurity(context.Background(), sec)
	mw := &PermissionMiddleware{Scope: scope, ConnID: "c1"}

	invokable := func(_ context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil }
	ep, err := mw.WrapInvokableToolCall(ctx, invokable, &adk.ToolContext{Name: "export_by_code"})
	if err != nil {
		t.Fatalf("wrap invokable failed: %v", err)
	}
	if _, callErr := ep(ctx, `{"code":"print(1)","outputFile":"a.txt"}`); !isPermissionError(callErr) {
		t.Fatalf("read-only scope must deny export_by_code sync call, got %v", callErr)
	}

	streamable := func(_ context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
		return schema.StreamReaderFromArray([]string{"ok"}), nil
	}
	streamEp, err := mw.WrapStreamableToolCall(ctx, streamable, &adk.ToolContext{Name: "export_by_code"})
	if err != nil {
		t.Fatalf("wrap streamable failed: %v", err)
	}
	if _, callErr := streamEp(ctx, `{"code":"print(1)","outputFile":"a.txt"}`); !isPermissionError(callErr) {
		t.Fatalf("read-only scope must deny export_by_code stream call, got %v", callErr)
	}
}

// TestToolPolicy_ToolSearchRegistered 防止框架注入的 tool_search 再次因未登记而 fail-closed。
func TestToolPolicy_ToolSearchRegistered(t *testing.T) {
	policy, ok := lookupToolPolicy("tool_search")
	if !ok {
		t.Fatal("tool_search must be registered to avoid fail-closed rejection")
	}
	if policy.Capability != CapabilityNone {
		t.Fatalf("tool_search should be CapabilityNone, got %s", policy.Capability)
	}
}

// TestAuthorizeAltConnQueryUsesTargetScope 验证自动路由使用目标连接 Scope，
// 默认连接全权不能放宽受限连接的表权限。
func TestAuthorizeAltConnQueryUsesTargetScope(t *testing.T) {
	full := &PermissionScope{IsRemote: false, AllowModify: true}
	restricted := &PermissionScope{
		UserID: "u1", ConnID: "B", IsRemote: true, Profile: ProfileWebRemote,
		FullAccessSchemas: map[string]bool{}, AllowedTables: map[string]bool{"s1.orders": true},
	}
	sec := &RunSecurityContext{
		UserID: "u1", DefaultConnID: "A", Masker: NewNoopMasker(),
		Scopes: map[string]*PermissionScope{"A": full, "B": restricted},
	}
	if err := authorizeAltConnQuery(sec, "B", "SELECT id FROM s1.secret"); err == nil {
		t.Fatal("restricted target connection must deny unauthorized table")
	}
	if err := authorizeAltConnQuery(sec, "B", "SELECT id FROM s1.orders"); err != nil {
		t.Fatalf("authorized table should pass: %v", err)
	}
}

// TestValidateContentSQLBindingsUsesTargetScope 验证报告内嵌 SQL 按 ref.connId 取目标 Scope。
func TestValidateContentSQLBindingsUsesTargetScope(t *testing.T) {
	full := &PermissionScope{IsRemote: false, AllowModify: true}
	restricted := &PermissionScope{
		UserID: "u1", ConnID: "B", IsRemote: true, Profile: ProfileWebRemote,
		FullAccessSchemas: map[string]bool{}, AllowedTables: map[string]bool{"s1.orders": true},
	}
	sec := &RunSecurityContext{
		UserID: "u1", DefaultConnID: "A", Masker: NewNoopMasker(),
		Scopes: map[string]*PermissionScope{"A": full, "B": restricted},
	}
	mw := &PermissionMiddleware{
		Scope: full, ConnID: "A",
		Schemas: []SchemaRef{{ConnID: "B", Schema: "s1"}},
	}
	ctx := WithRunSecurity(context.Background(), sec)
	bad := "```chart\n{\"sql\":\"SELECT id FROM s1.secret\",\"connId\":\"B\"}\n```"
	if err := mw.validateContentSQLBindings(ctx, "export_html", bad); err == nil {
		t.Fatal("embedded SQL against restricted target connection must be denied")
	}
	good := "```chart\n{\"sql\":\"SELECT id FROM s1.orders\",\"connId\":\"B\"}\n```"
	if err := mw.validateContentSQLBindings(ctx, "export_html", good); err != nil {
		t.Fatalf("authorized embedded SQL should pass: %v", err)
	}
}

// TestPermAgentMode 验证权限 Agent 运行模式开关（含 shadow）。
func TestPermAgentMode(t *testing.T) {
	cases := map[string]string{
		"":         "fallback",
		"on":       "on",
		"off":      "off",
		"shadow":   "shadow",
		"Fallback": "fallback",
	}
	for value, want := range cases {
		t.Setenv("AGENT_PERM_AGENT", value)
		if got := permAgentMode(); got != want {
			t.Fatalf("AGENT_PERM_AGENT=%q: got %q, want %q", value, got, want)
		}
	}
}

// TestPromptToolReferencesRegistered 防止提示词引用的工具名与 ToolPolicy 注册表漂移。
func TestPromptToolReferencesRegistered(t *testing.T) {
	scope := &PermissionScope{IsRemote: false, AllowModify: true, AIAllowModify: true}
	prompt := buildSystemPrompt("c1", "mysql", "s1", "8.0", nil, scope, nil, true, nil)
	names := []string{
		"query_data", "get_table_schema", "list_tables", "read_file_data", "read_offload",
		"dataset_query", "cross_db_join", "analyze_data", "get_current_date_info",
		"export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt",
		"export_html", "export_by_code", "exec_sql", "skill", "execute",
	}
	for _, name := range names {
		if !strings.Contains(prompt, name) {
			continue
		}
		if _, ok := lookupToolPolicy(name); !ok {
			t.Fatalf("prompt references unregistered tool: %s", name)
		}
	}
}

// TestResolveResumePrompt 验证 Resume 优先复用 checkpoint 提示词，缺失时重建。
func TestResolveResumePrompt(t *testing.T) {
	scope := &PermissionScope{IsRemote: false, AllowModify: true, AIAllowModify: true}
	sec := &RunSecurityContext{
		UserID: "u1", DefaultConnID: "c1", Masker: NewNoopMasker(),
		Scopes: map[string]*PermissionScope{"c1": scope},
	}
	a := &SQLAgent{dbType: "mysql", dbSchema: "s1"}

	cachedID := "phase2-resume-cached"
	globalCheckPointStore.Bind(cachedID, CheckpointBinding{UserID: "u1", SessionID: "s1", Prompt: "CACHED-PROMPT"})
	defer globalCheckPointStore.Release(cachedID, false)
	if prompt, source := a.resolveResumePrompt(cachedID, sec, CheckpointBinding{ConnID: "c1"}, nil); prompt != "CACHED-PROMPT" || source != "cached" {
		t.Fatalf("checkpoint prompt not reused: source=%s prompt=%q", source, prompt)
	}

	rebuildID := "phase2-resume-rebuild"
	globalCheckPointStore.Bind(rebuildID, CheckpointBinding{UserID: "u1", SessionID: "s1"})
	defer globalCheckPointStore.Release(rebuildID, false)
	prompt, source := a.resolveResumePrompt(rebuildID, sec, CheckpointBinding{ConnID: "c1"}, nil)
	if source != "rebuilt" || !strings.Contains(prompt, "## 核心准则") {
		t.Fatalf("missing prompt should be rebuilt: source=%s promptLen=%d", source, len(prompt))
	}
	// 验证重建结果经 WithRunPrompt 后确实覆盖到 Instruction（即模型 system 消息）。
	runCtx := &adk.ChatModelAgentContext{}
	_, got, err := NewDynamicPromptMiddleware().BeforeAgent(WithRunPrompt(context.Background(), prompt), runCtx)
	if err != nil || got.Instruction != prompt {
		t.Fatalf("resume prompt must reach Instruction: err=%v", err)
	}
}

// TestSystemPromptBudget 是提示词体积门禁：超出预算说明又发生了重复注入/失效段落回潮。
func TestSystemPromptBudget(t *testing.T) {
	dbScope := &PermissionScope{IsRemote: false, AllowModify: true, AIAllowModify: true, AIPolicyEnforce: false}
	prompt := buildSystemPrompt("c1", "mysql", "s1", "8.0", nil, dbScope, nil, false, nil)
	t.Logf("prompt budget (runes): static=%d dynamic=%d agents=%d total=%d",
		utf8.RuneCountInString(buildStaticPromptPart("mysql", false)),
		utf8.RuneCountInString(buildDynamicPromptPart("c1", "mysql", "s1", "8.0", nil, dbScope, nil, false, nil)),
		utf8.RuneCountInString(buildAgentsMDSection()), utf8.RuneCountInString(prompt))
	if n := utf8.RuneCountInString(prompt); n > 12000 {
		t.Fatalf("system prompt too large: %d runes", n)
	}
	if strings.Contains(prompt, "## SQL 规范（）") {
		t.Fatal("db prompt must not contain empty dialect header")
	}

	fileOnly := buildSystemPrompt("", "", "", "", nil, &PermissionScope{IsRemote: true}, nil, false, nil)
	if strings.Contains(fileOnly, "## SQL 规范") {
		t.Fatal("file-only prompt must not contain SQL dialect rules")
	}
	if !strings.Contains(fileOnly, "read_file_data") {
		t.Fatal("file-only prompt should mention read_file_data")
	}
}
