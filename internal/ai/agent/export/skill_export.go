package export

// 本文件原承载 Go 端 Skill 编排逻辑（SkillExportWord / SkillExportPPT /
// SkillGenerateChart / NewSkillExportAnalysisDocxFunc / NewSkillExportPPTFunc 等）。
//
// 重构后改为「Agent 编排 + Skill 渲染」模式：
//   - Agent 通过 query_data 等工具完成取数与统计
//   - Agent 通过 skill 工具加载 SKILL.md，按其指引调用 Python 脚本渲染专业产物
//   - Go 端 NewExportAnalysisDocxFunc / NewExportPPTFunc 仅作为 Python 不可用时的
//     原生兜底实现（见 tools.go）
//
// 因此本文件的编排函数已全部移除，仅保留被 tools.go 复用的 cleanupFiles。
// SkillEnv / RunPythonScript / IsPythonAvailable / CheckAndInstallDeps 等基础能力
// 仍保留在 skill_detector.go，供 Filesystem Middleware 与未来扩展使用。

import (
	"os"
)

// cleanupFiles 删除生成的临时图表文件。
// tools.go 在 Go 原生兜底路径中生成临时图表，导出完成后需清理。
func cleanupFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}
