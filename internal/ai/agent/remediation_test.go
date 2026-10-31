package agent

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// ─── F-03 多连接历史信任：默认连接 full 不得放行其他连接的受限历史 ───

func newTwoConnSecurity(defaultFull bool) *RunSecurityContext {
	scopeA := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1",
		IsRemote: true, HasFullConnAccess: defaultFull, FullAccessSchemas: map[string]bool{"s1": defaultFull},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	scopeB := &PermissionScope{
		UserID: "u1", ConnID: "c2", SchemaName: "s2",
		IsRemote: true, FullAccessSchemas: map[string]bool{},
		AllowedTables:  map[string]bool{},
		AllowedColumns: map[string]map[string]bool{"s2.orders": {"id": true}},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scopeA, "c2": scopeB},
		Masker: NewNoopMasker(),
	}
	sec.ScopeDigest = sec.computeScopeDigest()
	return sec
}

func TestHistoryMessageTrusted_MultiConnNoShortcut(t *testing.T) {
	sec := newTwoConnSecurity(true)
	msg := SessionMessage{
		Role:    "tool",
		Content: "敏感数据",
		Security: &MessageSecurityMeta{
			ScopeDigest: "旧的权限快照",
			Resources:   []ResourceRef{{ConnID: "c2", Schema: "s2", Table: "orders", Column: "phone"}},
			Untraceable: false,
		},
	}
	if historyMessageTrusted(msg, sec) {
		t.Fatal("默认连接 full 时不得放行其他连接已回收的列")
	}

	msg.Security.ScopeDigest = sec.ScopeDigest
	if !historyMessageTrusted(msg, sec) {
		t.Fatal("权限快照一致时应保留历史")
	}
}

func TestHistoryMessageTrusted_ConnectionLevelRefIsolated(t *testing.T) {
	sec := newTwoConnSecurity(true)
	msg := SessionMessage{
		Role:    "tool",
		Content: "表清单",
		Security: &MessageSecurityMeta{
			ScopeDigest: "旧的权限快照",
			Resources:   []ResourceRef{{ConnID: "c2"}},
		},
	}
	if historyMessageTrusted(msg, sec) {
		t.Fatal("连接级来源在权限快照变化后必须整体隔离")
	}
}

func TestHistoryMessageTrusted_DesktopLocalTrusted(t *testing.T) {
	scope := &PermissionScope{UserID: "u1", ConnID: "c1", IsRemote: false, HasFullConnAccess: true}
	sec := &RunSecurityContext{
		Profile: ProfileDesktopLocal, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope}, Masker: NewNoopMasker(), ScopeDigest: "d2",
	}
	msg := SessionMessage{Role: "assistant", Content: "x", Security: &MessageSecurityMeta{ScopeDigest: "d1"}}
	if !historyMessageTrusted(msg, sec) {
		t.Fatal("桌面本地模式历史应直接可信")
	}
}

// ─── F-04 摘要消息（user 角色）权限变化后隔离 ───

func TestFilterHistory_SummaryQuarantinedAfterScopeChange(t *testing.T) {
	sec := newTwoConnSecurity(false)
	msgs := []SessionMessage{
		{Role: "user", Content: "帮我分析销售"},
		{Role: "user", Content: "摘要：此前查询到 13800138000", Security: &MessageSecurityMeta{
			Untraceable: true, Sanitized: true, ScopeDigest: "旧的权限快照",
		}},
	}
	out := filterHistoryForRun(msgs, sec)
	if out[1].Content != HistoryQuarantinePlaceholder {
		t.Fatalf("摘要在权限快照变化后应被隔离，实际：%q", out[1].Content)
	}
	if out[0].Content != "帮我分析销售" {
		t.Fatal("普通用户提问必须保留")
	}
}

func TestFilterHistory_SummaryKeptWithinSameSnapshot(t *testing.T) {
	sec := newTwoConnSecurity(false)
	msgs := []SessionMessage{
		{Role: "user", Content: "摘要正文", Security: &MessageSecurityMeta{
			Untraceable: true, Sanitized: true, ScopeDigest: sec.ScopeDigest,
		}},
	}
	out := filterHistoryForRun(msgs, sec)
	if out[0].Content != "摘要正文" {
		t.Fatalf("同一权限快照下的摘要应可用，实际：%q", out[0].Content)
	}
}

// ─── F-26 连接标识归一化（Schema 名 → 真实 ConnID） ───

func TestResourcesFromToolCall_NormalizesSchemaNameToConnID(t *testing.T) {
	sec := newTwoConnSecurity(false)
	refs, _ := resourcesFromToolCall("query_data", `{"sql":"SELECT id FROM orders","connId":"s2"}`, sec)
	if len(refs) == 0 {
		t.Fatal("应解析出资源溯源")
	}
	for _, ref := range refs {
		if ref.ConnID != "c2" {
			t.Fatalf("connId=Schema 名应归一化为真实连接 ID，实际：%q", ref.ConnID)
		}
	}
}

// ─── F-35 表范围按所属连接校验 ───

func TestSanitizeTableContext_UsesOwningConnectionScope(t *testing.T) {
	sec := newTwoConnSecurity(true)
	kept := sanitizeTableContext([]string{"s2.orders"}, sec)
	if len(kept) != 1 || kept[0] != "s2.orders" {
		t.Fatalf("非默认连接的已授权表不应被丢弃，实际：%v", kept)
	}
	if dropped := sanitizeTableContext([]string{"s2.secret"}, sec); len(dropped) != 0 {
		t.Fatalf("未授权列级表应收缩：%v", dropped)
	}
}

// ─── F-11 导出表格来源不明一律 redact ───

