package sqlopt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	agentv2 "websql/internal/ai/agent"
	admin "websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/system"
	"websql/internal/config"
	"websql/internal/logger"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/safego"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
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

func ExplainSQL(c *gin.Context) {
	connId := c.PostForm("connId")
	_ = c.PostForm("schema")
	sqlStr := c.PostForm("sql")

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)
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
			logger.PrintErrf("EXPLAIN失败", execErr)
			c.JSON(200, gin.H{"code": 500, "msg": "EXPLAIN失败: " + execErr.Error()})
			return
		}
		explainSQL = "SELECT * FROM TABLE(DBMS_XPLAN.DISPLAY())"
	}

	rows, err := conn.Queryx(explainSQL)
	if err != nil {
		logger.PrintErrf("EXPLAIN失败", err)
		c.JSON(200, gin.H{"code": 500, "msg": "EXPLAIN失败: " + err.Error()})
		return
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

	jsonutil.WriteJson(c.Writer, result)
}

func keep(val *any) {}

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

func OptimizeSQLStream(c *gin.Context) {
	connId := c.PostForm("connId")
	dbSchema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")

	dbType, cfgSchema, dbVersion := agentv2.GetDBInfo(connId)
	if dbSchema == "" {
		dbSchema = cfgSchema
	}

	log.Printf("[OptAgent] 开始优化 - connID=%s, dbType=%s, schema=%s, sqlLen=%d\n", connId, dbType, dbSchema, len(sqlStr))

	if strings.TrimSpace(sqlStr) == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL不能为空"})
		return
	}

	aiCfg := system.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 模型未配置，请先在系统设置中配置 AI 模型"})
		return
	}

	var explainResult *ExplainResult
	if explainResultJSON := c.PostForm("explainResult"); explainResultJSON != "" {
		json.Unmarshal([]byte(explainResultJSON), &explainResult)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	var mu sync.Mutex
	dead := false

	writeSSE := func(data string) {
		mu.Lock()
		defer mu.Unlock()
		if dead {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	flush := func(chunk agentv2.StreamChunk) {
		data, _ := json.Marshal(chunk)
		writeSSE(string(data))
	}

	kaStop := make(chan struct{})
	defer close(kaStop)
	safego.GoWithName("sqlopt-keepalive", func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-ticker.C:
				mu.Lock()
				if !dead {
					c.Writer.WriteString("data: \n\n")
					c.Writer.Flush()
				}
				mu.Unlock()
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	safego.GoWithName("sqlopt-ctx-watch", func() {
		<-c.Request.Context().Done()
		mu.Lock()
		dead = true
		mu.Unlock()
		cancel()
	})

	cm, err := agentv2.BuildChatModel(ctx, aiCfg)
	if err != nil {
		logger.PrintErrf("创建优化Agent模型失败", err)
		flush(agentv2.StreamChunk{Type: "error", Content: "AI 模型初始化失败: " + err.Error()})
		flush(agentv2.StreamChunk{Type: "done"})
		return
	}
	log.Printf("[OptAgent] 模型初始化成功 - provider=%s, model=%s\n", aiCfg.Provider, aiCfg.Model)

	schemas := []agentv2.SchemaRef{{ConnID: connId, Schema: dbSchema}}
	authorization := c.GetHeader("Authorization")
	var optUserId string
	if authorization != "" {
		if user := admin.GetUser(authorization); user != nil {
			optUserId = user.Id
		}
	}
	optTools, err := buildOptTools(connId, dbType, dbSchema, schemas, optUserId)
	if err != nil {
		logger.PrintErrf("创建优化Agent工具失败", err)
		flush(agentv2.StreamChunk{Type: "error", Content: "工具初始化失败: " + err.Error()})
		flush(agentv2.StreamChunk{Type: "done"})
		return
	}
	log.Printf("[OptAgent] 工具初始化成功 - toolCount=%d\n", len(optTools))

	sysPrompt := buildOptSystemPrompt(dbType, dbVersion, explainResult)

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLOptimizer",
		Description: "SQL 优化专家，分析 SQL 性能问题并给出优化建议",
		Instruction: sysPrompt,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: optTools},
		},
		MaxIterations: 10,
	})
	if err != nil {
		logger.PrintErrf("创建优化Agent失败", err)
		flush(agentv2.StreamChunk{Type: "error", Content: "Agent 创建失败: " + err.Error()})
		flush(agentv2.StreamChunk{Type: "done"})
		return
	}
	log.Printf("[OptAgent] Agent 创建成功 - maxIterations=10\n")

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	userPrompt := fmt.Sprintf("请分析并优化以下 SQL：\n\n```sql\n%s\n```", sqlStr)
	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
		&schema.Message{Role: schema.User, Content: userPrompt},
	}

	log.Printf("[OptAgent] 开始执行 - sqlLen=%d\n", len(sqlStr))
	iter := runner.Run(ctx, messages)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("[OptAgent] 事件错误 - err=%+v\n", event.Err)
			logger.PrintErrf("优化Agent事件错误", event.Err)
			flush(agentv2.StreamChunk{Type: "error", Content: "AI 处理出错: " + event.Err.Error()})
			break
		}
		if event.Action != nil && event.Action.Exit {
			log.Printf("[OptAgent] Agent 执行完毕\n")
			break
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			log.Printf("[OptAgent] Agent 被中断\n")
			flush(agentv2.StreamChunk{Type: "error", Content: "AI 处理被中断，请重试"})
			break
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mo := event.Output.MessageOutput
		if mo.IsStreaming && mo.MessageStream != nil {
			for {
				chunk, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					break
				}
				if chunk.ReasoningContent != "" {
					flush(agentv2.StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
				}
				if chunk.Content != "" {
					flush(agentv2.StreamChunk{Type: "content", Content: chunk.Content})
				}
			}
		}
	}

	flush(agentv2.StreamChunk{Type: "done"})
	log.Printf("[OptAgent] 优化流程结束 - connID=%s\n", connId)
}

