package export

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	skill "github.com/cloudwego/eino/adk/middlewares/skill"
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
	verCtx, verCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer verCancel()
	cmd := exec.CommandContext(verCtx, pythonPath, "--version")
	if output, err := cmd.CombinedOutput(); err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

func detectPython() {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, path, "--version")
		output, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}

		versionStr := strings.TrimSpace(string(output))
		log.Printf("[SkillDetector] 发现 Python: %s (%s)", path, versionStr)
		pythonAvailable = true
		pythonPath = path
		return
	}

	log.Println("[SkillDetector] 未检测到 Python，Agent 将使用 Go 原生工具兜底")
	pythonAvailable = false
}
