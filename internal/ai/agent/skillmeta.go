// skillmeta.go — Skill 元信息体系：版本管理、依赖检测、错误提示、命令黑名单
//
// 本文件为 Skill 体系增加结构化的元信息（在 SKILL.md 的 YAML front matter 中声明），
// 用于提升 AI 分析的可靠性和强大性：
//   - version / min_agent_version：版本管理与兼容性检查
//   - dependencies：执行前依赖检测（skill / context / permission 三类）
//   - error_hints：失败提示增强（错误模式 → 友好提示 + 恢复建议）
//   - command_blacklist：命令黑名单（禁止执行的 SQL 命令模式）
//
// 设计原则：
//   1. 向后兼容：所有扩展字段均为可选，旧 SKILL.md 仍能正常工作
//   2. 不侵入 Eino：独立解析 front matter，不修改 Eino skill 库
//   3. 零值安全：字段为零值时跳过对应检查
package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AgentVersion 当前 Agent 的版本号（用于 Skill 版本兼容性检查）
const AgentVersion = "1.0.0"

// ──────────────────────────────────────────────
// 元信息结构定义
// ──────────────────────────────────────────────

// SkillMeta 描述 Skill 的扩展元信息（在 SKILL.md front matter 中声明）
type SkillMeta struct {
	Name             string            `yaml:"name"`              // Skill 名称（与目录名一致）
	Version          string            `yaml:"version"`           // Skill 版本号，如 "1.0.0"
	MinAgentVersion  string            `yaml:"min_agent_version"` // 要求的最低 Agent 版本
	Dependencies     []SkillDependency `yaml:"dependencies"`      // 执行前依赖列表
	ErrorHints       []SkillErrorHint  `yaml:"error_hints"`       // 错误提示映射
	CommandBlacklist []string          `yaml:"command_blacklist"` // 禁止的 SQL 命令模式（大写）
}

// SkillDependency 描述 Skill 的依赖项
//
// 依赖类型（Type）：
//   - skill      ：依赖其他 Skill 已注册可用（如"导出"依赖"查询"）
//   - context    ：依赖运行时上下文中存在特定字段（如 connection_id、schema）
//   - permission ：依赖特定权限（如 write、ddl）
type SkillDependency struct {
	Type        string `yaml:"type"`         // skill | context | permission
	Name        string `yaml:"name"`         // 依赖名（见下方各类型的可用名称）
	Description string `yaml:"description"`  // 人类可读的依赖说明
}

// SkillErrorHint 错误提示映射：将错误信息模式映射到友好提示和恢复建议
type SkillErrorHint struct {
	Pattern    string `yaml:"pattern"`    // 错误信息匹配模式（子字符串匹配，不区分大小写）
	Hint       string `yaml:"hint"`       // 友好提示
	Suggestion string `yaml:"suggestion"` // 建议的恢复操作
}

// SkillCheckContext 依赖检查的运行时上下文
type SkillCheckContext struct {
	ConnID          string          // 当前连接 ID
	Scope           *PermissionScope // 用户权限范围
	Schemas         []SchemaRef     // 可用的 schema 列表
	DBType          string          // 数据库类型
	DBSchema        string          // 当前 schema
	AvailableSkills []string        // 已注册的 Skill 名列表
}

// ──────────────────────────────────────────────
// 注册中心
// ──────────────────────────────────────────────

// SkillMetaRegistry Skill 元信息注册中心
type SkillMetaRegistry struct {
	mu        sync.RWMutex
	metas     map[string]*SkillMeta // skill name -> meta
	skillsDir string
}

var globalSkillMetaRegistry = &SkillMetaRegistry{
	metas: make(map[string]*SkillMeta),
}

// InitSkillMetaRegistry 初始化 Skill 元信息注册中心，从 skillsDir 加载所有 SKILL.md。
// 由 builder.go 在 Agent 创建时调用。
func InitSkillMetaRegistry(skillsDir string) {
	globalSkillMetaRegistry.skillsDir = skillsDir
	globalSkillMetaRegistry.loadAll()
}

