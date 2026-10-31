package agent

// 上下文安全过滤：会话历史、表范围、Schema 与工具结果在装配进模型前，
// 必须按「当前权限」重新裁决。
//
// 关键顺序：过滤必须发生在 summarization/reduction 之前，
// 否则无权数据会被摘要压缩后失去资源溯源，无法再隔离。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// tableContextMaxEntries 单次可注入的表范围上限，防止超长提示词与资源放大。
const tableContextMaxEntries = 50

// identifierMaxLen 标识符长度上限（表名/列名）。
const identifierMaxLen = 128

// safeIdentifierRE 受支持的标识符字符集：字母数字、下划线、$、#、@ 与点号（schema.table）。
var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9_$#@.]+$`)

// isSafeIdentifier 判断是否为受支持的标识符。
func isSafeIdentifier(s string) bool {
	if s == "" || len(s) > 3*identifierMaxLen {
		return false
	}
	// 禁止连续点号与首尾点号（畸形限定名）
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") {
		return false
	}
	return safeIdentifierRE.MatchString(s)
}

// sanitizeTableContext 过滤客户端声明的表范围。
//
// 规则：
//   - 只接受受支持的标识符（拒绝提示词注入文本、换行、引号、分号等）；
//   - 每个表按所属连接与当前 Scope 校验，无权表直接丢弃（客户端声明不能扩大权限）；
//   - 去重并限制数量。
func sanitizeTableContext(raw []string, sec *RunSecurityContext) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	dropped := 0

	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if !isSafeIdentifier(entry) {
			dropped++
			continue
		}
		if sec == nil {
			dropped++
			continue
		}
		scope, scopeErr := resolveTableContextScope(sec, entry)
		if scopeErr != nil {
			dropped++
			continue
		}
		// 使用与 Authorizer 同语义的引用判定：限定名按 schema 匹配，
		// 裸表名按「同名表多 schema 交集」判定（不自动补默认 Schema）。
		if !scope.IsTableRefAllowed(entry) {
			dropped++
			continue
		}
		key := strings.ToLower(entry)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
		if len(out) >= tableContextMaxEntries {
			break
		}
	}
	if dropped > 0 {
		log.Printf("[ContextGuard] 表范围过滤 - 丢弃=%d, 保留=%d\n", dropped, len(out))
		recordQuarantine("unauthorized_table_context")
	}
	return out
}

// resolveTableContextScope 按表引用中的 Schema 前缀选择目标连接 Scope。
//
// 多连接会话中 tableContext 可能指向非默认连接的表：必须用该连接自己的 Scope 校验，
// 不能一律套用默认连接（否则有权限的表会被静默丢弃）。
func resolveTableContextScope(sec *RunSecurityContext, entry string) (*PermissionScope, error) {
	if sec == nil {
		return nil, ErrSecurityScope
	}
	entry = trimIdent(entry)
	if !strings.Contains(entry, ".") {
		return sec.DefaultScope()
	}
	parts := splitScopeKey(entry)
	if parts.schema == "" {
		return nil, &PermissionError{Message: "表范围缺少 Schema", Objects: []string{entry}}
	}
	for _, scope := range sec.Scopes {
		if scope == nil {
			continue
		}
		if strings.EqualFold(scope.SchemaName, parts.schema) {
			return scope, nil
		}
		for schema := range scope.FullAccessSchemas {
			if strings.EqualFold(schema, parts.schema) {
				return scope, nil
			}
		}
		for key := range scope.AllowedTables {
			if strings.EqualFold(splitScopeKey(key).schema, parts.schema) {
				return scope, nil
			}
		}
		for key := range scope.AllowedColumns {
			if strings.EqualFold(splitScopeKey(key).schema, parts.schema) {
				return scope, nil
			}
		}
	}
	// 无法定位到具体连接时回到默认 Scope：后续 IsTableRefAllowed 仍会按 fail-closed 判定。
	return sec.DefaultScope()
}

func defaultSchemaOf(sec *RunSecurityContext) string {
	if sec == nil {
		return ""
	}
	if scope, err := sec.DefaultScope(); err == nil {
		return scope.SchemaName
	}
	return ""
}

