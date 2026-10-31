package export

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// 高级导出（export_by_code）
//
// 与既有固定格式导出工具（export_excel / export_pptx / export_html 等）不同，
// 本工具由大模型按用户描述的格式编写 Python 代码，连接「用户选中的数据库连接」
// 自行查询数据并生成任意格式文件（PDF/CSV/Markdown/XML/ZIP 等）。
//
// 硬约束：
//   - 仅支持 Python（工具不暴露语言参数，服务端只会以 python 解释器执行）；
//   - 代码「只读」：数据库经只读助手（websql_db）访问，仅允许查询；文件写仅限本次运行的
//     runDir（自建目录），导出产物回收后 runDir 即清理；
//   - 数据库连接信息仅以环境变量注入子进程，密码不进入模型上下文与工具入参。

// CodegenDBInfo 高级导出沙箱注入的数据库连接信息。
// 仅用于注入子进程环境变量，不得写入日志或返回给前端。
type CodegenDBInfo struct {
	DBType   string
	URL      string
	User     string
	Password string
	Schema   string
}

// ExportByCodeInput 高级导出入参。
type ExportByCodeInput struct {
	Code       string `json:"code" jsonschema:"required" jsonschema_description:"完整可运行的 Python 源码。只读约束：仅可通过 websql_db 只读助手查询数据库；禁止写数据库/改结构；禁止修改或删除系统与用户文件；禁止联网；只能在当前工作目录内创建文件。可用环境变量获取连接信息（见系统提示）。"`
	FileName   string `json:"fileName" jsonschema_description:"下载文件名（不含扩展名）"`
	OutputFile string `json:"outputFile" jsonschema:"required" jsonschema_description:"脚本在当前工作目录内产出的文件相对路径（如 report.pdf）。导出会回收该文件，其余临时文件随运行目录清理。"`
	TimeoutSec int    `json:"timeoutSec" jsonschema_description:"执行超时秒数（可选，默认 300，最大 600）"`
}

// ExportByCodeOutput 高级导出出参。
type ExportByCodeOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
	ExitCode    int    `json:"exitCode"`
	Logs        string `json:"logs"`
}

const (
	codegenRunTimeoutDefault = 300 * time.Second
	codegenRunTimeoutMax     = 600 * time.Second
)

// codegenBaseDir 高级导出的运行根目录（位于 exports 下，随后续导出清理任务一并淘汰）。
func codegenBaseDir() string { return filepath.Join("exports", ".codegen") }

// codegenQueueFallbackMessage 排队被拒时引导模型改用 Go 原生导出（不重试 export_by_code）。
func codegenQueueFallbackMessage(ctx context.Context, err error) string {
	if ctx != nil && ctx.Err() != nil {
		return "高级导出已取消（排队等待被中断）。"
	}
	reason := err
	var waited time.Duration
	var waitErr *QueueWaitError
	if errors.As(err, &waitErr) {
		reason = waitErr.Reason
		waited = waitErr.Wait
	}
	var msg string
	switch {
	case errors.Is(reason, ErrQueueDegraded):
		msg = fmt.Sprintf("用户已选择降级为 Go 原生渲染，本次高级导出未执行（排队等待 %s）。", waited.Round(time.Second))
	case errors.Is(reason, ErrQueueTimeout):
		msg = fmt.Sprintf("高级导出排队超过 %s 仍未获得执行资源，已自动降级为 Go 原生渲染。", waited.Round(time.Second))
	default:
		msg = "高级导出未能获得执行资源，本次已跳过。"
	}
	return msg + "\n请改用 export_analysis_docx / export_ppt / export_html（content 模式）或 export_excel / export_excel_with_chart 生成文件，不要再次调用 export_by_code。"
}

// codegenRunBaseDirs 返回运行目录的候选根（按优先级）。
// 首选 exports/.codegen；Windows 低完整性标注需要目录 ACL 授予 WRITE_OWNER
// （仅有 Modify 的目录会返回 Access is denied），此时退回系统临时目录。
func codegenRunBaseDirs() []string {
	primary := codegenBaseDir()
	out := []string{primary}
	if tmp := filepath.Join(os.TempDir(), "websql-codegen"); !strings.EqualFold(tmp, primary) {
		out = append(out, tmp)
	}
	return out
}

