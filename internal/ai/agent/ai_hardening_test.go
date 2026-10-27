package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/export"
	"websql/internal/ai/agent/sqlutil"
	"websql/internal/ai/agent/uploadstore"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────────────────────
// 安全加固回归测试
//
// 对应本轮审核整改项，确保以下问题不复现：
//   1. 危险 SQL 识别的空白字符绕过（DROP\tTABLE / DELETE\nFROM）
//   2. CTE（WITH ... DELETE）写操作绕过审批
//   3. query_data 权限仅由 LLM 裁决（缺程序化兜底）
//   4. 结果集过滤 fail-open（解析失败原样返回）
//   5. 只读角色仍可使用写工具/命令执行工具
//   6. 跨连接越权（connId 切到未授权连接）
//   7. 共享 Agent 的 per-run 状态串号（提示词 / 会话同步）
// ──────────────────────────────────────────────────────────────

// ─── 1/2. 危险 SQL 检测与审批触发 ─────────────────────────────

func TestIsDangerousSQL_WhitespaceBypass(t *testing.T) {
	bypassCases := []string{
		"DROP\tTABLE users",
		"DELETE\nFROM users",
		"UPDATE\r\nusers SET name='x'",
		"TRUNCATE\tTABLE users",
		"insert\ninto users values(1)",
		"  \t DROP   TABLE users",
		"/* c */ DROP\tTABLE users",
	}
	for _, sql := range bypassCases {
		if !sqlutil.IsDangerousSQL(sql) {
			t.Errorf("IsDangerousSQL(%q) = false，空白字符绕过回归", sql)
		}
	}

	safeCases := []string{
		"SELECT * FROM users",
		"SHOW TABLES",
		"UPDATE_TIME", // 词级匹配不应把标识符当关键字
		"",
	}
	for _, sql := range safeCases {
		if sqlutil.IsDangerousSQL(sql) {
			t.Errorf("IsDangerousSQL(%q) = true，误判为写操作", sql)
		}
	}
}

func TestDetectSQLType_WhitespaceBypass(t *testing.T) {
	cases := map[string]sqlutil.SQLType{
		"DROP\tTABLE users":        sqlutil.SQLTypeDrop,
		"DELETE\nFROM users":       sqlutil.SQLTypeDelete,
		"ALTER\nTABLE t ADD c INT": sqlutil.SQLTypeAlter,
		"INSERT\tINTO t VALUES(1)": sqlutil.SQLTypeInsert,
		"SELECT 1":                 sqlutil.SQLTypeUnknown,
		"DELETEFROM t":             sqlutil.SQLTypeUnknown, // 非关键字整体
	}
	for sql, want := range cases {
		if got := sqlutil.DetectSQLType(sql); got != want {
			t.Errorf("DetectSQLType(%q) = %q, want %q", sql, got, want)
		}
	}
}

func TestContainsDangerousSQL_CTEAndMultiStatement(t *testing.T) {
	shouldIntercept := []string{
		"WITH x AS (SELECT 1) DELETE FROM users",
		"WITH x AS (SELECT 1)\nINSERT INTO t SELECT * FROM x",
		"WITH RECURSIVE r AS (SELECT 1) UPDATE users SET a=1",
		"SELECT 1; DROP TABLE users",
		"DROP\tTABLE users",
	}
	for _, sql := range shouldIntercept {
		if !containsDangerousSQL(sql) {
			t.Errorf("containsDangerousSQL(%q) = false，写操作绕过审批回归", sql)
		}
	}

	shouldPass := []string{
		"WITH x AS (SELECT id FROM users) SELECT * FROM x",
		"SELECT * FROM update_log",
	}
	for _, sql := range shouldPass {
		if containsDangerousSQL(sql) {
			t.Errorf("containsDangerousSQL(%q) = true，纯读语句被误判", sql)
		}
	}
}

// ─── 3. query_data 程序化双重校验 ─────────────────────────────

func newRemoteScope(allowedTables []string, allowedColumns map[string][]string) *PermissionScope {
	s := &PermissionScope{
		UserID:         "u1",
		ConnID:         "conn1",
		SchemaName:     "db1",
		IsRemote:       true,
		AllowedTables:  map[string]bool{},
		AllowedColumns: map[string]map[string]bool{},
	}
	for _, t := range allowedTables {
		s.AllowedTables[t] = true
	}
	for table, cols := range allowedColumns {
		m := map[string]bool{}
		for _, c := range cols {
			m[c] = true
		}
		s.AllowedColumns[table] = m
	}
	return s
}

