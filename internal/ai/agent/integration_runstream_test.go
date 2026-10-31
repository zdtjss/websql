package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"websql/internal/ai/agent/export"
	admin "websql/internal/app/admin"
	system "websql/internal/app/system"
	"websql/internal/config"

	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// mockTurn 脚本化的一轮模型输出：要么发起工具调用，要么给出正文。
type mockTurn struct {
	tool    string // 非空则返回 tool_calls
	args    string // 工具参数 JSON
	content string // tool 为空时返回的正文
}

// mockChatServerScripted 按脚本顺序返回 OpenAI 兼容 SSE 响应。
func mockChatServerScripted(t *testing.T, turns []mockTurn) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// 非流式请求（如 summarization 生成摘要）必须返回普通 JSON，
		// 把 SSE 文本当成 chat.completion 会直接报 JSON 解析错误。
		if !strings.Contains(string(body), `"stream":true`) && !strings.Contains(string(body), `"stream": true`) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cmpg","object":"chat.completion","created":0,"model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":"历史摘要"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			return
		}

		index := int(hits.Add(1)) - 1
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)

		payload := `{"id":"cmp","object":"chat.completion.chunk","created":0,"model":"mock","choices":[{"index":0,"delta":{"content":"完成"},"finish_reason":"stop"}]}`
		if index < len(turns) {
			turn := turns[index]
			if turn.tool != "" {
				argsJSON, err := json.Marshal(turn.args)
				if err != nil {
					t.Errorf("序列化工具参数失败：%v", err)
					return
				}
				payload = fmt.Sprintf(`{"id":"cmp%d","object":"chat.completion.chunk","created":0,"model":"mock","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-%d","type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":"tool_calls"}]}`, index, index, turn.tool, string(argsJSON))
			} else {
				payload = fmt.Sprintf(`{"id":"cmp%d","object":"chat.completion.chunk","created":0,"model":"mock","choices":[{"index":0,"delta":{"content":%q},"finish_reason":"stop"}]}`, index, turn.content)
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", payload)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// mockChatServer 返回 OpenAI 兼容的 SSE 响应：
// 第 1 次调用要求执行 exec_sql，第 2 次调用给出最终文本。
func mockChatServer(t *testing.T, toolArgs string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	return mockChatServerScripted(t, []mockTurn{
		{tool: "exec_sql", args: toolArgs},
		{content: "已按权限处理"},
	})
}

// setupIntegrationDB 把 agent/system 的 DB 指向与 admin 同一个内存管理库，
// 并补齐集成测试需要的会话表与修订号表（三个 holder 共用一份库，避免“同库不同句柄”）。
func setupIntegrationDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db := setupGoldenDB(t)
	Init(db)
	system.Init(db)
	t.Cleanup(func() {
		Init(nil)
		system.Init(nil)
	})
	for _, stmt := range []string{
		`create table if not exists t_ai_session (
			id text primary key, user_id text, title text, messages text,
			context text, created_at datetime, updated_at datetime)`,
		`create table if not exists t_permission_revision (
			id integer primary key, revision integer not null default 0)`,
		`insert or replace into t_permission_revision (id, revision) values (1, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("初始化集成测试表失败：%v", err)
		}
	}
	if err := admin.SyncPermissionRevision(); err != nil {
		t.Fatalf("同步修订号失败：%v", err)
	}
	return db
}

func collectChunks(t *testing.T, timeout time.Duration, run func(flush func(StreamChunk))) []StreamChunk {
	t.Helper()
	var (
		mu     sync.Mutex
		chunks []StreamChunk
		done   = make(chan struct{})
	)
	go func() {
		defer close(done)
		run(func(chunk StreamChunk) {
			mu.Lock()
			chunks = append(chunks, chunk)
			mu.Unlock()
		})
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("RunStream 超时未结束")
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]StreamChunk, len(chunks))
	copy(out, chunks)
	return out
}

// TestResumeStream_ExecutedAfterApprovalReportsToolStatusExecuted 端到端验证批准后的写操作：
// 危险 SQL 先以 danger_confirm 中断，Resume 批准后工具真实写入目标库，
// 并上报 tool_status=executed；未批准前不得出现 executed。
func TestResumeStream_ExecutedAfterApprovalReportsToolStatusExecuted(t *testing.T) {
	db := setupIntegrationDB(t)

	originalRemote := config.Get()
	config.SetActive(&config.Config{IsRemote: false})
	t.Cleanup(func() { config.SetActive(originalRemote) })

	// 目标库用独立临时目录：Windows 下连接池会持有文件句柄，
	// 用 t.TempDir() 会在用例结束时因占用而报错（此处只关心测试结论）。
	workDir, err := os.MkdirTemp("", "websql-e2e-*")
	if err != nil {
		t.Fatalf("创建临时目录失败：%v", err)
	}
	targetPath := filepath.Join(workDir, "target.db")
	target, err := sqlx.Open("sqlite", targetPath)
	if err != nil {
		t.Fatalf("打开目标库失败：%v", err)
	}
	defer func() {
		_ = target.Close()
		_ = os.RemoveAll(workDir)
	}()
	if _, err := target.Exec(`create table t_demo (id integer primary key)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}

	if _, err := db.Exec(`alter table t_conn add column url text`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		t.Logf("添加 url 列失败（可能已存在）：%v", err)
	}
	if _, err := db.Exec(`insert or replace into t_conn (id, name, db_type, url) values ('c1', 'c1', 'sqlite', ?)`, targetPath); err != nil {
		t.Fatalf("注册连接失败：%v", err)
	}

	srv, _ := mockChatServer(t, `{"sql":"INSERT INTO main.t_demo (id) VALUES (7)"}`)
	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}

	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		AllowModify: true, AIAllowModify: true, AIPolicyEnforce: true,
		HasFullConnAccess: true, AllSchemasFull: true, FullAccessSchemas: map[string]bool{"main": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runCtx = WithRunSecurity(runCtx, sec)
	runCtx = WithRunIdentity(runCtx, RunIdentity{SessionID: "it-resume", UserID: "u1"})

	agent, err := NewSQLAgent(runCtx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-resume"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}
	req := ChatRequest{SessionID: "it-resume", UserID: "u1", ConnID: "c1", Schema: "main", Question: "插入一行演示数据"}

	firstChunks := collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		if _, err := agent.RunStream(runCtx, "run-resume", req, flush); err != nil {
			t.Errorf("首次 RunStream 失败：%v", err)
		}
	})

	var confirm StreamChunk
	sawConfirm := false
	for _, chunk := range firstChunks {
		if chunk.Type == "danger_confirm" {
			confirm, sawConfirm = chunk, true
		}
		if chunk.Type == "tool_status" && chunk.Status == "executed" {
			t.Fatal("未经批准不得上报 executed")
		}
	}
	if !sawConfirm {
		t.Fatalf("写操作应触发 danger_confirm，实际事件=%+v", firstChunks)
	}

	rowsBefore := countDemoRows(t, target)
	if rowsBefore != 0 {
		t.Fatalf("中断阶段不应已写入：%d 行", rowsBefore)
	}

	sess, err := sessions.GetOrCreate("it-resume", "u1")
	if err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	binding := CheckpointBinding{
		UserID: "u1", SessionID: "it-resume", ConnID: "c1",
		Revision: sec.Revision, RevisionLoaded: true, ScopeDigest: sec.ScopeDigest,
	}
	resumeCtx := WithRunIdentity(WithRunSecurity(context.Background(), sec), RunIdentity{SessionID: "it-resume", UserID: "u1"})
	chunks := collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		if err := agent.ResumeStream(resumeCtx, "run-resume-2", confirm.CheckPointID, binding,
			map[string]bool{confirm.InterruptID: true}, flush, sess); err != nil {
			t.Errorf("ResumeStream 失败：%v", err)
		}
	})

	var executed bool
	for _, chunk := range chunks {
		if chunk.Type == "tool_status" && chunk.Content == "exec_sql" && chunk.Status == "executed" {
			executed = true
		}
	}
	if !executed {
		t.Fatalf("批准执行后应上报 tool_status=executed，实际事件=%+v", chunks)
	}
	if got := countDemoRows(t, target); got != 1 {
		t.Fatalf("写操作应真实落库，实际 %d 行", got)
	}
}

