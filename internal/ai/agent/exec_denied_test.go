package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/system"
)

// exec_denied_test.go 补齐 F-27 的一条取证盲区（记录在整改文档 §8.8-G-2，编号 D-03）：
// ai_allow_modify=0 时 exec_sql 根本不下发，界面上的「权限拒绝」终态只能来自
// 「exec_sql 已注册、但因表级授权不足被拒」这条路径。既有集成用例只覆盖了连接级越权，
// 这里把表级拒绝也钉住：必须下发 tool_status=denied、不得上报 executed、
// 且拒绝发生在危险确认之前（否则界面只会停在“等待确认”而看不到拒绝原因）。

func TestRunStream_ExecSQLTableDeniedEmitsToolStatusDenied(t *testing.T) {
	db := setupIntegrationDB(t)

	args, err := json.Marshal(map[string]string{
		"sql":    "UPDATE main.secret_acct SET balance = balance WHERE id = -1",
		"connId": "c1",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := mockChatServer(t, string(args))
	cfg := &system.AIConfig{Provider: "openai", BaseURL: srv.URL, Model: "mock", ApiKey: "k", MaxContextTokens: 32000}

	// 表级授权只给 main.orders：写 secret_acct 必须被权限检查拒绝。
	// 双门槛通过（AIAllowModify=true），因此 exec_sql 会走到 checkExecAccess。
	scope := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "main", IsRemote: true,
		Profile:           ProfileWebRemote,
		AllowModify:       true,
		AIAllowModify:     true,
		AIPolicyEnforce:   true,
		HasFullConnAccess: false,
		AllSchemasFull:    false,
		FullAccessSchemas: map[string]bool{},
		AllowedTables:     map[string]bool{"main.orders": true},
		AllowedColumns:    map[string]map[string]bool{},
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

	const sessionID = "it-tbl-denied"
	const runID = "run-tbl-denied"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = WithRunSecurity(ctx, sec)
	ctx = WithRunIdentity(ctx, RunIdentity{SessionID: sessionID, UserID: "u1"})

	agent, err := NewSQLAgent(ctx, cfg, "c1", "sqlite", "main", "3", nil, sessions, scope,
		&ExecAuditCtx{ConnID: "c1", UserID: "u1", SessionID: sessionID})
	if err != nil {
		t.Fatalf("构建 Agent 失败：%v", err)
	}

	var mu sync.Mutex
	var statuses []StreamChunk
	sawDangerConfirm := false

	req := ChatRequest{SessionID: sessionID, UserID: "u1", ConnID: "c1", Schema: "main",
		Question: "把 secret_acct 的余额原样更新一遍"}
	collectChunks(t, 90*time.Second, func(flush func(StreamChunk)) {
		record := func(c StreamChunk) {
			switch c.Type {
			case "tool_status":
				mu.Lock()
				statuses = append(statuses, c)
				mu.Unlock()
			case "danger_confirm":
				mu.Lock()
				sawDangerConfirm = true
				mu.Unlock()
			}
			flush(c)
		}
		if _, err := agent.RunStream(ctx, runID, req, record); err != nil {
			t.Errorf("RunStream 返回错误：%v", err)
		}
	})

	mu.Lock()
	defer mu.Unlock()
	var sawDenied bool
	for _, c := range statuses {
		if c.Content == "exec_sql" && c.Status == "denied" {
			sawDenied = true
		}
		if c.Status == "executed" {
			t.Fatalf("表级无权的写操作不得上报 executed：%+v", statuses)
		}
	}
	if !sawDenied {
		t.Fatalf("表级拒绝必须下发 tool_status=denied（前端据此渲染「权限拒绝」），实际=%+v", statuses)
	}
	if sawDangerConfirm {
		t.Fatal("权限不足应在危险确认之前拦下，不应先弹确认再拒绝")
	}

	// 落库内容里也不得出现执行回执，确保语句确实没有被提交。
	var raw string
	if err := db.Get(&raw, `select messages from t_ai_session where id = ?`, sessionID); err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	var msgs []SessionMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("解析会话消息失败：%v", err)
	}
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "rows affected") {
			t.Fatalf("被拒绝的写操作不应产生执行回执：%s", m.Content)
		}
	}
}