func TestDoubleCheckReadAccess(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil)}

	// 有权表 → 放行
	if err := mw.doubleCheckReadAccess("SELECT id FROM orders", []string{"orders"}); err != nil {
		t.Errorf("有权表应放行，实际：%v", err)
	}
	// 无权表 → 拦截
	err := mw.doubleCheckReadAccess("SELECT * FROM secret", []string{"secret"})
	if err == nil {
		t.Fatal("无权表应被拦截，实际放行")
	}
	var pe *PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("错误类型应为 *PermissionError，实际 %T", err)
	}
	if len(permErrorObjects(err)) != 1 || permErrorObjects(err)[0] != "secret" {
		t.Errorf("拦截对象应为 secret，实际 %v", pe.Objects)
	}
	// 提取不到表名 → 不误拦（由结果过滤兜底）
	if err := mw.doubleCheckReadAccess("SELECT 1", nil); err != nil {
		t.Errorf("无法提取表名时不应拦截，实际：%v", err)
	}
}

// ─── 4. 结果集过滤 fail-closed ────────────────────────────────

func TestApplyQueryResultFilter_FailClosed(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope(nil, map[string][]string{"orders": {"id"}})}

	// 正常结果：按列过滤
	payload, _ := json.Marshal(QueryOutput{
		Columns: []string{"id", "amount"},
		Data:    []map[string]any{{"id": 1, "amount": 999}},
		Count:   1,
	})
	filtered, err := mw.applyQueryResultFilter(string(payload), []string{"orders"})
	if err != nil {
		t.Fatalf("正常结果不应报错：%v", err)
	}
	var out QueryOutput
	if err := json.Unmarshal([]byte(filtered), &out); err != nil {
		t.Fatalf("结果应为合法 JSON：%v", err)
	}
	if len(out.Columns) != 1 || out.Columns[0] != "id" {
		t.Errorf("列级权限应过滤掉 amount，实际保留：%v", out.Columns)
	}

	// 工具错误载荷：原样透传，便于模型自我修复
	errPayload := `{"error":true,"tool":"query_data","message":"语法错误"}`
	if got, err := mw.applyQueryResultFilter(errPayload, nil); err != nil || got != errPayload {
		t.Errorf("错误载荷应原样透传，实际 got=%q, err=%v", got, err)
	}

	// 非法载荷（无法确认内容）：fail-closed 拒绝返回
	if _, err := mw.applyQueryResultFilter("<html>not json</html>", nil); err == nil {
		t.Error("非法载荷应按 fail-closed 拒绝，实际放行")
	}
}

// ─── 5. 只读角色的工具裁剪与拦截 ──────────────────────────────

func TestBeforeModelRewriteState_ReadOnlyRemovesWriteTools(t *testing.T) {
	scope := newRemoteScope([]string{"orders"}, nil)
	scope.AllowModify = false
	mw := &PermissionMiddleware{Scope: scope}

	state := &adk.ChatModelAgentState{ToolInfos: []*schema.ToolInfo{
		{Name: "query_data"}, {Name: "exec_sql"}, {Name: "write_file"},
		{Name: "edit_file"}, {Name: "execute"}, {Name: "export_excel"},
	}}
	_, got, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState 失败：%v", err)
	}
	var names []string
	for _, ti := range got.ToolInfos {
		names = append(names, ti.Name)
	}
	for _, removed := range []string{"exec_sql", "write_file", "edit_file", "execute"} {
		if strings.Contains(strings.Join(names, ","), removed) {
			t.Errorf("只读角色不应看到 %s，实际可见：%v", removed, names)
		}
	}
	for _, kept := range []string{"query_data", "export_excel"} {
		if !strings.Contains(strings.Join(names, ","), kept) {
			t.Errorf("只读角色应保留 %s，实际可见：%v", kept, names)
		}
	}
}

// ─── 6. 跨连接越权拦截 ───────────────────────────────────────

func TestCheckConnScope(t *testing.T) {
	mw := &PermissionMiddleware{
		Scope:   newRemoteScope([]string{"orders"}, nil),
		ConnID:  "conn1",
		Schemas: []SchemaRef{{ConnID: "conn1", Schema: "db1"}, {ConnID: "conn2", Schema: "db2"}},
	}
	allow := []string{
		`{"sql":"select 1"}`,                  // 未指定 → 默认连接
		`{"sql":"select 1","connId":"conn1"}`, // 默认连接
		`{"sql":"select 1","connId":"conn2"}`, // 会话已授权的另一连接
		`{"sql":"select 1","connId":"db2"}`,   // 用 schema 名路由
	}
	for _, args := range allow {
		if err := mw.checkConnScope(args); err != nil {
			t.Errorf("checkConnScope(%s) 应放行，实际：%v", args, err)
		}
	}
	deny := []string{
		`{"sql":"select 1","connId":"conn-other"}`,
		`{"sql":"select 1","connId":"db-unknown"}`,
	}
	for _, args := range deny {
		if err := mw.checkConnScope(args); err == nil {
			t.Errorf("checkConnScope(%s) 应拦截未授权连接，实际放行", args)
		}
	}
	// 本地/全权模式跳过检查
	skip := &PermissionMiddleware{Scope: &PermissionScope{IsRemote: false}, ConnID: "conn1"}
	if err := skip.checkConnScope(`{"connId":"whatever"}`); err != nil {
		t.Errorf("本地模式应跳过连接检查，实际：%v", err)
	}
}

