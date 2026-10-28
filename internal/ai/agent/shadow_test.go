package agent

import (
	"errors"
	"testing"
)

// TestCandidateRollout_BaselineNoEvaluation baseline 不评估候选策略，也不产生任何指标。
func TestCandidateRollout_BaselineNoEvaluation(t *testing.T) {
	ResetSecurityMetrics()
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Mode:          RolloutBaseline,
		Scopes: map[string]*PermissionScope{
			"c1": newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil),
		},
		Masker: NewNoopMasker(),
	}
	if err := sec.Authorize(AccessRequest{ConnID: "c1", Table: "orders", Action: ActionRead}); err != nil {
		t.Fatalf("baseline 应允许裸表名（按交集判定）：%v", err)
	}
	for key := range SecurityMetricsSnapshot() {
		if len(key) > len("ai_security_check_total") && key[:len("ai_security_check_total")] == "ai_security_check_total" {
			t.Fatalf("baseline 不应记录候选策略指标：%s", key)
		}
	}
}

// TestCandidateRollout_ShadowRecordsWouldDenyWithoutBlocking shadow 只记指标，不改变授权结果。
func TestCandidateRollout_ShadowRecordsWouldDenyWithoutBlocking(t *testing.T) {
	ResetSecurityMetrics()
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Mode:          RolloutShadow,
		Scopes: map[string]*PermissionScope{
			"c1": newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil),
		},
		Masker: NewNoopMasker(),
	}
	if err := sec.Authorize(AccessRequest{ConnID: "c1", Table: "orders", Action: ActionRead}); err != nil {
		t.Fatalf("shadow 不得改变授权结果：%v", err)
	}
	key := "ai_security_check_total|" + StageScope + "|would_deny|shadow"
	if SecurityMetricsSnapshot()[key] == 0 {
		t.Fatalf("shadow 应记录 %s", key)
	}
	// Schema 限定引用不命中候选策略。
	before := SecurityMetricsSnapshot()[key]
	if err := sec.Authorize(AccessRequest{ConnID: "c1", Schema: "s1", Table: "orders", Action: ActionRead}); err != nil {
		t.Fatalf("限定名应放行：%v", err)
	}
	if SecurityMetricsSnapshot()[key] != before {
		t.Fatal("Schema 限定引用不应计入 would_deny")
	}
}

// TestCandidateRollout_EnforceDeniesUnqualifiedReference enforce 才真正拒绝裸表名。
func TestCandidateRollout_EnforceDeniesUnqualifiedReference(t *testing.T) {
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Mode:          RolloutEnforce,
		Scopes: map[string]*PermissionScope{
			"c1": newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil),
		},
		Masker: NewNoopMasker(),
	}
	err := sec.Authorize(AccessRequest{ConnID: "c1", Table: "orders", Action: ActionRead})
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("enforce 应返回 PermissionError，实际 %v", err)
	}
	if err := sec.Authorize(AccessRequest{ConnID: "c1", Schema: "s1", Table: "orders", Action: ActionRead}); err != nil {
		t.Fatalf("限定名应放行：%v", err)
	}
}

// TestCandidateMaskHighRiskColumns_Modes 冻结脱敏候选策略的三模式行为。
func TestCandidateMaskHighRiskColumns_Modes(t *testing.T) {
	origins := map[string]ResourceRef{
		"user_phone": {ConnID: "c1", Schema: "s1", Table: "orders", Column: "user_phone"},
		"amount":     {ConnID: "c1", Schema: "s1", Table: "orders", Column: "amount"},
	}
	columns := []string{"user_phone", "amount"}
	masker := NewNoopMasker()

	baseline := &RunSecurityContext{Mode: RolloutBaseline}
	if got := candidateMaskHighRiskColumns(baseline, columns, origins, masker); got != nil {
		t.Fatalf("baseline 不应返回脱敏列：%v", got)
	}

	ResetSecurityMetrics()
	shadow := &RunSecurityContext{Mode: RolloutShadow}
	if got := candidateMaskHighRiskColumns(shadow, columns, origins, masker); got != nil {
		t.Fatalf("shadow 不得改变结果：%v", got)
	}
	if SecurityMetricsSnapshot()["ai_security_check_total|"+StageOutput+"|would_mask|shadow"] == 0 {
		t.Fatal("shadow 应记录 would_mask")
	}

	enforce := &RunSecurityContext{Mode: RolloutEnforce}
	got := candidateMaskHighRiskColumns(enforce, columns, origins, masker)
	if !got["user_phone"] || got["amount"] {
		t.Fatalf("enforce 只应命中高敏感列，实际 %v", got)
	}

	// 已有生效规则的列不再重复计数。
	key := []byte("01234567890123456789012345678901")
	withRule, err := NewDataMasker([]MaskRule{
		{ConnID: "c1", Schema: "s1", Table: "orders", Column: "user_phone", Action: MaskRedact},
	}, key, "v1")
	if err != nil {
		t.Fatalf("构建脱敏器失败：%v", err)
	}
	if got := candidateMaskHighRiskColumns(enforce, columns, origins, withRule); got != nil {
		t.Fatalf("已配置规则的列不应命中候选策略：%v", got)
	}
}

