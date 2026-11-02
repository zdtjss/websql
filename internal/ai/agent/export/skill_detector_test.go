package export

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// pythonResolverStub 构造测试用探测依赖。
func pythonResolverStub(t *testing.T, dirs []string, versions map[string]string,
	lookPath func(string) (string, error)) pythonResolver {
	t.Helper()
	if lookPath == nil {
		lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	}
	return pythonResolver{
		getenv: func(key string) string {
			if key == pythonEnvOverride {
				return versions["__env__"]
			}
			return ""
		},
		readFile: os.ReadFile,
		lookPath: lookPath,
		validate: func(path string) (string, bool) {
			version, ok := versions[path]
			return version, ok
		},
		dirs: dirs,
	}
}

func writeManifest(t *testing.T, dir, pythonPath string) {
	t.Helper()
	content := `{"pythonPath":"` + filepath.ToSlash(pythonPath) + `","version":"3.15.2","source":"downloaded"}`
	if err := os.WriteFile(filepath.Join(dir, pythonManifestName), []byte(content), 0o600); err != nil {
		t.Fatalf("写入运行时清单失败: %v", err)
	}
}

func createBundledPython(t *testing.T, dir string) string {
	t.Helper()
	var relative string
	if runtime.GOOS == "windows" {
		relative = filepath.Join("python", "python.exe")
	} else {
		relative = filepath.Join("python", "bin", "python3")
	}
	path := filepath.Join(dir, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建随包目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatalf("创建解释器占位文件失败: %v", err)
	}
	return path
}

func TestResolvePythonPriority(t *testing.T) {
	t.Run("环境变量优先", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, "env-python")
		resolver := pythonResolverStub(t, []string{dir},
			map[string]string{"__env__": envPath, envPath: "Python 3.15.2"}, nil)
		path, version, source, ok := resolvePython(resolver)
		if !ok || path != envPath || version != "Python 3.15.2" || source != pythonEnvOverride {
			t.Fatalf("环境变量优先级不符: path=%q version=%q source=%q ok=%v",
				path, version, source, ok)
		}
	})

	t.Run("清单相对路径解析", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, filepath.Join("runtime", "bin", "python3"))
		expected := filepath.Join(dir, "runtime", "bin", "python3")
		resolver := pythonResolverStub(t, []string{dir},
			map[string]string{expected: "Python 3.15.2"}, nil)
		path, _, source, ok := resolvePython(resolver)
		if !ok || path != expected || source != pythonManifestName {
			t.Fatalf("运行时清单优先级/相对路径解析不符: path=%q source=%q ok=%v",
				path, source, ok)
		}
	})

	t.Run("清单损坏时回退随包目录", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, pythonManifestName), []byte("{"), 0o600); err != nil {
			t.Fatalf("写入损坏清单失败: %v", err)
		}
		bundled := createBundledPython(t, dir)
		resolver := pythonResolverStub(t, []string{dir},
			map[string]string{bundled: "Python 3.15.2"}, nil)
		path, _, source, ok := resolvePython(resolver)
		if !ok || path != bundled || source != "bundled" {
			t.Fatalf("回退随包目录不符: path=%q source=%q ok=%v", path, source, ok)
		}
	})

	t.Run("PATH 兜底", func(t *testing.T) {
		dir := t.TempDir()
		expected := filepath.Join(dir, "python3")
		resolver := pythonResolverStub(t, []string{dir},
			map[string]string{expected: "Python 3.15.2"},
			func(name string) (string, error) {
				if name == "python3" {
					return expected, nil
				}
				return "", os.ErrNotExist
			})
		path, _, source, ok := resolvePython(resolver)
		if !ok || path != expected || source != "PATH" {
			t.Fatalf("PATH 兜底不符: path=%q source=%q ok=%v", path, source, ok)
		}
	})

	t.Run("版本过低被拒绝", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := filepath.Join(dir, "python2")
		resolver := pythonResolverStub(t, []string{dir},
			map[string]string{"__env__": oldPath, oldPath: "Python 2.7.18"}, nil)
		if _, _, _, ok := resolvePython(resolver); ok {
			t.Fatal("Python 2.7 不应被识别为可用运行时")
		}
	})

	t.Run("无候选", func(t *testing.T) {
		resolver := pythonResolverStub(t, []string{t.TempDir()}, map[string]string{}, nil)
		if _, _, _, ok := resolvePython(resolver); ok {
			t.Fatal("没有候选时不应返回可用 Python")
		}
	})
}

func TestIsSupportedPythonVersion(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"Python 3.15.2", true},
		{"3.10.0", true},
		{"Python 3.8.10", true},
		{"Python 3.7.9", false},
		{"Python 2.7.18", false},
		{"python 3.15.2", true},
		{"garbage", false},
		{"", false},
	}
	for _, item := range cases {
		if got := isSupportedPythonVersion(item.input); got != item.want {
			t.Errorf("isSupportedPythonVersion(%q) = %v, want %v", item.input, got, item.want)
		}
	}
}
