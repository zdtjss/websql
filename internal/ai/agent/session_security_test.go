package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"websql/internal/config"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// setupSessionSecurityDB 构建带 t_ai_session 表的独立管理库。
//
// 用例结束后把全局句柄复位为 nil，避免污染同包其他用例（getDBOrNil 会 recover 后返回 nil）。
func setupSessionSecurityDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	db.SetMaxOpenConns(1)
	schema := `create table t_ai_session (
		id text primary key,
		user_id text,
		title text,
		messages text,
		context text,
		updated_at datetime
	)`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	Init(db)
	t.Cleanup(func() { Init(nil) })
	return db
}

func seedSessionRow(t *testing.T, db *sqlx.DB, id, userID, messages string) {
	t.Helper()
	if _, err := db.Exec(`insert into t_ai_session (id, user_id, title, messages, context)
		values (?, ?, '', ?, '{}')`, id, userID, messages); err != nil {
		t.Fatalf("插入会话失败：%v", err)
	}
}

func readSessionMessages(t *testing.T, db *sqlx.DB, id string) (string, []SessionMessage) {
	t.Helper()
	var raw string
	if err := db.Get(&raw, `select messages from t_ai_session where id = ?`, id); err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	var msgs []SessionMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("解析会话消息失败：%v", err)
	}
	return raw, msgs
}

func testSecurityContext(userID string) *RunSecurityContext {
	// 零值 Profile 即远程严格模式，enforcing=true 时 UPDATE 必须带 user_id 条件。
	return &RunSecurityContext{UserID: userID, ScopeDigest: "digest-1"}
}

// TestDoSave_WritesOwnerScopedSanitizedCopy 落库写入净化副本且不回写内存态。
func TestDoSave_WritesOwnerScopedSanitizedCopy(t *testing.T) {
	db := setupSessionSecurityDB(t)
	seedSessionRow(t, db, "s-ok", "u1", "[]")

	sess := &Session{
		ID:     "s-ok",
		UserID: "u1",
		messages: []SessionMessage{
			{Role: "user", Content: "查询订单"},
			{Role: "assistant", Content: "已完成", ReasoningContent: "内部推理"},
			{Role: "tool", ToolName: "query_data", ToolCallID: "c1", Content: `{"rows":[1]}`},
		},
	}
	sess.latestSecurity = testSecurityContext("u1")

	sess.mu.Lock()
	err := sess.doSave()
	sess.mu.Unlock()
	if err != nil {
		t.Fatalf("归属会话写入应成功：%v", err)
	}

	_, stored := readSessionMessages(t, db, "s-ok")
	if len(stored) != 3 {
		t.Fatalf("落库消息数应为 3，实际 %d", len(stored))
	}
	if stored[0].Security == nil || !stored[0].Security.Sanitized {
		t.Fatal("用户文本应标记为已净化")
	}
	if stored[1].ReasoningContent != "" {
		t.Fatal("推理内容不得落库")
	}
	if stored[1].Security == nil || !stored[1].Security.Untraceable {
		t.Fatal("无溯源的 assistant 数据消息应标记为不可溯源")
	}
	if stored[2].Security == nil || !stored[2].Security.Untraceable {
		t.Fatal("无 ResourceRef 的工具结果应标记为不可溯源")
	}
	// 内存态保持不变：避免同一消息被反复脱敏导致展示层与模型看到不同内容。
	for i, msg := range sess.messages {
		if msg.Security != nil {
			t.Fatalf("第 %d 条内存消息不应被回写 Security", i)
		}
	}
}

// TestDoSave_RejectsForeignSession 远程模式下拒绝写入他人会话。
func TestDoSave_RejectsForeignSession(t *testing.T) {
	db := setupSessionSecurityDB(t)
	seedSessionRow(t, db, "s-foreign", "u2", "[]")

	sess := &Session{
		ID:             "s-foreign",
		UserID:         "u1",
		messages:       []SessionMessage{{Role: "user", Content: "越权写入"}},
		latestSecurity: testSecurityContext("u1"),
	}
	sess.mu.Lock()
	err := sess.doSave()
	sess.mu.Unlock()
	if !errors.Is(err, ErrSessionForbidden) {
		t.Fatalf("应返回 ErrSessionForbidden，实际 %v", err)
	}
	// 数据库保持旧值。
	if raw, _ := readSessionMessages(t, db, "s-foreign"); raw != "[]" {
		t.Fatalf("越权写入不得落库，实际 %s", raw)
	}
}

