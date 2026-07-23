package export

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	skill "github.com/cloudwego/eino/adk/middlewares/skill"
	"websql/internal/config"
)

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

// logSkillStatus 启动时打印 Python 与 Skill 发现状态。
func logSkillStatus(ctx context.Context) {
	if !IsPythonAvailable() {
		log.Println("[SkillEnv] Python 未安装，Skill 脚本将无法执行，Agent 会回退 Go 原生工具")
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
// 检测顺序（桌面/本地模式下）：
//  1. 运行目录下的捆绑 Python 分发文件（python/python.exe 等）
//     —— 保证桌面版在用户未安装 Python 时仍可使用 Skill
//  2. 系统 PATH 查找（exec.LookPath，操作系统提供的搜索方案）
//
// Wails3 不提供 Python 搜索能力；操作系统 PATH 查找由 exec.LookPath 完成，
// 这就是"按操作系统办法执行"的实现。
func detectPython() {
	// 1. 本地/桌面模式：优先在运行目录查找捆绑的 Python 分发文件
	if config.IsLocalMode() {
		for _, dir := range candidateSearchDirs() {
			if path, version := findBundledPython(dir); path != "" {
				log.Printf("[SkillDetector] 发现运行目录 Python: %s (%s)", path, version)
				pythonAvailable = true
				pythonPath = path
				return
			}
		}
	}

	// 2. 回退到系统 PATH 查找（操作系统提供的搜索方案）
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if version, ok := validatePythonCandidate(path); ok {
			log.Printf("[SkillDetector] 发现系统 Python: %s (%s)", path, version)
			pythonAvailable = true
			pythonPath = path
			return
		}
	}

	log.Println("[SkillDetector] 未检测到 Python，Agent 将使用 Go 原生工具兜底")
	pythonAvailable = false
}

// candidateSearchDirs 返回需要检查捆绑 Python 分发文件的候选目录列表（去重）。
// 桌面模式下，可执行文件所在目录是首要候选（用户可能将便携 Python 放在 exe 同级）；
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
// 支持常见便携/嵌入式分发布局：
//   - Windows: python\python.exe, py\python.exe, python3.exe, python.exe
//   - Unix:    python/bin/python3, python/bin/python, bin/python3, bin/python
//
// 返回找到的可执行文件绝对路径及其版本号；未找到返回空字符串。
func findBundledPython(dir string) (path, version string) {
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
		if v, ok := validatePythonCandidate(candidate); ok {
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
