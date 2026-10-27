package sql

import (
	"strconv"
	"strings"

	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/dialect"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/apperror"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sqlguard"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// 结果集分页查询。
//
// 背景（缺陷 D7）：/execSQL 一次返回全部结果并序列化为 map 数组，
// 大结果集既占内存也占带宽；同时前端「行数上限」默认仅 15 行，
// 用户无法判断数据是不是被截断，也没有「继续加载」的手段。
//
// 本接口按页返回列式结果（columns + rows），前端虚拟列表滚动到底部时增量拉取，
// 既不破坏旧 /execSQL 的契约，也让大结果集有可用的加载路径。

const (
	// defaultPageSize 单页默认行数
	defaultPageSize = 200
	// maxPageSize 单页上限，防止一次拉取过大
	maxPageSize = 2000
)

// PageResult 分页结果（列式，避免每行重复列名）。
type PageResult struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Offset  int      `json:"offset"`
	Limit   int      `json:"limit"`
	HasMore bool     `json:"hasMore"`
	CanPage bool     `json:"canPage"`
}

// PageQuery POST /api/query/page
func PageQuery(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	sqlStr := strings.TrimSpace(c.PostForm("sql"))
	schema := c.PostForm("schema")
	offset, _ := strconv.Atoi(c.PostForm("offset"))
	limit, _ := strconv.Atoi(c.PostForm("limit"))

	if sqlStr == "" {
		response.WriteErr(c, 200, 400, "SQL 语句不能为空")
		return
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	// 仅支持单条 SELECT：分页包装依赖子查询语义，写语句/多语句一律拒绝。
	upper := strings.ToUpper(sqlguard.StripComments(sqlStr))
	if !strings.HasPrefix(strings.TrimSpace(upper), "SELECT") {
		response.WriteErr(c, 200, 400, "分页接口仅支持单条 SELECT 查询")
		return
	}
	if strings.Contains(strings.TrimSuffix(sqlStr, ";"), ";") {
		response.WriteErr(c, 200, 400, "分页接口不支持多语句")
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	dbConn := conn.GetConn(connId, authorization)
	if dbConn == nil {
		response.WriteErr(c, 200, 500, "数据库连接不可用")
		return
	}

	analysis := permission.AnalyzeSQL(sqlStr, schema)
	if permResult := permission.CheckAnalysisPermission(analysis, connId, authorization); !permResult.Allowed {
		response.WriteErr(c, 200, 500, permResult.Message)
		return
	}

	dbType := dbConn.DriverName()
	canPage := dialect.SupportsPaging(dbType) && !sqlguard.HasTopLevelLimit(sqlStr)

	querySQL, params, err := buildPageQuery(dbType, sqlStr, limit+1, offset, canPage)
	if err != nil {
		response.WriteErr(c, 200, 400, err.Error())
		return
	}

	queryCtx, queryCancel := withQueryContext(c, connId, c.PostForm("requestId"))
	defer queryCancel()

	rows, err := dbConn.QueryxContext(queryCtx, querySQL, params...)
	if err != nil {
		appErr := apperror.FromDatabase(err)
		response.WriteAppErr(c, appErr)
		return
	}
	defer rows.Close()

	columns, data, err := scanColumnarRows(dbType, rows)
	if err != nil {
		appErr := apperror.FromDatabase(err)
		response.WriteAppErr(c, appErr)
		return
	}

	hasMore := false
	if len(data) > limit {
		hasMore = true
		data = data[:limit]
	}

	response.WriteOK(c, PageResult{
		Columns: columns,
		Rows:    data,
		Offset:  offset,
		Limit:   limit,
		HasMore: hasMore,
		CanPage: canPage,
	})
}

// buildPageQuery 生成分页 SQL。
// 已带顶层 LIMIT 的语句不再包装（否则会产生双重 LIMIT），此时按原样执行并标记不可分页。
func buildPageQuery(dbType, sqlStr string, limit, offset int, canPage bool) (string, []any, error) {
	if !canPage {
		return strings.TrimSpace(strings.TrimSuffix(sqlStr, ";")), nil, nil
	}
	if !dialect.SupportsPaging(dbType) {
		return "", nil, nil
	}
	wrapped, params := dialect.WrapPage(dbType, sqlStr, limit, offset)
	return wrapped, params, nil
}

// scanColumnarRows 读取结果为「列信息 + 二维数组」，按方言做类型归一（与 /execSQL 一致）。
func scanColumnarRows(dbType string, rows *sqlx.Rows) ([]Column, [][]any, error) {
	cts, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	columns := make([]Column, len(cts))
	for i, ct := range cts {
		columns[i] = Column{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}

	out := make([][]any, 0, 64)
	for rows.Next() {
		values := make([]any, len(cts))
		ptrs := make([]any, len(cts))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		line := make([]any, len(values))
		for i := range values {
			colType := columns[i].Type
			val := derefAny(values[i])
			// 统一走方言注册表，方言/转换器缺失时安全降级
			if converted := dialect.ConvertValue(dbType, &colType, &val, true); converted != nil {
				line[i] = *converted
			} else {
				line[i] = val
			}
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return columns, out, nil
}

// derefAny 解开驱动返回的指针包装。
func derefAny(v any) any {
	switch p := v.(type) {
	case *any:
		if p == nil {
			return nil
		}
		return *p
	default:
		return v
	}
}
