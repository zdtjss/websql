package agent

// shadow.go — 候选策略（灰度机制）。
//
// 三种模式的分工：
//   - baseline：只执行永久安全基线（全部既有安全修复）；
//   - shadow  ：在 baseline 上评估候选策略，仅累加 would_deny / would_mask /
//     would_quarantine 指标，不改变工具执行、返回数据与 SSE；
//   - enforce ：候选策略真正生效。
//
// 候选策略必须是「比永久基线更严格」的新规则，且不得重复实现永久修复；
// 永久修复（ScopeSet、输入/输出守卫、持久化守卫、Revision 复核等）与模式无关。
//
// 当前候选集合：
//  1. require_schema_qualified：数据引用必须带 Schema 限定。
//     永久基线允许裸表名按「同名表多 Schema 交集」判定，存在 search_path 歧义风险。
//  2. mask_high_risk_column_names：列名命中高敏感特征（password/secret/id_card/phone…）
//     且无生效脱敏规则时，整列替换为 ***。
//  3. quarantine_assistant_without_resources：权限快照变化后，无 ResourceRef 的
//     assistant 文本消息按无溯源隔离（永久基线仅在 Untraceable 时隔离）。

import "strings"

// 候选策略名（低基数标签，禁止包含用户/连接/表/列标识）。
const (
	CandidateRequireSchemaQualified              = "require_schema_qualified"
	CandidateMaskHighRiskColumnNames             = "mask_high_risk_column_names"
	CandidateQuarantineAssistantWithoutResources = "quarantine_assistant_without_resources"
	candidateRedactedValue                       = "***"
)

// highRiskColumnNamePatterns 高敏感列名特征（大小写不敏感，按子串匹配）。
var highRiskColumnNamePatterns = []string{
	"id_card", "idcard", "identity", "ssn", "passport",
	"card_no", "cardno", "bank_card", "bankcard",
	"password", "passwd", "pwd", "secret", "token", "credential",
	"phone", "mobile", "email",
}

// isHighRiskColumnName 判断列名是否命中高敏感特征。
func isHighRiskColumnName(column string) bool {
	col := strings.ToLower(trimIdent(column))
	if col == "" {
		return false
	}
	for _, pattern := range highRiskColumnNamePatterns {
		if strings.Contains(col, pattern) {
			return true
		}
	}
	return false
}

// candidateHit 统一记录候选策略命中。
//
// shadow 只记录 would_<action> 并返回 false（不改变行为）；
// enforce 记录 action 并返回 true（由调用方执行）；
// baseline 直接返回 false。
func candidateHit(sec *RunSecurityContext, stage, action string) bool {
	if sec == nil {
		return false
	}
	switch sec.Mode {
	case RolloutShadow:
		recordSecurityEvent(stage, "would_"+action, "shadow")
		return false
	case RolloutEnforce:
		recordSecurityEvent(stage, action, "enforce")
		return true
	default:
		return false
	}
}

// candidateRequireSchemaQualified 候选策略 1：裸表名引用在受限用户下拒绝。
//
// 返回 true 表示 enforce 模式需要拒绝该请求。
func candidateRequireSchemaQualified(sec *RunSecurityContext, schema, table string) bool {
	if sec == nil || !CandidatePoliciesEvaluated(sec.Mode) {
		return false
	}
	if strings.TrimSpace(schema) != "" || strings.TrimSpace(table) == "" {
		return false
	}
	return candidateHit(sec, StageScope, "deny")
}

// candidateMaskHighRiskColumns 候选策略 2：高敏感列名整列脱敏。
//
// 已有生效脱敏规则的列不计入（避免重复脱敏与错误计数）。
// 返回非空集合表示 enforce 模式需要整列替换的列名。
func candidateMaskHighRiskColumns(
	sec *RunSecurityContext,
	columns []string,
	origins map[string]ResourceRef,
	masker *DataMasker,
) map[string]bool {
	if sec == nil || !CandidatePoliciesEvaluated(sec.Mode) || len(columns) == 0 {
		return nil
	}
	hits := make(map[string]bool)
	for _, column := range columns {
		if !isHighRiskColumnName(column) {
			continue
		}
		if ref, ok := origins[column]; ok && masker != nil {
			if masker.ActionFor(ref.ConnID, ref.Schema, ref.Table, ref.Column) != "" {
				continue
			}
		}
		hits[column] = true
	}
	if len(hits) == 0 {
		return nil
	}
	if !candidateHit(sec, StageOutput, "mask") {
		return nil
	}
	recordMaskEvent(StageOutput, CandidateMaskHighRiskColumnNames)
	return hits
}

// redactQueryOutputColumns 将指定列整列替换为脱敏标记（候选策略 enforce 用）。
func redactQueryOutputColumns(out *QueryOutput, columns map[string]bool) {
	if out == nil || len(columns) == 0 {
		return
	}
	for _, row := range out.Data {
		for column := range columns {
			if _, ok := row[column]; ok {
				row[column] = candidateRedactedValue
			}
		}
	}
}

// candidateQuarantineAssistant 候选策略 3：权限快照变化后隔离无溯源 assistant 文本。
//
// 永久基线只隔离 Untraceable 数据消息；带 ResourceRef 的消息会逐资源复核。
// 本候选覆盖「既未标记 Untraceable、也没有任何 ResourceRef」的历史 assistant 文本。
func candidateQuarantineAssistant(sec *RunSecurityContext, msg SessionMessage) bool {
	if sec == nil || !CandidatePoliciesEvaluated(sec.Mode) {
		return false
	}
	if msg.Role != "assistant" || msg.Security == nil {
		return false
	}
	if msg.Security.Untraceable || len(msg.Security.Resources) > 0 {
		return false
	}
	if msg.Security.ScopeDigest == "" || msg.Security.ScopeDigest == sec.ScopeDigest {
		return false
	}
	return candidateHit(sec, StageContext, "quarantine")
}
