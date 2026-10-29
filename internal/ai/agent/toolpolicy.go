package agent

// 工具能力策略注册表：把「工具名 → 能力要求 / 是否需要审批 / 资源解析」集中登记。
//
// 设计目的：避免每新增一个分析或导出工具就要在多处权限分支里补 switch。
// 未登记工具一律拒绝（fail-closed），登记项必须显式声明能力。
//
// 两层划分：
//   - 控制面：角色能力、连接/表/列权限、allow_modify、ai_allow_modify；
//   - 数据面：查询、元数据、文件、跨库分析、导出。

import (
	"context"
	"strings"

	admin "websql/internal/app/admin"
)

// Capability 工具能力。
type Capability string

const (
	// CapabilityRead 读取数据（查询、元数据、跨库分析、导出）
	CapabilityRead Capability = "data.read"
	// CapabilityAIWrite AI 写数据（受 allow_modify + ai_allow_modify 双门槛约束）
	CapabilityAIWrite Capability = "ai.data.write"
	// CapabilityCodeExecute 代码/命令执行与文件写入（保持既有 allow_modify 约束）
	CapabilityCodeExecute Capability = "ai.code.execute"
	// CapabilityFileRead 只读文件访问（本人上传文件、沙箱内文件）
	CapabilityFileRead Capability = "file.read"
	// CapabilityNone 不访问数据库、不落盘、不执行命令
	CapabilityNone Capability = "none"
)

// ToolPolicy 单个工具的安全策略。
type ToolPolicy struct {
	Capability Capability
	// RequiresApproval 写操作仍需危险 SQL 审批；权限通过不等于自动批准。
	RequiresApproval bool
	// DataTool 是否访问数据库数据（需要按目标连接取 Scope）。
	DataTool bool
}

// toolPolicies 工具策略注册表。
//
// 新增工具必须在此登记；未登记工具在 WrapInvokableToolCall / WrapStreamableToolCall
// 中会被 unregisteredToolError 拒绝。
var toolPolicies = map[string]ToolPolicy{
	"query_data":              {Capability: CapabilityRead, DataTool: true},
	"get_table_schema":        {Capability: CapabilityRead, DataTool: true},
	"list_tables":             {Capability: CapabilityRead, DataTool: true},
	"cross_db_join":           {Capability: CapabilityRead, DataTool: true},
	"exec_sql":                {Capability: CapabilityAIWrite, RequiresApproval: true, DataTool: true},
	"export_excel":            {Capability: CapabilityRead, DataTool: true},
	"export_excel_with_chart": {Capability: CapabilityRead, DataTool: true},
	"export_analysis_docx":    {Capability: CapabilityRead, DataTool: true},
	"export_ppt":              {Capability: CapabilityRead, DataTool: true},
	"export_html":             {Capability: CapabilityRead, DataTool: true},
	"export_by_code":          {Capability: CapabilityCodeExecute, DataTool: true},
	"execute":                 {Capability: CapabilityCodeExecute},
	"write_file":              {Capability: CapabilityCodeExecute},
	"edit_file":               {Capability: CapabilityCodeExecute},
	"read_file_data":          {Capability: CapabilityFileRead},
	"read_offload":            {Capability: CapabilityFileRead},
	"dataset_query":           {Capability: CapabilityRead},
	// analyze_data 的实际数据权限由子 agent 内部的 PermissionMiddleware 逐工具校验，
	// 父层只做能力/注册表层面的放行。
	"analyze_data":          {Capability: CapabilityRead},
	"read_file":             {Capability: CapabilityFileRead},
	"ls":                    {Capability: CapabilityFileRead},
	"glob":                  {Capability: CapabilityFileRead},
	"grep":                  {Capability: CapabilityFileRead},
	"skill":                 {Capability: CapabilityNone},
	"get_current_date_info": {Capability: CapabilityNone},
	// tool_search 由 eino toolsearch 中间件注入（当前已改为静态暴露工具，
	// 保留登记以防未来重新启用时被误判为未登记工具而 fail-closed）。
	"tool_search": {Capability: CapabilityNone},
}

// lookupToolPolicy 查询工具策略；未登记返回 ok=false。
func lookupToolPolicy(name string) (ToolPolicy, bool) {
	p, ok := toolPolicies[name]
	return p, ok
}

// isDataTool 判断工具是否访问数据库数据。
func isDataTool(name string) bool {
	p, ok := toolPolicies[name]
	return ok && p.DataTool
}

// isWriteCapability 判断工具是否会产生数据库写入。
func isWriteCapability(name string) bool {
	p, ok := toolPolicies[name]
	return ok && p.Capability == CapabilityAIWrite
}

