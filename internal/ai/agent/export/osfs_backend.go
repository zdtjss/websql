package export

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"

	"websql/internal/pkg/safego"
)

type OSFilesystemBackend struct {
	validateCommand func(string) error
	pythonPath      string // 检测到的 Python 可执行文件路径（如 "python" 或 "/usr/bin/python3"）
	userID          string // 沙箱归属用户；空值表示无用户上下文（旧行为/测试）
}

func NewOSFilesystemBackend() *OSFilesystemBackend {
	b := &OSFilesystemBackend{}
	// 使用沙箱校验器：在黑名单之上叠加「python/pip 白名单 + 绝对路径沙箱检查」
	b.SetValidateCommand(func(command string) error {
		return validateSandboxedCommandForUser(b.userID, command)
	})
	return b
}

// CloneForUser 克隆一个绑定指定用户的文件系统后端（共享 pythonPath，不共享沙箱身份）。
// Agent 实例按用户缓存，因此每个用户的 Agent 持有独立克隆，不会互相串号。
func (b *OSFilesystemBackend) CloneForUser(userID string) *OSFilesystemBackend {
	if b == nil {
		return nil
	}
	cp := *b
	cp.userID = strings.TrimSpace(userID)
	cp.SetValidateCommand(func(command string) error {
		return validateSandboxedCommandForUser(cp.userID, command)
	})
	return &cp
}

func (b *OSFilesystemBackend) SetValidateCommand(fn func(string) error) {
	b.validateCommand = fn
}

// SetPythonPath 设置检测到的 Python 可执行文件路径，用于在 Windows 上将 python3 替换为正确的命令。
func (b *OSFilesystemBackend) SetPythonPath(path string) {
	b.pythonPath = path
}

// defaultCommandValidator 默认命令安全校验器。
// 拦截可能危害系统安全、操作系统安全、文件或数据被篡改/删除的命令。
// 允许 Python 脚本执行、pip 安装、文件读写等安全操作。
// 动态生成的脚本通过 execute 工具执行时，此校验器保护系统不被破坏。
func defaultCommandValidator(command string) error {
	lower := strings.ToLower(command)

	// 危险命令黑名单：拦截可能危害系统/OS/文件安全的操作
	dangerousPatterns := []string{
		// 文件/目录批量删除
		"rm -rf", "rmdir /s", "del /s", "del /f", "rm -r /",
		// 磁盘/分区操作
		"format ", "mkfs", "dd if=", "diskpart",
		// 系统关机/重启
		"shutdown", "reboot", "halt", "poweroff",
		// 系统配置修改
		"reg delete", "reg add", "bcdedit", "defaults write",
		"chmod 777 /", "chown -r", "takeown /f",
		// 用户/权限管理
		"net user", "net localgroup", "useradd", "userdel", "passwd",
		// 服务管理
		"systemctl", "launchctl", "sc delete", "sc config",
		// 定时任务（可能用于持久化攻击）
		"crontab", "schtasks /create",
		// 编码/加密执行（可能隐藏恶意行为）
		"powershell -enc", "powershell -e ",
		// 网络工具（可能用于数据外传或反弹 shell）
		"curl ", "wget ", "scp ", "ssh ", "rsync ",
		"nc ", "netcat", "ncat ",
		// fork bomb
		":(){:|:&};:",
		// 直接写入设备
		"> /dev/sd", "> /dev/null 2>&1 &",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return fmt.Errorf("命令包含危险操作 [%s]，已被安全校验拦截", p)
		}
	}

	return nil
}

func (b *OSFilesystemBackend) LsInfo(ctx context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil || req.Path == "" {
		return nil, errors.New("path is required")
	}

	dir, err := resolveReadPathForUser(b.userID, req.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", req.Path, err)
	}

	var infos []filesystem.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		infos = append(infos, filesystem.FileInfo{
			Path:       filepath.Join(dir, entry.Name()),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
		})
	}

	return infos, nil
}

