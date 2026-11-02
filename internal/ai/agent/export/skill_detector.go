package export

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	skill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// Python 运行时发现约定（Web 版两种打包模式共用）
//
// 外置自动下载模式由 python_bootstrap.py 在目标机安装运行时，并写入
// python-runtime.json（记录解释器绝对路径、版本与来源）；同一目录下的
// python-env.bat / python-env.sh 供运维手动启动时复用相同环境。
const (
	// pythonEnvOverride 运维显式指定解释器路径（优先级最高）
	pythonEnvOverride = "WEBSQL_PYTHON"
	// pythonManifestName 外置模式安装脚本写入的运行时清单
	pythonManifestName = "python-runtime.json"
	// pythonMinMinor 接受的最低 Python 3 次版本号（低于该版本时自动安装脚本会改用托管运行时）
	pythonMinMinor = 8
)

// pythonRuntimeManifest 运行时清单（与 scripts/python_bootstrap.py 的 write_manifest 对应）。
type pythonRuntimeManifest struct {
	PythonPath string `json:"pythonPath"`
	Version    string `json:"version"`
	Source     string `json:"source"`
}

// pythonResolver 把外部依赖（环境变量/文件/命令查找/版本校验/候选目录）注入探测流程，
// 便于单元测试覆盖「环境变量 → 清单 → 随包目录 → 系统 PATH」的优先级。
type pythonResolver struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	lookPath func(string) (string, error)
	validate func(string) (string, bool)
	dirs     []string
}

// SkillEnv 封装 Eino Skill Backend 与 Filesystem Backend 的运行环境。
//
// 重构说明（v1.1）：
//   - 旧版包含 depsState/CheckAndInstallDeps/RunPythonScript 等自定义编排逻辑
//   - 新版由 Eino Skill Middleware（skill 工具）+ Filesystem Middleware（execute 工具）接管
//   - Agent 通过 skill 工具加载 SKILL.md，通过 execute 工具执行 Python 脚本
//   - 本结构仅保留 Backend 引用与 Python 检测能力（用于日志/状态展示）
type SkillEnv struct {
	backend     skill.Backend
	osfsBackend *OSFilesystemBackend
	rootDir     string
}

var (
	pythonAvailable bool
	pythonPath      string
	pythonCheckOnce sync.Once

	defaultSkillEnv     *SkillEnv
	defaultSkillEnvOnce sync.Once
)

// InitSkillEnv 初始化 Skill 环境，创建 Eino Skill Backend。
// 由 builder.go 调用，注册到 Skill Middleware + Filesystem Middleware。
func InitSkillEnv(ctx context.Context, skillsRootDir string) error {
	var initErr error
	defaultSkillEnvOnce.Do(func() {
		osfsBackend := NewOSFilesystemBackend()

		// 检测 Python 路径并设置到后端，用于 Windows 上 python3 → python 替换
		osfsBackend.SetPythonPath(GetPythonPath())

		backend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
			Backend: osfsBackend,
			BaseDir: skillsRootDir,
		})
		if err != nil {
			initErr = fmt.Errorf("创建 Skill Backend 失败: %w", err)
			return
		}

		defaultSkillEnv = &SkillEnv{
			backend:     backend,
			osfsBackend: osfsBackend,
			rootDir:     skillsRootDir,
		}

		logSkillStatus(ctx)
	})
	return initErr
}

// GetSkillEnv 返回全局 SkillEnv 实例。
func GetSkillEnv() *SkillEnv {
	return defaultSkillEnv
}

// skillsRootDir 返回 skills 目录：
// 优先 SkillEnv.rootDir（InitSkillEnv 初始化时确定），其次 SKILLS_DIR 环境变量，
// 最后当前工作目录下的 skills/。
func skillsRootDir() string {
	if env := GetSkillEnv(); env != nil && env.rootDir != "" {
		return env.rootDir
	}
	if dir := os.Getenv("SKILLS_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "skills"
	}
	return filepath.Join(cwd, "skills")
}