// denyByCapability 在分发前做能力双保险：即使模型伪造 tool_call，
// 也不能绕过工具注册阶段的能力裁剪。
func (m *PermissionMiddleware) denyByCapability(toolName string) error {
	if m == nil || m.Scope == nil {
		return &PermissionError{Message: "缺少权限上下文，已拒绝工具执行", Objects: []string{toolName}}
	}
	switch toolName {
	case "exec_sql":
		// AI 数据写：必须满足双门槛（兼容期沿用 allow_modify）
		if !m.Scope.CanAIModifyData() {
			return &PermissionError{
				Message: "当前角色未开放 AI 数据修改权限（需同时允许修改数据与允许 AI 对话修改数据）",
				Objects: []string{},
			}
		}
	case "execute", "write_file", "edit_file":
		// 代码/文件能力保持既有 allow_modify 语义，不引入新开关
		if !m.Scope.AllowModify {
			return &PermissionError{
				Message: "当前角色禁止修改数据（含写操作与命令执行），无法执行该工具",
				Objects: []string{},
			}
		}
	case "export_by_code":
		// 高级导出会执行模型编写的 Python 代码，本质是代码执行能力；
		// 与 execute/write_file 同一 allow_modify 门槛，并额外要求 AI 数据写能力
		//（避免只读/仅经典写角色通过代码绕过 AI 写策略）。
		if !m.Scope.AllowModify || !m.Scope.CanAIModifyData() {
			return &PermissionError{
				Message: "当前角色未开放 AI 数据修改与代码执行权限，无法使用高级导出",
				Objects: []string{},
			}
		}
	}
	return nil
}

// forScope 返回绑定指定连接 Scope 的中间件副本（不修改共享实例）。
func (m *PermissionMiddleware) forScope(scope *PermissionScope) *PermissionMiddleware {
	if scope == nil || scope == m.Scope {
		return m
	}
	cp := *m
	cp.Scope = scope
	return &cp
}

// resolveRunScopedMiddleware 根据工具参数中的目标连接，返回绑定该连接 Scope 的中间件副本。
//
// 规则：
//   - 没有运行安全上下文时拒绝（不允许工具自行沿用构建期静态 Scope）；
//   - 数据工具必须按目标连接取 Scope，未知连接拒绝；
//   - 非数据工具沿用默认连接的 Scope（能力开关是用户级的）。
func (m *PermissionMiddleware) resolveRunScopedMiddleware(ctx context.Context, toolName, args string) (*PermissionMiddleware, *RunSecurityContext, error) {
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return nil, nil, err
	}
	// 运行中权限复核：修订号变化或租约过期时，旧 Scope 不得继续放行数据工具。
	// 这里采用 fail-closed（拒绝并要求重新发起），不做运行内重建：
	// 重建需要重新获取 Schema 集合与历史，超出单次工具调用的职责。
	if sec.Profile.RequiresRevisionLease() {
		current, fresh := admin.SnapshotPermissionRevision()
		if !fresh {
			recordSecurityEvent(StageRevision, toolName, "deny")
			return nil, sec, &PermissionError{Message: "权限修订号不可信（租约过期），已暂停数据工具"}
		}
		if current != sec.Revision {
			recordSecurityEvent(StageRevision, toolName, "changed")
			return nil, sec, &PermissionError{Message: "权限已发生变化，本次操作已失效，请重新发起"}
		}
	}
	if isDataTool(toolName) && !sec.DataToolsAllowed() {
		reason := "数据访问安全依赖不可用"
		if sec.DataEgressBlocked {
			reason = "脱敏规则不可用，已禁止数据访问"
		} else if sec.RevisionUnavailable {
			reason = "权限修订号不可信（租约过期），已禁止数据访问"
		} else if sec.RestrictWrites && isWriteCapability(toolName) {
			reason = "本轮输入触发安全策略，已禁止写操作"
		}
		recordSecurityEvent(StageTool, toolName, "deny")
		return nil, sec, &PermissionError{Message: reason, Objects: []string{}}
	}

	if !isDataTool(toolName) {
		return m.forScope(defaultScopeOrDefault(sec, m.Scope)), sec, nil
	}

	rawConn := extractConnIDFromArgs(args)
	connLookup := buildConnLookup(m.Schemas)
	targetConnID := resolveConnID(sec.DefaultConnID, rawConn, connLookup)
	scope, err := sec.ScopeFor(targetConnID)
	if err != nil {
		recordSecurityEvent(StageTool, toolName, "deny")
		return nil, sec, err
	}
	recordSecurityEvent(StageScope, toolName, "allow")
	return m.forScope(scope), sec, nil
}

func defaultScopeOrDefault(sec *RunSecurityContext, fallback *PermissionScope) *PermissionScope {
	if sec != nil {
		if scope, err := sec.DefaultScope(); err == nil {
			return scope
		}
	}
	return fallback
}

// normalizeToolName 统一工具名大小写（注册表使用小写）。
func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
