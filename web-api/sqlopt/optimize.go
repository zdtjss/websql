package sqlopt

import (
	"encoding/json"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"go-web/web-api/ai"
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
	Sql         string         `json:"sql"`
	Suggestions []Suggestion   `json:"suggestions"`
	ExplainPlan *ExplainResult `json:"explainPlan"`
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

	dbVersion := ""
	var versionSQL string
	switch dbType {
	case "oracle":
		versionSQL = "SELECT BANNER FROM v$version WHERE BANNER LIKE 'Oracle%'"
	case "mysql", "mariadb":
		versionSQL = "SELECT VERSION()"
	default:
		versionSQL = "SELECT VERSION()"
	}
	row := conn.QueryRow(versionSQL)
	if row != nil {
		row.Scan(&dbVersion)
	}

	if strings.TrimSpace(sqlStr) == "" {
		utils.WriteJson(c.Writer, map[string]any{"code": 500, "msg": "SQL不能为空"})
		return
	}

	result := &OptimizationSuggestion{
		Sql:         sqlStr,
		Suggestions: make([]Suggestion, 0),
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

	aiSuggestions := analyzeWithAgent(sqlStr, dbType, dbVersion, explainResult)
	if len(aiSuggestions) > 0 {
		result.Suggestions = aiSuggestions
	}

	utils.WriteJson(c.Writer, result)
}

type aiSuggestionItem struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	FixSQL      string `json:"fixSql"`
}

type aiOptimizeResponse struct {
	Suggestions []aiSuggestionItem `json:"suggestions"`
	Summary     string             `json:"summary"`
}

func analyzeWithAgent(sqlStr, dbType, dbVersion string, explainResult *ExplainResult) []Suggestion {
	aiCfg := admin.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		return nil
	}

	explainInfo := ""
	if explainResult != nil {
		explainJSON, err := json.Marshal(explainResult.Rows)
		if err == nil {
			explainInfo = fmt.Sprintf("\nEXPLAIN执行计划结果：\n%s\n", string(explainJSON))
		} else {
			explainInfo = fmt.Sprintf("\nEXPLAIN执行计划原始数据：\n%s\n", explainResult.Raw)
		}
	}

	systemPrompt := `你是一个专业的数据库SQL优化专家。请分析用户提供的SQL语句，找出性能问题并给出优化建议。

要求：
1. 分析SQL的执行效率、索引使用、查询结构等方面
2. 给出具体的优化建议，每条建议包含：type（index/performance/structure）、severity（critical/warning/info）、title、description
3. 如果可能，提供优化后的fixSql
4. 最终以JSON格式返回，格式为：{"suggestions": [...], "summary": "总结"}

注意：
- 只返回JSON，不要包含其他解释文字
- 如果没有明显问题，suggestions 可以为空数组
- 只关注SQL层面的优化，不要建议修改表结构或索引`

	versionInfo := ""
	if dbVersion != "" {
		versionInfo = fmt.Sprintf("\n数据库版本：%s", dbVersion)
	}

	userPrompt := fmt.Sprintf("数据库类型：%s%s\n待优化SQL：\n```sql\n%s\n```%s\n请分析并给出优化建议。", dbType, versionInfo, sqlStr, explainInfo)

	messages := []ai.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	response, err := ai.CallAI(aiCfg, messages)
	if err != nil {
		logutils.PrintErr(fmt.Errorf("AI agent 分析失败: %v", err))
		return nil
	}

	response = extractJSON(response)

	var aiResp aiOptimizeResponse
	if err := json.Unmarshal([]byte(response), &aiResp); err != nil {
		logutils.PrintErr(fmt.Errorf("AI agent 响应解析失败: %v, response: %s", err, response))
		return nil
	}

	if len(aiResp.Suggestions) == 0 {
		return nil
	}

	suggestions := make([]Suggestion, 0, len(aiResp.Suggestions))
	for _, item := range aiResp.Suggestions {
		suggestions = append(suggestions, Suggestion{
			Type:        item.Type,
			Severity:    item.Severity,
			Title:       item.Title,
			Description: item.Description,
			FixSQL:      item.FixSQL,
		})
	}

	return suggestions
}

func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, "```json"); idx != -1 {
		text = text[idx+7:]
		if end := strings.Index(text, "```"); end != -1 {
			text = text[:end]
		}
	} else if idx := strings.Index(text, "```"); idx != -1 {
		text = text[idx+3:]
		if end := strings.Index(text, "```"); end != -1 {
			text = text[:end]
		}
	}

	start := strings.Index(text, "{")
	if start == -1 {
		return text
	}
	end := strings.LastIndex(text, "}")
	if end == -1 || end <= start {
		return text
	}
	return text[start : end+1]
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

func init() {
	_ = config.Cfg
}
