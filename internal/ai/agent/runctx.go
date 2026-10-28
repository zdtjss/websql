package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	admin "websql/internal/app/admin"
	"websql/internal/config"
)

// ──────────────────────────────────────────────
// 运行身份（单次 Run 的会话/用户）
// ──────────────────────────────────────────────

// RunIdentity 单次 Run 的身份信息，经 context 传给工具。
//
// 背景：SQLAgent 按 (连接, schema, 用户, 模型) 缓存复用，工具闭包在构建时捕获的
// ExecAuditCtx.SessionID 是「第一次构建时」的会话 ID（新会话时甚至为空），
// 同一 Agent 服务后续其他会话时审计记录会写错会话。工具执行时应以本次 Run 的身份为准。
type RunIdentity struct {
	SessionID string
	UserID    string
}

type runIdentityKey struct{}

// WithRunIdentity 把本次运行身份放入 context
func WithRunIdentity(ctx context.Context, id RunIdentity) context.Context {
	return context.WithValue(ctx, runIdentityKey{}, id)
}

func runIdentityFrom(ctx context.Context) (RunIdentity, bool) {
	if ctx == nil {
		return RunIdentity{}, false
	}
	id, ok := ctx.Value(runIdentityKey{}).(RunIdentity)
	return id, ok
}

// ForRun 返回绑定本次 Run 会话 ID 的审计上下文副本（不修改共享实例）
func (a *ExecAuditCtx) ForRun(ctx context.Context) *ExecAuditCtx {
	if a == nil {
		return nil
	}
	id, ok := runIdentityFrom(ctx)
	if !ok || id.SessionID == "" || id.SessionID == a.SessionID {
		return a
	}
	cp := *a
	cp.SessionID = id.SessionID
	return &cp
}

// ──────────────────────────────────────────────
// 运行时 Profile
// ──────────────────────────────────────────────

// RuntimeProfile 运行时安全 Profile。
//
// 仅「权限来源与部署策略」不同：输入/工具/输出/持久化守卫在两种 Profile 下完全共用。
// 零值必须按远程严格模式处理，避免漏初始化把 Web 远程错误地放宽为完整权限。
type RuntimeProfile uint8

const (
	// ProfileWebRemote Web 远程模式：按角色权限严格隔离
	ProfileWebRemote RuntimeProfile = iota
	// ProfileDesktopLocal 桌面/本地模式：完整数据权限，但其余安全边界不降级
	ProfileDesktopLocal
)

// Remote 判断是否为远程严格模式（未知枚举值同样按远程处理）。
func (p RuntimeProfile) Remote() bool { return p != ProfileDesktopLocal }

// RequiresRevisionLease 判断是否需要权限修订号租约校验。
func (p RuntimeProfile) RequiresRevisionLease() bool { return p.Remote() }

// RequiresRoleScope 判断是否需要按角色构建资源 Scope。
func (p RuntimeProfile) RequiresRoleScope() bool { return p.Remote() }

// CurrentRuntimeProfile 返回当前部署的运行时 Profile。
//
// 唯一入口：只能由服务端根据部署配置推导，HTTP 请求不得传入 Profile，
// 防止客户端把 Web 远程切换为桌面完整权限。
func CurrentRuntimeProfile() RuntimeProfile {
	if config.IsLocalMode() {
		return ProfileDesktopLocal
	}
	return ProfileWebRemote
}

// ──────────────────────────────────────────────
// 每次运行、每连接的权限上下文
// ──────────────────────────────────────────────

// ErrSecurityScope 安全上下文缺失或身份不一致。
var ErrSecurityScope = errors.New("security scope rejected")