// loadAll 从 skillsDir 加载所有 SKILL.md 的扩展元信息
func (r *SkillMetaRegistry) loadAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skillsDir == "" {
		return
	}

	entries, err := os.ReadDir(r.skillsDir)
	if err != nil {
		log.Printf("[SkillMeta] 读取 skills 目录失败 - dir=%s, err=%v\n", r.skillsDir, err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(r.skillsDir, entry.Name(), "SKILL.md")
		meta, err := parseSkillMeta(skillFile)
		if err != nil {
			log.Printf("[SkillMeta] 解析 SKILL.md 失败 - file=%s, err=%v\n", skillFile, err)
			continue
		}
		if meta == nil {
			continue
		}
		if meta.Name == "" {
			meta.Name = entry.Name()
		}
		r.metas[meta.Name] = meta
		log.Printf("[SkillMeta] 已加载 - name=%s, version=%s, deps=%d, hints=%d, blacklist=%d\n",
			meta.Name, meta.Version, len(meta.Dependencies), len(meta.ErrorHints), len(meta.CommandBlacklist))
	}
}

// GetSkillMeta 获取指定 Skill 的元信息（线程安全）
func (r *SkillMetaRegistry) GetSkillMeta(name string) *SkillMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metas[name]
}

// AllMetas 返回所有已加载的 Skill 元信息（副本）
func (r *SkillMetaRegistry) AllMetas() []*SkillMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*SkillMeta, 0, len(r.metas))
	for _, m := range r.metas {
		result = append(result, m)
	}
	return result
}

// AvailableSkillNames 返回所有已注册的 Skill 名列表
func (r *SkillMetaRegistry) AvailableSkillNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.metas))
	for name := range r.metas {
		result = append(result, name)
	}
	return result
}

// GlobalCommandBlacklist 返回所有 Skill 声明的命令黑名单（去重，大写）
func (r *SkillMetaRegistry) GlobalCommandBlacklist() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool)
	var result []string
	for _, m := range r.metas {
		for _, p := range m.CommandBlacklist {
			upper := strings.ToUpper(strings.TrimSpace(p))
			if upper != "" && !seen[upper] {
				seen[upper] = true
				result = append(result, upper)
			}
		}
	}
	return result
}

// ──────────────────────────────────────────────
// SKILL.md 解析
// ──────────────────────────────────────────────

// parseSkillMeta 解析 SKILL.md 的 YAML front matter，提取扩展元信息。
// 返回 nil 表示文件不存在或无 front matter（非错误，向后兼容）。
func parseSkillMeta(filePath string) (*SkillMeta, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	frontMatter := extractFrontMatter(content)
	if frontMatter == "" {
		return nil, nil
	}

	var meta SkillMeta
	if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
		return nil, fmt.Errorf("解析 YAML front matter 失败: %w", err)
	}
	return &meta, nil
}

// extractFrontMatter 提取 SKILL.md 的 YAML front matter（--- 之间的内容）
func extractFrontMatter(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ""
	}
	// 首行必须是 ---
	if strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	var sb strings.Builder
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return sb.String()
}

// ──────────────────────────────────────────────
// 依赖检测
// ──────────────────────────────────────────────

// CheckDependencies 检查 Skill 的依赖是否满足。
// 返回 nil 表示依赖满足，返回 error 表示依赖不满足（包含友好提示，不执行 Skill）。
func CheckDependencies(meta *SkillMeta, ctx *SkillCheckContext) error {
	if meta == nil || ctx == nil {
		return nil
	}
	for _, dep := range meta.Dependencies {
		switch strings.ToLower(dep.Type) {
		case "skill":
			if !containsString(ctx.AvailableSkills, dep.Name) {
				desc := dep.Description
				if desc == "" {
					desc = fmt.Sprintf("Skill %s 依赖的 Skill %s 未注册或不可用", meta.Name, dep.Name)
				}
				return fmt.Errorf("%s", desc)
			}
		case "context":
			if err := checkContextDependency(dep.Name, ctx, meta.Name); err != nil {
				return err
			}
		case "permission":
			if err := checkPermissionDependency(dep.Name, ctx, meta.Name); err != nil {
				return err
			}
		default:
			// 未知依赖类型，记录日志但不阻止执行（向后兼容）
			log.Printf("[SkillMeta] 未知依赖类型 - skill=%s, depType=%s, depName=%s\n",
				meta.Name, dep.Type, dep.Name)
		}
	}
	return nil
}

