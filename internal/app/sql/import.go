package sql

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

func ImportXlsx(c *gin.Context) {

	authorization := appctx.Ctx.GetAuthorization(c)

	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	table := c.PostForm("table")
	operType := c.PostForm("optType")
	mappingStr := c.PostForm("mapping")
	startRowStr := c.PostForm("startRow")

	log.Println("收到新增/更新请求，正在准备导入: " + table)

	permission.CheckTableWritePermission(connId, schema, table, nil, authorization)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[ImportXlsx] 文件上传失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "文件上传失败，请检查文件是否正确选择")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("[ImportXlsx] 打开文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "打开文件失败，请重试")
		return
	}
	defer file.Close()

	if mappingStr == "" {
		if fileHeader.Filename[strings.LastIndex(fileHeader.Filename, "-")+1:len(fileHeader.Filename)-5] != table {
			response.WriteErr(c, http.StatusBadRequest, 500, "表名不匹配（文件名中横线后为表名，如用户列数据导出: t_user.xlsx）")
			return
		}
	}

	excel, err := excelize.OpenReader(file)
	if err != nil {
		log.Printf("[ImportXlsx] 读取 Excel 文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "读取 Excel 文件失败，请检查文件格式")
		return
	}
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	tx, err := conn.GetConn(connId, authorization).Beginx()
	if err != nil {
		log.Printf("[ImportXlsx] 数据库连接失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "数据库连接失败，请检查配置")
		return
	}
	defer tx.Rollback()

	rows, err := excel.Rows("Sheet1")
	if err != nil {
		log.Printf("[ImportXlsx] 读取工作表失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "读取工作表失败，请检查文件内容")
		return
	}
	defer rows.Close()

	header := make([]string, 0)
	if rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取表头失败 - err=%v\n", err)
			response.WriteErr(c, http.StatusBadRequest, 500, "读取表头失败，请检查文件内容")
			return
		}
		header = append(header, row...)
	}

	var columnMapping map[string]string
	if mappingStr != "" {
		err = json.Unmarshal([]byte(mappingStr), &columnMapping)
		if err != nil {
			log.Printf("[ImportXlsx] 解析列映射失败 - err=%v\n", err)
			response.WriteErr(c, http.StatusBadRequest, 500, "解析列映射失败，请检查参数格式")
			return
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

	count := -1
	maxLines := 100

	totalValues := make([][]string, maxLines)
	var columns []string

	for rows.Next() {
		count++
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取行数据失败 - err=%v\n", err)
			response.WriteErr(c, http.StatusBadRequest, 500, "读取行数据失败，请检查文件内容")
			return
		}

		if len(columnMapping) > 0 {
			mappedColumns := make([]string, 0, len(columnMapping))
			mappedValues := make([]string, 0, len(columnMapping))

			for excelCol, dbCol := range columnMapping {
				for idx, h := range header {
					if h == excelCol {
						mappedColumns = append(mappedColumns, dbCol)
						if idx < len(row) {
							mappedValues = append(mappedValues, row[idx])
						} else {
							mappedValues = append(mappedValues, "")
						}
						break
					}
				}
			}

			if len(mappedColumns) > 0 {
				columns = mappedColumns
				row = mappedValues
			} else {
				continue
			}
		} else {
			columns = row
		}

		totalValues[count] = row
		if count+1 >= maxLines {
			if strings.EqualFold(operType, "insert") {
				if err := insertToDb(schema, table, columns, totalValues, tx); err != nil {
					log.Printf("[ImportXlsx] 插入数据失败 - err=%v\n", err)
					response.WriteErr(c, http.StatusInternalServerError, 500, "插入数据失败，请检查数据格式")
					return
				}
			} else {
				if err := UpdateToDb(schema, table, columns, totalValues, tx); err != nil {
					log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
					response.WriteErr(c, http.StatusInternalServerError, 500, "更新数据失败，请检查数据格式")
					return
				}
			}
			count = -1
		}
	}

	if count != -1 {
		if strings.EqualFold(operType, "insert") {
			if err := insertToDb(schema, table, columns, totalValues[:count+1], tx); err != nil {
				log.Printf("[ImportXlsx] 插入数据失败 - err=%v\n", err)
				response.WriteErr(c, http.StatusInternalServerError, 500, "插入数据失败，请检查数据格式")
				return
			}
		} else {
			if err := UpdateToDb(schema, table, columns, totalValues[:count+1], tx); err != nil {
				log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
				response.WriteErr(c, http.StatusInternalServerError, 500, "更新数据失败，请检查数据格式")
				return
			}
		}
	}

	if err = tx.Commit(); err != nil {
		log.Printf("提交事务失败: %v", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "提交事务失败，请重试")
		return
	} else {
		if strings.EqualFold(operType, "insert") {
			log.Println("导入完成")
			response.WriteOK(c, "导入完成")
		} else {
			log.Println("更新完成")
			response.WriteOK(c, "更新完成")
		}
	}
}

func insertToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) error {

	if len(data) == 0 {
		return nil
	}

	// 标识符白名单校验，防止 SQL 注入
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

	// 标识符白名单校验，防止 SQL 注入
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