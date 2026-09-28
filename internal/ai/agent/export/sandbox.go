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

// ──────────────────────────────────────────────
// execute 命令策略：只允许「运行技能目录内的 Python 脚本」，不经过 shell
// ──────────────────────────────────────────────
//
// 旧策略（首 token 白名单 python/pip + 危险子串黑名单 + 绝对路径检查）存在根本缺陷：
//   - `python -c "..."`、`python -m ...`、`pip install <任意包>` 本身就是任意代码执行；
//   - 模型可先 write_file 写一个 .py 到工作区再 `python x.py` 执行；
//   - 命令经 `cmd /C` / `sh -c` 执行，Windows 单个 `&`、`$(...)`、反引号等元字符未被切分校验。
// 在提示注入（上传文件、表注释、查询结果都可能携带指令）面前，上述任一缺口都等价于远程代码执行。
//
// 新策略：
//  1. 解析为 argv，直接 exec（不经 shell），任何未加引号的 shell 元字符一律拒绝；
//  2. 解释器必须是 python 系列，解释器参数仅允许 -u / -B；
//  3. 脚本必须位于只读技能目录（skills/）内且真实存在；
//  4. 其余参数只能是沙箱可读范围内的文件路径或不含特殊字符的普通值；
//  5. 允许 `< input.json` 形式的标准输入重定向（文件同样须在沙箱内）。

// skillInvocation 解析后的技能脚本调用
type skillInvocation struct {
	pyFlags   []string
	script    string
	args      []string
	stdinFile string
}

var (
	// 常见的无害前缀（编码/环境准备），解析前剥离
	harmlessPrefixRe = regexp.MustCompile(`(?i)^\s*(chcp\s+\d+(\s*>\s*nul)?|set\s+PYTHONIOENCODING=utf-?8|export\s+PYTHONIOENCODING=utf-?8|set\s+PYTHONUTF8=1|export\s+PYTHONUTF8=1)\s*&&\s*`)
	// 常见的无害后缀（日志截断/合并输出），解析前剥离
	harmlessSuffixRe = regexp.MustCompile(`(?i)(\s*2>&1)?(\s*\|\s*tail\s+-?n?\s*\d+)?\s*$`)
	envPrefixRe      = regexp.MustCompile(`^(PYTHONIOENCODING=utf-?8|PYTHONUTF8=1)\s+`)
	plainArgRe       = regexp.MustCompile(`^[\p{L}\p{N}_.,:=@+\-/\\ ]+$`)
)

var allowedPythonNames = map[string]bool{
	"python": true, "python3": true, "py": true, "python.exe": true, "python3.exe": true, "py.exe": true,
}

