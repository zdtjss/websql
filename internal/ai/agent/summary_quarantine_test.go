package agent

import (
	"testing"
)

// summary_quarantine_test.go 覆盖一处实测暴露的隔离缺口（整改文档 §九-B）：
// 纯正文 assistant 消息（本轮工具结果的汇总）原先不带资源溯源，
// 权限收缩后工具结果被占位，但模型仍能从这段正文引用已失权的数据。
// 现在把本轮已登记的资源聚合到正文上，并验证 filterHistoryForRun 能据此拦截。

func newRevokeScopes() *RunSecurityContext {
	scopeA := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		HasFullConnAccess: true, FullAccessSchemas: map[string]bool{"s1": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	// 权限收缩前：c2 的 notes 可读
	scopeBOld := &PermissionScope{
		UserID: "u1", ConnID: "c2", SchemaName: "s2", IsRemote: true,
		AllowedTables:  map[string]bool{"s2.notes": true},
		AllowedColumns: map[string]map[string]bool{},
	}
	oldSec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes:  map[string]*PermissionScope{"c1": scopeA, "c2": scopeBOld},
		Masker:  NewNoopMasker(),
	}
	oldSec.ScopeDigest = "digest-old"
	return oldSec
}

func newShrunkSec() *RunSecurityContext {
	scopeA := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		HasFullConnAccess: true, FullAccessSchemas: map[string]bool{"s1": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scopeA},
		Masker: NewNoopMasker(),
	}
	sec.ScopeDigest = "digest-new"
	return sec
}

func newKeepingSec() *RunSecurityContext {
	scopeA := &PermissionScope{
		UserID: "u1", ConnID: "c1", SchemaName: "s1", IsRemote: true,
		HasFullConnAccess: true, FullAccessSchemas: map[string]bool{"s1": true},
		AllowedTables: map[string]bool{}, AllowedColumns: map[string]map[string]bool{},
	}
	scopeB := &PermissionScope{
		UserID: "u1", ConnID: "c2", SchemaName: "s2", IsRemote: true,
		AllowedTables:  map[string]bool{"s2.notes": true},
		AllowedColumns: map[string]map[string]bool{},
	}
	sec := &RunSecurityContext{
		Profile: ProfileWebRemote, UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scopeA, "c2": scopeB},
		Masker: NewNoopMasker(),
	}
	sec.ScopeDigest = "digest-new" // 快照变了，但资源仍被授权
	return sec
}

// TestPersistAttempt_TextSummaryCarriesRunResources 纯正文汇总必须带上本轮命中的资源。
func TestPersistAttempt_TextSummaryCarriesRunResources(t *testing.T) {
	oldSec := newRevokeScopes()
	sess := &Session{ID: "s-summary", UserID: "u1"}

	var att streamAttempt
	att.safeContent.WriteString("汇总：notes 共 8 行")
	pending := map[string]pendingResourceInfo{
		"call-1": {refs: []ResourceRef{{ConnID: "c2", Schema: "s2", Table: "notes"}}, complete: true},
	}
	a := &SQLAgent{}
	a.persistAttempt(&att, sess, oldSec, pending)

	msgs := sess.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("应写入 1 条 assistant 消息，实际 %d", len(msgs))
	}
	meta := msgs[0].Security
	if meta == nil || len(meta.Resources) == 0 {
		t.Fatalf("汇总正文必须携带本轮资源溯源，实际 %+v", meta)
	}
	if meta.Untraceable {
		t.Fatal("来源可完整枚举时不应标记不可溯源")
	}
	if meta.Resources[0].Table != "notes" || meta.Resources[0].ConnID != "c2" {
		t.Fatalf("资源引用不正确：%+v", meta.Resources[0])
	}
}

// TestPersistAttempt_IncompleteSourcesMarkUntraceable 来源无法枚举时仍标不可溯源。
func TestPersistAttempt_IncompleteSourcesMarkUntraceable(t *testing.T) {
	oldSec := newRevokeScopes()
	sess := &Session{ID: "s-summary2", UserID: "u1"}
	var att streamAttempt
	att.safeContent.WriteString("汇总：复杂 SQL 结果")
	pending := map[string]pendingResourceInfo{
		"call-2": {refs: []ResourceRef{{ConnID: "c2", Schema: "s2", Table: "notes"}}, complete: false},
	}
	a := &SQLAgent{}
	a.persistAttempt(&att, sess, oldSec, pending)
	meta := sess.GetMessages()[0].Security
	if meta == nil || !meta.Untraceable {
		t.Fatalf("来源不完整时必须标记不可溯源，实际 %+v", meta)
	}
}

// TestFilterHistory_QuarantinesSummaryAfterRevocation 权限收缩后，带资源溯源的汇总正文必须被占位。
func TestFilterHistory_QuarantinesSummaryAfterRevocation(t *testing.T) {
	oldSec := newRevokeScopes()
	summary := "汇总：notes 共 8 行"
	msgs := []SessionMessage{
		{Role: "user", Content: "统计两个库", Security: newMessageSecurity(oldSec, "统计两个库", nil)},
		{Role: "tool", Content: `{"rows":[8]}`, ToolCallID: "c1", ToolName: "query_data",
			Security: newMessageSecurityWithTrace(oldSec, `{"rows":[8]}`,
				[]ResourceRef{{ConnID: "c2", Schema: "s2", Table: "notes"}}, false)},
		{Role: "assistant", Content: summary,
			Security: newMessageSecurityWithTrace(oldSec, summary,
				[]ResourceRef{{ConnID: "c2", Schema: "s2", Table: "notes"}}, false)},
	}

	shrunk := newShrunkSec()
	out := filterHistoryForRun(append([]SessionMessage(nil), msgs...), shrunk)
	if got := out[1].Content; got != HistoryQuarantinePlaceholder {
		t.Fatalf("回收后工具结果应被占位，实际 %q", got)
	}
	if got := out[2].Content; got != HistoryQuarantinePlaceholder {
		t.Fatalf("回收后汇总正文必须被占位（修复前这里会原样保留并被子模型引用），实际 %q", got)
	}

	// 对照：快照变了但资源仍被授权 → 正文保留
	keeping := newKeepingSec()
	out2 := filterHistoryForRun(append([]SessionMessage(nil), msgs...), keeping)
	if got := out2[2].Content; got != summary {
		t.Fatalf("资源仍被授权时不应隔离，实际 %q", got)
	}
}