// logSkillStatus 启动时打印 Python 与 Skill 发现状态。
func logSkillStatus(ctx context.Context) {
	if !IsPythonAvailable() {
		log.Println("[SkillEnv] Python 未安装，Skill 脚本不可用；报告类导出由 Go 原生渲染器完成，不受影响")
		return
	}

	log.Printf("[SkillEnv] Python: %s (%s)", GetPythonPath(), getPythonVersion())

	if env := GetSkillEnv(); env != nil {
		listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
		defer listCancel()
		if skills, err := env.ListSkills(listCtx); err == nil {
			for _, name := range skills {
				log.Printf("[SkillEnv] 已发现 Skill: %s", name)
			}
		}
	}
}

// ListSkills 列出所有已注册的 Skill 名称。
func (e *SkillEnv) ListSkills(ctx context.Context) ([]string, error) {
	matters, err := e.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(matters))
	for _, m := range matters {
		names = append(names, m.Name)
	}
	return names, nil
}

// Backend 返回 Eino Skill Backend，供 Skill Middleware 使用。
func (e *SkillEnv) Backend() skill.Backend {
	return e.backend
}

// FilesystemBackend 返回 OS 文件系统后端，供 Filesystem Middleware 使用。
// Agent 通过 Filesystem Middleware 的 execute 工具执行 Python 脚本。
func (e *SkillEnv) FilesystemBackend() *OSFilesystemBackend {
	return e.osfsBackend
}

// ──────────────────────────────────────────────
// Python 检测（仅用于日志/状态展示，不参与编排）
// ──────────────────────────────────────────────

// IsPythonAvailable 检测系统是否安装 Python。
// Agent 通过 execute 工具执行 Python 脚本前，可参考此状态。
func IsPythonAvailable() bool {
	pythonCheckOnce.Do(detectPython)
	return pythonAvailable
}

// GetPythonPath 返回 Python 可执行文件路径。
func GetPythonPath() string {
	pythonCheckOnce.Do(detectPython)
	return pythonPath
}

func getPythonVersion() string {
	if !IsPythonAvailable() {
		return ""
	}
	version, _ := validatePythonCandidate(pythonPath)
	return version
}

// detectPython 检测系统中可用的 Python 解释器。
//
// 检测顺序（与发行包 PYTHON-RUNTIME.md 中的说明一致）：
//  1. 环境变量 WEBSQL_PYTHON（运维显式指定；Web 外置模式亦可使用）
//  2. 运行目录下的 python-runtime.json（外置模式安装脚本写入的清单）
//  3. 随包分发的 python/ 目录（内置模式；桌面版/Web 发行包均可携带）
//  4. 系统 PATH 查找（exec.LookPath，操作系统提供的搜索方案）
//
// Wails3 不提供 Python 搜索能力；操作系统 PATH 查找由 exec.LookPath 完成，
// 这就是"按操作系统办法执行"的实现。
func detectPython() {
	path, version, source, ok := resolvePython(defaultPythonResolver())
	if ok {
		log.Printf("[SkillDetector] 发现 Python (%s): %s (%s)", source, path, version)
		pythonAvailable = true
		pythonPath = path
		return
	}

	log.Println("[SkillDetector] 未检测到 Python，Agent 将使用 Go 原生工具兜底")
	pythonAvailable = false
}

// defaultPythonResolver 构造生产环境使用的探测依赖。
func defaultPythonResolver() pythonResolver {
	return pythonResolver{
		getenv:   os.Getenv,
		readFile: os.ReadFile,
		lookPath: exec.LookPath,
		validate: validatePythonCandidate,
		dirs:     candidateSearchDirs(),
	}
}