// splitArgv 按 POSIX 风格切分参数（支持单双引号），遇到未加引号的 shell 元字符返回错误。
func splitArgv(command string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	quote := rune(0)
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case r == '<':
			// 标准输入重定向作为独立 token
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
			args = append(args, "<")
		case strings.ContainsRune("&|;$`()>\n\r*?!%^{}", r):
			return nil, fmt.Errorf("命令包含不允许的 shell 元字符 %q（execute 不经过 shell，仅支持运行技能脚本）", r)
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("命令引号未闭合")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// mapTmpPath 把模型习惯使用的 /tmp/ 路径映射到系统临时目录（跨平台）
func mapTmpPath(p string) string {
	if strings.HasPrefix(p, "/tmp/") {
		return filepath.Join(os.TempDir(), filepath.FromSlash(strings.TrimPrefix(p, "/tmp/")))
	}
	return p
}

// resolveSkillScript 解析脚本路径：必须位于技能根目录内、是 .py 普通文件、且不在任何可写根目录内
func resolveSkillScript(p string) (string, error) {
	if !strings.EqualFold(filepath.Ext(p), ".py") {
		return "", fmt.Errorf("只允许运行 .py 技能脚本，当前为 %q", p)
	}
	root, err := filepath.Abs(skillsRootDir())
	if err != nil {
		return "", err
	}
	p = filepath.FromSlash(mapTmpPath(p))
	candidates := []string{}
	if filepath.IsAbs(p) {
		candidates = append(candidates, filepath.Clean(p))
	} else {
		if abs, err := filepath.Abs(p); err == nil {
			candidates = append(candidates, abs)
		}
		trimmed := strings.TrimPrefix(strings.TrimPrefix(filepath.ToSlash(p), "./"), "skills/")
		candidates = append(candidates, filepath.Join(root, filepath.FromSlash(trimmed)))
	}
	writeRoots := getSandboxRoots().write
	for _, cand := range candidates {
		if !pathWithinRoot(cand, root) || withinAnyRoot(cand, writeRoots) {
			continue
		}
		fi, err := os.Lstat(cand)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		return cand, nil
	}
	return "", fmt.Errorf("脚本 %q 不在技能目录内或不存在（execute 仅允许运行 skills/ 下的技能脚本）", p)
}

// parseSkillCommand 把模型给出的命令解析为受限的技能脚本调用
func parseSkillCommand(command string) (*skillInvocation, error) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return nil, errors.New("命令为空")
	}
	for {
		stripped := harmlessPrefixRe.ReplaceAllString(cmd, "")
		stripped = envPrefixRe.ReplaceAllString(stripped, "")
		if stripped == cmd {
			break
		}
		cmd = stripped
	}
	cmd = strings.TrimSpace(harmlessSuffixRe.ReplaceAllString(cmd, ""))

	argv, err := splitArgv(cmd)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, errors.New("命令为空")
	}
	if !allowedPythonNames[strings.ToLower(filepath.Base(argv[0]))] {
		return nil, fmt.Errorf("命令 %q 不在允许范围内（execute 仅允许 python 运行 skills/ 下的技能脚本；依赖需随部署预装，禁止运行时 pip 安装）", argv[0])
	}

	inv := &skillInvocation{}
	i := 1
	for ; i < len(argv) && strings.HasPrefix(argv[i], "-"); i++ {
		if argv[i] != "-u" && argv[i] != "-B" {
			return nil, fmt.Errorf("不允许的解释器参数 %q（禁止 -c/-m 等内联代码或模块执行）", argv[i])
		}
		inv.pyFlags = append(inv.pyFlags, argv[i])
	}
	if i >= len(argv) {
		return nil, errors.New("缺少要运行的技能脚本路径")
	}
	script, err := resolveSkillScript(argv[i])
	if err != nil {
		return nil, err
	}
	inv.script = script

	for i++; i < len(argv); i++ {
		a := argv[i]
		if a == "<" {
			if i+1 >= len(argv) || inv.stdinFile != "" {
				return nil, errors.New("标准输入重定向格式错误")
			}
			p, err := resolveReadPath(mapTmpPath(argv[i+1]))
			if err != nil {
				return nil, err
			}
			inv.stdinFile = p
			i++
			continue
		}
		if looksLikePath(a) {
			p, err := resolveReadPath(mapTmpPath(a))
			if err != nil {
				// 输出文件路径可能尚不存在：只要落在可写根内即可
				wp, werr := resolveWritePath(mapTmpPath(a))
				if werr != nil {
					return nil, err
				}
				p = wp
			}
			inv.args = append(inv.args, p)
			continue
		}
		if !plainArgRe.MatchString(a) || strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("不允许的脚本参数 %q", a)
		}
		inv.args = append(inv.args, a)
	}
	return inv, nil
}

func looksLikePath(a string) bool {
	return strings.ContainsAny(a, `/\`) || strings.HasSuffix(strings.ToLower(a), ".json") ||
		(len(a) > 1 && a[1] == ':')
}

// validateSandboxedCommand 是 execute 工具的命令校验入口（保留原函数名供调用方与测试使用）
func validateSandboxedCommand(command string) error {
	_, err := parseSkillCommand(command)
	return err
}