func TestMaskTable_UnknownOriginWithRulesAlwaysRedacts(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	m, err := NewDataMasker([]MaskRule{
		{ConnID: "c9", Schema: "s9", Table: "other", Column: "salary", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		t.Fatalf("构建脱敏器失败：%v", err)
	}
	rows := [][]string{{"10000"}}
	// 本次查询的表正是规则所在表 → 来源不明也必须整列 redact
	out := m.MaskTable([]string{"salary"}, rows, map[string]ResourceRef{}, []string{"other"})
	if out[0][0] == "10000" {
		t.Fatalf("来源不明且规则命中本次查询表时必须整列 redact，实际 %q", out[0][0])
	}
	// 规则只存在于无关表时，不应把聚合/表达式列一划抹除（真实使用回归）
	safe := m.MaskTable([]string{"salary"}, [][]string{{"10000"}}, map[string]ResourceRef{}, []string{"orders"})
	if safe[0][0] != "10000" {
		t.Fatalf("无关表的聚合列不应被错抹：%q", safe[0][0])
	}
	// 无规则时仍只依赖内置文本保护，不改写普通数值
	plain := NewNoopMasker()
	if got := plain.MaskTable([]string{"salary"}, [][]string{{"10000"}}, nil, nil)[0][0]; got != "10000" {
		t.Fatalf("无规时普通列不应被改写：%q", got)
	}
}

// ─── F-18 格式字符绕过 ───

func TestMaskText_FormatCharBypassVariants(t *testing.T) {
	m := NewNoopMasker()
	cases := []string{
		"13" + "\u200B" + "800138000",
		"138" + "\u00AD" + "00138000",
		"1380" + "\uFEFF" + "0138000",
		"1380013" + "\u061C" + "8000",
	}
	for _, in := range cases {
		got := m.MaskText("电话 " + in + " 结束")
		if strings.Contains(got, "13800138000") {
			t.Fatalf("插入格式字符后手机号仍泄露：%q", got)
		}
	}
}

// ─── F-19 错误日志不泄露 URL 凭据 ───

// TestGuardWholeText_KeepsFullTextBeyondHoldback 回归：审计整段文本时必须
// 同时保留 Push 的安全前缀与 Close 的 holdback 尾部，不能只剩末尾窗口。
// （Web 白盒测试发现：工具结果超过 holdback 时会话里只存了末尾 64 字，
// 模型看不到查询结果而不断重复同一询问。）
func TestGuardWholeText_KeepsFullTextBeyondHoldback(t *testing.T) {
	sec := &RunSecurityContext{Profile: ProfileWebRemote, UserID: "u1", Masker: NewNoopMasker()}
	long := `{"columns":["a"],"data":[` + strings.Repeat(`{"a":1},`, 200) + `{"a":2}],"returnedRows":201}`
	got, err := guardWholeText(sec, long)
	if err != nil {
		t.Fatalf("审计失败：%v", err)
	}
	if len(got) != len(long) {
		t.Fatalf("整段审计不应缩短文本：got=%d want=%d", len(got), len(long))
	}
	if !strings.HasPrefix(got, `{"columns":["a"],"data":[`) || !strings.HasSuffix(got, `"returnedRows":201}`) {
		t.Fatal("文本头尾必须完整保留")
	}

	// 敏感内容仍要被脱敏
	withSecret := strings.Repeat("说明文字。", 60) + "联系电话 13800138000 结束"
	masked, err := guardWholeText(sec, withSecret)
	if err != nil {
		t.Fatalf("审计失败：%v", err)
	}
	if strings.Contains(masked, "13800138000") {
		t.Fatalf("整段审计应脱敏敏感值：%q", firstRunes(masked, 40))
	}
	if len(masked) < len(withSecret)-20 {
		t.Fatalf("审计后文本长度不应大幅缩水：%d vs %d", len(masked), len(withSecret))
	}
}

func TestSafeErrorForLog_HidesURLCredentials(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://user:pass@api.example.com/v1/chat", Err: errors.New("dial tcp: refused")}
	got := safeErrorForLog(err)
	if strings.Contains(got, "user:pass") || strings.Contains(got, "pass@") {
		t.Fatalf("错误日志包含凭据：%q", got)
	}
	if !strings.Contains(got, "api.example.com") {
		t.Fatalf("应保留主机名便于排障：%q", got)
	}
}

// ─── F-25 流式聚合区分 EOF 与真实错误 ───

func TestDrainStream_DistinguishesEOFAndError(t *testing.T) {
	okReader, okWriter := schema.Pipe[string](1)
	_ = okWriter.Send("part", nil)
	okWriter.Close()
	var sb strings.Builder
	if err := drainStream(okReader, &sb); err != nil {
		t.Fatalf("正常结束不应报错：%v", err)
	}
	if sb.String() != "part" {
		t.Fatalf("内容聚合异常：%q", sb.String())
	}

	errReader, errWriter := schema.Pipe[string](1)
	go func() {
		_ = errWriter.Send("part", nil)
		_ = errWriter.Send("", errors.New("boom"))
		errWriter.Close()
	}()
	var sb2 strings.Builder
	err := drainStream(errReader, &sb2)
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("中途错误必须返回真实错误：%v", err)
	}
}

// ─── F-29 多连接任一有权限即可用 ───

func TestRunSecurityContextHasAnyAccessAcrossConns(t *testing.T) {
	sec := newTwoConnSecurity(false)
	scopeA := sec.Scopes["c1"]
	scopeA.HasFullConnAccess = false
	scopeA.FullAccessSchemas = map[string]bool{}
	scopeA.AllowedTables = map[string]bool{}
	scopeA.AllowedColumns = map[string]map[string]bool{}
	if !sec.HasAnyAccess() {
		t.Fatal("默认连接无权限但 c2 有列级权限时应允许使用")
	}
}