// resolvePython 按优先级解析解释器，返回路径、版本、来源说明与是否命中。
//
// 每个候选都会执行 `--version` 校验并拒绝 Python 3.8 以下版本，
// 避免把 Windows Store 占位程序或 Python 2 误判为可用运行时。
func resolvePython(r pythonResolver) (path, version, source string, ok bool) {
	accept := func(candidate string) (string, bool) {
		if candidate == "" {
			return "", false
		}
		version, valid := r.validate(candidate)
		if !valid || !isSupportedPythonVersion(version) {
			return "", false
		}
		return version, true
	}

	// 1. 环境变量显式指定
	if override := strings.TrimSpace(r.getenv(pythonEnvOverride)); override != "" {
		candidate := override
		if !filepath.IsAbs(candidate) {
			if resolved, err := r.lookPath(candidate); err == nil {
				candidate = resolved
			}
		}
		if v, valid := accept(candidate); valid {
			return candidate, v, pythonEnvOverride, true
		}
		log.Printf("[SkillDetector] %s 指定的 Python 不可用: %s", pythonEnvOverride, override)
	}

	// 2. 外置模式运行时清单（相对路径按清单所在目录解析）
	for _, dir := range r.dirs {
		manifestPath := filepath.Join(dir, pythonManifestName)
		data, err := r.readFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest pythonRuntimeManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			log.Printf("[SkillDetector] 运行时清单解析失败: %s (%v)", manifestPath, err)
			continue
		}
		candidate := strings.TrimSpace(manifest.PythonPath)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		if v, valid := accept(candidate); valid {
			return candidate, v, pythonManifestName, true
		}
	}

	// 3. 随包分发的 python/ 目录
	for _, dir := range r.dirs {
		if candidate, v := findBundledPython(dir, r.validate); candidate != "" {
			return candidate, v, "bundled", true
		}
	}

	// 4. 系统 PATH
	for _, name := range []string{"python3", "python"} {
		candidate, err := r.lookPath(name)
		if err != nil {
			continue
		}
		if v, valid := accept(candidate); valid {
			return candidate, v, "PATH", true
		}
	}

	return "", "", "", false
}

// isSupportedPythonVersion 校验 `python --version` 输出（如 "Python 3.15.2"）。
func isSupportedPythonVersion(version string) bool {
	text := strings.TrimSpace(strings.ToLower(version))
	text = strings.TrimSpace(strings.TrimPrefix(text, "python"))
	parts := strings.SplitN(text, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return false
	}
	return major == 3 && minor >= pythonMinMinor
}

// candidateSearchDirs 返回需要检查捆绑 Python 分发文件的候选目录列表（去重）。
// 可执行文件所在目录是首要候选（zip 内 python/ 与二进制同级，桌面版/Web 版一致）；
// 当前工作目录作为补充（dev 模式下 exe 在 build 子目录，而 Python 在项目根）。
func candidateSearchDirs() []string {
	var dirs []string
	seen := make(map[string]bool)

	addDir := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			dirs = append(dirs, abs)
		}
	}

	if exePath, err := os.Executable(); err == nil {
		addDir(filepath.Dir(exePath))
	}
	if cwd, err := os.Getwd(); err == nil {
		addDir(cwd)
	}

	return dirs
}

// findBundledPython 在指定目录下查找捆绑的 Python 分发文件。
// 支持常见便携/嵌入式分发布局（内置模式来自 embeddable 包，外置模式来自
// python-build-standalone 的 install_only 布局）：
//   - Windows: python\python.exe, py\python.exe, python3.exe, python.exe
//   - Unix:    python/bin/python3, python/bin/python, bin/python3, bin/python
//
// validate 由调用方注入（生产环境为 validatePythonCandidate），便于测试。
// 返回找到的可执行文件绝对路径及其版本号；未找到返回空字符串。
func findBundledPython(dir string, validate func(string) (string, bool)) (path, version string) {
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(dir, "python", "python.exe"),
			filepath.Join(dir, "py", "python.exe"),
			filepath.Join(dir, "python3.exe"),
			filepath.Join(dir, "python.exe"),
		}
	} else {
		candidates = []string{
			filepath.Join(dir, "python", "bin", "python3"),
			filepath.Join(dir, "python", "bin", "python"),
			filepath.Join(dir, "bin", "python3"),
			filepath.Join(dir, "bin", "python"),
		}
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if v, ok := validate(candidate); ok {
			return candidate, v
		}
	}
	return "", ""
}

// validatePythonCandidate 执行 `<path> --version` 验证候选路径可用。
// 返回版本字符串（如 "Python 3.11.5"）和是否可用。
func validatePythonCandidate(path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--version")
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}
