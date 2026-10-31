package export

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareSkillPayload_OverridesOutputAndRejectsEscape outputPath 必须由服务端覆盖，
// chartPaths 只能引用 exports 内资源。
func TestPrepareSkillPayload_OverridesOutputAndRejectsEscape(t *testing.T) {
	t.Chdir(t.TempDir())
	payload, err := prepareSkillPayload(`{"mode":"content","markdown":"# T","outputPath":"/etc/passwd"}`, "skill_test", "docx")
	if err != nil {
		t.Fatalf("合法 payload 不应失败：%v", err)
	}
	if !strings.Contains(payload, `"outputPath":"exports/skill_test.docx"`) {
		t.Errorf("outputPath 未被服务端覆盖：%s", payload)
	}

	if _, err := prepareSkillPayload(`{"chartPaths":["/etc/passwd"]}`, "x", "docx"); err == nil {
		t.Fatal("越出 exports 的资源路径必须拒绝")
	}
	if _, err := prepareSkillPayload(`{"chartPaths":["/exports/a.png"]}`, "x", "docx"); err != nil {
		t.Fatalf("exports 内资源应允许：%v", err)
	}
	if _, err := prepareSkillPayload(`not-json`, "x", "docx"); err == nil {
		t.Fatal("非法 JSON 必须拒绝")
	}
}

// TestExportSkill_ContentModeEndToEnd Skill 产物必须登记 Artifact、可读 outline、可 patch 重跑。
// 依赖本机 Python 与 skills 依赖；缺失时跳过（CI 应预装）。
func TestExportSkill_ContentModeEndToEnd(t *testing.T) {
	if !IsPythonAvailable() {
		t.Skip("Python 不可用，跳过 Skill 集成测试")
	}
	repoSkills, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(repoSkills); err != nil || !fi.IsDir() {
		t.Skipf("skills 目录不存在：%s", repoSkills)
	}
	t.Chdir(t.TempDir())
	t.Setenv("SKILLS_DIR", repoSkills)

	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	out, err := NewExportSkillFunc(audit)(ctx, &ExportSkillInput{
		Skill:    "export-word",
		Payload:  `{"mode":"content","title":"Skill 测试","markdown":"# 摘要\n\n正文内容"}`,
		FileName: "skill-word",
	})
	if err != nil {
		if strings.Contains(err.Error(), "ModuleNotFoundError") {
			t.Skipf("缺少 Python 依赖：%v", err)
		}
		t.Fatalf("Skill 导出失败：%v", err)
	}
	if out.DownloadURL == "" {
		t.Fatalf("未返回下载链接：%+v", out)
	}
	if _, err := os.Stat(filepath.Join("exports", strings.TrimPrefix(out.DownloadURL, "/exports/"))); err != nil {
		t.Fatalf("Skill 产物不存在：%v", err)
	}

	got, err := NewGetExportArtifactFunc(audit)(ctx, &ExportArtifactInput{File: out.DownloadURL, IncludeOutline: true})
	if err != nil || !got.Found {
		t.Fatalf("读取 Skill 产物失败：%v", err)
	}
	if got.Skill != "export-word" || got.SkillPayload == "" || got.SourceKind != "skill" {
		t.Errorf("Skill 元数据不完整：%+v", got)
	}

	repaired, err := NewRepairExportFunc(nil, audit, nil)(ctx, &ExportRepairInput{
		File: got.DownloadURL,
		Patches: []ExportPatch{{
			Find: "正文内容", Replace: "正文内容（修订）",
		}},
	})
	if err != nil {
		t.Fatalf("Skill payload patch 重跑失败：%v", err)
	}
	if repaired.Version != 2 {
		t.Errorf("版本号应为 2：%d", repaired.Version)
	}
}

// TestSkillScriptSpec 仅允许内置 Word/PPT 技能。
func TestSkillScriptSpec(t *testing.T) {
	if _, ext, err := skillScriptSpec("export-word"); err != nil || ext != "docx" {
		t.Errorf("export-word 规格异常：ext=%s err=%v", ext, err)
	}
	if _, ext, err := skillScriptSpec("export-ppt"); err != nil || ext != "pptx" {
		t.Errorf("export-ppt 规格异常：ext=%s err=%v", ext, err)
	}
	if _, _, err := skillScriptSpec("cross-db-analysis"); err == nil {
		t.Error("非导出技能必须拒绝")
	}
}
