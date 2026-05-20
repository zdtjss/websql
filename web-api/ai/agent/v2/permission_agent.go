package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func init() {
	schema.RegisterName[*PermDecisionOutput]("agentv2.PermDecisionOutput")
}

func NewPermissionAgent(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbSchema, userID string) (*adk.ChatModelAgent, error) {
	cm, err := BuildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create permission agent model failed: %w", err)
	}

	tools, err := buildPermissionAgentTools(ctx, connID, userID, dbSchema)
	if err != nil {
		return nil, fmt.Errorf("create permission agent tools failed: %w", err)
	}

	sysPrompt := buildPermissionAgentPrompt(dbType, dbSchema, userID, connID)

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "PermissionAgent",
		Description: "数据权限审核专家，判断SQL操作是否在用户授权范围内",
		Instruction: sysPrompt,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 8,
	})
	if err != nil {
		return nil, fmt.Errorf("create PermissionAgent failed: %w", err)
	}

	return agent, nil
}

func buildPermissionAgentTools(_ context.Context, connID, userID, dbSchema string) ([]tool.BaseTool, error) {
	tableStructureTool, _ := utils.InferTool("get_table_structure",
		"获取指定表的结构信息（列名、类型、是否可空等）。用于了解SQL中涉及的表有哪些列",
		newGetTableStructureFunc(connID, dbSchema))

	userPermsTool, _ := utils.InferTool("get_user_permissions",
		"获取当前用户在此数据库连接上的数据权限配置。返回连接级、表级、字段级的授权范围",
		newGetUserPermissionsFunc(userID, connID, dbSchema))

	allTools := []tool.BaseTool{tableStructureTool, userPermsTool}
	var validTools []tool.BaseTool
	for _, t := range allTools {
		if t != nil {
			validTools = append(validTools, t)
		}
	}
	return validTools, nil
}