// codegenHelpersDir 注入沙箱的受信助手目录（sitecustomize 导入钩子 + websql_db 只读助手）。
func codegenHelpersDir() string { return filepath.Join(codegenBaseDir(), "_helpers") }

// NewExportByCodeFunc 创建高级导出工具。
// conn 为「用户选中连接」的数据库句柄（用于生成表结构摘要，只读查询）；
// dbInfo 为连接信息（仅注入子进程环境变量）。
func NewExportByCodeFunc(conn *sqlx.DB, dbInfo *CodegenDBInfo, auditCtx *AuditCtx) func(ctx context.Context, input *ExportByCodeInput) (*ExportByCodeOutput, error) {
	return func(ctx context.Context, input *ExportByCodeInput) (out *ExportByCodeOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_by_code", start, err) }()

		if input == nil {
			return nil, fmt.Errorf("参数不能为空")
		}
		if err := validateCodegenSource(input.Code); err != nil {
			return nil, err
		}
		outRel, err := validateCodegenOutputRel(input.OutputFile)
		if err != nil {
			return nil, err
		}
		python := GetPythonPath()
		if python == "" {
			return nil, fmt.Errorf("高级导出不可用：未检测到 Python 运行环境")
		}
		if !CodegenIsolationAvailable() {
			return nil, fmt.Errorf("高级导出不可用：当前环境无可用 OS 级隔离（fail-closed）")
		}

		// 与技能脚本共用同一并发闸门：高级导出同样会拉起 Python 子进程，
		// 不设上限时突发请求会在单机上先撞到内存。排队不占本次执行超时。
		release, waitErr := DefaultSkillQueue().Acquire(ctx, ownerOf(auditCtx), QueueNotifierFrom(ctx), DegradeSignalFrom(ctx))
		if waitErr != nil {
			log.Printf("[SkillQueue] 高级导出未获得执行槽位 - user=%s, err=%v, %s",
				ownerOf(auditCtx), waitErr, skillQueueStats())
			return &ExportByCodeOutput{Message: codegenQueueFallbackMessage(ctx, waitErr), ExitCode: -1}, nil
		}
		defer release()

		runDir, err := createCodegenRunDir()
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(runDir)

		if err := prepareCodegenHelpers(); err != nil {
			return nil, err
		}

		scriptPath := filepath.Join(runDir, "main.py")
		if err := os.WriteFile(scriptPath, []byte(input.Code), 0o600); err != nil {
			return nil, fmt.Errorf("写入脚本失败：%w", err)
		}
		if schemaJSON := buildCodegenSchemaSummary(conn, dbInfo); len(schemaJSON) > 0 {
			_ = os.WriteFile(filepath.Join(runDir, "schema.json"), schemaJSON, 0o600)
		}

		timeout := codegenRunTimeoutDefault
		if input.TimeoutSec > 0 {
			timeout = time.Duration(input.TimeoutSec) * time.Second
			if timeout > codegenRunTimeoutMax {
				timeout = codegenRunTimeoutMax
			}
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// 直接 exec 解释器（不经 shell），仅允许 -u/-B；脚本为 runDir 内普通文件。
		cmd := exec.CommandContext(execCtx, python, "-u", "-B", scriptPath)
		hideWindow(cmd)
		cmd.Dir = runDir
		cmd.Env = buildCodegenEnv(runDir, dbInfo)

		// OS 级隔离：Windows 低完整性令牌 / Linux bwrap（写仅限 runDir，断网）。
		isolationCleanup, isoErr := applyCodegenIsolation(cmd, runDir)
		if isoErr != nil {
			return nil, fmt.Errorf("应用 OS 级隔离失败：%w", isoErr)
		}
		if isolationCleanup != nil {
			defer isolationCleanup()
		}

		buf := &cappedBuffer{}
		cmd.Stdout = buf
		cmd.Stderr = buf

		runErr := cmd.Run()
		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		logs := maskCodegenSecrets(buf.String(), dbInfo)
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			logs += fmt.Sprintf("\n(执行超过 %v 已被终止)", timeout)
		}

		producedAbs := filepath.Join(runDir, outRel)
		if !pathWithinRoot(producedAbs, runDir) {
			return &ExportByCodeOutput{
				Message:  "高级导出失败：产出路径非法",
				ExitCode: exitCode,
				Logs:     logs,
			}, nil
		}
		fi, statErr := os.Lstat(producedAbs)
		if statErr != nil || !fi.Mode().IsRegular() {
			return &ExportByCodeOutput{
				Message:  fmt.Sprintf("高级导出未生成目标文件 %q，请检查代码与 outputFile。退出码=%d", input.OutputFile, exitCode),
				ExitCode: exitCode,
				Logs:     logs,
			}, nil
		}

		data, readErr := os.ReadFile(producedAbs)
		if readErr != nil {
			return nil, fmt.Errorf("读取导出产物失败：%w", readErr)
		}
		// 子进程环境包含数据库凭据；脚本可能（被提示注入诱导或自行）把凭据写入产物。
		// 日志已在 maskCodegenSecrets 中掩码，产物文件同样必须在落盘前拦住。
		if containsCodegenSecrets(data, dbInfo) {
			return nil, fmt.Errorf("导出产物包含连接凭据，已阻止下载")
		}
		ext := strings.TrimPrefix(filepath.Ext(outRel), ".")
		if ext == "" {
			ext = "bin"
		}
		url, writeErr := writeExportFile(SanitizeFileName(input.FileName, "export"), ext, data, auditCtx)
		if writeErr != nil {
			return nil, writeErr
		}
		log.Printf("[Tool:export_by_code] 成功 - exit=%d, bytes=%d, url=%s\n", exitCode, len(data), url)

		return &ExportByCodeOutput{
			Message:     fmt.Sprintf("已生成文件，[点击下载](%s)", url),
			DownloadURL: url,
			FileType:    ext,
			ExitCode:    exitCode,
			Logs:        logs,
		}, nil
	}
}