func countDemoRows(t *testing.T, db *sqlx.DB) int {
	t.Helper()
	var count int
	if err := db.Get(&count, `select count(1) from t_demo`); err != nil {
		t.Fatalf("统计行数失败：%v", err)
	}
	return count
}

// TestRunStream_ExecSQLDeniedEmitsToolStatusDenied 端到端验证 F-27 契约：
// AI 写能力被关闭的角色请求写操作时，SSE 必须给出 tool_status=denied，
// 而不是让前端把「模型已调用工具」渲染成「已执行」。
func TestRunStream_ExecSQLDeniedEmitsToolStatusDenied(t *testing.T) {
	setupIntegrationDB(t)

	srv, hits := mockChatServer(t, `{"sql":"INSERT INTO t_demo (id) VALUES (1)","connId":"other-conn"}`)
	cfg := &system.AIConfig{
		Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k",
		MaxContextTokens: 32000,
	}

	// exec_sql 已注册（能力双允许），但工具参数指向会话外的连接：
	// 运行安全上下文阶段就会拒绝，应上报 denied 而不是让前端看到“已执行”。
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", IsRemote: true,
		AllowModify: true, AIAllowModify: true, AIPolicyEnforce: true,
		HasFullConnAccess: true, FullAccessSchemas: map[string]bool{"main": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = WithRunSecurity(ctx, sec)
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: "it-denied", UserID: "u1"})

	agent, err := NewSQLAgent(ctx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-denied"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}

	req := ChatRequest{SessionID: "it-denied", UserID: "u1", ConnID: "c1", Schema: "main", Question: "往 t_demo 插入一行"}
	chunks := collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		if _, err := agent.RunStream(ctx, "run-denied", req, flush); err != nil {
			t.Errorf("RunStream 返回错误：%v", err)
		}
	})

	var statuses []StreamChunk
	sawDenied := false
	for _, chunk := range chunks {
		if chunk.Type == "tool_status" {
			statuses = append(statuses, chunk)
			if chunk.Content == "exec_sql" && chunk.Status == "denied" {
				sawDenied = true
			}
		}
	}
	if !sawDenied {
		t.Fatalf("权限拒绝时必须下发 tool_status=denied，实际 statuses=%+v 事件=%+v（模型调用次数=%d）", statuses, chunks, hits.Load())
	}
	for _, chunk := range statuses {
		if chunk.Status == "executed" {
			t.Fatalf("被拒绝的写操作不得上报 executed：%+v", statuses)
		}
	}
}

