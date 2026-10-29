package fileparse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"websql/internal/ai/agent/export"
	"websql/internal/pkg/strutil"
)

// Python 解析后端（docx / pdf）。
//
// 为什么用 Python：docx/pdf 领域的 Python 生态（python-docx、pdfplumber(pdfminer.six)）
// 在中文排版、表格与文本抽取质量上显著优于纯 Go 方案；这里以「子进程 + 结构化 JSON」
// 方式接入，作为延迟解析链路的一部分（仅在用户真正需要文件内容时启动）。
//
// 工程约束：
//   - 脚本为仓库内第一方代码（编译期嵌入，运行时写入一次性临时目录）；
//   - 环境加固（PYTHONUTF8/SAFEPATH 等）+ 超时 + 输出上限 + 隐藏窗口；
//   - OS 级隔离「可用则启用」（export.ApplyIsolationBestEffort）：与 codegen 的
//     fail-closed 不同，解析是可信只读脚本，无隔离时降级继续，否则绝大多数部署无法解析；
//   - 依赖缺失时自动降级：docx 回退内置 Go 解析（优先级 0），pdf 明确提示不支持。
const (
	// pythonProbeTTL 依赖探测结果缓存有效期（运维安装依赖后无需重启即可生效）
	pythonProbeTTL = 5 * time.Minute
	// pythonProbeTimeout 单次依赖探测超时
	pythonProbeTimeout = 10 * time.Second
	// pythonRunMaxStdoutBytes 解析结果输出上限（文本本身已由 MaxTextChars 约束）
	pythonRunMaxStdoutBytes = 32 << 20
	// pythonRunMaxStderrBytes stderr 收集上限（仅用于日志与错误提示）
	pythonRunMaxStderrBytes = 64 << 10

	// pythonModuleDocx docx 解析依赖的模块名（python-docx 的导入名）
	pythonModuleDocx = "docx"
	// pythonModulePDF pdf 解析依赖的模块名（pdfplumber）
	pythonModulePDF = "pdfplumber"
)

// moduleProbe 带 TTL 的模块可用性探测缓存
type moduleProbe struct {
	mu      sync.Mutex
	checked time.Time
	ok      bool
}

var (
	docxModuleProbe moduleProbe
	pdfModuleProbe  moduleProbe
)

func init() {
	// 优先级 100 > Go 实现（0）：依赖可用时优先 Python，不可用时自动降级
	Register("python-docx", 100, func() bool {
		return pythonModuleAvailable(pythonModuleDocx, &docxModuleProbe)
	}, []string{".docx"}, ParserFunc(parseDOCXViaPython))

	Register("python-pdf", 100, func() bool {
		return pythonModuleAvailable(pythonModulePDF, &pdfModuleProbe)
	}, []string{".pdf"}, ParserFunc(parsePDFViaPython))
}

// PythonAvailable 报告当前环境是否检测到 Python 解释器。
func PythonAvailable() bool {
	return export.GetPythonPath() != ""
}

// PythonModules 报告解析所需 Python 模块的可用性（供能力自检接口展示）。
func PythonModules() map[string]bool {
	return map[string]bool{
		pythonModuleDocx: pythonModuleAvailable(pythonModuleDocx, &docxModuleProbe),
		pythonModulePDF:  pythonModuleAvailable(pythonModulePDF, &pdfModuleProbe),
	}
}

// WarmPythonProbes 预热依赖探测（启动阶段异步调用，避免首个请求承担探测延迟）。
func WarmPythonProbes() {
	if !PythonAvailable() {
		return
	}
	_ = pythonModuleAvailable(pythonModuleDocx, &docxModuleProbe)
	_ = pythonModuleAvailable(pythonModulePDF, &pdfModuleProbe)
}

// pythonModuleAvailable 探测 Python 模块是否可用（结果带 TTL 缓存）。
func pythonModuleAvailable(module string, probe *moduleProbe) bool {
	probe.mu.Lock()
	defer probe.mu.Unlock()

	if !probe.checked.IsZero() && time.Since(probe.checked) < pythonProbeTTL {
		return probe.ok
	}
	probe.checked = time.Now()
	probe.ok = probeModule(module)
	return probe.ok
}

// probeModule 执行 `python -c "import <module>"` 判断依赖是否安装。
// module 为包内常量，不存在注入风险。
func probeModule(module string) bool {
	python := export.GetPythonPath()
	if python == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), pythonProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, python, "-c", "import "+module)
	export.HideCommandWindow(cmd)
	cmd.Env = pythonEnv()

	out := &limitedBuffer{limit: 4 << 10}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		log.Printf("[FileParse] Python 模块不可用 - module=%s, err=%v, out=%s\n",
			module, err, strutil.TruncateBytes(out.String(), 200))
		return false
	}
	return true
}

// pythonParseRequest 传给解析脚本的请求（stdin 单行 JSON）
type pythonParseRequest struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	MaxChars int    `json:"maxChars"`
}

// pythonParseResponse 解析脚本的响应（stdout 单行 JSON）
type pythonParseResponse struct {
	OK         bool           `json:"ok"`
	Error      string         `json:"error"`
	Text       string         `json:"text"`
	TotalChars int            `json:"totalChars"`
	Pages      int            `json:"pages"`
	Sections   []Section      `json:"sections"`
	PageList   []Page         `json:"pageList"`
	Meta       map[string]any `json:"meta"`
	Notice     string         `json:"notice"`
}