func (b *OSFilesystemBackend) Read(ctx context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	if req == nil || req.FilePath == "" {
		return nil, errors.New("filePath is required")
	}

	filePath, err := resolveReadPathForUser(b.userID, req.FilePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", req.FilePath, err)
	}

	content := string(data)

	if req.Offset > 0 || req.Limit > 0 {
		lines := strings.Split(content, "\n")
		offset := req.Offset
		if offset < 1 {
			offset = 1
		}
		if offset > len(lines) {
			return &filesystem.FileContent{Content: ""}, nil
		}

		start := offset - 1
		end := len(lines)
		if req.Limit > 0 {
			end = start + req.Limit
			if end > len(lines) {
				end = len(lines)
			}
		}

		content = strings.Join(lines[start:end], "\n")
	}

	return &filesystem.FileContent{Content: content}, nil
}

func (b *OSFilesystemBackend) GrepRaw(ctx context.Context, req *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	if req == nil || req.Pattern == "" {
		return nil, errors.New("pattern is required")
	}

	pattern := req.Pattern
	if req.CaseInsensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", req.Pattern, err)
	}

	var filesToSearch []string
	if req.Path != "" {
		searchDir, err := resolveReadPathForUser(b.userID, req.Path)
		if err != nil {
			return nil, err
		}
		if req.Glob != "" {
			globPattern := filepath.Join(searchDir, req.Glob)
			matches, _ := filepath.Glob(globPattern)
			for _, m := range matches {
				info, err := os.Stat(m)
				if err == nil && !info.IsDir() {
					filesToSearch = append(filesToSearch, m)
				}
			}
		} else {
			err := filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() {
					if req.FileType != "" {
						if filepath.Ext(path) != "."+req.FileType {
							return nil
						}
					}
					filesToSearch = append(filesToSearch, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to walk directory: %w", err)
			}
		}
	}

	var matches []filesystem.GrepMatch
	for _, filePath := range filesToSearch {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if req.EnableMultiline {
				if re.MatchString(line) {
					matches = append(matches, filesystem.GrepMatch{
						Path:    filePath,
						Line:    i + 1,
						Content: line,
					})
				}
			} else {
				locs := re.FindAllStringIndex(line, -1)
				for range locs {
					matches = append(matches, filesystem.GrepMatch{
						Path:    filePath,
						Line:    i + 1,
						Content: line,
					})
				}
			}
		}
	}

	return matches, nil
}

func (b *OSFilesystemBackend) GlobInfo(ctx context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil || req.Pattern == "" {
		return nil, errors.New("pattern is required")
	}

	baseDir, err := resolveReadPathForUser(b.userID, req.Path)
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(baseDir, req.Pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob pattern failed: %w", err)
	}

	var infos []filesystem.FileInfo
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		infos = append(infos, filesystem.FileInfo{
			Path:       m,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
		})
	}

	return infos, nil
}