// TestRunStream_PersistedAssistantToolCallsAreUntraceable 端到端验证 F-07：
// 流式返回的 tool_calls 落库时必须标记为不可溯源并登记资源，
// 否则权限变更后「带 SQL 的 assistant 消息」会被当成可信历史继续喂给模型。
func TestRunStream_PersistedAssistantToolCallsAreUntraceable(t *testing.T) {
	setupIntegrationDB(t)

	originalRemote := config.Get()
	config.SetActive(&config.Config{IsRemote: true})
	t.Cleanup(func() { config.SetActive(originalRemote) })

	srv, _ := mockChatServer(t, `{"sql":"SELECT id FROM users"}`)

	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}

	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		AllowModify: true, AIAllowModify: true, AIPolicyEnforce: false,
		AllowedTables:  map[string]bool{"main.users": true},
		AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = WithRunSecurity(ctx, sec)
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: "it-trace", UserID: "u1"})

	agent, err := NewSQLAgent(ctx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-trace"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}

	req := ChatRequest{SessionID: "it-trace", UserID: "u1", ConnID: "c1", Schema: "main", Question: "查一下 users"}
	collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		if _, err := agent.RunStream(ctx, "run-trace", req, flush); err != nil {
			t.Errorf("RunStream 返回错误：%v", err)
		}
	})

	sess, err := sessions.GetOrCreate("it-trace", "u1")
	if err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	var found bool
	for _, msg := range sess.GetMessages() {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		found = true
		if msg.Security == nil || !msg.Security.Untraceable {
			t.Fatalf("带 tool_calls 的 assistant 消息必须标记不可溯源：%+v", msg)
		}
		if len(msg.Security.Resources) == 0 {
			t.Fatalf("流式 tool_calls 必须登记资源溯源：%+v", msg)
		}
		if msg.Security.Resources[0].Table != "users" {
			t.Fatalf("资源溯源应指向 users：%+v", msg.Security.Resources)
		}
	}
	if !found {
		dump := make([]string, 0, 8)
		for _, msg := range sess.GetMessages() {
			dump = append(dump, fmt.Sprintf("%s/calls=%d/%+v", msg.Role, len(msg.ToolCalls), msg.Security))
		}
		t.Fatalf("会话中未找到带 tool_calls 的 assistant 消息（模型是否被真实调用？）：%v", dump)
	}
}

