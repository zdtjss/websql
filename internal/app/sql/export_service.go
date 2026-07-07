package sql

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/pkg/sanitize"

	"github.com/xuri/excelize/v2"
)

// ExportRequest 是按表导出 XLSX 的入参。
// Writer 由调用方注入:
//   - HTTP 模式: 传 c.Writer (gin response body)
//   - 桌面模式: 传 *os.File (临时文件,完成后返回 BlobResult)
type ExportRequest struct {
	ConnID        string
	Schema        string
	Table         string
	Authorization string
	Writer        io.Writer
}

// ExportBySQLRequest 是按 SQL 导出 XLSX 的入参。
type ExportBySQLRequest struct {
	ConnID        string
	Schema        string // 可空,空时自动推断
	Filename      string
	SQL           string
	Authorization string
	Writer        io.Writer
}

// ExportService 封装 XLSX 导出业务逻辑，与 gin.Context 解耦。
type ExportService struct{}

var (
	defaultExportService *ExportService
	defaultExportOnce    sync.Once
)

// ensureDefaultExport 返回单例 ExportService。
func ensureDefaultExport() *ExportService {
	defaultExportOnce.Do(func() {
		defaultExportService = &ExportService{}
	})
	return defaultExportService
}

// ExportTable 把指定表的数据导出为 XLSX，写入 req.Writer。
// 业务逻辑来自原 export.go 的 queryAndWrite。
// 标识符白名单校验、字段注释、类型转换等均与原行为一致。
func (s *ExportService) ExportTable(req *ExportRequest) error {
	table := req.Table
	schema := req.Schema
	connId := req.ConnID
	authorization := req.Authorization
	log.Println("正在导出: ", table)

	parts := strings.SplitN(table, ".", 2)
	var safeTable string
	if len(parts) == 2 {
		if !sanitize.IsValidIdentifier(parts[0]) || !sanitize.IsValidIdentifier(parts[1]) {
			return errors.New("非法的表名")
		}
		safeTable = fmt.Sprintf("`%s`.`%s`", parts[0], parts[1])
	} else {
		if !sanitize.IsValidIdentifier(table) {
			return errors.New("非法的表名")
		}
		safeTable = fmt.Sprintf("`%s`", table)
	}

	connCtx := conn.GetConn(connId, authorization)
	if connCtx == nil {
		return errors.New("数据库连接不可用")
	}
	rows, err := connCtx.Query("SELECT * from " + safeTable)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return errors.New("操作失败")
	}

	allColumns, err := rows.Columns()
	if err != nil {
		log.Printf("获取字段失败: %v", err)
		return errors.New("操作失败")
	}

	columnComment := make([]string, 0)
	columnMap := dbops.ColumnMapFiltered(table, schema, connId, authorization, connCtx)
	for i := range allColumns {
		columnComment = append(columnComment, columnMap[allColumns[i]])
	}

	cts, err := rows.ColumnTypes()
	if err != nil {
		log.Printf("获取字段类型失败: %v", err)
		return errors.New("操作失败")
	}

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	excel := excelize.NewFile()
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	if err != nil {
		log.Printf("创建流写入器失败: %v", err)
		return errors.New("操作失败")
	}

	columns2 := make([]any, len(allColumns))
	for idx := range allColumns {
		columns2[idx] = allColumns[idx]
	}
	columnComment2 := make([]any, len(columnComment))
	for idx := range columnComment {
		columnComment2[idx] = columnComment[idx]
	}
	streamWriter.SetRow("A1", columns2)
	streamWriter.SetRow("A2", columnComment2)

	values := make([]any, len(allColumns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	driverName := connCtx.DriverName()
	count := 2
	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			log.Printf("扫描行失败: %v", err)
			return errors.New("操作失败")
		}
		row := make([]any, 0, len(allColumns))
		for i := range allColumns {
			colType := colTypeMap[allColumns[i]]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}
		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		log.Printf("遍历行失败: %v", err)
		return errors.New("操作失败")
	}
	if err = streamWriter.Flush(); err != nil {
		log.Printf("导出excel失败: %v", err)
		return errors.New("操作失败")
	}
	if err = excel.Write(req.Writer); err != nil {
		log.Printf("写入输出流失败: %v", err)
		return errors.New("操作失败")
	}
	log.Println("导出完成: ", table)
	return nil
}

// ExportBySQL 把指定 SQL 的结果导出为 XLSX，写入 req.Writer。
// 业务逻辑来自原 export.go 的 queryAndWriteBySql。
func (s *ExportService) ExportBySQL(req *ExportBySQLRequest) error {
	connId := req.ConnID
	authorization := req.Authorization
	schema := req.Schema
	sqlStr := req.SQL
	log.Println("正在导出SQL: ", sqlStr)

	if schema == "" && connId != "" {
		dc := conn.GetConn(connId, authorization)
		if dc != nil {
			switch dc.DriverName() {
			case "mysql", "mariadb":
				dc.Get(&schema, "SELECT DATABASE()")
			case "oracle":
				dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
			case "sqlite":
				schema = "main"
			}
		}
	}

	analysis := permission.AnalyzeSQL(sqlStr, schema)
	permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
	if !permResult.Allowed {
		return errors.New(permResult.Message)
	}

	connCtx := conn.GetConn(connId, authorization)
	if connCtx == nil {
		return errors.New("数据库连接不可用")
	}
	rows, err := connCtx.Query(sqlStr)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return errors.New("操作失败")
	}

	columns, err := rows.Columns()
	if err != nil {
		log.Printf("获取字段失败: %v", err)
		return errors.New("操作失败")
	}

	cts, err := rows.ColumnTypes()
	if err != nil {
		log.Printf("获取字段类型失败: %v", err)
		return errors.New("操作失败")
	}

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	excel := excelize.NewFile()
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	if err != nil {
		log.Printf("创建流写入器失败: %v", err)
		return errors.New("操作失败")
	}

	columns2 := make([]any, len(columns))
	for idx := range columns {
		columns2[idx] = columns[idx]
	}
	streamWriter.SetRow("A1", columns2)

	values := make([]any, len(columns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	driverName := connCtx.DriverName()
	count := 1
	for rows.Next() {
		if err = rows.Scan(scanArgs...); err != nil {
			log.Printf("扫描行失败: %v", err)
			return errors.New("操作失败")
		}
		row := make([]any, 0, len(columns))
		for i, col := range columns {
			colType := colTypeMap[col]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}
		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		log.Printf("遍历行失败: %v", err)
		return errors.New("操作失败")
	}
	if err = streamWriter.Flush(); err != nil {
		log.Printf("导出 excel 失败: %v", err)
		return errors.New("操作失败")
	}
	if err = excel.Write(req.Writer); err != nil {
		log.Printf("写入输出流失败: %v", err)
		return errors.New("操作失败")
	}
	log.Println("导出完成")
	return nil
}

// ExportTableByService 是包级便捷函数，供 Wails binding 直接调用。
func ExportTableByService(req *ExportRequest) error {
	return ensureDefaultExport().ExportTable(req)
}

// ExportBySQLByService 是包级便捷函数，供 Wails binding 直接调用。
func ExportBySQLByService(req *ExportBySQLRequest) error {
	return ensureDefaultExport().ExportBySQL(req)
}

// exportFilename 构造下载文件名，供 handler 和 binding 共用。
func exportFilename(prefix string) string {
	return fmt.Sprintf("%s-%s.xlsx", prefix, time.Now().Format(time.DateOnly))
}
