package export

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// 高级导出（export_by_code）的静态策略校验。
//
// 安全背景：export_by_code 允许大模型编写 Python 并执行，等价于一条受限的代码执行通道。
// 「临时代码只读」由四层共同保证：
//  1. 数据库层：沙箱只读助手（websql_db）建立只读连接并逐语句校验（仅查询）；
//  2. 进程层：注入 sitecustomize 导入钩子，运行期拦截网络/进程/原生/裸驱动模块；
//  3. 文件层：每次运行独立 runDir，cwd=runDir，产物回收后清理（写仅限自建目录）；
//  4. 静态层（本文件）：执行前拒绝明显违规的源码，降低绕过概率。
//
// 说明：静态层只是「降低绕过概率」，真正兜底的是第 1~3 层。

// codegenBannedImportRoots 禁止在模型代码中导入的顶层模块。
// 覆盖网络外泄、进程/原生调用、动态序列化，以及各类数据库裸驱动
// （裸驱动会绕过 websql_db 的只读约束，访问数据库必须经只读助手）。
var codegenBannedImportRoots = map[string]bool{
	// 网络外泄
	"socket": true, "ssl": true, "http": true, "urllib": true, "urllib2": true,
	"urllib3": true, "requests": true, "httpx": true, "aiohttp": true,
	"ftplib": true, "smtplib": true, "poplib": true, "imaplib": true,
	"telnetlib": true, "xmlrpc": true, "asyncio": true, "websocket": true,
	// 进程 / 原生调用
	"subprocess": true, "multiprocessing": true, "ctypes": true, "_ctypes": true,
	"cffi": true, "pty": true, "signal": true,
	// 序列化 / 动态导入
	"pickle": true, "marshal": true, "shelve": true, "importlib": true,
	// 数据库裸驱动（必须经 websql_db 只读助手）
	"pymysql": true, "MySQLdb": true, "mysql": true, "sqlite3": true,
	"oracledb": true, "cx_Oracle": true, "psycopg2": true,
}

// codegenBannedCalls 明显违规的调用/属性访问子串（词级近似；运行期钩子为兜底）。
var codegenBannedCalls = []string{
	"os.system", "os.popen", "os.remove", "os.unlink", "os.rmdir",
	"os.removedirs", "os.rename", "os.replace", "os.chmod", "os.chown",
	"shutil.rmtree", "shutil.move", "shutil.copy",
	"eval(", "exec(", "__import__(", "compile(",
	"importlib.import_module",
}

var (
	reCodegenImport = regexp.MustCompile(`(?m)^\s*import\s+([^\n#]+)`)
	reCodegenFrom   = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z_][\w.]*)\s+import\b`)
)

// validateCodegenSource 对模型给出的 Python 源码做静态策略校验，返回首个违规原因。
func validateCodegenSource(code string) error {
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("代码不能为空")
	}
	if strings.ContainsRune(code, '\x00') {
		return fmt.Errorf("代码包含非法字符")
	}

	if err := checkCodegenImports(code); err != nil {
		return err
	}

	for _, bad := range codegenBannedCalls {
		if strings.Contains(code, bad) {
			return fmt.Errorf("高级导出沙箱禁止使用 %q（临时代码仅可查询数据并生成文件，不得修改系统或数据）", bad)
		}
	}
	return nil
}

// checkCodegenImports 扫描 import / from ... import 语句并拒绝黑名单模块。
func checkCodegenImports(code string) error {
	for _, m := range reCodegenImport.FindAllStringSubmatch(code, -1) {
		for _, part := range strings.Split(m[1], ",") {
			root := firstIdentifier(strings.TrimSpace(part))
			if root == "" {
				continue
			}
			if codegenBannedImportRoots[root] {
				return bannedImportError(root)
			}
		}
	}
	for _, m := range reCodegenFrom.FindAllStringSubmatch(code, -1) {
		root := strings.SplitN(m[1], ".", 2)[0]
		if codegenBannedImportRoots[root] {
			return bannedImportError(root)
		}
	}
	return nil
}

func bannedImportError(root string) error {
	return fmt.Errorf("高级导出沙箱禁止导入模块 %q（网络/进程/原生调用与数据库裸驱动均不可用；查询数据库请使用 websql_db 只读助手）", root)
}

// firstIdentifier 取出 "os as o" / "os.path" 中的顶层标识符（os）。
func firstIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	return s
}

// validateCodegenOutputRel 校验脚本声明的产出路径必须是 runDir 内的相对路径。
func validateCodegenOutputRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("outputFile 不能为空（须为脚本在当前工作目录内产出的文件名）")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") ||
		(len(rel) > 1 && rel[1] == ':') {
		return "", fmt.Errorf("outputFile 必须是相对于当前工作目录的路径，不能是绝对路径")
	}
	clean := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("outputFile 不允许逃逸当前工作目录")
	}
	return filepath.FromSlash(clean), nil
}