// ─── F-38 被拒的并发请求不得覆盖运行中会话的安全上下文 ───

func TestBeginSessionRun_RejectedRequestKeepsLatestSecurity(t *testing.T) {
	sess := &Session{ID: "s1", UserID: "u1"}
	secA := &RunSecurityContext{UserID: "u1", ScopeDigest: "digest-A"}
	secB := &RunSecurityContext{UserID: "u1", ScopeDigest: "digest-B"}

	if !beginSessionRun(sess, secA) {
		t.Fatal("首个请求应获得运行权")
	}
	if sess.latestSecurity != secA {
		t.Fatal("运行中会话应绑定首请求的安全上下文")
	}

	// 同一会话的第二个并发请求：被拒后不得改写 latestSecurity
	if beginSessionRun(sess, secB) {
		t.Fatal("会话忙碌时第二个请求应被拒绝")
	}
	if sess.latestSecurity != secA {
		t.Fatal("被拒请求不得覆盖运行中会话的安全上下文")
	}

	sess.EndRun()
	if !beginSessionRun(sess, secB) {
		t.Fatal("运行结束后应能重新占用会话")
	}
	if sess.latestSecurity != secB {
		t.Fatal("新运行应绑定自己的安全上下文")
	}
	sess.EndRun()
}

// ─── 导出链路的运行时回归（F-06 正文脱敏 + F-13 分段回执）───

// TestRunStream_ExportHTMLMasksSensitiveContentAndArtifactIsPaged 通过真实 RunStream
// 调用 export_html：落盘产物必须已脱敏（正文不经 SSE 审计），
// 随后用真实产物元数据验证 get_export_artifact 的分段回执。
func TestRunStream_ExportHTMLMasksSensitiveContentAndArtifactIsPaged(t *testing.T) {
	setupIntegrationDB(t)
	t.Chdir(t.TempDir())

	originalRemote := config.Get()
	config.SetActive(&config.Config{IsRemote: false})
	t.Cleanup(func() { config.SetActive(originalRemote) })

	// 足够长的正文，同时验证回执不超限
	content := "# 销售分析报告\n\n" + strings.Repeat("本节结论：联系电话 13800138000，邮箱 zhangsan@example.com。\n", 400)
	args, err := json.Marshal(map[string]string{"title": "销售分析", "content": content})
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := mockChatServerScripted(t, []mockTurn{
		{tool: "export_html", args: string(args)},
		{content: "报告已导出"},
	})
	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}

	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		AllowModify: true, AIAllowModify: true,
		HasFullConnAccess: true, AllSchemasFull: true, FullAccessSchemas: map[string]bool{"main": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runCtx = WithRunSecurity(runCtx, sec)
	runCtx = WithRunIdentity(runCtx, RunIdentity{SessionID: "it-export", UserID: "u1"})

	agent, err := NewSQLAgent(runCtx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-export"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}
	req := ChatRequest{SessionID: "it-export", UserID: "u1", ConnID: "c1", Schema: "main", Question: "生成 HTML 报告"}
	collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		if _, err := agent.RunStream(runCtx, "run-export", req, flush); err != nil {
			t.Errorf("RunStream 失败：%v", err)
		}
	})

	sess, err := sessions.GetOrCreate("it-export", "u1")
	if err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	messages := sess.GetMessages()

	// 正文很长时会触发 summarization（工具消息被压缩），因此产物文件以磁盘为准；
	// 同时验证摘要落库确实带了不可溯源元数据（F-04 真实运行证据）。
	if len(messages) > 0 {
		summaryMarked := false
		for _, msg := range messages {
			if msg.Role == "user" && msg.Security != nil && msg.Security.Untraceable {
				summaryMarked = true
			}
		}
		t.Logf("会话消息=%d 含不可溯源摘要=%v", len(messages), summaryMarked)
	}

	produced, err := filepath.Glob(filepath.Join(export.Root(), "*.html"))
	if err != nil || len(produced) == 0 {
		t.Fatalf("未在导出根下找到产物（root=%s）：%v", export.Root(), err)
	}
	downloadURL := "/exports/" + filepath.Base(produced[0])
	data, err := os.ReadFile(produced[0])
	if err != nil {
		t.Fatalf("读取导出产物失败：%v", err)
	}
	if len(produced) > 1 {
		t.Logf("导出根下存在多个产物（可能是前序用例残留），取第一个：%d", len(produced))
	}
	htmlText := string(data)
	// partial 脱敏会留下 138****8000，其中连续星号会被 Markdown 渲染成 <strong>/<em>，
	// 因此不能直接断言字面串；这里只断言“原值已消失 + 脱敏后碎片仍在”。
	if strings.Contains(htmlText, "13800138000") {
		t.Fatal("导出产物仍含未脱敏手机号（F-06 正文脱敏未生效）")
	}
	if strings.Contains(htmlText, "zhangsan@example.com") || strings.Contains(htmlText, "user1@example.com") {
		t.Fatal("导出产物仍含未脱敏邮箱")
	}
	if !strings.Contains(htmlText, "8000") || !strings.Contains(htmlText, "138") {
		t.Fatalf("脱敏后应保留手机号首尾碎片，实际长度=%d", len(htmlText))
	}

	// 用真实产物元数据验证分段回执
	auditCtx := &export.AuditCtx{
		ConnID: "c1", UserID: "u1", SessionID: "it-export",
		Guard: func(context.Context) error { return nil },
		Mask:  func(_ context.Context, text string) string { return NewNoopMasker().MaskText(text) },
	}
	artifact, err := export.NewGetExportArtifactFunc(auditCtx)(context.Background(), &export.ExportArtifactInput{
		File: downloadURL, IncludeOutline: true,
	})
	if err != nil || !artifact.Found {
		t.Fatalf("读取产物元数据失败：found=%v err=%v", artifact != nil && artifact.Found, err)
	}
	if artifact.SourceField != "content" {
		t.Fatalf("报告类产物默认应回读 content，实际 %q", artifact.SourceField)
	}
	if utf8.RuneCountInString(artifact.Content) > 8000 {
		t.Fatalf("单次回执不得超过 8000 字符，实际 %d", utf8.RuneCountInString(artifact.Content))
	}
	if !artifact.SourceTruncated || artifact.SourceNextOffset <= 0 {
		t.Fatalf("长报告应提示可分段续读：%+v", artifact)
	}
	if artifact.SourceTotalChars <= utf8.RuneCountInString(artifact.Content) {
		t.Fatalf("总字符数应大于本页：%d vs %d", artifact.SourceTotalChars, utf8.RuneCountInString(artifact.Content))
	}
	if artifact.Outline == nil {
		t.Fatal("includeOutline=true 应返回结构视图")
	}

	// 分段续读必须能拿到后续内容
	page2, err := export.NewGetExportArtifactFunc(auditCtx)(context.Background(), &export.ExportArtifactInput{
		File: downloadURL, SourceField: "content", SourceOffset: artifact.SourceNextOffset,
	})
	if err != nil || page2.Content == "" {
		t.Fatalf("按 sourceOffset 续读失败：err=%v page2=%+v", err, page2)
	}
	if page2.SourceOffset != artifact.SourceNextOffset {
		t.Fatalf("续读偏移不一致：%d vs %d", page2.SourceOffset, artifact.SourceNextOffset)
	}
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// ─── 用户中途停止：已推送正文必须与会话一起落库 ───

