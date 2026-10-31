package export

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── F-21 worker 计数字段与回收扫描共用同一锁 ───

func TestSkillWorkerPoolReleaseConcurrentUpdates(t *testing.T) {
	p := &skillWorkerPool{
		maxJobs: 100000,
		idleTTL: time.Hour,
		wake:    make(chan struct{}),
	}
	worker := &skillWorker{done: make(chan struct{})}

	const rounds = 200
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.release(worker)
		}()
	}
	// 并发模拟 reapLoop 对 lastUsed/jobs 的读取（旧实现无同步，-race 会报竞争）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			p.mu.Lock()
			_ = worker.lastUsed
			_ = worker.jobs
			p.mu.Unlock()
		}
	}()
	wg.Wait()

	p.mu.Lock()
	jobs := worker.jobs
	idle := len(p.idle)
	p.mu.Unlock()
	if jobs != rounds {
		t.Fatalf("jobs 应精确累加：%d != %d", jobs, rounds)
	}
	if idle != rounds {
		t.Fatalf("健康 worker 应回到 idle 队列：%d", idle)
	}
}

// ─── F-06 Skill payload 递归脱敏 ───

func TestMaskJSONStrings_MasksNestedLeaves(t *testing.T) {
	raw := `{"markdown":"联系电话 13800138000","data":{"columns":["phone"],"rows":[["13800138000"]]},"count":3,"ok":true}`
	mask := func(s string) string { return strings.ReplaceAll(s, "13800138000", "***") }
	got, err := MaskJSONStrings(raw, mask)
	if err != nil {
		t.Fatalf("脱敏失败：%v", err)
	}
	if strings.Contains(got, "13800138000") {
		t.Fatalf("嵌套字符串叶子仍含敏感值：%s", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("脱敏后不再是合法 JSON：%v", err)
	}
	if count, ok := parsed["count"].(float64); !ok || count != 3 {
		t.Fatalf("数值字段不应被改动：%v", parsed["count"])
	}
	if _, err := MaskJSONStrings("not-json", mask); err == nil {
		t.Fatal("非法 JSON 必须返回错误，由调用方 fail-closed")
	}
}

// ─── F-10 缺少权限执行器时必须拒绝取数 ───

func TestRunExportQuery_NilRunnerIsFailClosed(t *testing.T) {
	if _, err := runExportQuery(context.Background(), nil, nil, "SELECT 1"); err == nil {
		t.Fatal("runner 为空时应拒绝，不得回退为直查")
	}

	runner := stubRunner{result: &QueryResult{Columns: []string{"a"}, Data: []map[string]any{{"a": 1}}}}
	got, err := runExportQuery(context.Background(), nil, runner, "SELECT 1")
	if err != nil {
		t.Fatalf("注入 runner 后应成功：%v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("结果异常：%+v", got)
	}
}

type stubRunner struct {
	result *QueryResult
	err    error
}

func (s stubRunner) QueryForExport(context.Context, string, int) (*QueryResult, error) {
	return s.result, s.err
}

// ─── F-13 源内容分段回执 ───

func TestPageArtifactText_DefaultAndOffset(t *testing.T) {
	source := strings.Repeat("字", 30000)
	page, total, truncated, next := pageArtifactText(source, 0, 0)
	if total != 30000 {
		t.Fatalf("总字符数应为一整个字段：%d", total)
	}
	if len([]rune(page)) != maxArtifactSourceChars {
		t.Fatalf("默认页大小应为 %d，实际 %d", maxArtifactSourceChars, len([]rune(page)))
	}
	if !truncated || next != maxArtifactSourceChars {
		t.Fatalf("应提示继续读取：truncated=%v next=%d", truncated, next)
	}

	// 分段可拼出全文
	var sb strings.Builder
	offset := 0
	for {
		chunk, _, cut, nxt := pageArtifactText(source, offset, 8000)
		sb.WriteString(chunk)
		if !cut {
			break
		}
		offset = nxt
	}
	if sb.String() != source {
		t.Fatal("分段读取无法还原完整源内容")
	}

	// 超过硬上限的 limit 被收敛
	page2, _, _, _ := pageArtifactText(source, 0, 1_000_000)
	if len([]rune(page2)) != maxArtifactSourceLimit {
		t.Fatalf("limit 应被上限收敛，实际 %d", len([]rune(page2)))
	}
}

func TestDefaultArtifactSourceField(t *testing.T) {
	cases := map[string]string{
		"export_excel":            "sql",
		"export_excel_with_chart": "sql",
		"export_by_code":          "code",
		"export_skill":            "skillPayload",
		"export_ppt":              "content",
		"export_analysis_docx":    "content",
		"export_html":             "content",
	}
	for tool, want := range cases {
		if got := defaultArtifactSourceField(&ArtifactMeta{Tool: tool}); got != want {
			t.Errorf("%s 默认源字段应为 %s，实际 %s", tool, want, got)
		}
	}
}

func TestClipArtifactCharts(t *testing.T) {
	charts := make([]ExcelChart, 0, 50)
	for i := 0; i < 50; i++ {
		charts = append(charts, ExcelChart{ChartType: "bar", XAxisField: strings.Repeat("x", 500), ChartTitle: "标题"})
	}
	got, truncated := clipArtifactCharts(charts, 8000)
	if !truncated {
		t.Fatal("超大图表数组应被截断")
	}
	if len(got) >= len(charts) {
		t.Fatalf("截断后应少于原数量：%d", len(got))
	}
	if small, cut := clipArtifactCharts([]ExcelChart{{ChartType: "bar"}}, 8000); cut || len(small) != 1 {
		t.Fatal("小图表数组不应被截断")
	}
}

// ─── F-32 导出根目录集中解析 ───

func TestSetRootRedirectsAllExportPaths(t *testing.T) {
	dir := t.TempDir()
	// 用例结束后回到“未显式配置”状态：不能把 Root 固定回本次计算的绝对路径，
	// 否则会在包目录下创建 exports 并污染后续用例。
	t.Cleanup(func() {
		exportRootState.mu.Lock()
		exportRootState.dir = ""
		exportRootState.mu.Unlock()
		invalidateArtifactMetaIndex()
	})

	SetRoot(filepath.Join(dir, "custom-exports"))
	if Root() != filepath.Clean(filepath.Join(dir, "custom-exports")) {
		t.Fatalf("Root 应指向自定义目录：%s", Root())
	}
	if got := Path("a.html"); got != filepath.Clean(filepath.Join(dir, "custom-exports", "a.html")) {
		t.Fatalf("Path 应落在导出根内：%s", got)
	}
	if !strings.HasSuffix(artifactsDir(), filepath.Join("custom-exports", ".meta")) {
		t.Fatalf("元数据目录应随导出根变化：%s", artifactsDir())
	}
	if !strings.HasSuffix(ownersDir(), filepath.Join("custom-exports", ".owners")) {
		t.Fatalf("归属目录应随导出根变化：%s", ownersDir())
	}

	// 归属登记/校验必须落在新根下
	if err := os.WriteFile(Path("owner-check.html"), []byte("<html>ok</html>"), 0o600); err != nil {
		t.Fatalf("写入导出文件失败：%v", err)
	}
	RecordExportOwnerByURL(exportsURLPrefix+"owner-check.html", "u1")
	if allowed, known := CheckExportAccess("owner-check.html", "u1"); !known || !allowed {
		t.Fatal("自定义导出根下归属校验应生效")
	}
	if allowed, _ := CheckExportAccess("owner-check.html", "u2"); allowed {
		t.Fatal("非归属用户不得下载")
	}
	if _, err := os.Stat(filepath.Join(Root(), "owner-check.html")); err != nil {
		t.Fatalf("产物应落在自定义导出根内：%v", err)
	}
}

// ─── F-12 连接凭据不进入子进程环境与日志 ───

func TestURLHasUserInfo(t *testing.T) {
	cases := map[string]bool{
		"mysql://user:pass@host:3306/db": true,
		"postgres://u@h/db":              true,
		"mysql://host:3306/db":           false,
		"tcp(host:3306)/db":              false,
		"":                               false,
		"/data/app.db":                   false,
	}
	for raw, want := range cases {
		if got := urlHasUserInfo(raw); got != want {
			t.Errorf("urlHasUserInfo(%q)=%v want %v", raw, got, want)
		}
	}
}

func TestParseDBConnParts(t *testing.T) {
	host, port, name := parseDBConnParts(&CodegenDBInfo{DBType: "mysql", URL: "mysql://root:pw@10.0.0.1:3306/shop", Schema: "shop"})
	if host != "10.0.0.1" || port != "3306" || name != "shop" {
		t.Fatalf("带凭据 URL 解析错误：host=%s port=%s name=%s", host, port, name)
	}
	host, port, name = parseDBConnParts(&CodegenDBInfo{DBType: "mysql", URL: "tcp(127.0.0.1:3306)/testdb"})
	if host != "127.0.0.1" || port != "3306" || name != "testdb" {
		t.Fatalf("Go DSN 解析错误：host=%s port=%s name=%s", host, port, name)
	}
	host, _, name = parseDBConnParts(&CodegenDBInfo{DBType: "oracle", URL: "db-host:1521/ORCLPDB", Schema: "HR"})
	if host != "db-host" || name != "ORCLPDB" {
		t.Fatalf("Oracle DSN 解析错误：host=%s name=%s", host, name)
	}
	if h, _, n := parseDBConnParts(nil); h != "" || n != "" {
		t.Fatal("nil 入参应返回空值")
	}
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix), true
		}
	}
	return "", false
}

