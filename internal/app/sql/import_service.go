package sql

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"

	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/pkg/sanitize"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

// ImportRequest 是从 XLSX 导入数据的入参。
// Reader 由调用方注入:
//   - HTTP 模式: 传 multipart.File (来自 c.FormFile)
//   - 桌面模式: 传 *os.File (用户在桌面端选择的临时文件)
type ImportRequest struct {
	ConnID        string
	Schema        string
	Table         string
	OperType      string // "insert" 或 "update" (其他值视为 update)
	Mapping       string // JSON 字符串，列映射；为空表示按表头顺序
	StartRow      string // 字符串数字，从第几行开始 (1-based)
	Filename      string // 用于校验文件名后缀与表名匹配 (导入时)
	Authorization string
	Reader        io.Reader
}

// ImportService 封装 XLSX 导入业务逻辑，与 gin.Context 解耦。
type ImportService struct{}

var (
	defaultImportService *ImportService
	defaultImportOnce    sync.Once
)

func ensureDefaultImport() *ImportService {
	defaultImportOnce.Do(func() {
		defaultImportService = &ImportService{}
	})
	return defaultImportService
}

// ImportXlsx 从 Excel 导入数据到指定表。
// 业务逻辑来自原 import.go 的 ImportXlsx handler。
// 返回值 result 为 "导入完成" 或 "更新完成"。
func (s *ImportService) ImportXlsx(req *ImportRequest) (string, error) {
	authorization := req.Authorization
	connId := req.ConnID
	schema := req.Schema
	table := req.Table
	operType := req.OperType
	mappingStr := req.Mapping
	startRowStr := req.StartRow

	log.Println("收到新增/更新请求，正在准备导入: " + table)

	permission.CheckTableWritePermission(connId, schema, table, nil, authorization)

	if mappingStr == "" && req.Filename != "" {
		// 原逻辑：文件名形如 "xxx-table.xlsx"，从最后一个 "-" 后到 ".xlsx" 前的子串必须等于 table
		fname := req.Filename
		lastDash := strings.LastIndex(fname, "-")
		if lastDash >= 0 && strings.HasSuffix(fname, ".xlsx") {
			extracted := fname[lastDash+1 : len(fname)-5]
			if extracted != table {
				return "", errors.New("表名不匹配（文件名中横线后为表名，如用户列数据导出: t_user.xlsx）")
			}
		}
	}

	excel, err := excelize.OpenReader(req.Reader)
	if err != nil {
		log.Printf("[ImportXlsx] 读取 Excel 文件失败 - err=%v\n", err)
		return "", errors.New("读取 Excel 文件失败，请检查文件格式")
	}
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	dbConn := conn.GetConn(connId, authorization)
	if dbConn == nil {
		return "", errors.New("数据库连接不可用")
	}
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("[ImportXlsx] 数据库连接失败 - err=%v\n", err)
		return "", errors.New("数据库连接失败，请检查配置")
	}
	defer tx.Rollback()

	rows, err := excel.Rows("Sheet1")
	if err != nil {
		log.Printf("[ImportXlsx] 读取工作表失败 - err=%v\n", err)
		return "", errors.New("读取工作表失败，请检查文件内容")
	}
	defer rows.Close()

	header := make([]string, 0)
	if rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取表头失败 - err=%v\n", err)
			return "", errors.New("读取表头失败，请检查文件内容")
		}
		header = append(header, row...)
	}

	var columnMapping map[string]string
	if mappingStr != "" {
		err = json.Unmarshal([]byte(mappingStr), &columnMapping)
		if err != nil {
			log.Printf("[ImportXlsx] 解析列映射失败 - err=%v\n", err)
			return "", errors.New("解析列映射失败，请检查参数格式")
		}

		if len(columnMapping) > 0 {
			mappedDbCols := make([]string, 0, len(columnMapping))
			for _, dbCol := range columnMapping {
				mappedDbCols = append(mappedDbCols, dbCol)
			}
			permission.CheckTableWritePermission(connId, schema, table, mappedDbCols, authorization)
		}
	} else {
		if len(header) > 0 {
			permission.CheckTableWritePermission(connId, schema, table, header, authorization)
		}
	}

	startRow := 1
	if startRowStr != "" {
		parsedRow, err := strconv.Atoi(startRowStr)
		if err != nil {
			startRow = 1
		} else {
			startRow = parsedRow
		}
	}

	for i := 0; i < startRow-1; i++ {
		if !rows.Next() {
			break
		}
	}

	type mappedCol struct {
		dbCol     string
		headerIdx int
	}
	var mappingOrder []mappedCol
	var columns []string
	if len(columnMapping) > 0 {
		for idx, h := range header {
			if dbCol, ok := columnMapping[h]; ok {
				mappingOrder = append(mappingOrder, mappedCol{dbCol: dbCol, headerIdx: idx})
			}
		}
		if len(mappingOrder) == 0 {
			return "", errors.New("列映射与 Excel 表头不匹配")
		}
		columns = make([]string, 0, len(mappingOrder))
		for _, m := range mappingOrder {
			columns = append(columns, m.dbCol)
		}
	}

	count := -1
	maxLines := 100
	totalValues := make([][]string, maxLines)

	for rows.Next() {
		count++
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取行数据失败 - err=%v\n", err)
			return "", errors.New("读取行数据失败，请检查文件内容")
		}

		if len(mappingOrder) > 0 {
			mappedValues := make([]string, 0, len(mappingOrder))
			for _, m := range mappingOrder {
				if m.headerIdx < len(row) {
					mappedValues = append(mappedValues, row[m.headerIdx])
				} else {
					mappedValues = append(mappedValues, "")
				}
			}
			totalValues[count] = mappedValues
		} else {
			columns = row
			totalValues[count] = row
		}

		if count+1 >= maxLines {
			if strings.EqualFold(operType, "insert") {
				if err := insertToDb(schema, table, columns, totalValues, tx); err != nil {
					log.Printf("[ImportXlsx] 插入数据失败 - err=%v\n", err)
					return "", errors.New("插入数据失败，请检查数据格式")
				}
			} else {
				if err := UpdateToDb(schema, table, columns, totalValues, tx); err != nil {
					log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
					return "", errors.New("更新数据失败，请检查数据格式")
				}
			}
			count = -1
		}
	}

	if count != -1 {
		if strings.EqualFold(operType, "insert") {
			if err := insertToDb(schema, table, columns, totalValues[:count+1], tx); err != nil {
				log.Printf("[ImportXlsx] 插入数据失败 - err=%v\n", err)
				return "", errors.New("插入数据失败，请检查数据格式")
			}
		} else {
			if err := UpdateToDb(schema, table, columns, totalValues[:count+1], tx); err != nil {
				log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
				return "", errors.New("更新数据失败，请检查数据格式")
			}
		}
	}

	if err = tx.Commit(); err != nil {
		log.Printf("提交事务失败: %v", err)
		return "", errors.New("提交事务失败，请重试")
	}

	if strings.EqualFold(operType, "insert") {
		log.Println("导入完成")
		return "导入完成", nil
	}
	log.Println("更新完成")
	return "更新完成", nil
}

