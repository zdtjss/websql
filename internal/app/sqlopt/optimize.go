package sqlopt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agentv2 "websql/internal/ai/agent"
	"websql/internal/app/system"
	"websql/internal/config"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/gin-gonic/gin"
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

// ExplainSQL handler 是薄包装，业务逻辑下沉到 ExplainService.Explain。
// 仅保留协议层: gin.Context 参数提取、错误转 response。
func ExplainSQL(c *gin.Context) {
	req := &ExplainRequest{
		ConnID:        appctx.Ctx.GetConnID(c),
		Schema:        c.PostForm("schema"),
		SQL:           c.PostForm("sql"),
		Authorization: appctx.Ctx.GetAuthorization(c),
	}
	result, err := ensureDefaultExplain().Explain(req)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func OptimizeSQLStream(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	dbSchema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")

	if strings.TrimSpace(sqlStr) == "" {
		response.WriteErr(c, 200, 500, "SQL不能为空")
		return
	}

	aiCfg := system.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		response.WriteErr(c, 200, 500, "AI 模型未配置，请先在系统设置中配置 AI 模型")
		return
	}

	req := &OptimizeRequest{
		ConnID:        connId,
		Schema:        dbSchema,
		SQL:           sqlStr,
		Authorization: appctx.Ctx.GetAuthorization(c),
		ExplainResult: decodeExplainResult(c.PostForm("explainResult")),
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

	emit := func(chunk StreamChunk) {
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

	if err := ensureDefaultOptimize().Optimize(ctx, req, emit); err != nil {
		emit(StreamChunk{Type: "error", Content: err.Error()})
	}
	emit(StreamChunk{Type: "done"})
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
