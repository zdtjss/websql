package export

// Skill 产物登记与可编辑（Phase 4）。
//
// 背景：export-word / export-ppt 此前由模型通过 execute 直接运行 Python 脚本，
// 产物落在 exports 但没有元数据，无法 get_export_artifact / repair_export。
//
// 方案：新增专用工具 export_skill——服务端固定脚本、覆盖 outputPath、校验 payload
// 中的外部资源路径，执行后登记 Artifact（Skill + SkillPayload），修复时对 payload
// 打 patch 后重跑同一脚本，生成新版本。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportSkillInput export_skill 入参。
type ExportSkillInput struct {
	Skill string `json:"skill" jsonschema:"required" jsonschema_description:"技能：export-word（Word 模板）| export-ppt（PPT 模板）"`
	// Payload 与 SKILL.md 的输入契约一致：{"mode":"content","markdown":"...","outputPath":"..."}
	// 或 data 模式（columns/data/numericStats/findings / highlights）。
	Payload    string `json:"payload" jsonschema:"required" jsonschema_description:"技能输入 JSON（与 SKILL.md 契约一致）：content 模式传 markdown；data 模式传 columns/data 与统计字段。outputPath 会被服务端覆盖，无需传"`
	FileName   string `json:"fileName" jsonschema_description:"下载文件名（不含扩展名）"`
	TimeoutSec int    `json:"timeoutSec" jsonschema_description:"执行超时秒数（可选，默认 300，最大 600）"`
}

// ExportSkillOutput export_skill 出参。
type ExportSkillOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	FileType    string `json:"fileType,omitempty"`
	Version     int    `json:"version,omitempty"`
	SlideCount  int    `json:"slideCount,omitempty"`
	Logs        string `json:"logs,omitempty"`
}

const (
	skillExportTimeoutDefault = 300
	skillExportTimeoutMax     = 600
	skillPayloadMaxBytes      = 8 << 20
)

// skillScriptSpec 返回技能的脚本相对路径与产物扩展名。
func skillScriptSpec(skill string) (scriptRel, ext string, err error) {
	switch strings.ToLower(strings.TrimSpace(skill)) {
	case "export-word":
		return filepath.Join("export-word", "scripts", "word_generator.py"), "docx", nil
	case "export-ppt":
		return filepath.Join("export-ppt", "scripts", "export_ppt.py"), "pptx", nil
	}
	return "", "", fmt.Errorf("不支持的技能 %q（可选：export-word | export-ppt）", skill)
}

// NewExportSkillFunc 创建 Skill 导出工具。
func NewExportSkillFunc(auditCtx *AuditCtx) func(ctx context.Context, input *ExportSkillInput) (*ExportSkillOutput, error) {
	return func(ctx context.Context, input *ExportSkillInput) (out *ExportSkillOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_skill", start, err) }()

		if input == nil || strings.TrimSpace(input.Payload) == "" {
			return nil, fmt.Errorf("payload 不能为空（按 SKILL.md 契约传 JSON）")
		}
		if len(input.Payload) > skillPayloadMaxBytes {
			return nil, fmt.Errorf("payload 过大（%d 字节，上限 %d）", len(input.Payload), skillPayloadMaxBytes)
		}
		skill := strings.ToLower(strings.TrimSpace(input.Skill))
		_, ext, specErr := skillScriptSpec(skill)
		if specErr != nil {
			return nil, specErr
		}
		fileName := SanitizeFileName(input.FileName, skill)

		data, logs, runErr := runSkillExport(ctx, auditCtx, skill, input.Payload, fileName, input.TimeoutSec)
		if runErr != nil {
			return &ExportSkillOutput{Message: runErr.Error(), Logs: logs}, nil
		}
		if valErr := validateExportArtifact(ext, data, 0); valErr != nil {
			return nil, fmt.Errorf("技能产物校验未通过：%w", valErr)
		}
		url, writeErr := writeExportFile(ctx, fileName, ext, data, auditCtx, &ArtifactMeta{
			Tool: "export_skill", Skill: skill, SkillPayload: normalizeSkillPayload(input.Payload, fileName, ext),
			SourceKind: "skill", Validation: &ArtifactValidation{OK: true, Checks: []string{"artifact_integrity", ext}},
		})
		if writeErr != nil {
			return nil, writeErr
		}
		log.Printf("[Tool:export_skill] 成功 - skill=%s, bytes=%d, url=%s\n", skill, len(data), url)
		msg := fmt.Sprintf("已生成 %s 文件（.%s，%.1f KB），[点击下载](%s)", skill, ext, float64(len(data))/1024, url)
		out = &ExportSkillOutput{Message: msg, DownloadURL: url, FileType: ext, Version: 1, Logs: logs}
		if ext == "pptx" {
			out.SlideCount = countPPTXSlides(data)
			out.Message = fmt.Sprintf("已生成 %s 文件（.pptx，%d 页），[点击下载](%s)", skill, out.SlideCount, url)
		}
		return out, nil
	}
}