// checkContextDependency 检查上下文依赖
// 支持的 name：connection_id | schema | db_type
func checkContextDependency(name string, ctx *SkillCheckContext, skillName string) error {
	switch strings.ToLower(name) {
	case "connection_id", "connid":
		if ctx.ConnID == "" {
			return fmt.Errorf("Skill %s 依赖连接 ID（connection_id），但当前上下文未提供连接", skillName)
		}
	case "schema":
		if ctx.DBSchema == "" && len(ctx.Schemas) == 0 {
			return fmt.Errorf("Skill %s 依赖 schema 信息，但当前上下文未提供 schema", skillName)
		}
	case "db_type":
		if ctx.DBType == "" {
			return fmt.Errorf("Skill %s 依赖数据库类型（db_type），但当前上下文未提供", skillName)
		}
	default:
		// 未知 context 依赖，跳过（向后兼容）
	}
	return nil
}

// checkPermissionDependency 检查权限依赖
// 支持的 name：write | ddl | admin
func checkPermissionDependency(name string, ctx *SkillCheckContext, skillName string) error {
	switch strings.ToLower(name) {
	case "write", "write_permission":
		if ctx.Scope == nil || !ctx.Scope.AllowModify {
			return fmt.Errorf("Skill %s 需要写权限，当前用户无写权限。请联系管理员或切换有权限的连接", skillName)
		}
	case "ddl", "ddl_permission":
		if ctx.Scope == nil || !ctx.Scope.AllowModify {
			return fmt.Errorf("Skill %s 需要 DDL 权限，当前用户无 DDL 权限。请联系管理员", skillName)
		}
	case "admin", "admin_permission":
		// 远程模式下需要管理员权限；非远程模式默认通过
		if ctx.Scope != nil && ctx.Scope.IsRemote && !ctx.Scope.HasFullConnAccess {
			return fmt.Errorf("Skill %s 需要管理员权限，当前用户权限不足。请联系管理员", skillName)
		}
	default:
		// 未知 permission 依赖，跳过（向后兼容）
	}
	return nil
}

// ──────────────────────────────────────────────
// 失败提示增强
// ──────────────────────────────────────────────

// MatchErrorHint 根据错误信息匹配指定 Skill 的 errorHints，返回友好提示。
// 返回空字符串表示未匹配到。
func MatchErrorHint(meta *SkillMeta, err error) string {
	if meta == nil || err == nil || len(meta.ErrorHints) == 0 {
		return ""
	}
	return matchErrorHints(meta.ErrorHints, err.Error())
}

// MatchGlobalErrorHint 在所有已注册 Skill 的 errorHints 中匹配错误信息。
// 返回首个匹配的提示。适用于无法确定具体 Skill 的场景。
func MatchGlobalErrorHint(err error) string {
	if err == nil {
		return ""
	}
	metas := globalSkillMetaRegistry.AllMetas()
	errStr := err.Error()
	for _, m := range metas {
		if hint := matchErrorHints(m.ErrorHints, errStr); hint != "" {
			return hint
		}
	}
	return ""
}

// matchErrorHints 在提示列表中匹配错误信息
func matchErrorHints(hints []SkillErrorHint, errStr string) string {
	if len(hints) == 0 || errStr == "" {
		return ""
	}
	lowerErr := strings.ToLower(errStr)
	for _, hint := range hints {
		if hint.Pattern == "" {
			continue
		}
		if strings.Contains(lowerErr, strings.ToLower(hint.Pattern)) {
			result := hint.Hint
			if hint.Suggestion != "" {
				result += "\n建议操作：" + hint.Suggestion
			}
			return result
		}
	}
	return ""
}

// ──────────────────────────────────────────────
// 命令黑名单
// ──────────────────────────────────────────────