// TestDoSave_CompatibilityModeAllowsLegacyRow 兼容模式（无安全上下文）保留空归属会话写入。
func TestDoSave_CompatibilityModeAllowsLegacyRow(t *testing.T) {
	db := setupSessionSecurityDB(t)
	seedSessionRow(t, db, "s-legacy", "", "[]")

	sess := &Session{
		ID:       "s-legacy",
		UserID:   "u1",
		messages: []SessionMessage{{Role: "user", Content: "桌面端会话"}},
	}
	sess.mu.Lock()
	err := sess.doSave()
	sess.mu.Unlock()
	if err != nil {
		t.Fatalf("兼容模式应允许写入历史空归属会话：%v", err)
	}
	if _, stored := readSessionMessages(t, db, "s-legacy"); len(stored) != 1 {
		t.Fatalf("应写入 1 条消息，实际 %d", len(stored))
	}
}

// TestDoSave_RejectsOversizedSession 超过大小上限时终止写入并保留旧值。
func TestDoSave_RejectsOversizedSession(t *testing.T) {
	db := setupSessionSecurityDB(t)
	seedSessionRow(t, db, "s-big", "u1", "[]")

	sess := &Session{
		ID:     "s-big",
		UserID: "u1",
		messages: []SessionMessage{
			{Role: "user", Content: strings.Repeat("a", maxSessionBytes+1024)},
		},
		latestSecurity: testSecurityContext("u1"),
	}
	sess.mu.Lock()
	err := sess.doSave()
	sess.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "会话大小超过上限") {
		t.Fatalf("超限写入应被拒绝，实际 %v", err)
	}
	if raw, _ := readSessionMessages(t, db, "s-big"); raw != "[]" {
		t.Fatalf("超限写入不得落库，实际长度 %d", len(raw))
	}
}

// TestDoSave_AbortsOnSanitizeFailure 净化失败必须终止写入，不得回退到原始内容。
func TestDoSave_AbortsOnSanitizeFailure(t *testing.T) {
	db := setupSessionSecurityDB(t)
	seedSessionRow(t, db, "s-bad", "u1", "[]")

	sess := &Session{
		ID:             "s-bad",
		UserID:         "u1",
		messages:       []SessionMessage{{Role: "", Content: "缺少角色"}},
		latestSecurity: testSecurityContext("u1"),
	}
	sess.mu.Lock()
	err := sess.doSave()
	sess.mu.Unlock()
	if err == nil {
		t.Fatal("净化失败应返回错误")
	}
	if raw, _ := readSessionMessages(t, db, "s-bad"); raw != "[]" {
		t.Fatalf("净化失败不得落库，实际 %s", raw)
	}
}

// TestSessionBelongsTo_ProfileAware 归属判定按运行 Profile 区分空归属会话。
func TestSessionBelongsTo_ProfileAware(t *testing.T) {
	config.SetActive(&config.Config{IsRemote: true})
	if sessionBelongsTo("", "u1") {
		t.Fatal("远程模式必须拒绝无归属会话")
	}
	if !sessionBelongsTo("u1", "u1") {
		t.Fatal("归属一致应放行")
	}
	if sessionBelongsTo("u1", "u2") || sessionBelongsTo("u1", "") {
		t.Fatal("跨用户或无请求者必须拒绝")
	}

	config.SetActive(&config.Config{IsRemote: false})
	defer config.SetActive(&config.Config{IsRemote: true})
	if !sessionBelongsTo("", "u1") {
		t.Fatal("本地模式应保留无归属会话的兼容策略")
	}
}