// filterHistoryForRun 按当前权限过滤历史消息。
//
// 语义：
//   - 用户指令保留净化后的文本（已在输入阶段处理）；
//   - 工具结果与 assistant 数据在权限变化或无溯源时替换为隔离占位；
//   - 保留 tool_calls / tool_call_id，避免破坏配对导致模型拒绝继续。
func filterHistoryForRun(msgs []SessionMessage, sec *RunSecurityContext) []SessionMessage {
	if len(msgs) == 0 || sec == nil {
		return msgs
	}
	out := make([]SessionMessage, 0, len(msgs))
	quarantined := 0
	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			// summarization 摘要以 user 角色落库且内容包含历史数据：
			// 权限快照变化后必须整体隔离，不能当作普通用户指令送入模型。
			if msg.Security != nil && msg.Security.Untraceable && msg.Security.ScopeDigest != sec.ScopeDigest {
				quarantined++
				msg.Content = HistoryQuarantinePlaceholder
				msg.ReasoningContent = ""
				msg.Security = &MessageSecurityMeta{
					ScopeDigest: sec.ScopeDigest,
					MaskVersion: maskerVersion(sec),
					Sanitized:   true,
					ContentSHA:  contentSHA(HistoryQuarantinePlaceholder),
				}
				out = append(out, msg)
				continue
			}
			// 用户输入已在入口做规范化与风险处理；这里只补齐缺失的元数据
			if msg.Security == nil {
				msg.Security = newMessageSecurity(sec, msg.Content, nil)
			}
			out = append(out, msg)
		case "tool", "assistant":
			if !historyMessageTrusted(msg, sec) {
				quarantined++
				msg.Content = HistoryQuarantinePlaceholder
				msg.ReasoningContent = ""
				msg.Security = &MessageSecurityMeta{
					ScopeDigest: sec.ScopeDigest,
					MaskVersion: maskerVersion(sec),
					Sanitized:   true,
					ContentSHA:  contentSHA(HistoryQuarantinePlaceholder),
				}
				out = append(out, msg)
				continue
			}
			// 候选策略 3：权限快照变化后隔离无溯源 assistant 文本（shadow 只记指标）。
			if candidateQuarantineAssistant(sec, msg) {
				quarantined++
				msg.Content = HistoryQuarantinePlaceholder
				msg.ReasoningContent = ""
				msg.Security = &MessageSecurityMeta{
					ScopeDigest: sec.ScopeDigest,
					MaskVersion: maskerVersion(sec),
					Sanitized:   true,
					ContentSHA:  contentSHA(HistoryQuarantinePlaceholder),
				}
				out = append(out, msg)
				continue
			}
			msg.Content = maskTextOrRaw(sec, msg.Content)
			msg.ReasoningContent = ""
			out = append(out, msg)
		default:
			out = append(out, msg)
		}
	}
	if quarantined > 0 {
		log.Printf("[ContextGuard] 历史数据隔离 - 消息数=%d\n", quarantined)
		recordQuarantine("scope_changed_or_untraceable")
	}
	return out
}

// historyMessageTrusted 判断历史数据消息是否可以继续进入模型。
func historyMessageTrusted(msg SessionMessage, sec *RunSecurityContext) bool {
	scope, err := sec.DefaultScope()
	if err != nil {
		// 纯文件会话：没有数据库范围，文件数据由上传归属单独约束
		return true
	}
	// 仅桌面本地模式跳过：远程模式下即使默认连接是 full，也不能用
	// “默认连接全权”放行其他连接的受限历史。
	if !scope.IsRemote {
		return true
	}
	if msg.Security == nil {
		// 远程受限用户读到缺少安全元数据的旧消息：默认隔离
		return false
	}
	if msg.Security.ScopeDigest == sec.ScopeDigest {
		return true
	}
	// 权限快照不一致：来源无法枚举的数据消息（复杂 SQL / 工具衍生内容）必须隔离。
	if msg.Security.Untraceable {
		return false
	}
	// 否则只有重新证明全部资源仍被授权才保留
	for _, ref := range msg.Security.Resources {
		if ref.Table == "" {
			// 连接级来源（如 list_tables）：无法逐表再授权，权限快照变化后整体隔离。
			// digest 相同的情况已在上面提前返回。
			return false
		}
		if err := sec.Authorize(AccessRequest{
			Action: ActionRead,
			ConnID: ref.ConnID,
			Schema: ref.Schema,
			Table:  ref.Table,
			Column: ref.Column,
		}); err != nil {
			return false
		}
	}
	return true
}

// newMessageSecurity 构造消息安全元数据。
func newMessageSecurity(sec *RunSecurityContext, content string, resources []ResourceRef) *MessageSecurityMeta {
	return newMessageSecurityWithTrace(sec, content, resources, false)
}

// newMessageSecurityWithTrace 构造消息安全元数据并标记来源是否完整可枚举。
func newMessageSecurityWithTrace(sec *RunSecurityContext, content string, resources []ResourceRef, untraceable bool) *MessageSecurityMeta {
	meta := &MessageSecurityMeta{
		Sanitized:   true,
		ContentSHA:  contentSHA(content),
		Resources:   resources,
		Untraceable: untraceable,
	}
	if sec != nil {
		meta.ScopeDigest = sec.ScopeDigest
		meta.MaskVersion = maskerVersion(sec)
	}
	return meta
}

func maskerVersion(sec *RunSecurityContext) string {
	if sec == nil || sec.Masker == nil {
		return ""
	}
	return sec.Masker.Version()
}

// maskTextOrRaw 在脱敏器缺失时保持原样（只可能发生在手工构造的测试上下文）。
func maskTextOrRaw(sec *RunSecurityContext, text string) string {
	if sec == nil || sec.Masker == nil {
		return text
	}
	return sec.Masker.MaskText(text)
}

// contentSHA 返回内容摘要（只用于一致性校验，不可逆）。
func contentSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// SanitizeSessionContext 净化落库的会话上下文（schemas/tables）。
//
// 客户端原始 Context 不能先写入再补救：这里在持久化边界前完成表范围过滤。
func SanitizeSessionContext(ctxJSON string, sec *RunSecurityContext) (string, error) {
	if strings.TrimSpace(ctxJSON) == "" || ctxJSON == "{}" {
		return "{}", nil
	}
	var ctx SessionContext
	if err := json.Unmarshal([]byte(ctxJSON), &ctx); err != nil {
		return "", err
	}
	ctx.Tables = sanitizeTableContext(ctx.Tables, sec)
	if sec != nil {
		// Schema 只保留仍在授权连接集合内的引用
		filtered := make([]SchemaRef, 0, len(ctx.Schemas))
		for _, ref := range ctx.Schemas {
			if _, err := sec.ScopeFor(ref.ConnID); err == nil {
				filtered = append(filtered, ref)
			}
		}
		ctx.Schemas = filtered
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
