package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	skill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type SkillEnv struct {
	backend     skill.Backend
	osfsBackend *OSFilesystemBackend
	rootDir     string
	depsChecked map[string]bool
	depsCheckMu sync.Mutex
}

type SkillStatus struct {
	PythonAvailable bool            `json:"pythonAvailable"`
	PythonPath      string          `json:"pythonPath"`
	PythonVersion   string          `json:"pythonVersion"`
	Skills          []string        `json:"skills"`
	Dependencies    map[string]bool `json:"dependencies"`
}

const (
	maxConcurrentPython = 3
	stdinSizeThreshold  = 1 * 1024 * 1024
)

var (
	pythonAvailable bool
	pythonPath      string
	pythonCheckOnce sync.Once

	pythonSem = make(chan struct{}, maxConcurrentPython)

	defaultSkillEnv     *SkillEnv
	defaultSkillEnvOnce sync.Once
)

func InitSkillEnv(ctx context.Context, skillsRootDir string) error {
	var initErr error
	defaultSkillEnvOnce.Do(func() {
		osfsBackend := NewOSFilesystemBackend()

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
			depsChecked: make(map[string]bool),
		}

		logSkillStatus(ctx)
	})
	return initErr
}

func GetSkillEnv() *SkillEnv {
	return defaultSkillEnv
}