// RunSecurityContext 保存一次运行的安全事实：用户、默认连接、每连接 Scope、修订号、脱敏器。
//
// 生命周期：每次请求新建；构建完成后 Scopes 只读，可被本次运行的多个 goroutine 共享。
// 禁止放入跨用户的 Agent 缓存（Agent 缓存只保存按 Scope 指纹区分的实例）。
type RunSecurityContext struct {
	Profile       RuntimeProfile
	UserID        string
	DefaultConnID string
	// Scopes 每个连接独立构建；运行内只读。
	Scopes map[string]*PermissionScope
	// Revision 构建时的权限修订号。
	Revision uint64
	// Masker 本次运行的脱敏器（不可变）。
	Masker *DataMasker
	// Mode 灰度模式（baseline/shadow/enforce），一次运行内固定。
	Mode RolloutMode
	// RestrictWrites 输入中风险时为 true：本轮禁用 AI 写操作。
	RestrictWrites bool
	// DataEgressBlocked 脱敏规则不可用（管理库读取失败）时为 true：禁止数据结果出域。
	DataEgressBlocked bool
	// RevisionUnavailable 权限修订号租约过期或对账失败时为 true：远程模式禁止数据工具。
	RevisionUnavailable bool
	// ScopeDigest 本次运行的权限快照摘要（会话消息溯源 / checkpoint 绑定）。
	ScopeDigest string
}

// DataToolsAllowed 判断本次运行是否允许访问数据库数据（含只读查询与写操作）。
//
// 任一安全依赖不可用（脱敏规则无法加载、权限修订号不可信）都必须禁止数据出域，
// 而不是降级为「跳过检查继续查询」。
func (s *RunSecurityContext) DataToolsAllowed() bool {
	return s != nil && !s.DataEgressBlocked && !s.RevisionUnavailable
}

// RevisionStillValid 运行中权限复核：修订号变化或租约过期时返回 false。
//
// 用于每个安全边界（工具调用前、工具结果回模型前、每个 SSE 批次推送前）：
// 旧 Scope 不得在新修订号下继续放行数据。
func (s *RunSecurityContext) RevisionStillValid() bool {
	if s == nil || !s.Profile.RequiresRevisionLease() {
		return true
	}
	current, fresh := admin.SnapshotPermissionRevision()
	return fresh && current == s.Revision
}

type runSecurityContextKey struct{}

// WithRunSecurity 把运行安全上下文放入 context。
func WithRunSecurity(ctx context.Context, sec *RunSecurityContext) context.Context {
	return context.WithValue(ctx, runSecurityContextKey{}, sec)
}

// RunSecurityFrom 取出运行安全上下文。
//
// 缺失或身份为空一律拒绝：安全上下文必须由请求入口显式构建，
// 不允许工具在「没有安全上下文」的情况下自行沿用 Agent 构建期的静态 Scope。
func RunSecurityFrom(ctx context.Context) (*RunSecurityContext, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: missing run security context", ErrSecurityScope)
	}
	sec, ok := ctx.Value(runSecurityContextKey{}).(*RunSecurityContext)
	if !ok || sec == nil || sec.UserID == "" {
		return nil, fmt.Errorf("%w: missing run security context", ErrSecurityScope)
	}
	return sec, nil
}

// ScopeFor 返回目标连接对应的权限 Scope。
//
// connID 为空时使用默认连接；未知连接直接拒绝，禁止用默认连接 Scope 判断其他连接。
func (s *RunSecurityContext) ScopeFor(connID string) (*PermissionScope, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil run security context", ErrSecurityScope)
	}
	if strings.TrimSpace(connID) == "" {
		connID = s.DefaultConnID
	}
	// 连接标识大小写不敏感（会话中可能传 schema 名或连接 ID），但 Scope 仍按目标连接取。
	scope, ok := s.Scopes[connID]
	if !ok {
		for id, candidate := range s.Scopes {
			if strings.EqualFold(id, connID) {
				scope, ok = candidate, true
				break
			}
		}
	}
	if !ok || scope == nil {
		return nil, &PermissionError{
			Message: "目标连接不在本次会话授权范围内",
			Objects: []string{connID},
		}
	}
	return scope, nil
}

// ConnIDs 返回本次运行已授权的连接集合（稳定顺序不重要，用于诊断与审计）。
func (s *RunSecurityContext) ConnIDs() []string {
	if s == nil {
		return nil
	}
	ids := make([]string, 0, len(s.Scopes))
	for id := range s.Scopes {
		ids = append(ids, id)
	}
	return ids
}

// DefaultScope 返回默认连接的 Scope；未配置默认连接时返回第一个可用 Scope。
func (s *RunSecurityContext) DefaultScope() (*PermissionScope, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil run security context", ErrSecurityScope)
	}
	if s.DefaultConnID != "" {
		return s.ScopeFor(s.DefaultConnID)
	}
	for _, scope := range s.Scopes {
		return scope, nil
	}
	return nil, &PermissionError{Message: "当前会话没有可用的数据权限"}
}

