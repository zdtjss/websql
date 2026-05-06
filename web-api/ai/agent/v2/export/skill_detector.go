package export

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	pythonAvailable     bool
	pythonPath          string
	pythonCheckOnce     sync.Once
	depsChecked         = make(map[string]bool)
	depsCheckMu         sync.Mutex
	skillScriptsBaseDir string
)

type SkillStatus struct {
	PythonAvailable bool            `json:"pythonAvailable"`
	PythonPath      string          `json:"pythonPath"`
	PythonVersion   string          `json:"pythonVersion"`
	Skills          []string        `json:"skills"`
	Dependencies    map[string]bool `json:"dependencies"`
}

func SetSkillScriptsBaseDir(dir string) {
	skillScriptsBaseDir = dir
}

func GetSkillScriptsBaseDir() string {
	if skillScriptsBaseDir != "" {
		return skillScriptsBaseDir
	}
	cwd, _ := os.Getwd()
	return cwd
}

func IsPythonAvailable() bool {
	pythonCheckOnce.Do(detectPython)
	return pythonAvailable
}

func GetPythonPath() string {
	pythonCheckOnce.Do(detectPython)
	return pythonPath
}

func InitSkillEnv() {
	status := GetSkillStatus()
	if status.PythonAvailable {
		log.Printf("[SkillEnv] Python: %s (%s)", status.PythonPath, status.PythonVersion)
		for _, name := range status.Skills {
			log.Printf("[SkillEnv] 已发现 Skill: %s", name)
		}
	} else {
		log.Println("[SkillEnv] Python 未安装，文档导出将使用 Go 原生实现")
	}
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

func CheckAndInstallDeps(ctx context.Context, skillName string) error {
	if !IsPythonAvailable() {
		return fmt.Errorf("Python 不可用")
	}

	depsCheckMu.Lock()
	if depsChecked[skillName] {
		depsCheckMu.Unlock()
		return nil
	}
	depsCheckMu.Unlock()

	reqFilePath := filepath.Join(GetSkillScriptsBaseDir(), "skills", skillName, "scripts", "requirements.txt")

	if _, err := os.Stat(reqFilePath); os.IsNotExist(err) {
		log.Printf("[SkillDep] Skill [%s] 无 requirements.txt, 跳过", skillName)
		depsCheckMu.Lock()
		depsChecked[skillName] = true
		depsCheckMu.Unlock()
		return nil
	}

	missing, err := checkMissingDeps(ctx, reqFilePath)
	if err != nil {
		return fmt.Errorf("检查依赖失败: %w", err)
	}

	if len(missing) > 0 {
		log.Printf("[SkillDep] [%s] 缺失: %v, 正在安装...", skillName, missing)
		if err := installDeps(ctx, reqFilePath); err != nil {
			return fmt.Errorf("安装依赖失败: %w", err)
		}
		log.Printf("[SkillDep] [%s] 依赖安装完成", skillName)
	} else {
		log.Printf("[SkillDep] [%s] 依赖已就绪", skillName)
	}

	depsCheckMu.Lock()
	depsChecked[skillName] = true
	depsCheckMu.Unlock()
	return nil
}

func checkMissingDeps(ctx context.Context, reqFile string) ([]string, error) {
	if !IsPythonAvailable() {
		return nil, fmt.Errorf("Python 不可用")
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
		return fmt.Errorf("Python 不可用")
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

func GetSkillStatus() *SkillStatus {
	status := &SkillStatus{
		PythonAvailable: IsPythonAvailable(),
		PythonPath:      GetPythonPath(),
		Dependencies:    make(map[string]bool),
	}

	if status.PythonAvailable {
		verCtx, verCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verCancel()

		cmd := exec.CommandContext(verCtx, status.PythonPath, "--version")
		if output, err := cmd.CombinedOutput(); err == nil {
			status.PythonVersion = strings.TrimSpace(string(output))
		}
	}

	baseDir := GetSkillScriptsBaseDir()
	skillsDir := filepath.Join(baseDir, "skills")

	entries, err := os.ReadDir(skillsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			skillMD := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMD); err == nil {
				status.Skills = append(status.Skills, entry.Name())
			}
		}
	}

	depsCheckMu.Lock()
	for name, checked := range depsChecked {
		status.Dependencies[name] = checked
	}
	depsCheckMu.Unlock()

	return status
}

func RunPythonScript(ctx context.Context, scriptPath string, inputJSON string) (string, error) {
	if !IsPythonAvailable() {
		return "", fmt.Errorf("Python 不可用")
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonPath, scriptPath)
	cmd.Stdin = strings.NewReader(inputJSON)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Python 脚本执行超时")
		}
		return "", fmt.Errorf("Python 脚本执行失败: %v\n%s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