// ─── 7. per-run 状态隔离 ─────────────────────────────────────

func TestDynamicPromptMiddleware_ContextIsolation(t *testing.T) {
	mw := NewDynamicPromptMiddleware()

	// Run A 的提示词
	ctxA := WithRunPrompt(context.Background(), "PROMPT-A")
	runA := &adk.ChatModelAgentContext{Instruction: "original"}
	_, runA, err := mw.BeforeAgent(ctxA, runA)
	if err != nil || runA.Instruction != "PROMPT-A" {
		t.Fatalf("Run A 应使用 PROMPT-A，实际 %q (err=%v)", runA.Instruction, err)
	}

	// Run B 的提示词不应受 A 影响（中间件无共享状态）
	ctxB := WithRunPrompt(context.Background(), "PROMPT-B")
	runB := &adk.ChatModelAgentContext{Instruction: "original"}
	_, runB, err = mw.BeforeAgent(ctxB, runB)
	if err != nil || runB.Instruction != "PROMPT-B" {
		t.Fatalf("Run B 应使用 PROMPT-B，实际 %q (err=%v)", runB.Instruction, err)
	}

	// 未携带提示词（Resume 路径）不应改写 Instruction
	runC := &adk.ChatModelAgentContext{Instruction: "from-checkpoint"}
	_, runC, err = mw.BeforeAgent(context.Background(), runC)
	if err != nil || runC.Instruction != "from-checkpoint" {
		t.Fatalf("Resume 路径应保留原 Instruction，实际 %q", runC.Instruction)
	}
}

func TestSessionSyncMiddleware_PerRunIsolation(t *testing.T) {
	mw := &SessionSyncMiddleware{}
	sessA := &Session{ID: "A", messages: []SessionMessage{}}
	sessB := &Session{ID: "B", messages: []SessionMessage{}}

	ctxA := WithRunSessionState(context.Background(), newRunSessionState(sessA, 10))
	ctxB := WithRunSessionState(context.Background(), newRunSessionState(sessB, 10))

	// 两个 Run 各自发生压缩
	compressed := []*schema.Message{
		{Role: schema.User, Content: "摘要后的历史"},
	}
	for _, c := range []struct {
		ctx  context.Context
		sess *Session
	}{{ctxA, sessA}, {ctxB, sessB}} {
		st := runSessionStateFrom(c.ctx)
		st.mu.Lock()
		st.summarizationHit = true
		st.mu.Unlock()
		if _, err := mw.AfterAgent(c.ctx, &adk.ChatModelAgentState{Messages: compressed}); err != nil {
			t.Fatalf("AfterAgent 失败：%v", err)
		}
	}

	if len(sessA.GetMessages()) != 1 || len(sessB.GetMessages()) != 1 {
		t.Fatalf("两个会话都应收到压缩消息，实际 A=%d, B=%d", len(sessA.GetMessages()), len(sessB.GetMessages()))
	}
	// 交叉污染检查：A 的内容不能跑到 B（这里用不同 content 区分）
	stB := runSessionStateFrom(ctxB)
	stB.mu.Lock()
	stB.summarizationHit = true
	stB.mu.Unlock()
	_, _ = mw.AfterAgent(ctxA, &adk.ChatModelAgentState{Messages: []*schema.Message{{Role: schema.User, Content: "A-only"}}})
	if got := sessB.GetMessages()[0].Content; got != "摘要后的历史" {
		t.Errorf("会话 B 被 Run A 覆盖，实际内容：%q", got)
	}
}

// ─── 8. 会话归属判定 ─────────────────────────────────────────

func TestSessionBelongsTo(t *testing.T) {
	cases := []struct {
		owner, requester string
		want             bool
	}{
		{"u1", "u1", true},
		{"u1", "u2", false},
		{"", "u1", true},  // 存量无归属：兼容放行
		{"u1", "", false}, // 未认证：拒绝
	}
	for _, c := range cases {
		if got := sessionBelongsTo(c.owner, c.requester); got != c.want {
			t.Errorf("sessionBelongsTo(%q,%q) = %v, want %v", c.owner, c.requester, got, c.want)
		}
	}
}

