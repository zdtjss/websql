package export

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"

	"websql/internal/pkg/safego"
)

type OSFilesystemBackend struct {
	validateCommand func(string) error
	pythonPath      string // 检测到的 Python 可执行文件路径（如 "python" 或 "/usr/bin/python3"）
}

func NewOSFilesystemBackend() *OSFilesystemBackend {
	b := &OSFilesystemBackend{}
	b.SetValidateCommand(defaultCommandValidator)
	return b
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

	entries, err := os.ReadDir(req.Path)
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
			Path:       filepath.Join(req.Path, entry.Name()),
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

	data, err := os.ReadFile(req.FilePath)
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
		if req.Glob != "" {
			globPattern := filepath.Join(req.Path, req.Glob)
			matches, _ := filepath.Glob(globPattern)
			for _, m := range matches {
				info, err := os.Stat(m)
				if err == nil && !info.IsDir() {
					filesToSearch = append(filesToSearch, m)
				}
			}
		} else {
			err := filepath.WalkDir(req.Path, func(path string, d fs.DirEntry, err error) error {
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

	pattern := filepath.Join(req.Path, req.Pattern)
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

	dir := filepath.Dir(req.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(req.FilePath, []byte(req.Content), 0644); err != nil {
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

	data, err := os.ReadFile(req.FilePath)
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

	if err := os.WriteFile(req.FilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write edited file: %w", err)
	}

	return nil
}

var (
	tailPipeRegex = regexp.MustCompile(`\s*2>&1\s*\|\s*tail\s+-?\d+\s*`)
	pythonCRegex   = regexp.MustCompile(`python(?:3|\.exe)?\s+-c\s+"((?:[^"\\]|\\.)*)"`)
	python3Regex   = regexp.MustCompile(`(^|\s)python3(\s|$)`)
)

// unescapeShellCode 对从 python -c "..." 中提取的代码进行 shell 反转义。
// LLM 生成的 python -c "code" 中，code 内部的双引号会被转义为 \"，
// 反斜杠会被转义为 \\。写入 .py 文件前需要还原，否则 Python 会报 SyntaxError。
// 注意：只反转义 shell 双引号上下文中的 \" 和 \\，保留 \n \t 等 Python 转义序列。
func unescapeShellCode(code string) string {
	var sb strings.Builder
	sb.Grow(len(code))
	i := 0
	for i < len(code) {
		if code[i] == '\\' && i+1 < len(code) {
			next := code[i+1]
			// 仅反转义 shell 双引号转义：\" → ", \\ → \
			if next == '"' {
				sb.WriteByte('"')
				i += 2
				continue
			}
			if next == '\\' {
				sb.WriteByte('\\')
				i += 2
				continue
			}
			// 其他 \x 序列（如 \n \t）是 Python 转义，保留原样
		}
		sb.WriteByte(code[i])
		i++
	}
	return sb.String()
}

// getPythonCommand 根据检测结果返回正确的 Python 命令名称。
// detectPython 优先检测 python3，其次 python。如果检测到的是 python3，
// 则命令中保留 python3；如果检测到的是 python（Windows 常见），则返回 python。
func (b *OSFilesystemBackend) getPythonCommand() string {
	if b.pythonPath == "" {
		return "python"
	}
	base := filepath.Base(b.pythonPath)
	base = strings.TrimSuffix(base, ".exe")
	if base == "python3" || base == "python" {
		return base
	}
	return "python"
}

// preprocessCommand 跨平台命令预处理。
// 1. 剥离 tail 管道（部分平台无 tail 命令）
// 2. 将 python3 替换为检测到的 Python 命令（当 python3 不可用时）
// 3. 将 /tmp/ 路径映射到 os.TempDir()（跨平台兼容）
// 4. 将 python -c "code" 提取为临时 .py 文件执行（彻底避免 shell 引号转义问题）
// 5. Windows 特定：设置 UTF-8 编码
func (b *OSFilesystemBackend) preprocessCommand(command string) string {
	// 1. 剥离 tail 管道
	command = tailPipeRegex.ReplaceAllString(command, "")
	command = strings.ReplaceAll(command, "2>&1 | tail", "")
	command = strings.ReplaceAll(command, "| tail -5", "")
	command = strings.ReplaceAll(command, "| tail-5", "")

	// 2. 确定正确的 Python 命令，若 python3 不可用则替换
	pythonCmd := b.getPythonCommand()
	if pythonCmd != "python3" {
		// 先替换 Unix 绝对路径（python3Regex 无法匹配 /usr/bin/python3 中的 python3）
		for _, unixPath := range []string{
			"/usr/local/bin/python3", "/usr/bin/python3",
			"/usr/local/bin/python", "/usr/bin/python",
		} {
			command = strings.ReplaceAll(command, unixPath, pythonCmd)
		}
		// 再替换独立的 python3 token
		command = python3Regex.ReplaceAllString(command, "${1}"+pythonCmd+"${2}")
	}

	// 3. 将 /tmp/ 映射到系统临时目录（使用正斜杠，跨平台兼容且不引发 Python 转义问题）
	tmpDir := strings.ReplaceAll(os.TempDir(), "\\", "/")
	command = strings.ReplaceAll(command, "/tmp/", tmpDir+"/")

	// 4. 将 python -c "code" 提取为临时文件执行
	// 这在所有平台上都避免了 shell 引号转义导致的 SyntaxError
	command = pythonCRegex.ReplaceAllStringFunc(command, func(match string) string {
		parts := pythonCRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		code := unescapeShellCode(parts[1])

		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("websql_py_%d.py", time.Now().UnixNano()))
		if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
			return match
		}

		// 执行临时脚本，并在执行后删除（平台相关的删除命令）
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("%s %s && del %s", pythonCmd, tmpFile, tmpFile)
		}
		return fmt.Sprintf("%s %s && rm -f %s", pythonCmd, tmpFile, tmpFile)
	})

	// 5. Windows 特定：强制 UTF-8 编码，避免 GBK 编码问题
	if runtime.GOOS == "windows" {
		command = "chcp 65001 >nul && set PYTHONIOENCODING=utf-8 && " + command
	}

	return command
}

func (b *OSFilesystemBackend) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || input.Command == "" {
		return nil, errors.New("command is required")
	}

	// 安全校验：在预处理之前验证原始命令（检查用户意图）
	if b.validateCommand != nil {
		if err := b.validateCommand(input.Command); err != nil {
			return nil, fmt.Errorf("command validation failed: %w", err)
		}
	}

	// 跨平台命令预处理
	command := b.preprocessCommand(input.Command)

	// 根据操作系统选择正确的 shell
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	hideWindow(cmd)

	// 安全措施：设置环境变量
	// PYTHONIOENCODING：统一 UTF-8 编码，避免 Windows GBK 编码问题
	// PYTHONDONTWRITEBYTECODE：禁止生成 .pyc 文件，避免代码注入风险
	// 注意：不设置 cmd.Dir，因为 execute 工具同时用于 skill 脚本（需要在其自身目录下运行）
	// 注意：不设置 PYTHONNOUSERSITE，因为 Windows 上 pip install 默认安装到用户 site-packages
	cmd.Env = append(os.Environ(),
		"PYTHONIOENCODING=utf-8",
		"PYTHONDONTWRITEBYTECODE=1",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	sr, sw := schema.Pipe[*filesystem.ExecuteResponse](10)

	safego.GoWithName("osfs-exec-collector", func() {
		defer sw.Close()

		var output strings.Builder

		stdoutDone := make(chan struct{})
		safego.GoWithName("osfs-stdout-reader", func() {
			defer close(stdoutDone)
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				output.WriteString(scanner.Text())
				output.WriteString("\n")
			}
		})

		stderrDone := make(chan struct{})
		safego.GoWithName("osfs-stderr-reader", func() {
			defer close(stderrDone)
			scanner := bufio.NewScanner(stderr)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				output.WriteString(scanner.Text())
				output.WriteString("\n")
			}
		})

		<-stdoutDone
		<-stderrDone

		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		sw.Send(&filesystem.ExecuteResponse{
			Output:   strings.TrimSpace(output.String()),
			ExitCode: &exitCode,
		}, nil)
	})

	return sr, nil
}