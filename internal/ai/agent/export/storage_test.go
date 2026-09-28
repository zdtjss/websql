package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFileName_Unpredictable(t *testing.T) {
	a := SanitizeFileName("销售月报", "export")
	b := SanitizeFileName("销售月报", "export")
	if a == b {
		t.Fatalf("文件名应含随机后缀，两次生成不应相同：%s", a)
	}
	parts := strings.Split(a, "_")
	if len(parts) < 3 {
		t.Fatalf("文件名应形如 name_时间戳_随机串，实际 %q", a)
	}
	if len(parts[len(parts)-1]) != 8 {
		t.Errorf("随机后缀应为 8 位十六进制，实际 %q", parts[len(parts)-1])
	}
}

func TestExportOwnerRegistry(t *testing.T) {
	t.Chdir(t.TempDir())

	fileName := SanitizeFileName("报告", "report") + ".docx"
	url := "/exports/" + fileName
	if err := os.MkdirAll("exports", 0755); err != nil {
		t.Fatalf("创建导出目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join("exports", fileName), []byte("x"), 0644); err != nil {
		t.Fatalf("写入导出文件失败：%v", err)
	}

	// 未登记：known=false（历史文件兼容放行）
	if allowed, known := CheckExportAccess(fileName, "u1"); known || allowed {
		t.Fatalf("未登记文件应为 known=false，实际 known=%v allowed=%v", known, allowed)
	}

	RecordExportOwnerByURL(url, "u1")

	if allowed, known := CheckExportAccess(fileName, "u1"); !known || !allowed {
		t.Errorf("归属用户应可下载：known=%v allowed=%v", known, allowed)
	}
	if allowed, known := CheckExportAccess(fileName, "u2"); !known || allowed {
		t.Errorf("非归属用户应被拒绝：known=%v allowed=%v", known, allowed)
	}

	// 非法文件名不登记、不读取
	RecordExportOwnerByURL("/exports/../evil", "u1")
	if _, err := os.Stat(ownerFilePath("../evil")); err == nil {
		t.Error("含目录成分的文件名不应写入归属记录")
	}
	if _, known := CheckExportAccess("../evil", "u1"); known {
		t.Error("含目录成分的文件名不应有归属记录")
	}

	// 空 userID（无用户上下文）不登记
	anonymous := SanitizeFileName("匿名", "report") + ".docx"
	RecordExportOwnerByURL("/exports/"+anonymous, "")
	if _, known := CheckExportAccess(anonymous, "u1"); known {
		t.Error("空用户不应登记归属")
	}

	// 孤儿记录清理：文件删除后 owner 记录被清理
	if err := os.Remove(filepath.Join("exports", fileName)); err != nil {
		t.Fatalf("删除导出文件失败：%v", err)
	}
	CleanupOrphanOwners()
	if _, known := CheckExportAccess(fileName, "u1"); known {
		t.Error("文件已删除，孤儿归属记录应被清理")
	}
}