// AccessAction 访问动作。
type AccessAction string

const (
	// ActionRead 读取（查询、元数据、导出）
	ActionRead AccessAction = "read"
	// ActionWrite 写入（AI 数据写工具）
	ActionWrite AccessAction = "write"
)

// AccessRequest 统一权限判定请求。
type AccessRequest struct {
	Action AccessAction
	ConnID string
	Schema string
	Table  string
	Column string
}

// Authorize 统一判定连接、Schema、表、列与写操作权限。
//
// 该函数不重新实现角色合并：角色合并仍发生在 BuildPermissionScope 内。
// 大小写：表权限沿用历史严格匹配语义（IgnoreCase 只用于「只会更严格」的结果过滤路径）。
func (s *RunSecurityContext) Authorize(req AccessRequest) error {
	start := time.Now()
	defer func() { recordSecurityDuration(StageScope, time.Since(start)) }()
	if s == nil {
		return fmt.Errorf("%w: nil run security context", ErrSecurityScope)
	}
	switch req.Action {
	case ActionRead, ActionWrite:
	default:
		return &PermissionError{Message: "未登记的访问动作", Objects: []string{string(req.Action)}}
	}
	scope, err := s.ScopeFor(req.ConnID)
	if err != nil {
		return err
	}
	if req.Action == ActionWrite && !scope.CanAIModifyData() {
		return &PermissionError{Message: "当前角色禁止通过 AI 修改数据"}
	}
	if req.Table == "" {
		return &PermissionError{Message: "缺少待校验的数据表"}
	}

	if scope.SkipChecks() {
		// 仅在「选中 Schema + 权限明细全部 Schema」完整评估前提下才成立
		return nil
	}
	// AllSchemasFull 只覆盖构建 Scope 时的完整评估集合，不能放行集合外对象。
	if scope.AllSchemasFull {
		if req.Schema != "" && scope.FullAccessSchemas[req.Schema] {
			return s.applyCandidateSchemaQualified(req)
		}
		return &PermissionError{Message: "请求 Schema 不在本次授权范围内", Objects: []string{req.Schema}}
	}
	if req.Schema != "" && scope.FullAccessSchemas[req.Schema] {
		return s.applyCandidateSchemaQualified(req)
	}

	qualified := scopeKey(req.Schema, req.Table)
	keys := scope.scopeKeys(qualified)
	if scope.tableLevel(keys) == "none" {
		return &PermissionError{Message: "无权访问数据表", Objects: []string{qualified}}
	}
	if req.Column != "" && scope.tableLevel(keys) == "column" && !scope.columnAllowed(keys, req.Column) {
		return &PermissionError{
			Message: "无权访问数据字段",
			Objects: []string{qualified + "." + req.Column},
		}
	}
	return s.applyCandidateSchemaQualified(req)
}

// applyCandidateSchemaQualified 候选策略 1 的接入点：仅对原本允许的请求评估。
//
// shadow 只记录 would_deny（仍返回 nil），enforce 返回拒绝。
func (s *RunSecurityContext) applyCandidateSchemaQualified(req AccessRequest) error {
	if candidateRequireSchemaQualified(s, req.Schema, req.Table) {
		return &PermissionError{
			Message: "候选策略：数据表引用必须带 Schema 限定，请使用 schema.table 形式重试",
			Objects: []string{req.Table},
		}
	}
	return nil
}

