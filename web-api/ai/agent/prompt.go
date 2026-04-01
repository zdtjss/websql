package agent

import (
	"fmt"
	"strings"
)

// SystemInstruction 构建系统指令。
func SystemInstruction(dbSchema string, dialect string, tools []*ToolDesc) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`你是一个专业的数据库 SQL 智能助手。你可以帮助用户：
1. 根据自然语言描述生成精确的 SQL 查询语句
2. 分析数据并回答问题
3. 协助数据导出操作
4. 执行数据库结构查询

当前数据库: %s
数据库类型: %s

`, dbSchema, dialect))

	if len(tools) > 0 {
		sb.WriteString("你可以使用以下工具，通过输出指定的 JSON 格式来调用：\n\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("工具名: %s\n描述: %s\n参数: %s\n\n", t.Name, t.Desc, t.ParamsDesc))
		}
		sb.WriteString(`调用工具时，请严格按以下 JSON 格式输出（不要包含其他内容）：
{"tool_call": {"name": "工具名", "arguments": {参数对象}}}

例如获取表结构：
{"tool_call": {"name": "get_table_schema", "arguments": {"tables": ["t_user"]}}}

例如执行查询：
{"tool_call": {"name": "query_data", "arguments": {"sql": "SELECT * FROM t_user LIMIT 10"}}}

重要规则：
- 需要调用工具时，整条回复只输出上述 JSON，不要附加任何其他文字
- 工具返回结果后，根据结果回答用户问题
- 当工具返回下载链接（如 /exports/xxx.xlsx）时，直接告知用户可以下载，不要编造其他路径（如 sandbox:/mnt/data/）
`)
	}

	sb.WriteString(`工作规则：
- 当用户描述查询需求时，先用 get_table_schema 工具获取相关表结构，再生成 SQL
- 生成的 SQL 必须准确、高效，符合当前数据库方言
- 对于 SELECT 查询，可以使用 query_data 工具执行并返回结果
- 对于 INSERT/UPDATE/DELETE/ALTER/DROP 等写操作，先生成 SQL 展示给用户，等待用户确认后再执行
- 当用户需要导出数据时，先生成查询 SQL，然后使用 export_data 工具
- 回复使用中文，SQL 语句不要用 markdown 代码块包裹
- 如果用户的问题不明确，主动询问澄清`)
	return sb.String()
}

// ToolDesc 工具描述（用于 system prompt）。
type ToolDesc struct {
	Name       string
	Desc       string
	ParamsDesc string
}

// BuildContextPrompt 构建包含表上下文的提示。
func BuildContextPrompt(question string, tables []string, tableSchema string) string {
	var sb strings.Builder
	if tableSchema != "" {
		sb.WriteString("相关表结构：\n")
		sb.WriteString(tableSchema)
		sb.WriteString("\n\n")
	}
	if len(tables) > 0 {
		sb.WriteString("可用表: ")
		sb.WriteString(strings.Join(tables, ", "))
		sb.WriteString("\n\n")
	}
	sb.WriteString(question)
	return sb.String()
}
