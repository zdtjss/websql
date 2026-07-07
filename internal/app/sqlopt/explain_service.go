package sqlopt

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"websql/internal/app/conn"
	"websql/internal/logger"

	"github.com/jmoiron/sqlx"
)

// ExplainRequest 是 EXPLAIN 的入参。
type ExplainRequest struct {
	ConnID        string
	Schema       string // 当前预留，未使用
	SQL           string
	Authorization string
}

// ExplainService 封装 EXPLAIN 业务逻辑，与 gin.Context 解耦。
type ExplainService struct{}

var (
	defaultExplainService *ExplainService
	defaultExplainOnce    = sync.Once{}
)

func ensureDefaultExplain() *ExplainService {
	defaultExplainOnce.Do(func() {
		defaultExplainService = &ExplainService{}
	})
	return defaultExplainService
}

// Explain 执行 EXPLAIN 并返回结构化结果。
// 业务逻辑来自原 optimize.go 的 ExplainSQL handler。
func (s *ExplainService) Explain(req *ExplainRequest) (*ExplainResult, error) {
	if strings.TrimSpace(req.SQL) == "" {
		return nil, errors.New("SQL不能为空")
	}

	connCtx := conn.GetConn(req.ConnID, req.Authorization)
	if connCtx == nil {
		return nil, errors.New("数据库连接不可用")
	}

	dbType := connCtx.DriverName()
	explainSQL := "EXPLAIN " + req.SQL
	if dbType == "oracle" {
		explainSQL = "EXPLAIN PLAN FOR " + req.SQL
		if _, execErr := connCtx.Exec(explainSQL); execErr != nil {
			logger.PrintErrf("EXPLAIN失败", execErr)
			return nil, fmt.Errorf("EXPLAIN失败: %w", execErr)
		}
		explainSQL = "SELECT * FROM TABLE(DBMS_XPLAN.DISPLAY())"
	}

	rows, err := connCtx.Queryx(explainSQL)
	if err != nil {
		logger.PrintErrf("EXPLAIN失败", err)
		return nil, fmt.Errorf("EXPLAIN失败: %w", err)
	}
	defer rows.Close()

	return readExplainRows(rows)
}

// readExplainRows 把 sqlx.Rows 转成 ExplainResult。
// 抽出独立函数，便于复用与测试。
func readExplainRows(rows *sqlx.Rows) (*ExplainResult, error) {
	cols, _ := rows.Columns()
	result := &ExplainResult{
		Columns: make([]ExplainColumn, 0),
		Rows:    make([]map[string]any, 0),
	}

	for _, col := range cols {
		result.Columns = append(result.Columns, ExplainColumn{Name: col, Align: "left"})
	}

	for rows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}
		row := make(map[string]any)
		for i, col := range cols {
			if vals[i] != nil {
				row[col] = vals[i]
			}
		}
		result.Rows = append(result.Rows, row)
	}

	var lines []string
	for _, row := range result.Rows {
		for _, col := range cols {
			if v, ok := row[col]; ok {
				lines = append(lines, fmt.Sprintf("%s=%v", col, v))
			}
		}
	}
	result.Raw = strings.Join(lines, "\n")
	return result, nil
}

// ExplainByService 是包级便捷函数，供 Wails binding 直接调用。
func ExplainByService(req *ExplainRequest) (*ExplainResult, error) {
	return ensureDefaultExplain().Explain(req)
}
