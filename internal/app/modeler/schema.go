// Package modeler: schema.go — ER 图统一元数据接口
//
// 设计目标（见 docs/技术方案-ER图绘制与交互优化-确认报告.md 第十章）：
//   - 方言差异、权限过滤、批量取数收敛到服务端，前端不再拼接 information_schema SQL；
//   - 权限口径与侧栏树一致：表级过滤（经典模式下列级权限按表级处理）；
//   - 一次 HTTP 请求返回表、字段、唯一键组、外键关系；
//   - 大 schema 可按表分页，relations 全量返回，nextCursor 为上一页最后一张表名。
//
// 只读接口：不写库、不改结构、不落审计，仅保留访问日志。
package modeler

import (
	"errors"
	"log"
	"sort"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// ErrUnsupportedDialect 表示当前数据库类型暂不支持 ER 元数据加载。
var ErrUnsupportedDialect = errors.New("暂不支持该数据库类型")

const (
	// maxERTableLimit 单页最多返回的表数量，避免单次响应体过大。
	maxERTableLimit = 500
)

// ERSchemaRequest POST /api/er/schema 请求体。
// limit<=0 表示不分页返回全部可见表（前端默认行为）。
type ERSchemaRequest struct {
	ConnID string `json:"connId"`
	Schema string `json:"schema"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// ERSchemaResponse POST /api/er/schema 响应体。
type ERSchemaResponse struct {
	DbType           string             `json:"dbType"`
	Schema           string             `json:"schema"`
	Tables           []ERSchemaTable    `json:"tables"`
	Relations        []ERSchemaRelation `json:"relations"`
	TotalTables      int                `json:"totalTables"`
	LoadedTableCount int                `json:"loadedTableCount"`
	NextCursor       string             `json:"nextCursor"`
}

// ERSchemaTable ER 图中的一张表（含视图）。
type ERSchemaTable struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Comment    string           `json:"comment"`
	Columns    []ERSchemaColumn `json:"columns"`
	UniqueKeys [][]string       `json:"uniqueKeys"`
}

// ERSchemaColumn 表字段。唯一性以 UniqueKeys 为唯一事实源，这里只保留展示所需信息。
type ERSchemaColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Comment    string `json:"comment"`
	PrimaryKey bool   `json:"primaryKey"`
}

// ERSchemaRelation 外键关系（一条边对应一组字段映射）。
// SQLite 没有约束名，使用 ConstraintID 表示 PRAGMA foreign_key_list 的 id，供前端成组。
type ERSchemaRelation struct {
	Source         string `json:"source"`
	SourceCol      string `json:"sourceCol"`
	Target         string `json:"target"`
	TargetCol      string `json:"targetCol"`
	ConstraintName string `json:"constraintName"`
	ConstraintID   string `json:"constraintId,omitempty"`
}

// ERSchemaHandler POST /api/er/schema
func ERSchemaHandler(c *gin.Context) {
	var req ERSchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.WriteErr(c, 200, 500, "参数解析失败："+err.Error())
		return
	}
	connID := strings.TrimSpace(req.ConnID)
	if connID == "" {
		connID = appctx.Ctx.GetConnID(c)
	}
	if connID == "" {
		response.WriteErr(c, 200, 500, "缺少连接 ID")
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db, connErr := conn.GetConnWithReason(connID, authorization)
	if connErr != nil {
		conn.WriteConnError(c, connErr)
		return
	}
	dbType := db.DriverName()
	schema := strings.TrimSpace(req.Schema)
	if schema == "" {
		schema = currentSchema(db, dbType)
	}

	tables, relations, err := loadERSchema(db, dbType, schema)
	if err != nil {
		if errors.Is(err, ErrUnsupportedDialect) {
			response.WriteErr(c, 200, 500, "暂不支持该数据库类型："+dbType)
			return
		}
		log.Printf("[ER-Schema] 加载失败 connId=%s schema=%s dbType=%s err=%v", connID, schema, dbType, err)
		response.WriteErr(c, 200, 500, "加载 ER 元数据失败："+err.Error())
		return
	}

	// 表级权限过滤：与侧栏树口径一致。经典模式下列级权限降级为表级（仅判断表是否可见）。
	visible := filterVisibleTables(tables, connID, schema, authorization)
	relations = filterRelationsByVisibility(relations, visible)

	// 按表名排序，保证游标分页稳定。
	sort.SliceStable(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })

	limit := req.Limit
	if limit > maxERTableLimit {
		limit = maxERTableLimit
	}
	if limit < 0 {
		limit = 0
	}
	page, nextCursor, loadedCount := paginateTables(visible, req.Cursor, limit)

	response.WriteOK(c, ERSchemaResponse{
		DbType:           dbType,
		Schema:           schema,
		Tables:           page,
		Relations:        relations,
		TotalTables:      len(visible),
		LoadedTableCount: loadedCount,
		NextCursor:       nextCursor,
	})
}

// filterVisibleTables 按表级权限过滤，返回保序的表列表。
func filterVisibleTables(tables []ERSchemaTable, connID, schema, authorization string) []ERSchemaTable {
	userPower := admin.GetUserPower(authorization)
	candidates := make([]*conn.Table, 0, len(tables))
	for _, t := range tables {
		candidates = append(candidates, &conn.Table{Name: t.Name})
	}
	allowed := conn.FilterTablesByPermission(candidates, connID, schema, userPower)
	allowedNames := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		allowedNames[t.Name] = true
	}

	visible := make([]ERSchemaTable, 0, len(allowedNames))
	for _, t := range tables {
		if allowedNames[t.Name] {
			visible = append(visible, t)
		}
	}
	return visible
}

// filterRelationsByVisibility 丢弃任意一端不可见的表关系，避免泄露不可见表的约束名。
func filterRelationsByVisibility(relations []ERSchemaRelation, visible []ERSchemaTable) []ERSchemaRelation {
	visibleNames := make(map[string]bool, len(visible))
	for _, t := range visible {
		visibleNames[t.Name] = true
	}
	out := make([]ERSchemaRelation, 0, len(relations))
	for _, r := range relations {
		if visibleNames[r.Source] && visibleNames[r.Target] {
			out = append(out, r)
		}
	}
	return out
}

// paginateTables 按表名游标分页。
// cursor 为上一页最后一张表名；limit<=0 表示不分页。
// loadedCount 为包含当前页在内的累计已加载表数量。
func paginateTables(tables []ERSchemaTable, cursor string, limit int) ([]ERSchemaTable, string, int) {
	if limit <= 0 || limit > len(tables) {
		limit = len(tables)
	}
	start := 0
	if cursor != "" {
		start = sort.Search(len(tables), func(i int) bool { return tables[i].Name > cursor })
	}
	if start > len(tables) {
		start = len(tables)
	}
	end := start + limit
	if end > len(tables) {
		end = len(tables)
	}
	page := tables[start:end]
	loadedCount := end
	nextCursor := ""
	if end < len(tables) && len(page) > 0 {
		nextCursor = page[len(page)-1].Name
	}
	return page, nextCursor, loadedCount
}

// currentSchema 解析当前 schema（与侧栏树逻辑保持一致）。
func currentSchema(db *sqlx.DB, dbType string) string {
	switch dbType {
	case "mysql", "mariadb":
		var s string
		if err := db.Get(&s, "SELECT DATABASE()"); err == nil {
			return s
		}
	case "oracle":
		var s string
		if err := db.Get(&s, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL"); err == nil {
			return s
		}
	case "sqlite":
		return "main"
	}
	return ""
}

func loadERSchema(db *sqlx.DB, dbType, schema string) ([]ERSchemaTable, []ERSchemaRelation, error) {
	switch dbType {
	case "mysql", "mariadb":
		return loadMySQLSchema(db, schema)
	case "sqlite":
		return loadSQLiteSchema(db)
	case "oracle":
		return loadOracleSchema(db)
	default:
		return nil, nil, ErrUnsupportedDialect
	}
}

// appendUniqueKey 追加唯一键组并去重（列顺序敏感：索引列顺序不同视为不同键）。
func appendUniqueKey(dst *[][]string, key []string) {
	if len(key) == 0 {
		return
	}
	normalized := make([]string, 0, len(key))
	for _, c := range key {
		c = strings.TrimSpace(c)
		if c == "" {
			return // 函数索引/表达式索引等无法确定列，整组忽略
		}
		normalized = append(normalized, c)
	}
	sig := strings.Join(normalized, "\x00")
	for _, existing := range *dst {
		if strings.Join(existing, "\x00") == sig {
			return
		}
	}
	*dst = append(*dst, normalized)
}