// createCodegenRunDir 为本次运行创建隔离的随机工作目录。
// 依次尝试候选根：目录创建成功且隔离标注成功才采用，否则清理后换下一个根。
func createCodegenRunDir() (string, error) {
	token, err := secureToken(16)
	if err != nil {
		return "", fmt.Errorf("生成运行目录失败：%w", err)
	}
	var lastErr error
	for _, base := range codegenRunBaseDirs() {
		dir := filepath.Join(base, "run_"+token)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			lastErr = err
			continue
		}
		if err := isolateCodegenRunDir(dir); err != nil {
			_ = os.RemoveAll(dir)
			lastErr = err
			continue
		}
		if base != codegenBaseDir() {
			log.Printf("[Codegen] exports 运行目录不可用，已退回临时目录：%s\n", dir)
		}
		return dir, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的运行目录候选")
	}
	return "", fmt.Errorf("创建隔离运行目录失败：%w", lastErr)
}

func secureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// prepareCodegenHelpers 幂等地写出沙箱助手文件（导入钩子 + 只读数据库助手）。
func prepareCodegenHelpers() error {
	dir := codegenHelpersDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建沙箱助手目录失败：%w", err)
	}
	files := map[string]string{
		"sitecustomize.py": codegenSiteCustomizePy,
		"websql_db.py":     codegenWebSQLDBPy,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("写入沙箱助手 %s 失败：%w", name, err)
		}
	}
	return nil
}

