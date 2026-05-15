package export

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"
)

type OSFilesystemBackend struct {
	validateCommand func(string) error
}

func NewOSFilesystemBackend() *OSFilesystemBackend {
	return &OSFilesystemBackend{}
}

func (b *OSFilesystemBackend) SetValidateCommand(fn func(string) error) {
	b.validateCommand = fn
}

func (b *OSFilesystemBackend) LsInfo(ctx context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil || req.Path == "" {
		return nil, fmt.Errorf("path is required")
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
		return nil, fmt.Errorf("filePath is required")
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
		return nil, fmt.Errorf("pattern is required")
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
		return nil, fmt.Errorf("pattern is required")
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
		return fmt.Errorf("filePath is required")
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
		return fmt.Errorf("filePath is required")
	}
	if req.OldString == "" {
		return fmt.Errorf("oldString is required")
	}
	if req.OldString == req.NewString {
		return fmt.Errorf("newString must be different from oldString")
	}

	data, err := os.ReadFile(req.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, req.OldString)

	if count == 0 {
		return fmt.Errorf("oldString not found in file")
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

func (b *OSFilesystemBackend) ExecuteStreaming(ctx context.Context, input *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	if input == nil || input.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	if b.validateCommand != nil {
		if err := b.validateCommand(input.Command); err != nil {
			return nil, fmt.Errorf("command validation failed: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "cmd", "/C", input.Command)

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

	go func() {
		defer sw.Close()

		var output strings.Builder

		stdoutDone := make(chan struct{})
		go func() {
			defer close(stdoutDone)
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				output.WriteString(scanner.Text())
				output.WriteString("\n")
			}
		}()

		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			scanner := bufio.NewScanner(stderr)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				output.WriteString(scanner.Text())
				output.WriteString("\n")
			}
		}()

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
	}()

	return sr, nil
}