func (b *OSFilesystemBackend) Write(ctx context.Context, req *filesystem.WriteRequest) error {
	if req == nil || req.FilePath == "" {
		return errors.New("filePath is required")
	}

	filePath, err := resolveWritePathForUser(b.userID, req.FilePath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(filePath, []byte(req.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", req.FilePath, err)
	}

	return nil
}

func (b *OSFilesystemBackend) Edit(ctx context.Context, req *filesystem.EditRequest) error {
	if req == nil || req.FilePath == "" {
		return errors.New("filePath is required")
	}
	if req.OldString == "" {
		return errors.New("oldString is required")
	}
	if req.OldString == req.NewString {
		return errors.New("newString must be different from oldString")
	}

	filePath, err := resolveWritePathForUser(b.userID, req.FilePath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, req.OldString)

	if count == 0 {
		return errors.New("oldString not found in file")
	}
	if !req.ReplaceAll && count > 1 {
		return fmt.Errorf("oldString appears %d times, use replaceAll=true or make it unique", count)
	}

	if req.ReplaceAll {
		content = strings.ReplaceAll(content, req.OldString, req.NewString)
	} else {
		content = strings.Replace(content, req.OldString, req.NewString, 1)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write edited file: %w", err)
	}

	return nil
}

// executeDefaultTimeout execute 子进程默认执行上限。
// 脚本可能死循环或长时间运行，必须设置服务端兜底超时，
// 避免会话被挂死且进程不回收（超时后 CommandContext 会自动 Kill 进程）。
const executeDefaultTimeout = 300 * time.Second

// executeMaxOutput execute 输出上限（stdout+stderr 合计），超出部分截断，
// 防止脚本输出海量内容撑爆内存与模型上下文。
const executeMaxOutput = 256 * 1024

// cappedBuffer 并发安全、带容量上限的输出缓冲（stdout/stderr 两个 goroutine 同时写入）
type cappedBuffer struct {
	mu        sync.Mutex
	buf       strings.Builder
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remain := executeMaxOutput - b.buf.Len()
	if remain <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		b.buf.Write(p[:remain])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := strings.TrimSpace(b.buf.String())
	if b.truncated {
		s += "\n...(输出过长，已截断)"
	}
	return s
}

// pythonExecutable 返回用于执行技能脚本的解释器路径
func (b *OSFilesystemBackend) pythonExecutable() string {
	if b.pythonPath != "" {
		return b.pythonPath
	}
	return "python"
}

// ExecuteStreaming 执行技能脚本。
//
// 安全模型：命令先经 parseSkillCommand 解析为受限 argv（只允许运行 skills/ 目录内的
// Python 脚本），再直接 exec 解释器——不经过 cmd /C 或 sh -c，shell 元字符没有任何效果。
func (b *OSFilesystemBackend) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || strings.TrimSpace(input.Command) == "" {
		return nil, errors.New("command is required")
	}
	if b.validateCommand != nil {
		if err := b.validateCommand(input.Command); err != nil {
			return nil, fmt.Errorf("command validation failed: %w", err)
		}
	}
	inv, err := parseSkillCommandForUser(b.userID, input.Command)
	if err != nil {
		return nil, fmt.Errorf("command validation failed: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, executeDefaultTimeout)
	argv := append(append(append([]string{}, inv.pyFlags...), inv.script), inv.args...)
	cmd := exec.CommandContext(execCtx, b.pythonExecutable(), argv...)
	hideWindow(cmd)
	if ws := rootsForSkillExecution(b.userID).workspace; ws != "" {
		cmd.Dir = ws
	}
	env := sandboxProcessEnv(map[string]string{
		"WEBSQL_EXPORT_DIR": exportsDirAbs(),
		// 保留部署可能设置的 PYTHONPATH（用于定位预装依赖）；不继承其他环境变量。
		"PYTHONPATH": os.Getenv("PYTHONPATH"),
		// 临时目录按用户隔离，避免跨用户读写系统临时目录。
		"TEMP": userTempDir(b.userID), "TMP": userTempDir(b.userID), "TMPDIR": userTempDir(b.userID),
	})
	if dir := b.pythonEnvDir(); dir != "" {
		for i, kv := range env {
			if strings.HasPrefix(kv, "PATH=") {
				env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
				break
			}
		}
	}
	cmd.Env = env
	if inv.stdinFile != "" {
		f, err := os.Open(inv.stdinFile)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("打开输入文件失败：%w", err)
		}
		cmd.Stdin = f
		defer f.Close()
	}

	out := &cappedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out

	sr, sw := schema.Pipe[*filesystem.ExecuteResponse](1)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}
	safego.GoWithName("osfs-exec-wait", func() {
		defer sw.Close()
		defer cancel()
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		output := out.String()
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			output += fmt.Sprintf("\n(执行超过 %v 已被终止)", executeDefaultTimeout)
		}
		if ctx.Err() != nil {
			return
		}
		sw.Send(&filesystem.ExecuteResponse{Output: output, ExitCode: &exitCode}, nil)
	})
	return sr, nil
}

// pythonEnvDir 返回需要加入子进程 PATH 的 Python 目录（捆绑的便携 Python 不在系统 PATH 中）
func (b *OSFilesystemBackend) pythonEnvDir() string {
	if b.pythonPath == "" || !filepath.IsAbs(b.pythonPath) {
		return ""
	}
	return filepath.Dir(b.pythonPath)
}
