//go:build desktop

package bindings

import (
	"os"
	"path/filepath"

	"websql/internal/app/datadict"
	"websql/internal/pkg/rpc"
)

// registerDatadict 注册 datadict 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 datadict 模块的方法:
//   - POST /api/datadict/generate       → datadict.GenerateDict
//   - POST /api/datadict/export/html   → datadict.ExportDictHTML  (BlobHandler)
//   - POST /api/datadict/export/pdf     → datadict.ExportDictPDF   (BlobHandler)
//   - GET  /api/datadict/tables         → datadict.GetDictTables
//
// 调用 service: internal/app/datadict/binding_delegates.go
func registerDatadict(r *Registry) {
	r.register("datadict", "GenerateDict", func(req rpc.Request) rpc.Response {
		schema := req.StringBody("schema")
		if schema == "" {
			schema = req.StringParam("schema")
		}
		tablesStr := req.StringBody("tables")
		if tablesStr == "" {
			tablesStr = req.StringParam("tables")
		}
		dict := datadict.GenerateDictByService(req.ConnID, schema, tablesStr, req.Authorization)
		return okResponse(dict)
	})

	r.register("datadict", "GetDictTables", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		result := datadict.GetDictTablesByService(req.ConnID, schema, req.Authorization)
		return okResponse(result)
	})

	// ExportDictHTML: 文件下载
	r.registerBlob("datadict", "ExportDictHTML", func(req rpc.Request) (BlobResult, error) {
		schema := req.StringBody("schema")
		if schema == "" {
			schema = req.StringParam("schema")
		}
		tablesStr := req.StringBody("tables")
		if tablesStr == "" {
			tablesStr = req.StringParam("tables")
		}
		html, filename := datadict.ExportDictHTMLByService(req.ConnID, schema, tablesStr, req.Authorization)
		path, err := writeHTMLToTemp(filename, html)
		if err != nil {
			return BlobResult{}, err
		}
		return BlobResult{Path: path, Filename: filename, Mime: "text/html; charset=utf-8"}, nil
	})

	// ExportDictPDF: 文件下载(实际为可打印 HTML)
	r.registerBlob("datadict", "ExportDictPDF", func(req rpc.Request) (BlobResult, error) {
		schema := req.StringBody("schema")
		if schema == "" {
			schema = req.StringParam("schema")
		}
		tablesStr := req.StringBody("tables")
		if tablesStr == "" {
			tablesStr = req.StringParam("tables")
		}
		html, filename := datadict.ExportDictPDFByService(req.ConnID, schema, tablesStr, req.Authorization)
		path, err := writeHTMLToTemp(filename, html)
		if err != nil {
			return BlobResult{}, err
		}
		return BlobResult{Path: path, Filename: filename, Mime: "text/html; charset=utf-8"}, nil
	})
}

// writeHTMLToTemp 将 HTML 字符串写入临时文件,返回临时文件路径。
// 失败时清理已创建的临时文件。
func writeHTMLToTemp(filename, content string) (string, error) {
	tmpDir := os.TempDir()
	path := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}
