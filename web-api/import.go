package webapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
	"golang.org/x/exp/slices"
)

func ImportXlsx(c *gin.Context) {

	authorization := c.GetHeader("Authorization")

	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	table := c.PostForm("table")
	operType := c.PostForm("optType")
	mappingStr := c.PostForm("mapping")
	startRowStr := c.PostForm("startRow")

	log.Println("收到新增/更新请求，正在准备导入" + table)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[ImportXlsx] 文件上传失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, "文件上传失败，请检查文件是否正确选择")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("[ImportXlsx] 打开文件失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, "打开文件失败，请重试")
		return
	}
	defer file.Close()

	// 如果没有 mapping 参数，使用旧的文件名匹配逻辑
	if mappingStr == "" {
		if fileHeader.Filename[strings.LastIndex(fileHeader.Filename, "-")+1:len(fileHeader.Filename)-5] != table {
			c.JSON(http.StatusBadRequest, "表名不匹配（文件名中横线后为表名，如用户列数据导入-t_user.xlsx）")
			return
		}
	}

	excel, err := excelize.OpenReader(file)
	if err != nil {
		log.Printf("[ImportXlsx] 读取 Excel 文件失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, "读取 Excel 文件失败，请检查文件格式")
		return
	}
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	tx, err := admin.GetConn(connId, authorization).Beginx()
	if err != nil {
		log.Printf("[ImportXlsx] 数据库连接失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, "数据库连接失败，请检查配置")
		return
	}
	defer tx.Rollback()

	rows, err := excel.Rows("Sheet1")
	if err != nil {
		log.Printf("[ImportXlsx] 读取工作表失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, "读取工作表失败，请检查文件内容")
		return
	}
	defer rows.Close()

	header := make([]string, 0)
	if rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取表头失败 - err=%v\n", err)
			c.JSON(http.StatusBadRequest, "读取表头失败，请检查文件内容")
			return
		}
		header = append(header, row...)
	}

	var columnMapping map[string]string
	if mappingStr != "" {
		err = json.Unmarshal([]byte(mappingStr), &columnMapping)
		if err != nil {
			log.Printf("[ImportXlsx] 解析列映射失败 - err=%v\n", err)
			c.JSON(http.StatusBadRequest, "解析列映射失败，请检查参数格式")
			return
		}
	}

	// 解析起始行（前端传入的是从 1 开始的行号，需要转换为从 0 开始的索引）
	startRow := 1 // 默认从第 1 行数据开始（跳过表头）
	if startRowStr != "" {
		parsedRow, err := strconv.Atoi(startRowStr)
		if err != nil {
			startRow = 1
		} else {
			// 前端传入的 startRow 是数据起始行号（从 1 开始）
			// 我们需要跳过表头（第 1 行）和前面的数据行
			// 例如：startRow=1 表示从第 2 行开始（跳过表头），startRow=2 表示从第 3 行开始
			startRow = parsedRow
		}
	}

	// 跳过表头（第 1 行）和前面的数据行
	// 表头已经在上面读取了，现在需要跳过 startRow-1 行数据
	for i := 0; i < startRow-1; i++ {
		if !rows.Next() {
			break
		}
	}

	count := -1
	maxLines := 100

	//存所有行的内容 totalValues
	totalValues := make([][]string, maxLines)
	var columns []string

	for rows.Next() {
		count++
		row, err := rows.Columns()
		if err != nil {
			log.Printf("[ImportXlsx] 读取行数据失败 - err=%v\n", err)
			c.JSON(http.StatusBadRequest, "读取行数据失败，请检查文件内容")
			return
		}

		// 如果有 mapping，进行列映射
		if len(columnMapping) > 0 {
			// 构建两个数组：数据库列名和对应的值
			mappedColumns := make([]string, 0, len(columnMapping))
			mappedValues := make([]string, 0, len(columnMapping))

			// 按照 mapping 的顺序处理，保持列顺序一致
			// 只处理已成功映射的字段（即 mapping 中存在的字段）
			for excelCol, dbCol := range columnMapping {
				// 找到 Excel 列在 header 中的索引
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

			// 只有当有映射的列时才处理
			if len(mappedColumns) > 0 {
				columns = mappedColumns
				row = mappedValues
			} else {
				// 如果没有映射任何列，跳过此行
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
				c.JSON(http.StatusInternalServerError, "插入数据失败，请检查数据格式")
				return
			}
		} else {
				if err := UpdateToDb(schema, table, columns, totalValues, tx); err != nil {
				log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
				c.JSON(http.StatusInternalServerError, "更新数据失败，请检查数据格式")
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
			c.JSON(http.StatusInternalServerError, "插入数据失败，请检查数据格式")
			return
		}
	} else {
			if err := UpdateToDb(schema, table, columns, totalValues[:count+1], tx); err != nil {
			log.Printf("[ImportXlsx] 更新数据失败 - err=%v\n", err)
			c.JSON(http.StatusInternalServerError, "更新数据失败，请检查数据格式")
			return
		}
		}
	}

	if err = tx.Commit(); err != nil {
		log.Printf("提交事务失败：%v", err)
		c.JSON(http.StatusInternalServerError, "提交事务失败，请重试")
		return
	} else {
		if strings.EqualFold(operType, "insert") {
			log.Println("导入完成")
			c.JSON(http.StatusOK, "导入完成")
		} else {
			log.Println("更新完成")
			c.JSON(http.StatusOK, "更新完成")
		}
	}
}

func insertToDb(schema, table string, columns []string, data [][]string, tx *sqlx.Tx) error {

	if len(data) == 0 {
		return nil
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

	colTypeMap := admin.QueryColType(schema, table, tx)
	driverName := tx.DriverName()

	for _, val := range data {
		anyVal := make([]any, colNum)
		for i := range val {
			if i+1 > colNum {
				return errors.New("excel 中字段数量超出了表字段数量")
			}
			colType := colTypeMap[columns[i]]
			anyVal[i] = *admin.ParseVal(&driverName, &colType, &val[i])
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

	keys, err := admin.QueryPrimaryKey(schema, table, tx)
	if err != nil {
		log.Printf("[ImportXlsx] 查询主键失败 - err=%v\n", err)
		return errors.New("查询主键失败")
	}

	keyIdx := dbutils.KeyIdx(keys, columns)

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

	colTypeMap := admin.QueryColType(schema, table, tx)

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
				anyVal[valCount] = *admin.ParseVal(&driverName, &colType, &val[i])
			} else {
				paramCount++
				anyVal[colNum-len(keys)+paramCount] = *admin.ParseVal(&driverName, &colType, &val[i])
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