// ─── 9. 工具分发 fail-closed（未登记工具拒绝） ─────────────────

func TestToolDispatch_UnregisteredToolDenied(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil)}
	endpoint := func(_ context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil }

	cases := []struct {
		name     string
		wantDeny bool
	}{
		{"get_current_date_info", false}, // 只读白名单
		{"ls", false},                    // 只读文件工具白名单
		{"unknown_new_tool", true},       // 未登记 → 拒绝
		{"mcp_sql_exec", true},           // 伪造工具名 → 拒绝
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := mw.WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: tc.name})
			if err != nil {
				t.Fatalf("WrapInvokableToolCall 失败：%v", err)
			}
			_, callErr := ep(context.Background(), "{}")
			if tc.wantDeny {
				var pe *PermissionError
				if callErr == nil || !errors.As(callErr, &pe) {
					t.Errorf("未登记工具 %s 应按 fail-closed 拒绝，实际 err=%v", tc.name, callErr)
				}
				return
			}
			if callErr != nil {
				t.Errorf("白名单工具 %s 不应报错，实际：%v", tc.name, callErr)
			}
		})
	}
}

// ─── 10. 导出链路：只读强制 + 列级严格校验 ────────────────────

func TestCheckExportAccess_RejectsWriteSQL(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope(nil, map[string][]string{"orders": {"id"}})}
	endpoint := func(_ context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil }

	for _, sql := range []string{
		"WITH x AS (SELECT 1) DELETE FROM orders",
		"WITH/*c*/x AS (SELECT 1) DELETE FROM orders",
		"DELETE/*c*/FROM orders",
		"UPDATE orders SET id=1",
	} {
		args, _ := json.Marshal(ExportInput{SQL: sql})
		if _, err := mw.checkExportAccess(context.Background(), string(args), endpoint, "export_excel"); err == nil {
			t.Errorf("导出链路应拒绝写语句：%s", sql)
		}
	}
}

func TestCheckSelectColumnAccessStrict_RejectsStar(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope(nil, map[string][]string{"orders": {"id"}})}

	// 非严格路径（query_data）：SELECT * 由结果列过滤兜底
	if err := mw.checkSelectColumnAccess("SELECT * FROM orders", []string{"orders"}, "query_data"); err != nil {
		t.Errorf("query_data 路径不应因 SELECT * 报错：%v", err)
	}
	// 严格路径（导出）：column 级表禁止 SELECT *
	if err := mw.checkSelectColumnAccessStrict("SELECT * FROM orders", []string{"orders"}, "export_excel"); err == nil {
		t.Error("导出路径应拒绝 column 级表上的 SELECT *")
	}
	// 显式列出授权字段：放行；列出未授权字段：拦截
	if err := mw.checkSelectColumnAccessStrict("SELECT id FROM orders", []string{"orders"}, "export_excel"); err != nil {
		t.Errorf("授权字段应放行：%v", err)
	}
	if err := mw.checkSelectColumnAccessStrict("SELECT amount FROM orders", []string{"orders"}, "export_excel"); err == nil {
		t.Error("导出路径应拒绝未授权字段")
	}
}

// ── 11. exec_sql 列级程序化复核 ──────────────────────────────

func TestCheckWriteColumnAccess_DeniesUnauthorizedColumn(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope(nil, map[string][]string{"orders": {"id"}})}

	if err := mw.checkWriteColumnAccess("UPDATE orders SET id=1 WHERE id=2"); err != nil {
		t.Errorf("授权字段应放行：%v", err)
	}
	if err := mw.checkWriteColumnAccess("UPDATE orders SET amount=1 WHERE id=2"); err == nil {
		t.Error("UPDATE 未授权字段应拦截")
	}
	if err := mw.checkWriteColumnAccess("INSERT INTO orders (amount) VALUES (1)"); err == nil {
		t.Error("INSERT 未授权字段应拦截")
	}

	// 表级（full）授权不受列级限制
	full := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil)}
	if err := full.checkWriteColumnAccess("UPDATE orders SET amount=1"); err != nil {
		t.Errorf("表级授权应放行：%v", err)
	}
}

// ─── 12. 上传文件归属校验（IDOR） ─────────────────────────────