// parseDOCXViaPython 使用 python-docx 解析 docx（段落 + 表格）
func parseDOCXViaPython(ctx context.Context, src Source) (*Result, error) {
	return parseDocKindViaPython(ctx, src, "docx")
}

// parsePDFViaPython 使用 pdfplumber 解析 pdf（逐页文本）
func parsePDFViaPython(ctx context.Context, src Source) (*Result, error) {
	return parseDocKindViaPython(ctx, src, "pdf")
}

func parseDocKindViaPython(ctx context.Context, src Source, kind string) (*Result, error) {
	resp, err := runPythonParse(ctx, src, kind)
	if err != nil {
		return nil, err
	}
	return &Result{
		Kind:       KindDoc,
		Text:       resp.Text,
		TotalChars: resp.TotalChars,
		Pages:      resp.Pages,
		Meta:       resp.Meta,
		Notice:     resp.Notice,
		Sections:   resp.Sections,
		PageList:   resp.PageList,
	}, nil
}

// runPythonParse 执行一次解析脚本并返回结构化结果。
//
// 失败语义：脚本正常结束但业务失败时返回脚本给出的中文提示（结构化 error），
// 便于直接透传给用户；进程级失败（超时/启动失败）返回可读的技术错误。
func runPythonParse(ctx context.Context, src Source, kind string) (*pythonParseResponse, error) {
	python := export.GetPythonPath()
	if python == "" {
		return nil, errors.New("当前环境未检测到 Python 运行环境")
	}

	runDir, err := os.MkdirTemp("", "websql_parse_")
	if err != nil {
		return nil, fmt.Errorf("创建解析运行目录失败：%w", err)
	}
	defer os.RemoveAll(runDir)

	scriptPath, err := writePythonScripts(runDir)
	if err != nil {
		return nil, fmt.Errorf("准备解析脚本失败：%w", err)
	}

	absPath, err := filepath.Abs(src.Path)
	if err != nil {
		absPath = src.Path
	}
	payload, err := json.Marshal(pythonParseRequest{Path: absPath, Kind: kind, MaxChars: MaxTextChars})
	if err != nil {
		return nil, fmt.Errorf("构造解析请求失败：%w", err)
	}

	cmd := exec.CommandContext(ctx, python, "-u", "-B", scriptPath)
	export.HideCommandWindow(cmd)
	cmd.Dir = runDir
	cmd.Env = pythonEnv()
	cmd.Stdin = bytes.NewReader(payload)

	stdout := &limitedBuffer{limit: pythonRunMaxStdoutBytes}
	stderr := &limitedBuffer{limit: pythonRunMaxStderrBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	cleanup, isolated := export.ApplyIsolationBestEffort(cmd, runDir)
	defer cleanup()

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("[FileParse] 解析超时 - kind=%s, file=%s, elapsed=%v\n", kind, src.FileName, elapsed)
			return nil, fmt.Errorf("解析超时（超过 %v），文件可能过大或结构过于复杂", ParseTimeout)
		}
		log.Printf("[FileParse] 解析子进程失败 - kind=%s, file=%s, isolated=%v, err=%v, stderr=%s\n",
			kind, src.FileName, isolated, runErr, strutil.TruncateBytes(stderr.String(), 300))
		return nil, fmt.Errorf("解析进程异常退出：%v", runErr)
	}

	resp := &pythonParseResponse{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), resp); err != nil {
		log.Printf("[FileParse] 解析结果非 JSON - kind=%s, file=%s, stderr=%s\n",
			kind, src.FileName, strutil.TruncateBytes(stderr.String(), 300))
		return nil, errors.New("解析结果格式错误，请重试或改用其他格式")
	}

	log.Printf("[FileParse] Python 解析完成 - kind=%s, file=%s, impl=%s, chars=%d, pages=%d, isolated=%v, elapsed=%v\n",
		kind, src.FileName, kind, resp.TotalChars, resp.Pages, isolated, elapsed)

	if !resp.OK {
		msg := strings.TrimSpace(resp.Error)
		if msg == "" {
			msg = "解析失败，请检查文件内容"
		}
		return nil, errors.New(msg)
	}
	return resp, nil
}

// pythonEnv 构造解析子进程环境：继承系统环境并覆盖 Python 运行时约定。
//
// 与 codegen 环境一致的部分：UTF-8 输出、禁用字节码写入、SAFEPATH 防止把
// 运行目录（可能被写入同名模块）加入 sys.path。不同之处：解析不需要注入任何
// 数据库/密钥环境变量，因此不做额外注入。
func pythonEnv() []string {
	env := os.Environ()
	env = setEnvValue(env, "PYTHONIOENCODING", "utf-8")
	env = setEnvValue(env, "PYTHONUTF8", "1")
	env = setEnvValue(env, "PYTHONDONTWRITEBYTECODE", "1")
	env = setEnvValue(env, "PYTHONSAFEPATH", "1")
	return env
}

// setEnvValue 覆盖/追加环境变量
func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// limitedBuffer 限制写入总量的缓冲区（防子进程输出耗尽内存）。
//
// 超出上限后丢弃剩余内容但仍然“接受”写入：保持写端不阻塞，
// 避免子进程因管道写满而挂起（最终由超时兜底）。
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if remaining > len(p) {
		remaining = len(p)
	}
	b.buf.Write(p[:remaining])
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }
