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
}

func NewOSFilesystemBackend() *OSFilesystemBackend {
	b := &OSFilesystemBackend{}
	b.SetValidateCommand(defaultCommandValidator)
	return b
}

func (b *OSFilesystemBackend) SetValidateCommand(fn func(string) error) {
	b.validateCommand = fn
}

// defaultCommandValidator 默认命令安全校验器。
// 拦截危险命令，允许 Python 脚本执行、pip 安装、文件读写等安全操作。
func defaultCommandValidator(command string) error {
	lower := strings.ToLower(command)

	// 危险命令黑名单
	dangerousPatterns := []string{
		"rm -rf", "rmdir /s", "del /s", "del /f", "format ", "shutdown",
		"mkfs", "dd if=", ":(){:|:&};:", "chmod 777 /",
		"taskkill /f", "reg delete", "reg add",
		"net user", "net localgroup",
		"powershell -enc", "curl ", "wget ", "scp ", "ssh ",
		"nc ", "netcat", "ncat ",
		"> /dev/sda", "> /dev/null 2>&1 &",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return fmt.Errorf("命令包含危险操作 [%s]，已被安全校验拦截", p)
		}
	}

	// 允许的命令前缀白名单
	allowedPrefixes := []string{
		"python", "python3", "pip", "chcp", "set ",
		"echo ", "type ", "dir ", "ls ", "cat ",
		"mkdir ", "cd ",
	}
	hasAllowed := false
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(lower, p) || strings.Contains(lower, " "+p) || strings.Contains(lower, "|"+p) {
			hasAllowed = true
			break
		}
	}
	// chcp/set 是 Windows 预处理自动添加的，总是允许
	if strings.Contains(lower, "chcp 65001") || strings.Contains(lower, "set pythonioencoding=utf-8") {
		hasAllowed = true
	}
	if !hasAllowed && lower != "" {
		// 不在白名单但不一定危险，记录但不拦截（避免误伤合法操作）
		// 仅对明确的危险命令拦截，其他命令放行
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
	pythonCRegex   = regexp.MustCompile(`python(?:\.exe)?\s+-c\s+"((?:[^"\\]|\\.)*)"`)
)

func preprocessCommandForWindows(command string) string {
	command = tailPipeRegex.ReplaceAllString(command, "")
	command = strings.ReplaceAll(command, "2>&1 | tail", "")
	command = strings.ReplaceAll(command, "| tail -5", "")
	command = strings.ReplaceAll(command, "| tail-5", "")

	command = pythonCRegex.ReplaceAllStringFunc(command, func(match string) string {
		parts := pythonCRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		code := parts[1]

		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("websql_py_%d.py", time.Now().UnixNano()))
		if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
			return match
		}

		// ReplaceAllStringFunc 会保留 match 之外的文本，
		// 所以 match 之后的参数（如 python -c "code" arg1 中的 arg1）会自动保留。
		// 返回的命令执行临时脚本，并在执行后删除临时文件。
		return fmt.Sprintf("python %s && del %s", tmpFile, tmpFile)
	})

	command = "chcp 65001 >nul && set PYTHONIOENCODING=utf-8 && " + command
	return command
}

func (b *OSFilesystemBackend) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || input.Command == "" {
		return nil, errors.New("command is required")
	}

	command := input.Command

	if runtime.GOOS == "windows" {
		command = preprocessCommandForWindows(command)
	}

	if b.validateCommand != nil {
		if err := b.validateCommand(input.Command); err != nil {
			return nil, fmt.Errorf("command validation failed: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "cmd", "/C", command)

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