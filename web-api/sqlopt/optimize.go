package sqlopt

import (
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type ExplainResult struct {
	Columns []ExplainColumn  `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Raw     string           `json:"raw"`
}

type ExplainColumn struct {
	Name  string `json:"name"`
	Align string `json:"align"`
}

type OptimizationSuggestion struct {
	Sql          string       `json:"sql"`
	Suggestions  []Suggestion `json:"suggestions"`
	Score        int          `json:"score"`
	ExplainPlan  *ExplainResult `json:"explainPlan"`
}

type Suggestion struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	FixSQL      string `json:"fixSql"`
}

func ExplainSQL(c *gin.Context) {
	connId := c.PostForm("connId")
	_ = c.PostForm("schema")
	sqlStr := c.PostForm("sql")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	if strings.TrimSpace(sqlStr) == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL不能为空"})
		return
	}

	explainSQL := "EXPLAIN " + sqlStr
	if dbType == "oracle" {
		explainSQL = "EXPLAIN PLAN FOR " + sqlStr
		_, execErr := conn.Exec(explainSQL)
		if execErr != nil {
			logutils.PanicErrf("EXPLAIN失败", execErr)
		}
		explainSQL = "SELECT * FROM TABLE(DBMS_XPLAN.DISPLAY())"
	}

	rows, err := conn.Queryx(explainSQL)
	if err != nil {
		logutils.PanicErrf("EXPLAIN失败", err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	ctss, _ := rows.ColumnTypes()

	result := &ExplainResult{
		Columns: make([]ExplainColumn, 0),
		Rows:    make([]map[string]any, 0),
		Raw:     "",
	}

	for _, ct := range ctss {
		result.Columns = append(result.Columns, ExplainColumn{
			Name:  ct.Name(),
			Align: "left",
		})
	}

	var rawLines []string
	for i := 0; i < len(cols); i++ {
		valPtr := new(any)
		val := valPtr
		rawLines = append(rawLines, cols[i])
		keep(val)
	}

	for rows.Next() {
		vals := make([]any, len(cols))
		valPtrs := make([]any, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)
		row := make(map[string]any)
		for i, col := range cols {
			if vals[i] != nil {
				row[col] = vals[i]
			}
		}
		result.Rows = append(result.Rows, row)
	}

	simpleLines := make([]string, 0)
	for _, row := range result.Rows {
		for _, col := range cols {
			if v, ok := row[col]; ok {
				simpleLines = append(simpleLines, fmt.Sprintf("%s=%v", col, v))
			}
		}
	}
	result.Raw = strings.Join(simpleLines, "\n")

	utils.WriteJson(c.Writer, result)
}

func keep(val *any) {}

func OptimizeSQL(c *gin.Context) {
	connId := c.PostForm("connId")
	_ = c.PostForm("schema")
	sqlStr := c.PostForm("sql")
	useExplain := c.DefaultPostForm("useExplain", "true")

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	dbType := conn.DriverName()

	if strings.TrimSpace(sqlStr) == "" {
		utils.WriteJson(c.Writer, map[string]any{"code": 500, "msg": "SQL不能为空"})
		return
	}

	result := &OptimizationSuggestion{
		Sql:         sqlStr,
		Suggestions: make([]Suggestion, 0),
		Score:       100,
	}

	upperSQL := strings.ToUpper(strings.TrimSpace(sqlStr))

	var explainResult *ExplainResult
	if useExplain == "true" && strings.HasPrefix(upperSQL, "SELECT") {
		explainSQL := "EXPLAIN " + sqlStr
		r, err := execExplain(conn, dbType, explainSQL)
		if err == nil {
			explainResult = r
			result.ExplainPlan = explainResult
		}
	}

	analyzeStaticPatterns(result, sqlStr, upperSQL, dbType)

	if explainResult != nil {
		analyzeExplainPlan(result, explainResult)
	}

	if len(result.Suggestions) > 0 {
		result.Score = maxInt(50, 100-len(result.Suggestions)*8)
	}
	for _, s := range result.Suggestions {
		if s.Severity == "critical" {
			result.Score = maxInt(20, result.Score-15)
		} else if s.Severity == "warning" {
			result.Score = maxInt(40, result.Score-8)
		}
	}

	utils.WriteJson(c.Writer, result)
}

func execExplain(conn *sqlx.DB, dbType, sql string) (*ExplainResult, error) {
	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
		rows.Scan(valPtrs...)
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

func analyzeStaticPatterns(result *OptimizationSuggestion, originalSQL, upperSQL, dbType string) {
	if strings.Contains(upperSQL, "SELECT *") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "index",
			Severity:    "warning",
			Title:       "避免使用 SELECT *",
			Description: "SELECT * 会返回所有列，增加网络传输和内存开销。建议明确列出需要的列名。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, " LIKE '%") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "index",
			Severity:    "critical",
			Title:       "LIKE 前置模糊查询无法使用索引",
			Description: "LIKE '%xxx' 或 LIKE '%xxx%' 无法使用索引，会导致全表扫描。考虑使用全文索引或调整查询方式。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, "NOT IN") || strings.Contains(upperSQL, "NOT EXISTS") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "performance",
			Severity:    "warning",
			Title:       "注意 NOT IN/NOT EXISTS 性能",
			Description: "NOT IN 和 NOT EXISTS 在数据量大时性能较差，建议改用 LEFT JOIN...IS NULL 方式，或考虑使用反连接优化。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, "OR") {
		orCount := strings.Count(upperSQL, " OR ")
		if orCount > 2 {
			result.Suggestions = append(result.Suggestions, Suggestion{
				Type:        "index",
				Severity:    "warning",
				Title:       "大量 OR 条件可能导致索引失效",
				Description: "多个 OR 条件可能导致索引失效，建议改用 UNION ALL 优化。",
				FixSQL:      "",
			})
		}
	}

	if strings.Contains(upperSQL, "ORDER BY RAND()") || strings.Contains(upperSQL, "ORDER BY RANDOM()") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "performance",
			Severity:    "critical",
			Title:       "避免使用 ORDER BY RAND()",
			Description: "ORDER BY RAND() 会导致全表扫描和排序，性能极差。建议在应用层随机取数据，或使用其他随机方案。",
			FixSQL:      "",
		})
	}

	funcUsed := strings.Contains(upperSQL, "WHERE ") && (strings.Contains(upperSQL, "DATE(") ||
		strings.Contains(upperSQL, "YEAR(") || strings.Contains(upperSQL, "MONTH(") ||
		strings.Contains(upperSQL, "UPPER(") || strings.Contains(upperSQL, "LOWER(") ||
		strings.Contains(upperSQL, "CONCAT("))
	if funcUsed && strings.Contains(upperSQL, "WHERE ") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "index",
			Severity:    "warning",
			Title:       "WHERE子句中对列使用函数会导致索引失效",
			Description: "在 WHERE 条件中对列使用函数（如 DATE(), YEAR()）会导致索引失效。建议使用范围查询或创建函数索引。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, " GROUP BY ") && strings.Count(upperSQL, " JOIN ") > 2 {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "performance",
			Severity:    "warning",
			Title:       "多表JOIN后GROUP BY可能效率低",
			Description: "多表 JOIN 后再 GROUP BY 会产生大量临时数据。建议先对子查询做聚合，再 JOIN。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, "SELECT") && strings.Count(upperSQL, "SELECT") > 2 {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "structure",
			Severity:    "warning",
			Title:       "嵌套子查询可能影响性能",
			Description: "多层嵌套子查询可能导致查询优化器选择低效的执行计划。建议改写为 JOIN 或使用 CTE。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, "OFFSET") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "performance",
			Severity:    "info",
			Title:       "大偏移量分页优化",
			Description: "当 OFFSET 较大时，MySQL 仍需扫描大量行。建议使用基于主键的游标分页（WHERE id > last_id LIMIT n）。",
			FixSQL:      "",
		})
	}

	if strings.Contains(upperSQL, "JOIN") && !strings.Contains(upperSQL, "ON") {
		result.Suggestions = append(result.Suggestions, Suggestion{
			Type:        "structure",
			Severity:    "critical",
			Title:       "JOIN缺少ON条件",
			Description: "JOIN 语句必须有 ON 条件来明确关联关系，否则会产生笛卡尔积。",
			FixSQL:      "",
		})
	}
}

func analyzeExplainPlan(result *OptimizationSuggestion, explain *ExplainResult) {
	for _, row := range explain.Rows {
		if extra, ok := row["Extra"]; ok {
			extraStr := fmt.Sprintf("%v", extra)
			if strings.Contains(extraStr, "Using filesort") {
				result.Suggestions = append(result.Suggestions, Suggestion{
					Type:        "index",
					Severity:    "warning",
					Title:       "发现 Using filesort",
					Description: "使用了文件排序（filesort），说明ORDER BY/GROUP BY 没有使用索引。建议为排序列添加索引。",
					FixSQL:      "",
				})
			}
			if strings.Contains(extraStr, "Using temporary") {
				result.Suggestions = append(result.Suggestions, Suggestion{
					Type:        "index",
					Severity:    "critical",
					Title:       "发现 Using temporary",
					Description: "使用了临时表，这通常意味着 GROUP BY 或 ORDER BY 列没有合适的索引。建议创建复合索引。",
					FixSQL:      "",
				})
			}
		}

		if typ, ok := row["type"]; ok {
			typeStr := fmt.Sprintf("%v", typ)
			if typeStr == "ALL" {
				result.Suggestions = append(result.Suggestions, Suggestion{
					Type:        "index",
					Severity:    "critical",
					Title:       "发现全表扫描（type=ALL）",
					Description: "执行计划显示全表扫描。建议为 WHERE 条件中的列添加索引，或检查索引是否被正确使用。",
					FixSQL:      "",
				})
			}
		}

		if key, ok := row["key"]; ok {
			keyStr := fmt.Sprintf("%v", key)
			if keyStr == "<nil>" || keyStr == "" {
				if possibleKey, ok := row["possible_keys"]; ok {
					pkStr := fmt.Sprintf("%v", possibleKey)
					if pkStr != "<nil>" && pkStr != "" && pkStr != "nil" {
						result.Suggestions = append(result.Suggestions, Suggestion{
							Type:        "index",
							Severity:    "warning",
							Title:       "可用索引未被使用",
							Description: fmt.Sprintf("存在可用索引 %v，但未被使用。可能原因：查询条件不符合索引最左前缀，或优化器认为全表扫描更快。", pkStr),
							FixSQL:      "",
						})
					}
				}
			}
		}

		if rows, ok := row["rows"]; ok {
			rowsStr := fmt.Sprintf("%v", rows)
			if rowsStr != "<nil>" && rowsStr != "" {
				rowsFloat := parseFloat(rowsStr)
				if rowsFloat > 10000 {
					result.Suggestions = append(result.Suggestions, Suggestion{
						Type:        "performance",
						Severity:    "warning",
						Title:       fmt.Sprintf("预估扫描行数过多（约%s行）", rowsStr),
						Description: "优化器预计将扫描大量行，建议优化查询条件或添加合适的索引。",
						FixSQL:      "",
					})
				}
			}
		}
	}
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func AnalyzeSQLSuggestions(c *gin.Context) {
	sqlStr := c.PostForm("sql")
	dbType := c.PostForm("dbType")

	result := &OptimizationSuggestion{
		Sql:         sqlStr,
		Suggestions: make([]Suggestion, 0),
		Score:       100,
	}

	upperSQL := strings.ToUpper(strings.TrimSpace(sqlStr))
	analyzeStaticPatterns(result, sqlStr, upperSQL, dbType)

	if len(result.Suggestions) > 0 {
		result.Score = maxInt(50, 100-len(result.Suggestions)*8)
	}

	utils.WriteJson(c.Writer, result)
}

func init() {
	_ = config.Cfg
}