func logSkillStatus(ctx context.Context) {
	if !IsPythonAvailable() {
		log.Println("[SkillEnv] Python 未安装，文档导出将使用 Go 原生实现")
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

func (e *SkillEnv) ResolveScriptPath(ctx context.Context, skillName, scriptName string) (string, error) {
	sk, err := e.backend.Get(ctx, skillName)
	if err != nil {
		return "", fmt.Errorf("Skill 未注册: %s (%v)", skillName, err)
	}
	scriptPath := filepath.Join(sk.BaseDirectory, "scripts", scriptName)
	if !fileExists(scriptPath) {
		return "", fmt.Errorf("Skill 脚本不存在: %s", scriptPath)
	}
	return scriptPath, nil
}

func (e *SkillEnv) ResolveFilePath(ctx context.Context, skillName, relativePath string) (string, error) {
	sk, err := e.backend.Get(ctx, skillName)
	if err != nil {
		return "", fmt.Errorf("Skill 未注册: %s (%v)", skillName, err)
	}
	return filepath.Join(sk.BaseDirectory, relativePath), nil
}

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

func (e *SkillEnv) Backend() skill.Backend {
	return e.backend
}

func (e *SkillEnv) FilesystemBackend() *OSFilesystemBackend {
	return e.osfsBackend
}

func (e *SkillEnv) CheckAndInstallDeps(ctx context.Context, skillName string) error {
	if !IsPythonAvailable() {
		return errors.New("Python 不可用")
	}

	e.depsCheckMu.Lock()
	if e.depsChecked[skillName] {
		e.depsCheckMu.Unlock()
		return nil
	}
	e.depsChecked[skillName] = true
	e.depsCheckMu.Unlock()

	reqFilePath, err := e.ResolveFilePath(ctx, skillName, "scripts/requirements.txt")
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(reqFilePath); os.IsNotExist(statErr) {
		log.Printf("[SkillDep] Skill [%s] 无 requirements.txt, 跳过", skillName)
		return nil
	}

	missing, checkErr := checkMissingDeps(ctx, reqFilePath)
	if checkErr != nil {
		return fmt.Errorf("检查依赖失败: %w", checkErr)
	}

	if len(missing) > 0 {
		log.Printf("[SkillDep] [%s] 缺失: %v, 正在安装...", skillName, missing)
		if installErr := installDeps(ctx, reqFilePath); installErr != nil {
			return fmt.Errorf("安装依赖失败: %w", installErr)
		}
		log.Printf("[SkillDep] [%s] 依赖安装完成", skillName)
	} else {
		log.Printf("[SkillDep] [%s] 依赖已就绪", skillName)
	}

	return nil
}

func (e *SkillEnv) GetStatus(ctx context.Context) *SkillStatus {
	status := &SkillStatus{
		PythonAvailable: IsPythonAvailable(),
		PythonPath:      GetPythonPath(),
		PythonVersion:   getPythonVersion(),
		Dependencies:    make(map[string]bool),
	}

	skills, err := e.ListSkills(ctx)
	if err == nil {
		status.Skills = skills
	}

	e.depsCheckMu.Lock()
	for name, checked := range e.depsChecked {
		status.Dependencies[name] = checked
	}
	e.depsCheckMu.Unlock()

	return status
}

func IsPythonAvailable() bool {
	pythonCheckOnce.Do(detectPython)
	return pythonAvailable
}

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

	log.Println("[SkillDetector] 未检测到 Python，将使用 Go 原生实现")
	pythonAvailable = false
}

func RunPythonScript(ctx context.Context, scriptPath string, inputJSON string) (string, error) {
	if !IsPythonAvailable() {
		return "", errors.New("Python 不可用")
	}

	select {
	case pythonSem <- struct{}{}:
		defer func() { <-pythonSem }()
	case <-ctx.Done():
		return "", fmt.Errorf("等待 Python 执行槽位超时: %w", ctx.Err())
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonPath, scriptPath)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")

	var tmpFilePath string
	var tmpFileReader *os.File
	if len(inputJSON) > stdinSizeThreshold {
		tmpFile, err := os.CreateTemp("", "websql-skill-*.json")
		if err != nil {
			return "", fmt.Errorf("创建临时文件失败: %w", err)
		}
		tmpFilePath = tmpFile.Name()

		if _, err := tmpFile.WriteString(inputJSON); err != nil {
			tmpFile.Close()
			os.Remove(tmpFilePath)
			return "", fmt.Errorf("写入临时文件失败: %w", err)
		}
		tmpFile.Close()

		tmpFileReader, err = os.Open(tmpFilePath)
		if err != nil {
			os.Remove(tmpFilePath)
			return "", fmt.Errorf("打开临时文件失败: %w", err)
		}
		cmd.Stdin = tmpFileReader
	} else {
		cmd.Stdin = strings.NewReader(inputJSON)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if tmpFilePath != "" {
		tmpFileReader.Close()
		os.Remove(tmpFilePath)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errors.New("Python 脚本执行超时")
		}
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("Python 脚本执行失败: %v\n%s", err, stderrStr)
		}
		return "", fmt.Errorf("Python 脚本执行失败: %v", err)
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			if len(stderrStr) > 200 {
				stderrStr = stderrStr[:200] + "..."
			}
			return "", fmt.Errorf("Python 脚本未输出 JSON 结果（stderr: %s）", stderrStr)
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return "", fmt.Errorf("Python 脚本无输出（exit=%d），可能缺少依赖库，请检查 requirements.txt", exitCode)
	}

	return result, nil
}

func checkMissingDeps(ctx context.Context, reqFile string) ([]string, error) {
	if !IsPythonAvailable() {
		return nil, errors.New("Python 不可用")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	absReqFile, _ := filepath.Abs(reqFile)

	checkScript := fmt.Sprintf(`
import sys
missing = []
with open(%q, 'r') as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        pkg = line.split('>=')[0].split('==')[0].split('>')[0].split('<')[0].strip()
        if not pkg:
            continue
        try:
            __import__(pkg.replace('-', '_'))
        except ImportError:
            missing.append(pkg)
print(','.join(missing))
`, absReqFile)

	cmd := exec.CommandContext(ctx, pythonPath, "-c", checkScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("依赖检查失败: %v, output: %s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}

	return strings.Split(result, ","), nil
}

func installDeps(ctx context.Context, reqFile string) error {
	if !IsPythonAvailable() {
		return errors.New("Python 不可用")
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	absReqFile, _ := filepath.Abs(reqFile)

	cmd := exec.CommandContext(ctx, pythonPath, "-m", "pip", "install",
		"-r", absReqFile,
		"-q",
		"--disable-pip-version-check",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install 失败: %v\n%s", err, string(output))
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}