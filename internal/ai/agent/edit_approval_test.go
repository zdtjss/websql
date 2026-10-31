package agent

import (
	"strings"
	"testing"

	"websql/internal/ai/agent/export"

	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

// TestExportToolSchemasInfer 新增/扩展的工具结构必须能被 InferTool 生成 schema
// （嵌套 operations/patch 不能被静默忽略）。
func TestExportToolSchemasInfer(t *testing.T) {
	if _, err := toolutils.InferTool("get_export_artifact", "schema test", export.NewGetExportArtifactFunc(nil)); err != nil {
		t.Fatalf("get_export_artifact schema 生成失败：%v", err)
	}
	if _, err := toolutils.InferTool("repair_export", "schema test", export.NewRepairExportFunc(nil, nil, nil)); err != nil {
		t.Fatalf("repair_export schema 生成失败：%v", err)
	}
	if _, err := toolutils.InferTool("export_skill", "schema test", export.NewExportSkillFunc(nil)); err != nil {
		t.Fatalf("export_skill schema 生成失败：%v", err)
	}
}

// TestOperationTexts_ExtractsWriteValues 结构化编辑的写入内容必须被提取，
// 以便中间件对其做与 content 相同的内嵌 SQL 校验。
func TestOperationTexts_ExtractsWriteValues(t *testing.T) {
	args := `{"operations":[` +
		`{"op":"insert","kind":"block","value":"图表定义 A"},` +
		`{"op":"patch","kind":"patch","patch":{"find":"a","replace":"替换值 B"}},` +
		`{"op":"replace","kind":"cell","sheet":"S","ref":"A1","value":"纯文本 C"}` +
		`]}`
	texts := operationTexts(args)
	if len(texts) != 3 {
		t.Fatalf("应提取 3 段写入内容，实际 %d：%v", len(texts), texts)
	}
	joined := strings.Join(texts, "\n")
	for _, want := range []string{"图表定义 A", "替换值 B", "纯文本 C"} {
		if !strings.Contains(joined, want) {
			t.Errorf("未提取到 %q：%v", want, texts)
		}
	}
}

// TestEditOperationsFromArgs 审批门必须能解析 operations 并识别删除类操作。
func TestEditOperationsFromArgs(t *testing.T) {
	ops := editOperationsFromArgs(`{"operations":[{"op":"delete","kind":"slide","index":3}]}`)
	if len(ops) != 1 || ops[0].Kind != "slide" || ops[0].Index != 3 {
		t.Fatalf("解析失败：%+v", ops)
	}
	if !export.OperationNeedsApproval(ops) {
		t.Error("删除页应需要确认")
	}
	if export.DescribeDestructiveOperations(ops) == "" {
		t.Error("删除页应生成审核清单")
	}
}