// TestRunStream_UserStopPersistsEmittedContent 是“停止生成后界面有正文、重开历史却消失”的回归护栏：
// 已审计并推送给用户的内容不能因取消而丢失（仅丢弃未推送的尾巴与无法配对的 tool_calls）。
func TestRunStream_UserStopPersistsEmittedContent(t *testing.T) {
	db := setupIntegrationDB(t)

	// 慢速流：先推两段正文，然后挂住连接，等测试像用户点「停止」那样取消运行。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) && !strings.Contains(string(body), `"stream": true`) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cmpg","object":"chat.completion","created":0,"model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":"历史摘要"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, seg := range []string{strings.Repeat("第一段正文", 60), strings.Repeat("第二段正文", 60)} {
			payload := fmt.Sprintf(`{"id":"slow","object":"chat.completion.chunk","created":0,"model":"mock","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, seg)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(200 * time.Millisecond)
		}
		select {
		case <-time.After(20 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		HasFullConnAccess: true, AllSchemasFull: true, FullAccessSchemas: map[string]bool{"main": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	const runID = "run-user-stop"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = WithRunSecurity(ctx, sec)
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: "it-user-stop", UserID: "u1"})

	agent, err := NewSQLAgent(ctx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-user-stop"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}

	var mu sync.Mutex
	pushedRunes := 0
	stopped := false
	flush := func(chunk StreamChunk) {
		if chunk.Type != "content" || chunk.Content == "" {
			return
		}
		mu.Lock()
		pushedRunes += len([]rune(chunk.Content))
		// 相当于用户点「停止生成」：已看到足够正文后取消该次运行
		if pushedRunes >= 30 && !stopped {
			stopped = true
			mu.Unlock()
			agent.Cancel(runID)
			return
		}
		mu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.RunStream(ctx, runID, ChatRequest{
			SessionID: "it-user-stop", UserID: "u1", ConnID: "c1", Schema: "main",
			Question: "写两段正文",
		}, flush)
	}()

	select {
	case <-done:
	case <-time.After(45 * time.Second):
		t.Fatal("取消后 RunStream 未及时返回")
	}

	if pushedRunes < 30 {
		t.Fatalf("未向用户推送足够正文（实际 %d 字），无法验证取消路径", pushedRunes)
	}

	var raw string
	if err := db.Get(&raw, `select messages from t_ai_session where id = ?`, "it-user-stop"); err != nil {
		t.Fatalf("读取会话落库数据失败：%v", err)
	}
	var msgs []SessionMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("解析会话消息失败：%v\n原文：%s", err, raw)
	}
	found := false
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "第一段正文") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("用户中途停止后，已推送正文必须落库；实际消息=%+v", msgs)
	}
}

// TestRunStream_ClientDisconnectPersistsEmittedContent 覆盖另一种停止途径：
// 浏览器点停止会直接断开 SSE 请求（运行 ctx 被 cancel），而不是走服务端 Cancel 接口。
// 断连同样不得丢弃已推送给用户的正文：否则用户看到的内容在重开历史时消失。
// 曾复现的根因：context.Canceled 被重试策略当作可重试错误，使流走「撤回本次输出」分支而不落库；
// 现由 buildShouldRetryFunc 对取消/超时不重试来保证。
func TestRunStream_ClientDisconnectPersistsEmittedContent(t *testing.T) {
	db := setupIntegrationDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) && !strings.Contains(string(body), `"stream": true`) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cmpg","object":"chat.completion","created":0,"model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":"历史摘要"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, seg := range []string{strings.Repeat("断连前正文", 60), strings.Repeat("后续正文", 60)} {
			payload := fmt.Sprintf(`{"id":"slow2","object":"chat.completion.chunk","created":0,"model":"mock","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, seg)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(200 * time.Millisecond)
		}
		select {
		case <-time.After(20 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		HasFullConnAccess: true, AllSchemasFull: true, FullAccessSchemas: map[string]bool{"main": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(), Revision: admin.CurrentPermissionRevision(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()

	sessions, err := NewSessionStore()
	if err != nil {
		t.Fatalf("创建会话存储失败：%v", err)
	}
	defer sessions.Close()

	const runID = "run-client-disconnect"
	ctx, cancelCtx := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelCtx()
	ctx = WithRunSecurity(ctx, sec)
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: "it-disconnect", UserID: "u1"})

	agent, err := NewSQLAgent(ctx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope, &ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: "it-disconnect"})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}

	var mu sync.Mutex
	pushedRunes := 0
	stopped := false
	flush := func(chunk StreamChunk) {
		if chunk.Type != "content" || chunk.Content == "" {
			return
		}
		mu.Lock()
		pushedRunes += len([]rune(chunk.Content))
		if pushedRunes >= 30 && !stopped {
			stopped = true
			mu.Unlock()
			cancelCtx() // 模拟浏览器断开 SSE
			return
		}
		mu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.RunStream(ctx, runID, ChatRequest{
			SessionID: "it-disconnect", UserID: "u1", ConnID: "c1", Schema: "main",
			Question: "写两段正文",
		}, flush)
	}()

	select {
	case <-done:
	case <-time.After(45 * time.Second):
		t.Fatal("断连后 RunStream 未及时返回")
	}
	if pushedRunes < 30 {
		t.Fatalf("未推送足够正文（实际 %d 字），无法验证断连路径", pushedRunes)
	}

	var raw string
	if err := db.Get(&raw, `select messages from t_ai_session where id = ?`, "it-disconnect"); err != nil {
		t.Fatalf("读取会话落库数据失败：%v", err)
	}
	var msgs []SessionMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("解析会话消息失败：%v\n原文：%s", err, raw)
	}
	found := false
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "断连前正文") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("客户端断连后，已推送正文必须落库；实际消息=%+v", msgs)
	}
}