func buildOptTools(connId, dbType, dbSchema string, schemas []agentv2.SchemaRef, userId string) ([]tool.BaseTool, error) {
	schemaTool, _ := toolutils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息，包含字段名、类型、索引等", agentv2.NewSchemaFunc(connId, dbType, dbSchema, schemas, userId))
	queryTool, _ := toolutils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", agentv2.NewQueryFunc(connId, schemas, nil, userId))

	var validTools []tool.BaseTool
	if schemaTool != nil {
		validTools = append(validTools, schemaTool)
	}
	if queryTool != nil {
		validTools = append(validTools, queryTool)
	}
	return validTools, nil
}

func buildOptSystemPrompt(dbType, dbVersion string, explainResult *ExplainResult) string {
	var sb strings.Builder

	sb.WriteString("你是一名资深 SQL 优化专家，专注于通过改写 SQL 提升查询性能。\n")
	sb.WriteString("你必须基于工具返回的实际数据进行分析，禁止凭猜测给出结论。\n")
	sb.WriteString("请逐步推理，每一步都需要有数据支撑，避免跳步结论。\n\n")

	fmt.Fprintf(&sb, "## 环境信息\n- 数据库类型：%s\n", dbType)
	if dbVersion != "" {
		fmt.Fprintf(&sb, "- 数据库版本：%s\n", dbVersion)
	}
	sb.WriteString("\n")

	sb.WriteString("## 可用工具\n")
	sb.WriteString("- `get_table_schema(table)` — 获取指定表的建表语句，包含字段名、类型、索引定义\n")
	sb.WriteString("  - 对 SQL 中涉及的所有表，应一次性并行调用此工具获取结构\n")
	sb.WriteString("- `query_data(sql)` — 执行 SELECT / SHOW / DESCRIBE / EXPLAIN / WITH 语句并返回结果\n")
	sb.WriteString("  - 仅当已提供的 EXPLAIN 结果不足以定位问题时才使用\n\n")

	sb.WriteString("## 工作流程\n")
	sb.WriteString("1. **审查 EXPLAIN 结果**：优先分析已预执行的 EXPLAIN 输出，识别关键指标（type、rows、Extra、key 等）\n")
	sb.WriteString("2. **获取表结构**：调用 `get_table_schema` 获取 SQL 涉及的所有表的完整结构（可并行调用）\n")
	sb.WriteString("3. **定位问题**：综合表结构与 EXPLAIN 结果，识别全表扫描、索引未命中、子查询低效、排序/过滤冗余、类型隐式转换等问题\n")
	sb.WriteString("4. **提出方案**：针对每个问题给出具体优化手段，说明优化原理\n")
	sb.WriteString("5. **输出优化 SQL**：给出改写后的完整 SQL，多个优化点合并为一条最终 SQL\n\n")

	sb.WriteString("## 输出格式（Markdown）\n")
	sb.WriteString("### 表结构分析\n")
	sb.WriteString("列出与性能相关的字段、索引信息，标注当前 SQL 是否有效利用\n\n")
	sb.WriteString("### 问题定位\n")
	sb.WriteString("逐条列出性能瓶颈，引用 EXPLAIN 中的具体字段值作为依据\n")
	sb.WriteString("格式：`表名.字段` - 问题描述（EXPLAIN: type=ALL, rows=10000）\n\n")
	sb.WriteString("### 优化方案\n")
	sb.WriteString("每个问题对应的优化手段及原理\n\n")
	sb.WriteString("### 优化后 SQL（若无需优化则省略）\n")
	sb.WriteString("```sql\n-- 优化后的完整 SQL，与原 SQL 语义等价\n```\n\n")
	sb.WriteString("### 性能预期（若无需优化则省略）\n")
	sb.WriteString("对比优化前后的执行计划变化（如：全表扫描 → 索引范围扫描，扫描行数从 N 降至 M）\n\n")

	sb.WriteString("## 约束\n")
	sb.WriteString("- 不做索引建议，除非索引缺失直接导致当前 SQL 无法高效执行\n")
	sb.WriteString("- 不输出与性能优化无关的建议（如代码风格、命名规范）\n")
	sb.WriteString("- 优化后 SQL 必须语义等价，返回结果集与原 SQL 一致\n")
	sb.WriteString("- 如果原 SQL 已经足够高效，直接说明无需优化，不要强行改写\n")
	sb.WriteString("- 输出内容应简洁专业，避免冗余铺垫\n\n")

	if explainResult != nil && len(explainResult.Rows) > 0 {
		sb.WriteString("## EXPLAIN 结果（已预执行）\n")
		sb.WriteString("```\n")
		// Format as aligned columns for readability
		colNames := make([]string, 0)
		for _, col := range explainResult.Columns {
			colNames = append(colNames, col.Name)
		}
		sb.WriteString(strings.Join(colNames, "\t"))
		sb.WriteString("\n")
		for _, row := range explainResult.Rows {
			vals := make([]string, 0)
			for _, col := range explainResult.Columns {
				if v, ok := row[col.Name]; ok {
					vals = append(vals, fmt.Sprintf("%v", v))
				} else {
					vals = append(vals, "NULL")
				}
			}
			sb.WriteString(strings.Join(vals, "\t"))
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}

	return sb.String()
}

func init() {
	_ = config.Cfg
}