// TestRedactQueryOutputColumns 只替换命中的列，其他列保持原值。
func TestRedactQueryOutputColumns(t *testing.T) {
	out := &QueryOutput{
		Columns: []string{"user_phone", "amount"},
		Data:    []map[string]any{{"user_phone": "13800138000", "amount": 10}},
	}
	redactQueryOutputColumns(out, map[string]bool{"user_phone": true})
	if out.Data[0]["user_phone"] != candidateRedactedValue {
		t.Fatalf("命中列应替换为 %s，实际 %v", candidateRedactedValue, out.Data[0]["user_phone"])
	}
	if out.Data[0]["amount"] != 10 {
		t.Fatalf("未命中列必须保持原值，实际 %v", out.Data[0]["amount"])
	}
}

// TestCandidateQuarantineAssistant_Modes 冻结历史隔离候选策略的三模式行为。
func TestCandidateQuarantineAssistant_Modes(t *testing.T) {
	msg := SessionMessage{
		Role:    "assistant",
		Content: "历史建议文本",
		Security: &MessageSecurityMeta{
			ScopeDigest: "old-digest",
			Sanitized:   true,
		},
	}
	base := &RunSecurityContext{ScopeDigest: "new-digest"}

	base.Mode = RolloutBaseline
	if candidateQuarantineAssistant(base, msg) {
		t.Fatal("baseline 不应隔离")
	}

	ResetSecurityMetrics()
	base.Mode = RolloutShadow
	if candidateQuarantineAssistant(base, msg) {
		t.Fatal("shadow 不得改变历史内容")
	}
	if SecurityMetricsSnapshot()["ai_security_check_total|"+StageContext+"|would_quarantine|shadow"] == 0 {
		t.Fatal("shadow 应记录 would_quarantine")
	}

	base.Mode = RolloutEnforce
	if !candidateQuarantineAssistant(base, msg) {
		t.Fatal("enforce 应隔离无溯源 assistant 文本")
	}

	// 快照一致、带资源溯源或已标记不可溯源时不命中。
	base.Mode = RolloutShadow
	same := msg
	same.Security = &MessageSecurityMeta{ScopeDigest: "new-digest"}
	if candidateQuarantineAssistant(base, same) {
		t.Fatal("快照一致不应命中")
	}
	traced := msg
	traced.Security = &MessageSecurityMeta{
		ScopeDigest: "old-digest",
		Resources:   []ResourceRef{{ConnID: "c1", Schema: "s1", Table: "orders"}},
	}
	if candidateQuarantineAssistant(base, traced) {
		t.Fatal("带 ResourceRef 的消息应走资源复核而非候选隔离")
	}
	untraceable := msg
	untraceable.Security = &MessageSecurityMeta{ScopeDigest: "old-digest", Untraceable: true}
	if candidateQuarantineAssistant(base, untraceable) {
		t.Fatal("永久基线已隔离的消息不应重复计入候选策略")
	}
}

// TestFilterHistoryForRun_EnforceQuarantinesAssistant 端到端验证 enforce 模式接入。
func TestFilterHistoryForRun_EnforceQuarantinesAssistant(t *testing.T) {
	scope := newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil)
	sec := &RunSecurityContext{
		Profile:       ProfileWebRemote,
		UserID:        "u1",
		DefaultConnID: "c1",
		Mode:          RolloutEnforce,
		ScopeDigest:   "new-digest",
		Scopes:        map[string]*PermissionScope{"c1": scope},
		Masker:        NewNoopMasker(),
	}
	msgs := []SessionMessage{
		{Role: "user", Content: "继续"},
		{
			Role:    "assistant",
			Content: "旧结论：订单总额 100 万",
			Security: &MessageSecurityMeta{
				ScopeDigest: "old-digest",
				Sanitized:   true,
			},
		},
	}
	out := filterHistoryForRun(msgs, sec)
	if out[1].Content != HistoryQuarantinePlaceholder {
		t.Fatalf("enforce 应隔离无溯源 assistant 文本，实际 %q", out[1].Content)
	}
	if out[0].Content != "继续" {
		t.Fatal("用户指令应保留")
	}
}
