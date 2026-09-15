// sandbox.go — execute / 文件工具的沙箱与命令白名单校验。
//
// 安全背景：LLM 可被提示注入诱导（用户上传文件内容、表注释、SQL 注释都可携带指令），
// 而 filesystem 中间件暴露的 read_file/write_file/edit_file/glob/grep/execute
// 原先没有任何路径约束与命令白名单，等价于「任意文件读写 + 任意命令执行」。
//
// 本文件把文件与命令访问收敛到限定根目录内：
//   - 读：workspace（工作区）/ exports / skills（技能目录，只读）/ data/reduction / 系统临时目录
//   - 写：workspace / exports / 系统临时目录（skills 只读，防止技能脚本被篡改）
//   - 命令：首个 token 必须是 python/pip 系列（与 skills 实际用法一致），
//     叠加危险命令黑名单与「绝对路径必须落在允许根目录内」的检查
//
// 说明：
//   - 相对路径按「进程工作目录 → 工作区目录」顺序尝试，均落在允许根内才放行，
//     因此 `exports/a.xlsx`（旧行为）与 `ppt_input.json`（写入工作区）都可用，
//     而 `../../etc/passwd`、`C:\Windows\...` 一律拒绝
//   - 不解析符号链接，但对已存在的符号链接目标直接拒绝写入
package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	envAgentWorkspace = "AGENT_WORKSPACE_DIR"
	envReductionDir   = "REDUCTION_OFFLOAD_DIR"
)

type sandboxRoots struct {
	workspace string
	read      []string
	write     []string
}

var (
	sandboxOnce  sync.Once
	sandboxValue sandboxRoots
)

// allowedCommandPrefixes execute 工具允许执行的命令白名单（按分段首 token 精确匹配）。
// python/pip 系列是 skills 渲染脚本的实际用法；set/export/chcp/cd 用于编码与环境准备，
// 它们只影响子进程环境与工作目录，不构成文件系统越权。
var allowedCommandPrefixes = map[string]bool{
	"python": true, "python3": true, "python.exe": true, "python3.exe": true,
	"py": true,
	"pip": true, "pip3": true, "pip.exe": true, "pip3.exe": true,
	"set": true, "export": true, "chcp": true, "cd": true,
}

// commandBlacklistPatterns 危险命令黑名单（小写归一化后子串匹配）。
// 与白名单叠加使用：白名单限制「能跑什么」，黑名单限制「明显危险的参数形态」。
var commandBlacklistPatterns = []string{
	// 文件/目录批量删除（覆盖 -rf/-fr/-Rf/-r 与 Windows /s /q /f 变体）
	"rm -rf", "rm -fr", "rm -r ", "rm -rf/", "rm --recursive",
	"rmdir /s", "rd /s", "del /s", "del /q", "del /f", "erase /s",
	// 磁盘/分区/系统
	"format ", "mkfs", "dd if=", "diskpart", "fdisk",
	"shutdown", "reboot", "halt", "poweroff", "bcdedit",
	// 注册表 / 系统配置 / 权限
	"reg add", "reg delete", "reg import", "takeown", "icacls", "chmod 777 /", "chown -r",
	// 账户与服务
	"net user", "net localgroup", "useradd", "userdel", "passwd",
	"systemctl", "launchctl", "sc delete", "sc config", "schtasks", "crontab",
	// 间接执行 / 编码执行 / 网络外传
	"powershell", "pwsh", "wmic", "certutil", "bitsadmin", "mshta",
	"curl ", "wget ", "scp ", "ssh ", "rsync ", "nc ", "ncat ", "netcat",
	"invoke-webrequest", "invoke-expression", "start-process",
	// 环境变量劫持（会影响后续 python 解析路径）
	"set path=", "export path=", "set pythonpath=", "export pythonpath=",
	// fork bomb
	":(){:|:&};:",
	// 设备直写
	"> /dev/sd",
}

// winAbsPathRegex / unixAbsPathRegex 用于从命令中提取绝对路径，做沙箱越界检查。
var (
	winAbsPathRegex  = regexp.MustCompile(`[A-Za-z]:[\\/][^\s"'<>|;&]*`)
	unixAbsPathRegex = regexp.MustCompile(`(?:^|[\s"'=(,:<>])/[A-Za-z0-9_.][^\s"'<>|;&]*`)
)

// devicePathAllowlist 允许访问的设备文件（重定向到 null 是常见用法）
var devicePathAllowlist = map[string]bool{
	"/dev/null": true, "/dev/stdout": true, "/dev/stderr": true,
}

func agentWorkspaceDir() string {
	if dir := strings.TrimSpace(os.Getenv(envAgentWorkspace)); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.TempDir()
	}
	return filepath.Join(cwd, "data", "agent_workspace")
}

func reductionOffloadDir() string {
	if dir := strings.TrimSpace(os.Getenv(envReductionDir)); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.TempDir()
	}
	return filepath.Join(cwd, "data", "reduction")
}

func cwdDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return os.TempDir()
	}
	return cwd
}

