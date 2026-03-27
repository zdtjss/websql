package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"go-web/logutils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// HandleSaveConfig saves the AI configuration.
func HandleSaveConfig(c *gin.Context) {
	var cfg AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败: " + err.Error()})
		return
	}
	if err := SaveAIConfig(cfg); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "保存配置失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": "保存成功"})
}

// HandleGetConfig returns the AI configuration with the apiKey masked.
func HandleGetConfig(c *gin.Context) {
	cfg, err := GetAIConfig()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败: " + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 200, "data": nil})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": cfg})
}

// HandleTestConfig tests the AI connection by sending a simple ping message.
func HandleTestConfig(c *gin.Context) {
	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败: " + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 400, "msg": "请先配置 AI 服务"})
		return
	}

	messages := []ChatMessage{{Role: "user", Content: "ping"}}
	reply, err := CallAI(cfg, messages)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 服务调用失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": gin.H{"reply": reply}})
}

// tableSchemaInfo holds the fetched structure of one table for prompt building.
type tableSchemaInfo struct {
	TableName    string
	TableComment string
	Columns      []columnInfo
}

type columnInfo struct {
	Name     string
	Type     string
	Nullable string
	Comment  string
}

// fetchTableSchemas queries column definitions and table comments for the given tables.
// Returns the db driver name and the schema info list.
func fetchTableSchemas(connId, schema, authorization string, tableNames []string) (string, []tableSchemaInfo) {
	if connId == "" {
		return "", nil
	}
	dc := admin.GetConn(connId, authorization)
	dbType := dc.DriverName()
	dialect := dbutils.SQL_DIALECT[dbType]

	if len(tableNames) == 0 {
		return dbType, nil
	}

	// Fetch table comments in one query
	tableComments := map[string]string{}
	if sqlStr, ok := dialect["listTable"]; ok {
		rows, err := dc.Queryx(sqlStr, schema)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var name, typ, comment string
				if err := rows.Scan(&name, &typ, &comment); err == nil {
					tableComments[name] = comment
				}
			}
		}
	}

	// Fetch columns per table
	sqlStr, ok := dialect["listTableColumns"]
	if !ok {
		return dbType, nil
	}

	result := make([]tableSchemaInfo, 0, len(tableNames))
	for _, tbl := range tableNames {
		var args []any
		if dbType == "oracle" {
			args = []any{"notexists", tbl}
		} else {
			args = []any{schema, tbl}
		}
		rows, err := dc.Queryx(sqlStr, args...)
		if err != nil {
			logutils.PrintErr(err)
			continue
		}
		rawCols := dbutils.GetResultRows(dbType, rows)

		cols := make([]columnInfo, 0, len(rawCols))
		for _, r := range rawCols {
			col := columnInfo{
				Name:     strVal(r, "COLUMN_NAME", "column_name"),
				Type:     strVal(r, "COLUMN_TYPE", "column_type", "DATA_TYPE"),
				Nullable: strVal(r, "IS_NULLABLE", "is_nullable", "NULLABLE"),
				Comment:  strVal(r, "COLUMN_COMMENT", "column_comment", "COMMENTS"),
			}
			if col.Name != "" {
				cols = append(cols, col)
			}
		}
		result = append(result, tableSchemaInfo{
			TableName:    tbl,
			TableComment: tableComments[tbl],
			Columns:      cols,
		})
	}
	return dbType, result
}

// strVal picks the first non-empty string value from a map by trying multiple keys.
// Handles both direct string values and *any pointer values returned by GetResultRows.
func strVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		// GetResultRows stores *any pointers
		if ptr, ok := v.(*any); ok && ptr != nil {
			if s, ok := (*ptr).(string); ok && s != "" {
				return s
			}
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// BuildGenerateSqlPrompt constructs the system prompt, enriched with real table schema info.
func BuildGenerateSqlPrompt(req GenerateSqlRequest, authorization string) string {
	var sb strings.Builder

	dbType, schemas := fetchTableSchemas(req.ConnId, req.Schema, authorization, req.TableContext)

	sb.WriteString("你是一个资深数据库工程师，擅长编写高质量 SQL 语句。")
	sb.WriteString("\n请严格按照以下要求回答：")
	sb.WriteString("\n1. 只输出 SQL 语句本身，不要任何解释、注释或 markdown 格式（不要用 ``` 包裹）")
	sb.WriteString("\n2. SQL 语句必须可以直接执行，语法正确")

	if dbType != "" {
		sb.WriteString(fmt.Sprintf("\n3. 目标数据库：%s", dbType))
		switch strings.ToLower(dbType) {
		case "mysql", "mariadb":
			sb.WriteString("，表名和字段名用反引号包裹，字符串用单引号")
		case "oracle":
			sb.WriteString("，表名和字段名用双引号包裹，字符串用单引号，分页用 ROWNUM 或 FETCH FIRST")
		case "sqlite":
			sb.WriteString("，表名和字段名用双引号包裹")
		}
	}

	if req.Schema != "" {
		sb.WriteString(fmt.Sprintf("\n4. 当前 Schema：%s", req.Schema))
	}

	if len(schemas) > 0 {
		sb.WriteString("\n\n-- 相关表结构如下：\n")
		for _, t := range schemas {
			sb.WriteString(fmt.Sprintf("\nCREATE TABLE `%s`", t.TableName))
			if t.TableComment != "" {
				sb.WriteString(fmt.Sprintf(" -- %s", t.TableComment))
			}
			sb.WriteString(" (\n")
			for i, c := range t.Columns {
				nullable := "NOT NULL"
				if strings.EqualFold(c.Nullable, "YES") || strings.EqualFold(c.Nullable, "Y") {
					nullable = "NULL"
				}
				line := fmt.Sprintf("  `%s` %s %s", c.Name, c.Type, nullable)
				if c.Comment != "" {
					line += fmt.Sprintf(" COMMENT '%s'", c.Comment)
				}
				if i < len(t.Columns)-1 {
					line += ","
				}
				sb.WriteString(line + "\n")
			}
			sb.WriteString(");\n")
		}
	} else if len(req.TableContext) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n-- 相关表：%s", strings.Join(req.TableContext, ", ")))
	}

	return sb.String()
}