// BuildRunSecurityContext 构建本次运行的安全上下文。
//
// 关键点：
//   - 按 SchemaRef.ConnID 分组，每连接单独调用 BuildPermissionScope；
//   - 远程 Profile 先校验连接归属；
//   - 评估范围 = 选中 Schema ∪ 权限明细中出现的全部 Schema（含其下级限制），
//     否则只选无限制的 A 就可能把 HasFullConnAccess 误判为 true；
//   - Scope map 构建完成后不再修改。
func BuildRunSecurityContext(
	profile RuntimeProfile,
	userID, defaultConnID string,
	refs []SchemaRef,
	revision uint64,
	masker *DataMasker,
) (*RunSecurityContext, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: missing user", ErrSecurityScope)
	}
	if masker == nil {
		masker = NewNoopMasker()
	}
	sec := &RunSecurityContext{
		Profile:       profile,
		UserID:        userID,
		DefaultConnID: defaultConnID,
		Scopes:        make(map[string]*PermissionScope),
		Revision:      revision,
		Masker:        masker,
		Mode:          ResolveRolloutMode(userID),
	}

	schemasByConn := make(map[string][]string)
	for _, ref := range refs {
		if strings.TrimSpace(ref.ConnID) == "" {
			return nil, &PermissionError{Message: "连接参数无效", Objects: []string{ref.ConnID}}
		}
		if strings.TrimSpace(ref.Schema) == "" {
			continue
		}
		schemasByConn[ref.ConnID] = append(schemasByConn[ref.ConnID], ref.Schema)
	}
	if defaultConnID != "" {
		if _, ok := schemasByConn[defaultConnID]; !ok {
			schemasByConn[defaultConnID] = nil // 由既有逻辑补默认 Schema
		}
	}
	if len(schemasByConn) == 0 && defaultConnID != "" {
		schemasByConn[defaultConnID] = nil
	}
	if len(schemasByConn) == 0 {
		// 纯文件分析：没有连接时只保留默认脱敏器，由调用方判断数据来源
		sec.ScopeDigest = userID + "|no-conn"
		return sec, nil
	}

	for connID, schemas := range schemasByConn {
		// 远程 Profile 先验证连接归属；桌面 Profile 使用本地完整权限。
		if profile.RequiresRoleScope() && !admin.CheckConnAccessByUserId(userID, connID) {
			return nil, &PermissionError{Message: "无权访问所选连接", Objects: []string{connID}}
		}
		evaluationSchemas := permissionEvaluationSchemas(userID, connID, schemas)
		scope := BuildPermissionScope(userID, connID, evaluationSchemas)
		if scope.UserID != userID || scope.ConnID != connID {
			return nil, fmt.Errorf("%w: scope identity mismatch", ErrSecurityScope)
		}
		scope.Revision = revision
		scope.Profile = profile
		sec.Scopes[connID] = scope
	}
	sec.ScopeDigest = sec.computeScopeDigest()
	return sec, nil
}

// computeScopeDigest 汇总所有连接 Scope 的摘要（稳定排序）。
func (s *RunSecurityContext) computeScopeDigest() string {
	if s == nil {
		return ""
	}
	ids := make([]string, 0, len(s.Scopes))
	for id := range s.Scopes {
		ids = append(ids, id)
	}
	// 简单插入排序，避免为几个元素引入额外 import 语义
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString(id)
		sb.WriteString("=")
		sb.WriteString(s.Scopes[id].ScopeDigest())
		sb.WriteString(";")
	}
	return sb.String()
}

// permissionEvaluationSchemas 计算权限评估范围：选中 Schema ∪ 权限明细中出现的 Schema。
//
// 只把「用户实际选中」的 Schema 作为业务上下文（Prompt 投影），
// 评估集合更宽仅用于正确计算 HasFullConnAccess / AllSchemasFull，防止：
//   - 只选无限制的 A 时误判连接级完整权限，从而放行有限制的 B；
//   - 漏掉未选中但确有 schema 级授权的对象。
func permissionEvaluationSchemas(userID, connID string, selected []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(selected)+4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, s := range selected {
		add(s)
	}
	for _, p := range admin.FindUserPowerDetails(userID) {
		if p.ConnId != connID || p.SchemaName == nil {
			continue
		}
		add(*p.SchemaName)
	}
	return out
}

// RestrictForInputRisk 返回本轮禁止写操作的副本（中风险输入）。
func (s *RunSecurityContext) RestrictForInputRisk() *RunSecurityContext {
	if s == nil {
		return nil
	}
	cp := *s
	cp.RestrictWrites = true
	scopes := make(map[string]*PermissionScope, len(s.Scopes))
	for id, scope := range s.Scopes {
		scopes[id] = scope.CloneWithRestrictedWrites()
	}
	cp.Scopes = scopes
	cp.ScopeDigest = cp.computeScopeDigest()
	return &cp
}

// RevisionState 权限修订号比较结果。
type RevisionState uint8