func TestUploadOwnership(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-1", UserID: "u1", DiskPath: path, FileName: "a.md",
		Ext: ".md", Size: 5, Kind: "text", FileType: "markdown",
	})
	defer RemoveUploadedFile("f-1")

	if !IsUploadOwner("f-1", "u1") {
		t.Error("本人应可访问自己的上传文件")
	}
	if IsUploadOwner("f-1", "u2") {
		t.Error("他人不得访问非本人上传的文件")
	}
	if IsUploadOwner("f-not-exist", "u1") {
		t.Error("文件不存在应返回 false")
	}

	if _, err := GetUploadedFile(context.Background(), "f-1", "u2"); err == nil {
		t.Error("跨用户读取应被拒绝")
	}
	up, err := GetUploadedFile(context.Background(), "f-1", "u1")
	if err != nil {
		t.Fatalf("本人读取失败：%v", err)
	}
	if up.Text != "hello" {
		t.Errorf("读取内容错误，实际：%q", up.Text)
	}
}

// ─── 13. 导出 content 内嵌 SQL 的权限闭环 ─────────────────────

// mdFence 生成 Markdown 代码块（Go 原始字符串无法包含反引号）
func mdFence(lang, body string) string {
	return "```" + lang + "\n" + body + "\n```\n"
}

func TestCheckExportAccess_ContentEmbeddedSQL(t *testing.T) {
	mw := &PermissionMiddleware{Scope: newRemoteScope([]string{"orders"}, nil), ConnID: "conn1"}
	endpoint := func(_ context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil }
	call := func(content string) error {
		args, _ := json.Marshal(export.ExportReportInput{Content: content})
		_, err := mw.checkExportAccess(context.Background(), string(args), endpoint, "export_html")
		return err
	}

	// 纯文字 content（无内嵌 SQL）→ 放行
	if err := call("## 摘要\n\n本季度经营良好。"); err != nil {
		t.Errorf("纯文字 content 应放行：%v", err)
	}

	// 内嵌 SQL 访问有权表 → 放行
	okMD := mdFence("table", `{"sql":"SELECT id FROM orders","title":"明细"}`)
	if err := call(okMD); err != nil {
		t.Errorf("内嵌有权 SQL 应放行：%v", err)
	}

	// 内嵌 SQL 访问无权表 → 整篇导出拒绝（防止「content 免检 + 内嵌 SQL 取数」越权通道）
	badMD := mdFence("chart", `{"type":"line","sql":"SELECT amount FROM secret_table","x":"a","y":["amount"]}`)
	if err := call(badMD); err == nil {
		t.Error("content 内嵌越权 SQL 应被拒绝")
	}

	// 内嵌写语句 → 拒绝（导出链路必须只读）
	writeMD := mdFence("table", `{"sql":"DELETE FROM orders","title":"x"}`)
	if err := call(writeMD); err == nil {
		t.Error("content 内嵌写语句应被拒绝")
	}

	// 内嵌 SQL 指向未授权连接 → 拒绝
	foreignMD := mdFence("table", `{"sql":"SELECT id FROM orders","connId":"conn-other","title":"x"}`)
	if err := call(foreignMD); err == nil {
		t.Error("content 内嵌 SQL 使用未授权连接应被拒绝")
	}

	// 顶层 sql 与 content 同时存在：内嵌 SQL 同样被校验
	args, _ := json.Marshal(export.ExportReportInput{SQL: "SELECT id FROM orders", Content: badMD})
	if _, err := mw.checkExportAccess(context.Background(), string(args), endpoint, "export_html"); err == nil {
		t.Error("同时提供 sql 与 content 时，content 内嵌越权 SQL 也应被拒绝")
	}
}

func TestValidateExportReadSQL_ColumnLevel(t *testing.T) {
	scope := newRemoteScope(nil, map[string][]string{"orders": {"id"}})

	if err := validateExportReadSQL(scope, "SELECT id FROM orders"); err != nil {
		t.Errorf("授权字段应放行：%v", err)
	}
	if err := validateExportReadSQL(scope, "SELECT * FROM orders"); err == nil {
		t.Error("column 级表上的 SELECT * 应被拒绝")
	}
	if err := validateExportReadSQL(scope, "SELECT amount FROM orders"); err == nil {
		t.Error("未授权字段应被拒绝")
	}
	if err := validateExportReadSQL(scope, "DELETE FROM orders"); err == nil {
		t.Error("写语句应被拒绝")
	}
	if err := validateExportReadSQL(scope, "SELECT id FROM other_table"); err == nil {
		t.Error("无权表应被拒绝")
	}
	// 无表名语句（如 SHOW）：无法做表级校验时放行（与既有 query 路径一致）
	if err := validateExportReadSQL(scope, "SHOW TABLES"); err != nil {
		t.Errorf("SHOW 语句应放行：%v", err)
	}
}