// ImportXlsxByService 是包级便捷函数，供 Wails binding 直接调用。
func ImportXlsxByService(req *ImportRequest) (string, error) {
	return ensureDefaultImport().ImportXlsx(req)
}

// 以下是原 import.go 中的辅助函数，与 sqlx.Tx 直接交互，不依赖 gin.Context，
// 因此保留为包内私有，service 和 handler 共用。

func insertToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) error {
	if len(data) == 0 {
		return nil
	}

	if !sanitize.IsValidIdentifier(schema) {
		return errors.New("非法的 schema 名")
	}
	if !sanitize.IsValidIdentifier(table) {
		return errors.New("非法的表名")
	}
	for _, col := range columns {
		if !sanitize.IsValidIdentifier(col) {
			return fmt.Errorf("非法的列名: %q", col)
		}
	}

	colNum := len(columns)
	sql := bytes.Buffer{}

	sql.WriteString("insert into ")
	sql.WriteString(schema)
	sql.WriteString(".")
	sql.WriteString(table)
	sql.WriteString(" (")
	sql.WriteString(strings.Join(columns, ","))
	sql.WriteString(") values (")
	if tx.DriverName() == "oracle" {
		plc := make([]string, colNum)
		for idx := range colNum {
			plc[idx] = ":" + fmt.Sprint(idx+1)
		}
		sql.WriteString(strings.Join(plc, ","))
	} else {
		plc := strings.Repeat("?,", colNum)
		sql.Write([]byte(plc[:len(plc)-1]))
	}
	sql.WriteString(" )")

	log.Println(sql.String())

	stmt, err := tx.Tx.Prepare(sql.String())
	if err != nil {
		log.Printf("[ImportXlsx] 准备插入语句失败 - err=%v\n", err)
		return errors.New("准备 SQL 语句失败")
	}

	colTypeMap := database.QueryColType(schema, table, tx)
	driverName := tx.DriverName()

	for _, val := range data {
		anyVal := make([]any, colNum)
		for i := range val {
			if i+1 > colNum {
				return errors.New("excel 中字段数量超出了表字段数量")
			}
			colType := colTypeMap[columns[i]]
			anyVal[i] = *database.ParseVal(&driverName, &colType, &val[i])
		}
		_, err = stmt.Exec(anyVal...)
		if err != nil {
			log.Printf("[ImportXlsx] 执行插入失败 - err=%v\n", err)
			return errors.New("执行插入失败")
		}
	}

	return nil
}

func UpdateToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) error {
	if len(data) == 0 {
		return nil
	}

	if !sanitize.IsValidIdentifier(schema) {
		return errors.New("非法的 schema 名")
	}
	if !sanitize.IsValidIdentifier(table) {
		return errors.New("非法的表名")
	}
	for _, col := range columns {
		if !sanitize.IsValidIdentifier(col) {
			return fmt.Errorf("非法的列名: %q", col)
		}
	}

	keys, err := dbops.QueryPrimaryKey(schema, table, tx)
	if err != nil {
		log.Printf("[ImportXlsx] 查询主键失败 - err=%v\n", err)
		return errors.New("查询主键失败")
	}

	keyIdx := database.KeyIdx(keys, columns)

	sql := bytes.Buffer{}
	where := bytes.Buffer{}
	where.WriteString(" where ")

	sql.WriteString("update ")
	sql.WriteString(schema + "." + table)
	sql.WriteString(" set ")

	for i, val := range columns {
		if !slices.Contains(keyIdx, i) {
			sql.WriteString(val)
			sql.WriteString(" = ?,")
		} else {
			where.WriteString(val)
			where.WriteString(" = ? and ")
		}
	}

	realSql := strings.TrimRight(sql.String(), ",") + strings.TrimRight(where.String(), " and ")

	log.Println(realSql)

	stmt, err := tx.Tx.Prepare(realSql)
	if err != nil {
		log.Printf("[ImportXlsx] 准备更新语句失败 - err=%v\n", err)
		return errors.New("准备 SQL 语句失败")
	}

	valCount := -1
	paramCount := -1

	colTypeMap := database.QueryColType(schema, table, tx)

	colNum := len(columns)
	driverName := tx.DriverName()
	anyVal := make([]any, colNum)
	for _, val := range data {
		for i := range val {
			if i+1 > colNum {
				return errors.New("excel 中字段数量超出了表字段数量")
			}
			colType := colTypeMap[columns[i]]
			if !slices.Contains(keyIdx, i) {
				valCount++
				anyVal[valCount] = *database.ParseVal(&driverName, &colType, &val[i])
			} else {
				paramCount++
				anyVal[colNum-len(keys)+paramCount] = *database.ParseVal(&driverName, &colType, &val[i])
			}
		}

		valCount = -1
		paramCount = -1
		log.Println(anyVal...)
		_, err = stmt.Exec(anyVal...)
		if err != nil {
			log.Printf("[ImportXlsx] 执行更新失败 - err=%v\n", err)
			return errors.New("执行更新失败")
		}
	}

	return nil
}
