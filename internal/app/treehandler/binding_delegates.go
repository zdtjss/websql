package treehandler

// 本文件集中存放供 Wails binding 直接调用的包级委托函数。
// 命名采用 <Method>ByService 后缀，与 admin/system 等模块保持一致。
// 这些函数与对应 HTTP handler 共用同一份业务逻辑，区别仅在于：
//   - HTTP handler 通过 *gin.Context + c.Writer 写入 SSE 流
//   - ByService 函数通过 emit 回调推送结构体，由调用方决定如何序列化

import (
	"bytes"
	"context"
	"log"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/dbops"
)

// SchemaDTO 单个 schema 摘要，与 HTTP 模式响应字段保持一致。
type SchemaDTO struct {
	Name string `json:"name"`
}

// UserConnSchemaDTO 单个连接的 schema 列表项。
// 与 HTTP 模式 ListUserConnSchemasStream 推送的 JSON 结构对齐。
type UserConnSchemaDTO struct {
	ConnId    string      `json:"connId"`
	Name      string      `json:"name"`
	DbSchema  *string     `json:"dbSchema"`
	DirName   *string     `json:"dirName"`
	DbType    string      `json:"dbType"`
	Schemas   []SchemaDTO `json:"schemas"`
	Available bool        `json:"available"`
	Err       string      `json:"err,omitempty"`
}

// ListUserConnSchemasStreamByService 流式返回用户连接的 schema 列表。
//
// 业务逻辑来自 tree.go 的 ListUserConnSchemasStream handler：
//   - 查询 t_conn 表，按权限过滤
//   - 对每个 conn 并发查询 schema 列表（dbops.ListSchema）
//   - 每个 conn 完成后通过 emit 推送一条 UserConnSchemaDTO
//   - 全部完成后返回 "ok" 或 "empty"（无数据时）
//
// emit 不感知 SSE 协议，调用方（HTTP handler 或 Wails binding）自行决定序列化方式。
// 单个 conn 查询超时不会中断其他 conn，超时项的 Available=false、Schemas 为空数组。
//
// 返回值: "ok" 表示正常完成，"empty" 表示无连接数据。
func ListUserConnSchemasStreamByService(ctx context.Context, authorization string, emit func(UserConnSchemaDTO)) string {
	userPower := admin.GetUserPower(authorization)

	param := []any{}
	sqlBuf := bytes.Buffer{}
	sqlBuf.WriteString("select c.id, c.name, c.db_schema, c.db_type, t.label as dir_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	admin.AppendPmsn(&sqlBuf, "c.id", &param, userPower)
	sqlBuf.WriteString(" order by t.label, c.name ")

	type rawRow struct {
		ConnId   string  `db:"id"`
		Name     string  `db:"name"`
		DbSchema *string `db:"db_schema"`
		DbType   string  `db:"db_type"`
		DirName  *string `db:"dir_name"`
	}
	rows := []rawRow{}
	if err := getDB().Select(&rows, sqlBuf.String(), param...); err != nil {
		return "empty"
	}

	if len(rows) == 0 {
		return "empty"
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 60*time.Second)
	defer queryCancel()

	ch := make(chan UserConnSchemaDTO, len(rows))

	for i := range rows {
		go func(r rawRow) {
			defer func() {
				if rc := recover(); rc != nil {
					log.Printf("[ListUserConnSchemasStream] goroutine panic %s %s: %v", r.ConnId, r.Name, rc)
					ch <- UserConnSchemaDTO{
						ConnId:    r.ConnId,
						Name:      r.Name,
						DbSchema:  r.DbSchema,
						DirName:   r.DirName,
						DbType:    r.DbType,
						Schemas:   []SchemaDTO{},
						Available: false,
					}
				}
			}()

			schemas := []SchemaDTO{}
			schemaTrees := dbops.ListSchema(r.ConnId, authorization)
			for _, st := range schemaTrees {
				schemas = append(schemas, SchemaDTO{Name: st.Label})
			}

			ch <- UserConnSchemaDTO{
				ConnId:    r.ConnId,
				Name:      r.Name,
				DbSchema:  r.DbSchema,
				DirName:   r.DirName,
				DbType:    r.DbType,
				Schemas:   schemas,
				Available: true,
			}
		}(rows[i])
	}

	// 通过 channel 同步等待所有 goroutine 完成（与 HTTP 模式一致）
	for i := 0; i < len(rows); i++ {
		select {
		case dto := <-ch:
			emit(dto)
		case <-queryCtx.Done():
			log.Printf("[ListUserConnSchemasStream] 查询超时")
			emit(UserConnSchemaDTO{Err: "timeout"})
			// 超时后继续 drain channel，避免 goroutine 泄漏
			for ; i < len(rows); i++ {
				select {
				case dto := <-ch:
					emit(dto)
				default:
				}
			}
			return "ok"
		}
	}
	return "ok"
}