const (
	// RevisionUnchanged 修订号未变且租约有效：沿用当前不可变 Scope
	RevisionUnchanged RevisionState = iota
	// RevisionExpanded 权限只增不减：本轮可继续，但不得追溯解除已隔离历史
	RevisionExpanded
	// RevisionShrunk 权限收缩或无法确定：立即拒绝待执行工具并隔离受影响历史
	RevisionShrunk
	// RevisionUnavailable 修订号不可用（租约过期/读取失败）：远程模式 fail-closed
	RevisionUnavailable
)

func (r RevisionState) String() string {
	switch r {
	case RevisionUnchanged:
		return "unchanged"
	case RevisionExpanded:
		return "expanded"
	case RevisionShrunk:
		return "shrunk"
	default:
		return "unavailable"
	}
}

// Recheck 在安全边界复核权限修订号。
//
// rebuild 由调用方提供：用最新权限重新构建同用户的 ScopeSet（同一组 SchemaRef）。
// 返回值：
//   - unchanged：沿用当前 Scope；
//   - expanded：返回新上下文，可继续本轮；
//   - shrunk/unavailable：调用方必须拒绝待执行工具。
func (s *RunSecurityContext) Recheck(rebuild func() (*RunSecurityContext, error)) (RevisionState, *RunSecurityContext, error) {
	if s == nil {
		return RevisionUnavailable, nil, fmt.Errorf("%w: nil run security context", ErrSecurityScope)
	}
	if !s.Profile.RequiresRevisionLease() {
		// 本地/桌面：不启用多实例租约，仍保留权限变更复核能力
		current := admin.CurrentPermissionRevision()
		if current == s.Revision {
			return RevisionUnchanged, s, nil
		}
	} else {
		current, fresh := admin.SnapshotPermissionRevision()
		if !fresh {
			return RevisionUnavailable, s, nil
		}
		if current == s.Revision {
			return RevisionUnchanged, s, nil
		}
	}

	if rebuild == nil {
		return RevisionShrunk, s, nil
	}
	next, err := rebuild()
	if err != nil {
		return RevisionUnavailable, s, err
	}
	if next.ScopeDigest == s.ScopeDigest {
		// 修订号变化但权限投影未变（例如其他用户/角色变更），继续使用新上下文
		return RevisionUnchanged, next, nil
	}
	if scopeSetCovers(next, s) {
		return RevisionExpanded, next, nil
	}
	return RevisionShrunk, next, nil
}

// scopeSetCovers 判断 next 是否覆盖 old 的全部授权（用于区分「权限只增」与「权限收缩」）。
//
// 保守实现：只要存在任何无法确认被覆盖的授权，就判定为未覆盖（fail-closed）。
func scopeSetCovers(next, old *RunSecurityContext) bool {
	if next == nil || old == nil {
		return false
	}
	for connID, oldScope := range old.Scopes {
		newScope, err := next.ScopeFor(connID)
		if err != nil {
			return false
		}
		if !scopeCovers(newScope, oldScope) {
			return false
		}
	}
	return true
}

// scopeCovers 判断 newScope 是否覆盖 oldScope 的全部表/列授权。
func scopeCovers(newScope, oldScope *PermissionScope) bool {
	if newScope == nil || oldScope == nil {
		return false
	}
	if oldScope.SkipChecks() {
		return newScope.SkipChecks()
	}
	if newScope.SkipChecks() {
		return true
	}
	for schema := range oldScope.FullAccessSchemas {
		if !newScope.FullAccessSchemas[schema] {
			return false
		}
	}
	for table := range oldScope.AllowedTables {
		// 表级授权 = 全列授权：新 Scope 必须仍为 full 才算覆盖（收缩为 column 是权限回收）
		if newScope.tableLevel(newScope.scopeKeys(table)) != "full" {
			return false
		}
	}
	for table, cols := range oldScope.AllowedColumns {
		if newScope.tableLevel(newScope.scopeKeys(table)) != "column" {
			return false
		}
		for col := range cols {
			if !newScope.columnAllowed(newScope.scopeKeys(table), col) {
				return false
			}
		}
	}
	return true
}

// LogSecurityDenied 记录安全拒绝事件（只记录数量与对象，不记录数据内容）。
func LogSecurityDenied(stage, reason string, objects []string) {
	log.Printf("[Security] 拒绝 - stage=%s reason=%s objects=%d", stage, reason, len(objects))
	recordSecurityEvent(stage, "deny", reason)
}