// loadSandboxRoots 计算沙箱根目录，并确保工作区目录存在。
// 工作区创建失败时降级到系统临时目录，保证功能可用且仍受限于沙箱。
func loadSandboxRoots() sandboxRoots {
	workspace := agentWorkspaceDir()
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		fallback := filepath.Join(os.TempDir(), "websql-agent-workspace")
		if err2 := os.MkdirAll(fallback, 0o755); err2 == nil {
			workspace = fallback
		} else {
			workspace = ""
		}
	}

	cwd := cwdDir()
	read := []string{workspace, filepath.Join(cwd, "exports"), filepath.Join(cwd, "skills"), reductionOffloadDir(), os.TempDir()}
	write := []string{workspace, filepath.Join(cwd, "exports"), os.TempDir()}

	// 过滤空项
	filterEmpty := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, p := range in {
			if strings.TrimSpace(p) != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return sandboxRoots{workspace: workspace, read: filterEmpty(read), write: filterEmpty(write)}
}

func getSandboxRoots() sandboxRoots {
	sandboxOnce.Do(func() { sandboxValue = loadSandboxRoots() })
	return sandboxValue
}

// pathWithinRoot 判断 absPath 是否位于 root 之内（Windows 下大小写不敏感）。
func pathWithinRoot(absPath, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func withinAnyRoot(absPath string, roots []string) bool {
	for _, root := range roots {
		if pathWithinRoot(absPath, root) {
			return true
		}
	}
	return false
}

// candidatePaths 生成待检查的候选绝对路径：
// 绝对路径只取自身；相对路径依次尝试「进程工作目录」与「工作区目录」。
func candidatePaths(p string) []string {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	if filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			return []string{abs}
		}
		return nil
	}
	var out []string
	if abs, err := filepath.Abs(p); err == nil {
		out = append(out, abs)
	}
	if ws := getSandboxRoots().workspace; ws != "" {
		out = append(out, filepath.Join(ws, p))
	}
	return out
}

// resolveReadPath 校验并返回可读的绝对路径。
func resolveReadPath(p string) (string, error) {
	roots := getSandboxRoots()
	for _, cand := range candidatePaths(p) {
		if withinAnyRoot(cand, roots.read) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("路径 %q 不在允许访问的范围内（仅允许：工作区、exports 导出目录、skills 技能目录、data/reduction、系统临时目录）", p)
}

// resolveWritePath 校验并返回可写的绝对路径。
// 额外拒绝写入已存在的符号链接，避免通过链接把写入重定向到沙箱之外。
func resolveWritePath(p string) (string, error) {
	roots := getSandboxRoots()
	for _, cand := range candidatePaths(p) {
		if !withinAnyRoot(cand, roots.write) {
			continue
		}
		if fi, err := os.Lstat(cand); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("目标路径 %q 是符号链接，出于安全考虑拒绝写入", p)
		}
		return cand, nil
	}
	return "", fmt.Errorf("路径 %q 不在允许写入的范围内（仅允许：工作区、exports 导出目录、系统临时目录）", p)
}

// splitCommandSegments 按 shell 连接符切分命令，用于逐段做白名单校验。
func splitCommandSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "\r\n", "\n", "\r", "\n")
	return strings.Split(replacer.Replace(command), "\n")
}

// firstToken 返回命令片段的首个 token（小写，去除包裹引号）。
func firstToken(segment string) string {
	segment = strings.TrimSpace(segment)
	// 跳过前导环境变量前缀（如 FOO=bar python x.py）
	for {
		fields := strings.Fields(segment)
		if len(fields) == 0 {
			return ""
		}
		first := fields[0]
		if eq := strings.Index(first, "="); eq > 0 && !strings.HasPrefix(first, "=") {
			segment = strings.TrimSpace(strings.TrimPrefix(segment, first))
			continue
		}
		return strings.ToLower(strings.Trim(first, `"'`))
	}
}

// extractAbsolutePaths 从命令中提取绝对路径（含 Windows 盘符路径与 Unix 路径）。
func extractAbsolutePaths(command string) []string {
	var out []string
	for _, m := range winAbsPathRegex.FindAllString(command, -1) {
		out = append(out, m)
	}
	for _, m := range unixAbsPathRegex.FindAllString(command, -1) {
		trimmed := strings.TrimLeft(m, " \t\"'=(,:<>")
		if trimmed == "" || strings.Contains(trimmed, "://") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// validateSandboxedCommand 是 execute 工具的最终命令校验器：
//  1. 危险命令黑名单（保留原有能力，防止明显破坏性操作）
//  2. 分段白名单：命令的每一段首 token 必须是 python/pip 系列或环境准备指令
//  3. 绝对路径沙箱检查：命令中出现的绝对路径必须落在允许根目录内
func validateSandboxedCommand(command string) error {
	if err := defaultCommandValidator(command); err != nil {
		return err
	}
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if lower == "" {
		return errors.New("命令为空")
	}
	for _, pattern := range commandBlacklistPatterns {
		if strings.Contains(lower, pattern) {
			return fmt.Errorf("命令包含危险操作 [%s]，已被安全校验拦截", pattern)
		}
	}

	for _, seg := range splitCommandSegments(lower) {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		token := firstToken(seg)
		if token == "" {
			continue
		}
		if !allowedCommandPrefixes[token] {
			return fmt.Errorf("命令 %q 不在允许范围内（execute 仅允许 python/pip 系列命令）", token)
		}
	}

	roots := getSandboxRoots()
	for _, p := range extractAbsolutePaths(command) {
		if devicePathAllowlist[strings.ToLower(p)] {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("命令中的路径 %q 无法解析，已拦截", p)
		}
		if !withinAnyRoot(abs, roots.read) {
			return fmt.Errorf("命令引用了沙箱之外的路径 %q，已拦截", p)
		}
	}
	return nil
}