// runSkillExport 执行技能脚本并回收产物；不落 Artifact（由调用方决定版本）。
func runSkillExport(ctx context.Context, auditCtx *AuditCtx, skill, payload, fileName string, timeoutSec int) ([]byte, string, error) {
	scriptRel, ext, err := skillScriptSpec(skill)
	if err != nil {
		return nil, "", err
	}
	script := filepath.Join(skillsRootDir(), scriptRel)
	if !pathWithinRoot(script, skillsRootDir()) {
		return nil, "", fmt.Errorf("技能脚本路径非法")
	}
	if fi, statErr := os.Stat(script); statErr != nil || !fi.Mode().IsRegular() {
		return nil, "", fmt.Errorf("技能脚本不存在：%s", script)
	}
	python := GetPythonPath()
	if python == "" {
		return nil, "", fmt.Errorf("技能导出不可用：未检测到 Python 运行环境；请改用 Go 原生导出")
	}
	payloadJSON, err := prepareSkillPayload(payload, fileName, ext)
	if err != nil {
		return nil, "", err
	}

	// 与 execute 相同的并发闸门（全局 + 单用户），避免突发请求压垮单机。
	release, waitErr := DefaultSkillQueue().Acquire(ctx, ownerOf(auditCtx), QueueNotifierFrom(ctx), DegradeSignalFrom(ctx))
	if waitErr != nil {
		return nil, "", fmt.Errorf("技能执行未获得资源：%v；请改用 Go 原生导出（export_analysis_docx / export_ppt）", waitErr)
	}
	defer release()

	timeout := time.Duration(skillExportTimeoutDefault) * time.Second
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
		if timeout > skillExportTimeoutMax*time.Second {
			timeout = skillExportTimeoutMax * time.Second
		}
	}

	// payload 写入用户隔离临时目录，作为脚本的位置参数（脚本支持 argv[1]）。
	tmpDir := userTempDir(ownerOf(auditCtx))
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("创建临时目录失败：%w", err)
	}
	inputFile, err := os.CreateTemp(tmpDir, "skill_input_*.json")
	if err != nil {
		return nil, "", fmt.Errorf("创建技能输入文件失败：%w", err)
	}
	inputPath := inputFile.Name()
	if _, writeErr := inputFile.WriteString(payloadJSON); writeErr != nil {
		inputFile.Close()
		os.Remove(inputPath)
		return nil, "", fmt.Errorf("写入技能输入失败：%w", writeErr)
	}
	inputFile.Close()
	defer os.Remove(inputPath)

	envDir := ""
	if filepath.IsAbs(python) {
		envDir = filepath.Dir(python)
	}
	res, runErr := runSkillScript(ctx, skillExecSpec{
		Script:       script,
		Args:         []string{inputPath},
		Python:       python,
		PythonEnvDir: envDir,
		Cwd:          cwdDir(),
		Env: map[string]string{
			"WEBSQL_EXPORT_DIR": exportsDirAbs(),
			"PYTHONPATH":        os.Getenv("PYTHONPATH"),
			"TEMP":              tmpDir, "TMP": tmpDir, "TMPDIR": tmpDir,
		},
		Timeout: timeout,
	})
	if runErr != nil {
		return nil, "", fmt.Errorf("技能执行失败：%w", runErr)
	}
	logs := ""
	if res != nil {
		logs = res.Output
		if res.ExitCode != 0 {
			return nil, logs, fmt.Errorf("技能脚本执行失败（退出码=%d）：%s", res.ExitCode, truncateForRecord(logs))
		}
	}

	outPath := Path(fileName + "." + ext)
	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		_ = os.Remove(outPath)
		return nil, logs, fmt.Errorf("技能未生成目标文件 %s（日志：%s）", outPath, truncateForRecord(logs))
	}
	return data, logs, nil
}

// prepareSkillPayload 校验并规范化 payload：覆盖 outputPath、限制外部资源路径。
func prepareSkillPayload(payload, fileName, ext string) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return "", fmt.Errorf("payload 不是合法 JSON 对象：%w", err)
	}
	if raw == nil {
		return "", fmt.Errorf("payload 必须是 JSON 对象")
	}
	// outputPath 由服务端控制，忽略模型传入值。
	raw["outputPath"] = skillOutputPath(fileName, ext)
	if err := validateSkillAssetPaths(raw["chartPaths"]); err != nil {
		return "", err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("序列化 payload 失败：%w", err)
	}
	return string(data), nil
}

// skillOutputPath 技能脚本的输出路径。
//
// 默认导出根就是「进程目录/exports」时保持相对路径（可回放、不绑定安装位置）；
// 配置了自定义导出根时必须给绝对路径，因为脚本 Cwd 仍是项目目录。
func skillOutputPath(fileName, ext string) string {
	target := Path(fileName + "." + ext)
	if defaultAbs, err := filepath.Abs("exports"); err == nil && filepath.Clean(defaultAbs) == filepath.Clean(Root()) {
		return filepath.ToSlash(filepath.Join("exports", fileName+"."+ext))
	}
	return filepath.ToSlash(target)
}

// normalizeSkillPayload 保存可重放的 payload（含服务端确定的 outputPath）。
func normalizeSkillPayload(payload, fileName, ext string) string {
	normalized, err := prepareSkillPayload(payload, fileName, ext)
	if err != nil {
		return payload
	}
	return normalized
}

// validateSkillAssetPaths 限制 chartPaths 只能引用 exports 目录内的资源，
// 避免技能脚本把任意本地文件嵌入产物（数据外泄）。
func validateSkillAssetPaths(v any) error {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("chartPaths 必须是字符串数组")
	}
	exportsRoot := Root()
	for _, it := range arr {
		s, ok := it.(string)
		if !ok {
			return fmt.Errorf("chartPaths 只能包含字符串路径")
		}
		p := strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(s)), "/")
		if p == "" {
			continue
		}
		abs := filepath.Join(cwdDir(), filepath.FromSlash(p))
		if !pathWithinRoot(abs, exportsRoot) {
			return fmt.Errorf("chartPaths 仅允许引用 exports 目录内的图片：%s", s)
		}
	}
	return nil
}
