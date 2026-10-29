package export

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveReadPathForUserIsolation 验证 Agent 文件工具不能跨用户读取导出/工作区文件。
func TestResolveReadPathForUserIsolation(t *testing.T) {
	owner := "phase2-owner"
	other := "phase2-other"

	ownerExports := filepath.Join(cwdDir(), "exports", "users", SandboxUserSegment(owner))
	if err := os.MkdirAll(ownerExports, 0o700); err != nil {
		t.Fatalf("mkdir owner exports: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(ownerExports) })
	secret := filepath.Join(ownerExports, "secret.txt")
	if err := os.WriteFile(secret, []byte("owner-only"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	if _, err := resolveReadPathForUser(owner, secret); err != nil {
		t.Fatalf("owner should read own export: %v", err)
	}
	if _, err := resolveReadPathForUser(other, secret); err == nil {
		t.Fatal("other user must not read the export file")
	}
	// 共享 exports 根目录与 .owners 不能作为 Agent 文件工具的入口。
	if _, err := resolveReadPathForUser(other, filepath.Join(cwdDir(), "exports")); err == nil {
		t.Fatal("agent file tools must not read the shared exports root")
	}
}

// TestResolveWritePathForUserIsolation 验证写路径按用户隔离，不能改写其他用户文件或 .owners。
func TestResolveWritePathForUserIsolation(t *testing.T) {
	owner := "phase2-owner-w"
	other := "phase2-other-w"
	ownerExports := filepath.Join(cwdDir(), "exports", "users", SandboxUserSegment(owner))
	if err := os.MkdirAll(ownerExports, 0o700); err != nil {
		t.Fatalf("mkdir owner exports: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(ownerExports) })
	target := filepath.Join(ownerExports, "report.html")
	if err := os.WriteFile(target, []byte("owner"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if _, err := resolveWritePathForUser(other, target); err == nil {
		t.Fatal("other user must not overwrite the export file")
	}
	owners := filepath.Join(cwdDir(), "exports", ".owners")
	if _, err := resolveWritePathForUser(other, filepath.Join(owners, "victim.owner")); err == nil {
		t.Fatal("agent file tools must not write .owners")
	}
}
