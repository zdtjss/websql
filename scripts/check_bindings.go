//go:build ignore

// check_bindings 是 WebSQL 桌面版的路由完整性校验脚本。
//
// 用途:
//   对比 internal/app/router.go 中注册的 HTTP 路由与 desktop/bindings/*.go 中注册的 Wails binding,
//   输出差异并可选地以非 0 退出码用于 CI 卡点。
//
// 运行:
//   go run -tags ignore scripts/check_bindings.go           # 报告模式,仅打印差异
//   go run -tags ignore scripts/check_bindings.go -strict   # 严格模式,有缺失则 exit 1
//
// 设计:
//   1. 解析 router.go 中所有 routerGroup.(GET|POST)("path", pkg.Handler) 调用,
//      提取 (module=pkg, method=Handler) 二元组,作为 HTTP 路由全集。
//   2. 解析 desktop/bindings/*.go 中所有 r.register[rBlob|Stream]("module", "method", ...) 调用,
//      提取 (module, method) 二元组,作为 binding 注册全集。
//   3. 对照两边,输出:
//      - HTTP 有但 binding 缺失 (新增路由忘记加 binding)
//      - binding 有但 HTTP 没有 (已删除路由但 binding 残留)
//
// 例外白名单:
//   部分 HTTP 路由设计上不需要 binding (如健康检查、文件代理、SPA NoRoute)。
//   这些路由在脚本顶部 WHITELIST 中显式声明。
//
// 退出码:
//   0 - 无差异或非严格模式
//   1 - 严格模式下存在差异
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// WHITELIST 列出 HTTP 路由中不需要 binding 的方法名。
// 这些通常是基础设施路由(健康检查、SPA、代理),桌面版不需要。
var WHITELIST = map[string]bool{
	"proxy":            true, // /api/ext/ 代理
	"handleExportDownload": true, // /exports/:filename 静态文件下载
}

// HANDLER_SUFFIX 是 HTTP handler 命名后缀。
// router.go 中的 handler 命名风格不统一：部分带 Handler 后缀 (如 SaveSystemConfigHandler)，
// 部分不带 (如 admin.Login)。binding 注册时统一去掉后缀，前端 API 更简洁。
// 脚本比对时把 HTTP handler 名去掉该后缀后再与 binding 名对齐。
const HANDLER_SUFFIX = "Handler"

// HTTPRoute 表示一条 HTTP 路由的元数据。
type HTTPRoute struct {
	Path    string // /api/snippet/list
	Verb    string // GET/POST
	Module  string // snippet (从 handler pkg 推断)
	Method  string // List (从 handler 函数名推断)
	Handler string // snippet.List (原始 handler 引用)
}

// BindingEntry 表示一条 binding 注册项。
type BindingEntry struct {
	Module string
	Method string
	Kind   string // handler/blob/stream
	File   string
}

func main() {
	strict := flag.Bool("strict", false, "严格模式:有缺失则以非 0 退出")
	flag.Parse()

	projectRoot := findProjectRoot()
	routerFile := filepath.Join(projectRoot, "internal", "app", "router.go")
	bindingsDir := filepath.Join(projectRoot, "desktop", "bindings")

	httpRoutes, err := parseRouterFile(routerFile)
	if err != nil {
		fail("解析 router.go 失败: %v", err)
	}
	bindings, err := parseBindingsDir(bindingsDir)
	if err != nil {
		fail("解析 bindings 目录失败: %v", err)
	}

	report(httpRoutes, bindings, *strict)
}

// findProjectRoot 通过查找 go.mod 向上回溯定位项目根。
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		fail("无法获取工作目录: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	fail("未找到 go.mod,请在本项目根目录下运行")
	return ""
}

// parseRouterFile 解析 router.go,提取所有 routerGroup.(GET|POST)(...) 调用。
var routerRe = regexp.MustCompile(`routerGroup\.(GET|POST)\("([^"]+)",\s*([a-zA-Z0-9_.]+)`)

func parseRouterFile(path string) ([]HTTPRoute, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var routes []HTTPRoute
	matches := routerRe.FindAllStringSubmatch(string(data), -1)
	for _, m := range matches {
		verb := m[1]
		routePath := m[2]
		handler := m[3]
		module, method := splitHandler(handler)
		// 匿名函数 (handler="func") 与白名单路由跳过
		if module == "" || WHITELIST[method] {
			continue
		}
		routes = append(routes, HTTPRoute{
			Path:    routePath,
			Verb:    verb,
			Module:  module,
			Method:  method,
			Handler: handler,
		})
	}
	return routes, nil
}