// buildCodegenEnv 构造子进程环境：只保留运行 Python 必需的白名单变量，
// 不继承父进程全部环境（父进程可能带有 API Key、云凭据等敏感信息）。
func buildCodegenEnv(runDir string, db *CodegenDBInfo) []string {
	extra := map[string]string{
		"PYTHONIOENCODING": "utf-8",
		"PYTHONUTF8":       "1",
		// 不把脚本目录加入 sys.path：避免模型自建同名文件覆盖注入的沙箱助手（Python 3.11+ 生效）。
		"PYTHONSAFEPATH": "1",
		// 受信助手目录置于 PYTHONPATH 首位，并保留既有 PYTHONPATH（部署可能用它定位预装依赖）。
		"PYTHONPATH":        prependPathList(codegenHelpersDir(), os.Getenv("PYTHONPATH")),
		"WEBSQL_EXPORT_DIR": exportsDirAbs(),
		"WEBSQL_RUN_DIR":    runDir,
		// 临时目录指向 runDir：低完整性/沙箱下子进程仍可写临时文件，且不越出运行目录。
		"TEMP": runDir, "TMP": runDir, "TMPDIR": runDir,
	}
	if db != nil {
		extra["WEBSQL_DB_TYPE"] = db.DBType
		extra["WEBSQL_DB_URL"] = db.URL
		extra["WEBSQL_DB_USER"] = db.User
		extra["WEBSQL_DB_PASSWORD"] = db.Password
		extra["WEBSQL_DB_SCHEMA"] = db.Schema
	}
	return sandboxProcessEnv(extra)
}

// containsCodegenSecrets 判断产物是否包含不应外带的连接凭据。
func containsCodegenSecrets(data []byte, db *CodegenDBInfo) bool {
	if db == nil {
		return false
	}
	secrets := []string{db.Password}
	// URL 中内嵌凭据（user:pass@host）时才扫描，避免把普通主机名误判为泄露。
	if strings.Contains(db.URL, "@") {
		secrets = append(secrets, db.URL)
	}
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) >= 4 && bytes.Contains(data, []byte(s)) {
			return true
		}
	}
	return false
}

// prependPathList 把 head 置于 tail 之前（tail 为空时只返回 head）。
func prependPathList(head, tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return head
	}
	return head + string(os.PathListSeparator) + tail
}

// setEnvValue 覆盖式设置环境变量（同名只保留一条），避免重复项导致的取值歧义。
func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+value)
}

// maskCodegenSecrets 掩码输出中的敏感值（密码），避免其回流到模型上下文或日志。
func maskCodegenSecrets(s string, db *CodegenDBInfo) string {
	if db == nil || db.Password == "" {
		return s
	}
	return strings.ReplaceAll(s, db.Password, "***")
}

// buildCodegenSchemaSummary 生成「用户选中连接」的表结构摘要（schema.json），供脚本参考。
// 任何失败都降级为空摘要，不影响导出主流程。
func buildCodegenSchemaSummary(conn *sqlx.DB, db *CodegenDBInfo) []byte {
	type column struct {
		Name string `json:"name"`
	}
	type table struct {
		Name    string   `json:"name"`
		Columns []column `json:"columns"`
	}
	summary := struct {
		DBType string  `json:"dbType"`
		Schema string  `json:"schema"`
		Tables []table `json:"tables"`
	}{Tables: []table{}}
	if db != nil {
		summary.DBType = db.DBType
		summary.Schema = db.Schema
	}
	write := func() []byte {
		b, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return []byte("{}")
		}
		return b
	}
	if conn == nil {
		return write()
	}

	var query string
	var args []any
	switch strings.ToLower(summary.DBType) {
	case "sqlite", "sqlite3":
		query = "SELECT m.name AS t, p.name AS c FROM sqlite_master m JOIN pragma_table_info(m.name) p " +
			"WHERE m.type='table' AND m.name NOT LIKE 'sqlite_%' ORDER BY m.name, p.cid"
	case "oracle":
		query = "SELECT TABLE_NAME, COLUMN_NAME FROM ALL_TAB_COLUMNS WHERE OWNER = :1 ORDER BY TABLE_NAME, COLUMN_ID"
		args = []any{strings.ToUpper(summary.Schema)}
	default: // mysql / mariadb
		if strings.TrimSpace(summary.Schema) == "" {
			query = "SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME, ORDINAL_POSITION"
		} else {
			query = "SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION"
			args = []any{summary.Schema}
		}
	}

	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return write()
	}
	defer rows.Close()

	index := map[string]int{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			continue
		}
		i, ok := index[t]
		if !ok {
			summary.Tables = append(summary.Tables, table{Name: t, Columns: []column{}})
			i = len(summary.Tables) - 1
			index[t] = i
		}
		summary.Tables[i].Columns = append(summary.Tables[i].Columns, column{Name: c})
	}
	return write()
}