func TestBuildCodegenEnv_KeepsCredentialsOutOfURL(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		exportRootState.mu.Lock()
		exportRootState.dir = ""
		exportRootState.mu.Unlock()
		invalidateArtifactMetaIndex()
	})
	SetRoot(filepath.Join(dir, "exports"))

	env := buildCodegenEnv(dir, &CodegenDBInfo{
		DBType: "mysql", URL: "mysql://root:secret-pw@10.0.0.1:3306/shop",
		User: "root", Password: "secret-pw", Schema: "shop",
	})
	if _, ok := envValue(env, "WEBSQL_DB_URL"); ok {
		t.Fatal("带凭据的连接 URL 不得注入子进程环境")
	}
	if host, ok := envValue(env, "WEBSQL_DB_HOST"); !ok || host != "10.0.0.1" {
		t.Fatalf("应注入分项 host：%q", host)
	}
	if port, ok := envValue(env, "WEBSQL_DB_PORT"); !ok || port != "3306" {
		t.Fatalf("应注入分项 port：%q", port)
	}
	if name, ok := envValue(env, "WEBSQL_DB_NAME"); !ok || name != "shop" {
		t.Fatalf("应注入分项库名：%q", name)
	}

	// 不带凭据时保持原行为（脚本仍可直接用 URL）
	safe := buildCodegenEnv(dir, &CodegenDBInfo{DBType: "mysql", URL: "tcp(127.0.0.1:3306)/testdb"})
	if got, ok := envValue(safe, "WEBSQL_DB_URL"); !ok || got != "tcp(127.0.0.1:3306)/testdb" {
		t.Fatalf("无凭据连接串应继续注入：%q", got)
	}

	// sqlite 的 URL 是文件路径，即使含 @ 也不能剥离
	sqlite := buildCodegenEnv(dir, &CodegenDBInfo{DBType: "sqlite", URL: "/data/my@db.db"})
	if got, ok := envValue(sqlite, "WEBSQL_DB_URL"); !ok || got != "/data/my@db.db" {
		t.Fatalf("sqlite 路径必须原样注入：%q", got)
	}
}

