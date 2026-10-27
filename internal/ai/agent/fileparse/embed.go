package fileparse

import (
	"embed"
	"os"
	"path/filepath"
)

// 解析脚本以编译期嵌入方式随二进制分发：
// 无需在部署包中额外维护脚本目录，也避免运行期被篡改（skills 目录是只读策略的例外，
// 而这里连"读脚本"都不依赖外部文件）。

//go:embed pyscripts/*.py
var pythonScripts embed.FS

// parseScriptName 解析入口脚本文件名
const parseScriptName = "parse_document.py"

// writePythonScripts 把内嵌脚本写入运行目录，返回入口脚本绝对路径。
//
// 运行目录为一次性临时目录（调用方负责清理），脚本以 0600 写入：
// 子进程只能读到本次写入的受信脚本，避免从其他可写目录加载到同名文件。
func writePythonScripts(dir string) (string, error) {
	entries, err := pythonScripts.ReadDir("pyscripts")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := pythonScripts.ReadFile("pyscripts/" + entry.Name())
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), content, 0o600); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, parseScriptName), nil
}
