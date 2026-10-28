package agent

// ResourceRef 标识一条数据来源（连接 / Schema / 表 / 列）。
//
// 用途：
//   - 工具结果与会话消息的安全溯源（权限变更后据此重新裁决是否仍可进入模型）；
//   - 结果列脱敏时把「输出列（含别名）」映射回原始列规则。
//
// 空字段表示「该层级未限定」，例如只有 ConnID 表示连接级来源。
type ResourceRef struct {
	ConnID string `json:"connId"`
	Schema string `json:"schema,omitempty"`
	Table  string `json:"table,omitempty"`
	Column string `json:"column,omitempty"`
}

// MessageSecurityMeta 会话消息的安全元数据。
//
// ScopeDigest 记录消息产生时的权限快照摘要：当前运行的摘要与之不一致时，
// 历史数据消息（tool/assistant）默认隔离，只有重新证明资源仍全部授权时才保留。
type MessageSecurityMeta struct {
	ScopeDigest string        `json:"scopeDigest,omitempty"`
	MaskVersion string        `json:"maskVersion,omitempty"`
	Resources   []ResourceRef `json:"resources,omitempty"`
	ContentSHA  string        `json:"contentSha,omitempty"`
	Sanitized   bool          `json:"sanitized,omitempty"`
	// Untraceable 表示该消息的数据来源无法完整枚举（如复杂 SQL、工具参数解析失败、
	// 由工具结果衍生的 assistant 内容）。权限快照变化时这类消息必须隔离，不能默认放行。
	Untraceable bool `json:"untraceable,omitempty"`
}

// HistoryQuarantinePlaceholder 历史数据因权限变更或缺少溯源被隔离时的占位文本。
//
// 该文本会替代原消息内容进入模型上下文，不含任何原数据片段。
const HistoryQuarantinePlaceholder = "[历史数据因权限变更或缺少权限溯源已隐藏，请基于当前授权范围重新查询。]"
