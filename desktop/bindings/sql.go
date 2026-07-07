//go:build desktop

package bindings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/app/sql"
	"websql/internal/pkg/rpc"
)

// registerSql 注册 sql 模块的所有 binding。
//
// sql 模块包含 4 个方法，覆盖了三种 binding 调用类型：
//   1. ExecSQL      → 普通 Handler (调用 sql.ExecSQLByService)
//   2. ExportXlsx   → BlobHandler  (按表导出 XLSX，生成临时文件)
//   3. ExportXlsxBySql → BlobHandler (按 SQL 导出 XLSX)
//   4. ImportXlsx   → 普通 Handler (从 XLSX 导入，接收文件路径)
//
// 对应 HTTP 路由: internal/app/router.go
//   - GET  /api/exportXlsx       → sql.ExportXlsx
//   - POST /api/exportXlsxBySql  → sql.ExportXlsxBySql
//   - POST /api/importXlsx       → sql.ImportXlsx
//   - POST /api/execSQL          → sql.ExecSQL
func registerSql(r *Registry) {
	// ExecSQL: 执行 SQL，返回结果集或批量结果。
	// 入参 (Body): sql, schema, tableName, maxLine, batch, isFile
	r.register("sql", "ExecSQL", func(req rpc.Request) rpc.Response {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// 桌面模式 ClientIP 用固定值，不依赖网络栈
		execReq := &sql.ExecRequest{
			ConnID:        req.ConnID,
			Schema:        req.StringBody("schema"),
			TableName:     req.StringBody("tableName"),
			SQL:           req.StringBody("sql"),
			MaxLine:       req.StringBody("maxLine"),
			Batch:         req.StringBody("batch"),
			IsFile:        req.StringBody("isFile") == "true",
			Authorization: req.Authorization,
			ClientIP:      "127.0.0.1",
		}
		userId := extractUserId(req.Authorization)
		execReq.UserID = userId

		result, err := sql.ExecSQLByService(ctx, execReq)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})

	// ExportXlsx: 按表导出 XLSX，写入临时文件后返回 BlobResult。
	// 入参 (Params): schema, table
	r.registerBlob("sql", "ExportXlsx", func(req rpc.Request) (BlobResult, error) {
		table := req.StringParam("table")
		if table == "" {
			return BlobResult{}, fmt.Errorf("缺少 table 参数")
		}

		filename := exportFilename(table)
		path, err := writeBlobToTemp(filename, func(w *os.File) error {
			return sql.ExportTableByService(&sql.ExportRequest{
				ConnID:        req.ConnID,
				Schema:        req.StringParam("schema"),
				Table:         table,
				Authorization: req.Authorization,
				Writer:        w,
			})
		})
		if err != nil {
			return BlobResult{}, err
		}
		return BlobResult{
			Path:     path,
			Filename: filename,
			Mime:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		}, nil
	})

	// ExportXlsxBySql: 按自定义 SQL 导出 XLSX，写入临时文件后返回 BlobResult。
	// 入参 (Body): schema, filename, sql
	r.registerBlob("sql", "ExportXlsxBySql", func(req rpc.Request) (BlobResult, error) {
		sqlStr := req.StringBody("sql")
		if sqlStr == "" {
			return BlobResult{}, fmt.Errorf("缺少 sql 参数")
		}
		filename := req.StringBody("filename")
		if filename == "" {
			filename = "export"
		}

		fullFilename := exportFilename(filename)
		path, err := writeBlobToTemp(fullFilename, func(w *os.File) error {
			return sql.ExportBySQLByService(&sql.ExportBySQLRequest{
				ConnID:        req.ConnID,
				Schema:        req.StringBody("schema"),
				Filename:      filename,
				SQL:           sqlStr,
				Authorization: req.Authorization,
				Writer:        w,
			})
		})
		if err != nil {
			return BlobResult{}, err
		}
		return BlobResult{
			Path:     path,
			Filename: fullFilename,
			Mime:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		}, nil
	})

	// ImportXlsx: 从 XLSX 导入数据。
	// 入参 (Body): schema, table, optType, mapping, startRow, filePath
	// filePath 由前端通过 OpenFileDialog 选择后传入，是用户本地文件绝对路径。
	r.register("sql", "ImportXlsx", func(req rpc.Request) rpc.Response {
		filePath := req.StringBody("filePath")
		if filePath == "" {
			return rpc.Err(400, "缺少 filePath 参数")
		}
		// 路径校验，防止相对路径或目录穿越
		if !filepath.IsAbs(filePath) {
			return rpc.Err(400, "filePath 必须为绝对路径")
		}

		f, err := os.Open(filePath)
		if err != nil {
			return rpc.Err(400, "打开文件失败: "+err.Error())
		}
		defer f.Close()

		importReq := &sql.ImportRequest{
			ConnID:        req.ConnID,
			Schema:        req.StringBody("schema"),
			Table:         req.StringBody("table"),
			OperType:      req.StringBody("optType"),
			Mapping:       req.StringBody("mapping"),
			StartRow:      req.StringBody("startRow"),
			Filename:      filepath.Base(filePath),
			Authorization: req.Authorization,
			Reader:        f,
		}

		result, err := sql.ImportXlsxByService(importReq)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})
}

// writeBlobToTemp 创建一个临时文件并执行写入。
// 写入成功后返回绝对路径；失败时清理临时文件。
// 调用方在收到 BlobResult.Path 后，应在用户保存或取消后通过 CleanupBlob 删除。
func writeBlobToTemp(filename string, write func(w *os.File) error) (string, error) {
	tmpDir := os.TempDir()
	// 文件名去重，避免并发场景覆盖
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	ext := filepath.Ext(filename)
	finalName := fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), ext)
	path := filepath.Join(tmpDir, finalName)

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	if err := write(f); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("关闭临时文件失败: %w", err)
	}
	return path, nil
}

// exportFilename 构造下载文件名，与 HTTP handler 一致：prefix-YYYY-MM-DD.xlsx
func exportFilename(prefix string) string {
	return fmt.Sprintf("%s-%s.xlsx", prefix, time.Now().Format(time.DateOnly))
}
