package treehandler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/strutil"

	"github.com/gin-gonic/gin"
)

func ShowTree(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	key := c.Query("key")
	curType := c.Query("type")
	level := c.Query("level")
	schema := c.Query("schema")
	authorization := appctx.Ctx.GetAuthorization(c)
	userPower := admin.GetUserPower(authorization)

	nextType := getNextType(curType)

	var data = make([]*conn.Tree, 0)
	switch nextType {
	case conn.TREE_NODE_TYPE_DIR:
		if !strings.EqualFold(curType, conn.TREE_NODE_TYPE_COLUMN) {
			if level == "0" {
				data = permission.FilterDirTreeWithPermission(key, userPower)
				data = append(data, permission.FilterConnsWithPermission("noneParent", userPower)...)
			} else {
				data = permission.FilterDirTreeWithPermission(key, userPower)
				if len(data) == 0 || data[0] == nil {
					data = permission.FilterConnsWithPermission(key, userPower)
				}
			}
		}
	case conn.TREE_NODE_TYPE_CONN:
		data = permission.FilterConnsWithPermission(key, userPower)
	case conn.TREE_NODE_TYPE_SCHEMA:
		data = permission.FilterSchemasWithPermission(connId, authorization)
	case conn.TREE_NODE_TYPE_TABLE:
		data = permission.FilterTablesWithPermission(connId, key, authorization)
	case conn.TREE_NODE_TYPE_COLUMN:
		data = dbops.ListColumns(connId, key, schema, authorization)
	case conn.TREE_NODE_TYPE_ALLCOLUMN:
		data = dbops.ListAllColumns(connId, key, authorization)
	}
	response.WriteOK(c, data)
}

func ListTableColumns(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)

	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)

	columns := dbops.ListTableColumns(param.ConnId, param.TableName, param.Schema, authorization)
	response.WriteOK(c, strutil.SnakeToCamel(columns))
}

func ListUserConnSchemasStream(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	userPower := admin.GetUserPower(authorization)

	type SchemaDTO struct {
		Name string `json:"name"`
	}

	type UserConnSchemaDTO struct {
		ConnId    string      `json:"connId"`
		Name      string      `json:"name"`
		DbSchema  *string     `json:"dbSchema"`
		DirName   *string     `json:"dirName"`
		DbType    string      `json:"dbType"`
		Schemas   []SchemaDTO `json:"schemas"`
		Available bool        `json:"available"`
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.id, c.name, c.db_schema, c.db_type, t.label as dir_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	admin.AppendPmsn(&sql, "c.id", &param, userPower)
	sql.WriteString(" order by t.label, c.name ")

	type rawRow struct {
		ConnId   string  `db:"id"`
		Name     string  `db:"name"`
		DbSchema *string `db:"db_schema"`
		DbType   string  `db:"db_type"`
		DirName  *string `db:"dir_name"`
	}
	rows := []rawRow{}
	err := getDB().Select(&rows, sql.String(), param...)
	if err != nil {
		return
	}

	if len(rows) == 0 {
		fmt.Fprintf(c.Writer, "event: done\ndata: \"empty\"\n\n")
		c.Writer.Flush()
		return
	}

	queryCtx, queryCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer queryCancel()

	ch := make(chan json.RawMessage, len(rows))

	for i, row := range rows {
		go func(idx int, r rawRow) {
			func() {
				defer func() {
					if rc := recover(); rc != nil {
						log.Printf("[ListUserConnSchemasStream] goroutine panic %s %s: %v", r.ConnId, r.Name, rc)
						data, _ := json.Marshal(UserConnSchemaDTO{
							ConnId:    r.ConnId,
							Name:      r.Name,
							DbSchema:  r.DbSchema,
							DirName:   r.DirName,
							DbType:    r.DbType,
							Schemas:   []SchemaDTO{},
							Available: false,
						})
						ch <- json.RawMessage(data)
					}
				}()

				schemas := []SchemaDTO{}
				schemaTrees := dbops.ListSchema(r.ConnId, authorization)
				for _, st := range schemaTrees {
					schemas = append(schemas, SchemaDTO{Name: st.Label})
				}

				data, _ := json.Marshal(UserConnSchemaDTO{
					ConnId:    r.ConnId,
					Name:      r.Name,
					DbSchema:  r.DbSchema,
					DirName:   r.DirName,
					DbType:    r.DbType,
					Schemas:   schemas,
					Available: true,
				})
				ch <- json.RawMessage(data)
			}()
		}(i, row)
	}

	for i := 0; i < len(rows); i++ {
		select {
		case data := <-ch:
			fmt.Fprintf(c.Writer, "event: schema\ndata: %s\n\n", string(data))
			c.Writer.Flush()
		case <-queryCtx.Done():
			log.Printf("[ListUserConnSchemasStream] 查询超时")
			fmt.Fprintf(c.Writer, "event: schema\ndata: {\"err\":\"timeout\"}\n\n")
			c.Writer.Flush()
		}
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: \"ok\"\n\n")
	c.Writer.Flush()
}

type SchemaRef struct {
	ConnId string `json:"connId"`
	Schema string `json:"schema"`
}

type tableNameDTO struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
	Schema  string `json:"schema,omitempty"`
}

func ListTableNames(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	schemasJSON := c.Query("schemas")

	if schemasJSON != "" {
		var refs []SchemaRef
		if err := json.Unmarshal([]byte(schemasJSON), &refs); err != nil || len(refs) == 0 {
			response.WriteOK(c, []any{})
			return
		}
		userPower := admin.GetUserPower(authorization)
		seen := make(map[string]bool)
		result := []tableNameDTO{}
		for _, ref := range refs {
			if ref.ConnId == "" || ref.Schema == "" {
				continue
			}
			key := ref.ConnId + "::" + ref.Schema
			if seen[key] {
				continue
			}
			seen[key] = true
			tables := dbops.QueryTableInfo(ref.ConnId, ref.Schema, authorization)
			filteredTables := conn.FilterTablesByPermission(tables, ref.ConnId, ref.Schema, userPower)
			for _, table := range filteredTables {
				result = append(result, tableNameDTO{
					Name:    table.Name,
					Comment: table.Comment,
					Schema:  ref.Schema,
				})
			}
		}
		response.WriteOK(c, result)
		return
	}

	if connId == "" {
		response.WriteOK(c, []any{})
		return
	}

	if schema == "" {
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

	userPower := admin.GetUserPower(authorization)
	tables := dbops.QueryTableInfo(connId, schema, authorization)
	filteredTables := conn.FilterTablesByPermission(tables, connId, schema, userPower)

	result := make([]tableNameDTO, len(filteredTables))
	for i, table := range filteredTables {
		result[i] = tableNameDTO{Name: table.Name, Comment: table.Comment}
	}

	response.WriteOK(c, result)
}

func getNextType(curType string) string {
	t := conn.TREE_NODE_TYPE_DIR
	switch curType {
	case conn.TREE_NODE_TYPE_DIR:
		t = conn.TREE_NODE_TYPE_DIR
	case conn.TREE_NODE_TYPE_CONN:
		t = conn.TREE_NODE_TYPE_SCHEMA
	case conn.TREE_NODE_TYPE_SCHEMA:
		t = conn.TREE_NODE_TYPE_TABLE
	case conn.TREE_NODE_TYPE_TABLE:
		t = conn.TREE_NODE_TYPE_COLUMN
	case conn.TREE_NODE_TYPE_ALLCOLUMN:
		t = conn.TREE_NODE_TYPE_ALLCOLUMN
	}
	return t
}