// defaultCommandBlacklist 默认命令黑名单（即使 SKILL.md 未声明也始终生效）
// 这些是极其危险的命令，在 Agent 场景下应始终禁止
var defaultCommandBlacklist = []string{
	"DROP DATABASE",
	"DROP SCHEMA",
	"SHUTDOWN",
	"KILL ",        // MySQL KILL 连接
	"LOAD DATA INFILE", // 可能读取服务端文件
}

// CheckCommandBlacklist 检查 SQL 是否命中命令黑名单。
// 合并默认黑名单和 Skill 声明的黑名单进行检查。
// 返回 nil 表示未命中，返回 error 表示命中（包含友好提示）。
func CheckCommandBlacklist(sql string, blacklist []string) error {
	if sql == "" {
		return nil
	}
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	// 先检查默认黑名单
	for _, pattern := range defaultCommandBlacklist {
		if matchesBlacklistPattern(upperSQL, pattern) {
			return fmt.Errorf("此命令已被安全策略禁止：%s。如需执行，请联系管理员调整配置", pattern)
		}
	}

	// 再检查 Skill 声明的黑名单
	for _, pattern := range blacklist {
		upper := strings.ToUpper(strings.TrimSpace(pattern))
		if upper == "" {
			continue
		}
		if matchesBlacklistPattern(upperSQL, upper) {
			return fmt.Errorf("此命令已被安全策略禁止：%s。如需执行，请联系管理员调整配置", upper)
		}
	}
	return nil
}

// matchesBlacklistPattern 检查 SQL 是否包含黑名单模式
// 使用前缀或包含匹配：如 "DROP DATABASE" 匹配 "DROP DATABASE mydb"
func matchesBlacklistPattern(upperSQL, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	// 对以命令关键字开头的模式，使用前缀匹配（避免误判包含该字符串的注释）
	// 对其他模式，使用包含匹配
	if isCommandPrefix(pattern) {
		return strings.HasPrefix(upperSQL, pattern)
	}
	return strings.Contains(upperSQL, pattern)
}

// isCommandPrefix 判断模式是否为 SQL 命令前缀（以大写字母开头）
func isCommandPrefix(pattern string) bool {
	if pattern == "" {
		return false
	}
	first := pattern[0]
	return first >= 'A' && first <= 'Z'
}

// ──────────────────────────────────────────────
// 版本管理
// ──────────────────────────────────────────────

// CheckVersionCompatibility 检查所有 Skill 的版本兼容性。
// 返回警告信息列表（不阻止运行，仅记录日志，降级模式）。
func CheckVersionCompatibility(agentVersion string, metas []*SkillMeta) []string {
	var warnings []string
	for _, m := range metas {
		if m.MinAgentVersion == "" {
			continue
		}
		if compareVersions(agentVersion, m.MinAgentVersion) < 0 {
			warnings = append(warnings, fmt.Sprintf(
				"Skill %s 声明需要 Agent 版本 >= %s，当前 Agent 版本为 %s（降级模式运行）",
				m.Name, m.MinAgentVersion, agentVersion,
			))
		}
	}
	return warnings
}

// compareVersions 比较两个语义化版本号
// 返回 -1 表示 v1 < v2，0 表示相等，1 表示 v1 > v2
func compareVersions(v1, v2 string) int {
	p1 := parseVersion(v1)
	p2 := parseVersion(v2)
	for i := 0; i < 3; i++ {
		if p1[i] < p2[i] {
			return -1
		}
		if p1[i] > p2[i] {
			return 1
		}
	}
	return 0
}

// parseVersion 解析版本号为 [major, minor, patch] 三段整数
func parseVersion(v string) [3]int {
	var parts [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	segs := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(segs); i++ {
		num := 0
		for _, ch := range segs[i] {
			if ch >= '0' && ch <= '9' {
				num = num*10 + int(ch-'0')
			} else {
				break
			}
		}
		parts[i] = num
	}
	return parts
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

// containsString 检查字符串切片是否包含指定字符串（不区分大小写）
func containsString(slice []string, target string) bool {
	if target == "" {
		return false
	}
	for _, s := range slice {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}