func TestMaskCodegenSecrets(t *testing.T) {
	db := &CodegenDBInfo{DBType: "mysql", URL: "mysql://appuser:topsecret@10.0.0.1:3306/shop", User: "appuser", Password: "topsecret"}
	logs := "dsn=mysql://appuser:topsecret@10.0.0.1:3306/shop\nother=postgresql://other:***@h/db"
	got := maskCodegenSecrets(logs, db)
	for _, leak := range []string{"topsecret", "appuser:"} {
		if strings.Contains(got, leak) {
			t.Fatalf("日志仍含凭据 %q：%q", leak, got)
		}
	}
	if !strings.Contains(got, "***@10.0.0.1") {
		t.Fatalf("应保留主机名便于排障：%q", got)
	}
	// 无凭据的普通 URL 不被误删
	plain := maskCodegenSecrets("host tcp(127.0.0.1:3306)", &CodegenDBInfo{Password: "pw"})
	if plain != "host tcp(127.0.0.1:3306)" {
		t.Fatalf("无敏感内容不应被改写：%q", plain)
	}
}

func TestContainsCodegenSecretsDetectsURLAndUser(t *testing.T) {
	db := &CodegenDBInfo{URL: "mysql://appuser:topsecret@h/db", User: "appuser", Password: "topsecret"}
	if !containsCodegenSecrets([]byte("leak: mysql://appuser:topsecret@h/db"), db) {
		t.Fatal("应识别完整凭据 URL 外带")
	}
	if containsCodegenSecrets([]byte("host: h, no creds"), db) {
		t.Fatal("未包凭据的文本不应误判")
	}
}

// ─── F-31 .meta 索引避免全量扫描 ───

func TestArtifactMetaIndexRebuildOnInvalidate(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		exportRootState.mu.Lock()
		exportRootState.dir = ""
		exportRootState.mu.Unlock()
		invalidateArtifactMetaIndex()
	})
	invalidateArtifactMetaIndex()

	for _, name := range []string{"a_20260101_000000_aa.html", "b_20260101_000001_bb.html"} {
		if err := saveArtifactMeta(&ArtifactMeta{FileName: name, UserID: "u1", SessionID: "s1", Tool: "export_html", Version: 1}); err != nil {
			t.Fatalf("写入元数据失败：%v", err)
		}
	}
	items := loadArtifactMetaIndex()
	if len(items) != 2 {
		t.Fatalf("索引应包含 2 条：%d", len(items))
	}

	// 命中缓存：新写入后未失效时仍为旧快照（仅按目录 mtime/显式失效重建）
	if again := loadArtifactMetaIndex(); len(again) < 2 {
		t.Fatal("索引不应丢失已加载条目")
	}

	meta := &ArtifactMeta{FileName: "c_20260101_000002_cc.html", UserID: "u1", SessionID: "s1", Tool: "export_html", Version: 1}
	if err := saveArtifactMeta(meta); err != nil {
		t.Fatalf("写入元数据失败：%v", err)
	}
	items = loadArtifactMetaIndex()
	if len(items) != 3 {
		t.Fatalf("saveArtifactMeta 后索引应重建：%d", len(items))
	}

	// 版本推进使用索引，不重新全量读取
	if next := nextArtifactVersion(meta); next != 2 {
		t.Fatalf("下一版本应为 2，实际 %d", next)
	}
}