func buildPermissionAgentPrompt(dbType, dbSchema, userID, connID string) string {
	var sb strings.Builder

	sb.WriteString(`你是数据权限审核专家，职责是判断用户提交的 SQL 是否在其授权的数据范围内。

你收到的请求格式固定如下：
  检查以下SQL的权限
  工具名称：<query_data | exec_sql | export>
  SQL语句：
  <SQL>

你需要调用工具获取信息后，给出是否允许的判断。

## 权限层级（由高到低，高优先级可覆盖低优先级）
级别1 - 连接级(conn)：拥有该连接下所有库、表、字段的完整访问权限（hasFullConnAccess=true）
级别2 - 库级(schema)：拥有该库下所有表、字段的完整访问权限（hasFullSchemaAccess=true）
级别3 - 表级(table)：拥有该表所有字段的完整访问权限（accessLevel=full）
级别4 - 字段级(column)：仅拥有该表指定字段的访问权限（accessLevel=column, allowedColumns非空）

## 最具体优先原则（核心规则）
当同一连接下同时存在高级别与低级别权限时，以最具体的权限配置为准：
- 若存在表级或字段级权限，则连接级/库级权限自动降级，不得越权放行
- 例如：用户对 orders 表有 column 级权限 [id,name]，即使同时有 schema 级权限，
  也只能访问 orders 表的 id 和 name 两列

## 工作流程（严格按顺序执行）
步骤1 — 解析 SQL：从 SQL 中提取所有涉及的表和字段
  - SELECT：提取 FROM / JOIN / 子查询中的表，以及 SELECT 子句中的字段名
  - INSERT：提取目标表名和 VALUES / SELECT 中的字段
  - UPDATE：提取目标表名和 SET 子句中的字段
  - DELETE：提取目标表名
  - DDL（CREATE/ALTER/DROP/TRUNCATE）：提取目标表名
  - 注意：WITH 定义的 CTE 名称不属于需检查的表；子查询中的表必须逐层检查
  - 注意：若 SQL 中包含 SELECT * 或 table.*，识别为"所有字段"，单独标记
  - 注意：若表名带 schema 前缀（如 schema.table），提取纯表名用于匹配

步骤2 — 查询表结构：调用 get_table_structure，传入所有提取到的表名列表
  - 若表不存在（exists=false），该表无需权限检查，直接跳过

步骤3 — 查询用户权限：调用 get_user_permissions 获取用户的权限配置
  - 若 tablePermissions 为空且 hasFullConnAccess/hasFullSchemaAccess 均为 false，
    说明用户无任何权限，所有操作均应拒绝

步骤4 — 逐表逐字段比对：
  - hasFullConnAccess=true → 全部允许
  - hasFullSchemaAccess=true → 全部允许（但仅限当前 schema）
  - 表级权限(accessLevel=full) → 该表全部字段允许
  - 字段级权限(accessLevel=column) → 仅 allowedColumns 内的字段允许，
    其他字段拒绝；将拒绝字段写入 deniedColumns
  - 不在权限列表中的表 → 拒绝，写入 deniedTables

## 特殊规则
- SELECT * 拦截：若用户仅有字段级权限(column)，且 SQL 中使用 SELECT * 或 SELECT table.*，
  必须拒绝。原因：* 会查出未授权字段。deniedColumns 填入 ["*"] 或 ["table.*"]
- 视图(View)：按表级规则处理，检查视图名是否在权限列表中
- 多表 JOIN：每个表独立检查，任一表无权限即拒绝该表
- UNION 查询：分别检查每个 SELECT 分支
- 系统表/信息模式表（如 information_schema.*、pg_catalog.*、sqlite_master）：直接放行
- 不检查 SQL 语法正确性，只检查数据访问权限

## 工具名称语义
- query_data：只读查询工具，检查 SELECT 涉及的所有表和字段
- exec_sql：写操作工具，检查 INSERT/UPDATE/DELETE/DDL 涉及的表和字段
- export：数据导出工具，与 query_data 类似，检查 SELECT 涉及的表和字段

## 输出格式（严格遵守）

只输出纯 JSON 对象，一行或多行均可，但不得包含：
- 任何其他文字说明
- Markdown 代码块标记（json）
- 额外的换行和空格之外的字符

当权限检查通过时，格式如下：
{
  "allowed": true,
  "deniedTables": [],
  "deniedColumns": [],
  "reason": "权限检查通过：具体的通过原因（如：用户拥有表 orders 的完整权限）"
}

当权限不足时，格式如下：
{
  "allowed": false,
  "deniedTables": ["无权限的表名"],
  "deniedColumns": ["表名.字段名", "表名.字段名"],
  "reason": "具体且完整的拒绝原因（列出每个被拒绝的表和字段及其原因）"
}

reason 字段要求：
- allowed=true 时：说明通过的权限级别和涉及的表
- allowed=false 时：必须逐个列出每个被拒绝的表名和字段名，说明原因
- 若用户有字段级权限(column)，reason 中必须明确区分"授权字段"和"拒绝字段"

## 判断速查
- hasFullConnAccess=true        → 全部允许
- hasFullSchemaAccess=true      → 全部允许
- 表在 AllowedTables 中         → 该表全部字段允许
- 表在 AllowedColumns 中        → 仅 allowedColumns 内的字段允许，其余拒绝
- 表不在任何权限列表中          → 拒绝
- column级 + SELECT *           → 必须拒绝

`)
	fmt.Fprintf(&sb, "## 上下文信息\n")
	fmt.Fprintf(&sb, "- 数据库类型：%s\n", dbType)
	fmt.Fprintf(&sb, "- 默认 Schema：%s\n", dbSchema)
	fmt.Fprintf(&sb, "- 用户 ID：%s\n", userID)
	fmt.Fprintf(&sb, "- 连接 ID：%s\n", connID)

	return sb.String()
}

func callPermissionAgent(ctx context.Context, permAgent tool.BaseTool, sql, toolName string) (*PermDecisionOutput, error) {
	invokable, ok := permAgent.(tool.InvokableTool)
	if !ok {
		return nil, errors.New("permission agent does not support invokable mode")
	}

	request := fmt.Sprintf("检查以下SQL的权限\n工具名称：%s\nSQL语句：\n%s", toolName, sql)

	type agentToolInput struct {
		Request string `json:"request"`
	}
	input := agentToolInput{Request: request}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal permission input failed: %w", err)
	}

	log.Printf("[PermAgent] 开始检查 - tool=%s, sql=%s\n", toolName, sql)
	result, err := invokable.InvokableRun(ctx, string(inputJSON))
	if err != nil {
		log.Printf("[PermAgent] 调用失败 - tool=%s, err=%v\n", toolName, err)
		return nil, fmt.Errorf("permission agent run failed: %w", err)
	}

	decision, err := unmarshalPermDecision(result)
	if err != nil {
		log.Printf("[PermAgent] 结果解析失败 - tool=%s, err=%v, raw=%s\n", toolName, err, result)
		return nil, fmt.Errorf("parse permission agent result failed: %w, raw=%s", err, result)
	}

	if decision.Allowed {
		log.Printf("[PermAgent] 允许 - tool=%s, reason=%s\n", toolName, decision.Reason)
	} else {
		log.Printf("[PermAgent] 拒绝 - tool=%s, reason=%s, deniedTables=%v, deniedColumns=%v\n",
			toolName, decision.Reason, decision.DeniedTables, decision.DeniedColumns)
	}

	return decision, nil
}