// splitHandler 把 "snippet.List" 拆成 ("snippet", "List")。
// 同时统一命名风格，使 HTTP handler 名与 binding 注册名对齐：
//   - module 去掉 Handler 后缀 (agentHandler → agent)
//   - method 去掉 Handle 前缀 (HandleUploadExcel → UploadExcel)
//   - method 去掉 Handler 后缀 (SaveSystemConfigHandler → SaveSystemConfig)
//
// 这样无论 router.go 中写 admin.Login / agent.HandleUploadExcel / agentHandler.ChatStream
// / system.SaveSystemConfigHandler，都能与 binding 中 admin.Login / agent.UploadExcel /
// agent.ChatStream / system.SaveSystemConfig 对齐。
//
// 内联匿名函数(如 func(c *gin.Context) {...})不在此匹配,自动跳过。
func splitHandler(handler string) (string, string) {
	idx := strings.LastIndex(handler, ".")
	if idx == -1 {
		return "", ""
	}
	module := strings.TrimSuffix(handler[:idx], HANDLER_SUFFIX)
	method := handler[idx+1:]
	method = strings.TrimPrefix(method, "Handle")
	method = strings.TrimSuffix(method, HANDLER_SUFFIX)
	return module, method
}

// parseBindingsDir 解析 desktop/bindings/ 下所有 .go 文件,
// 提取 r.register / r.registerBlob / r.registerStream 调用。
var (
	registerRe       = regexp.MustCompile(`r\.register\(\s*"([^"]+)"\s*,\s*"([^"]+)"`)
	registerBlobRe   = regexp.MustCompile(`r\.registerBlob\(\s*"([^"]+)"\s*,\s*"([^"]+)"`)
	registerStreamRe = regexp.MustCompile(`r\.registerStream\(\s*"([^"]+)"\s*,\s*"([^"]+)"`)
)

func parseBindingsDir(dir string) ([]BindingEntry, error) {
	var entries []BindingEntry
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		content := string(data)
		base := filepath.Base(f)
		for _, m := range registerRe.FindAllStringSubmatch(content, -1) {
			entries = append(entries, BindingEntry{Module: m[1], Method: m[2], Kind: "handler", File: base})
		}
		for _, m := range registerBlobRe.FindAllStringSubmatch(content, -1) {
			entries = append(entries, BindingEntry{Module: m[1], Method: m[2], Kind: "blob", File: base})
		}
		for _, m := range registerStreamRe.FindAllStringSubmatch(content, -1) {
			entries = append(entries, BindingEntry{Module: m[1], Method: m[2], Kind: "stream", File: base})
		}
	}
	return entries, nil
}

// report 输出对照报告。
func report(httpRoutes []HTTPRoute, bindings []BindingEntry, strict bool) {
	bindingSet := make(map[string]BindingEntry)
	for _, b := range bindings {
		key := b.Module + "." + b.Method
		bindingSet[key] = b
	}

	httpSet := make(map[string]HTTPRoute)
	for _, r := range httpRoutes {
		key := r.Module + "." + r.Method
		httpSet[key] = r
	}

	var httpOnly []string
	for k, r := range httpSet {
		if _, ok := bindingSet[k]; !ok {
			httpOnly = append(httpOnly, fmt.Sprintf("  %s.%s  (%s %s  handler=%s)",
				r.Module, r.Method, r.Verb, r.Path, r.Handler))
		}
	}
	sort.Strings(httpOnly)

	var bindingOnly []string
	for k, b := range bindingSet {
		if _, ok := httpSet[k]; !ok {
			bindingOnly = append(bindingOnly, fmt.Sprintf("  %s.%s  (kind=%s  file=%s)",
				b.Module, b.Method, b.Kind, b.File))
		}
	}
	sort.Strings(bindingOnly)

	fmt.Println("=== 路由完整性校验报告 ===")
	fmt.Printf("HTTP 路由总数: %d\n", len(httpRoutes))
	fmt.Printf("binding 注册总数: %d\n", len(bindings))
	fmt.Println()

	if len(httpOnly) > 0 {
		fmt.Printf("[WARN] HTTP 路由有但 binding 缺失 (%d 项):\n", len(httpOnly))
		for _, line := range httpOnly {
			fmt.Println(line)
		}
		fmt.Println()
	}

	if len(bindingOnly) > 0 {
		fmt.Printf("[WARN] binding 有但 HTTP 路由没有 (%d 项):\n", len(bindingOnly))
		for _, line := range bindingOnly {
			fmt.Println(line)
		}
		fmt.Println()
	}

	if len(httpOnly) == 0 && len(bindingOnly) == 0 {
		fmt.Println("[OK] HTTP 路由与 binding 完全对齐")
		return
	}

	if strict {
		os.Exit(1)
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[FAIL] "+format+"\n", args...)
	os.Exit(2)
}