// BuildChatPrompt constructs the system prompt for chat/analysis requests.
func BuildChatPrompt(req ChatRequest) string {
	var sb strings.Builder
	sb.WriteString("你是一个专业的数据分析助手，擅长解读数据、发现规律并给出业务建议。")

	if req.TableName != "" {
		sb.WriteString(fmt.Sprintf("\n当前分析的表：%s", req.TableName))
	}
	if req.Schema != "" {
		sb.WriteString(fmt.Sprintf("（Schema：%s）", req.Schema))
	}

	if len(req.DataSample) > 0 {
		// Show column names
		cols := make([]string, 0, len(req.DataSample[0]))
		for k := range req.DataSample[0] {
			cols = append(cols, k)
		}
		sb.WriteString(fmt.Sprintf("\n字段列表：%s", strings.Join(cols, "、")))

		// Show up to 3 sample rows
		limit := len(req.DataSample)
		if limit > 3 {
			limit = 3
		}
		sb.WriteString(fmt.Sprintf("\n数据样本（共 %d 行，展示前 %d 行）：", len(req.DataSample), limit))
		for i := 0; i < limit; i++ {
			pairs := make([]string, 0, len(cols))
			for _, k := range cols {
				pairs = append(pairs, fmt.Sprintf("%s=%v", k, req.DataSample[i][k]))
			}
			sb.WriteString(fmt.Sprintf("\n  第%d行：%s", i+1, strings.Join(pairs, "，")))
		}
	}

	sb.WriteString("\n\n请用中文回答，回答要简洁、有洞察力。")
	return sb.String()
}

// HandleGenerateSql generates SQL from a natural language question using AI.
func HandleGenerateSql(c *gin.Context) {
	var req GenerateSqlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败: " + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败: " + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 400, "msg": "请先配置 AI 服务"})
		return
	}

	messages := []ChatMessage{
		{Role: "system", Content: BuildGenerateSqlPrompt(req, c.GetHeader("Authorization"))},
		{Role: "user", Content: req.Question},
	}

	reply, err := CallAI(cfg, messages)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 服务调用失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": gin.H{"sql": reply}})
}

// HandleGenerateSqlStream streams SQL generation via SSE (text/event-stream).
func HandleGenerateSqlStream(c *gin.Context) {
	var req GenerateSqlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败: " + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败: " + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 400, "msg": "请先配置 AI 服务"})
		return
	}

	messages := []ChatMessage{
		{Role: "system", Content: BuildGenerateSqlPrompt(req, c.GetHeader("Authorization"))},
		{Role: "user", Content: req.Question},
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)

	flusher, canFlush := c.Writer.(interface{ Flush() })

	writeChunk := func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	if err := StreamAI(cfg, messages, writeChunk); err != nil {
		writeChunk(StreamChunk{Type: "error", Content: err.Error()})
		return
	}
	writeChunk(StreamChunk{Type: "done", Content: ""})
}

// HandleChat handles a conversational AI request with table structure and data sample context.
func HandleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败: " + err.Error()})
		return
	}

	cfg, err := GetAIConfigRaw()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败: " + err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 400, "msg": "请先配置 AI 服务"})
		return
	}

	messages := []ChatMessage{
		{Role: "system", Content: BuildChatPrompt(req)},
	}
	messages = append(messages, req.Messages...)

	reply, err := CallAI(cfg, messages)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 服务调用失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": gin.H{"reply": reply}})
}
