---
name: cross-db-analysis
description: 跨数据库/多 Schema 大数据量分析。Agent 负责用 query_data 工具分别查询多个数据库，在内存中完成聚合、对比、关联计算，避免将海量数据加载到上下文。适用于跨库关联、对比统计、复杂聚合等数据量可能超过 10 万行的场景。当用户涉及多个 schema 或数据库的分析、对比、统计时必须使用此技能。
version: "2.1.0"
min_agent_version: "1.0.0"
dependencies:
  - type: context
    name: connection_id
    description: 跨库分析需要至少一个数据库连接
  - type: context
    name: schema
    description: 跨库分析需要 schema 信息用于路由查询
error_hints:
  - pattern: "connection refused"
    hint: "数据库连接被拒绝。请检查目标数据库是否运行、网络是否可达"
    suggestion: "确认连接配置正确，或联系 DBA 检查数据库状态"
  - pattern: "timeout"
    hint: "查询超时。可能是数据量过大或缺少索引"
    suggestion: "添加 WHERE 条件缩小范围，或使用聚合下推（GROUP BY）减少返回行数"
  - pattern: "syntax error"
    hint: "SQL 语法错误。注意跨库查询时各库方言可能不同"
    suggestion: "确认目标库类型，使用兼容的 SQL 语法"
  - pattern: "table not found"
    hint: "表不存在。可能是 schema 路由错误或表名大小写不匹配"
    suggestion: "先用 list_tables 确认表名，注意 schema 前缀"
  - pattern: "permission denied"
    hint: "权限不足。当前用户对目标库/表无查询权限"
    suggestion: "联系管理员开通对应 schema 的查询权限"
command_blacklist:
  - DROP DATABASE
  - DROP SCHEMA
  - TRUNCATE
  - SHUTDOWN
---

# 跨库大数据量分析 Skill

本 Skill 指导 Agent 完成跨数据库/多 Schema 的大数据量分析。**Agent 用 query_data 工具取数，在 SQL 层面下推聚合，仅返回统计结论**。

## 核心原则

1. **聚合下推**：在数据库端执行 COUNT/SUM/AVG/GROUP BY，只返回统计结果
2. **分库查询**：用 `query_data` 的 `connId` 参数分别查多个库
3. **内存关联**：跨库 JOIN 在 Agent 内存中用 Hash Join 完成
4. **结果精简**：单次返回不超过 5000 行，保护上下文窗口

## 工作流（Agent 必须按序执行）

### 步骤 1：评估数据规模

对每个涉及的表先执行计数：
```sql
SELECT COUNT(*) AS cnt FROM <schema>.<table>
```
若任一表 > 10 万行，必须用聚合下推，禁止全量拉取。

### 步骤 2：选择任务类型

| 类型 | 适用场景 | 执行方式 |
|------|---------|---------|
| **aggregate** | 单表聚合统计 | 在各库执行 GROUP BY 聚合 SQL |
| **compare** | 多源同名表对比 | 各库分别聚合，Agent 对比差异 |
| **join** | 跨库关联 | 同库直接 SQL JOIN；跨库 Agent 内存 Hash Join |
| **custom** | 自定义 SQL | 各库分别执行，Agent 合并结果 |

### 步骤 3：执行查询

用 `query_data` 工具，通过 `connId` 参数指定目标库：

```json
{"sql": "SELECT date, SUM(amount) FROM orders GROUP BY date", "connId": "1"}
{"sql": "SELECT date, SUM(amount) FROM orders GROUP BY date", "connId": "2"}
```

### 步骤 4：Agent 内存处理

#### aggregate 任务
直接汇总各库返回的聚合结果。

#### compare 任务
1. 各库返回 `[{dimension, metric}]` 结构
2. Agent 按维度对齐，计算差异：
   - 绝对变化量：`metric_b - metric_a`
   - 百分比变化：`(metric_b - metric_a) / metric_a * 100`

#### join 任务
**同库 JOIN**（connId 相同）：直接写 SQL JOIN
```sql
SELECT a.id, b.name FROM schemaA.orders a JOIN schemaB.users b ON a.user_id = b.id
```

**跨库 JOIN**（connId 不同）：
1. 分别查两侧数据（带 LIMIT）
2. Agent 在内存中构建 Hash 索引（右表 key → row）
3. 遍历左表，匹配右表，输出关联结果
4. 结果列加 `left_` / `right_` 前缀避免冲突

### 步骤 5：组织结论

把分析结果整理成 Markdown 表格 + 文字结论，返回给用户。可选调用 `export_html` 生成报告。

## 安全机制

- **标识符校验**：表名/列名仅允许 `[a-zA-Z_][a-zA-Z0-9_]*`
- **查询超时**：query_data 工具内置 60s 超时
- **结果截断**：query_data 内置 500 行限制；聚合查询不受此限
- **权限**：所有查询经 PermissionMiddleware 校验

## 大模型协作流程

1. Agent 识别跨库分析需求 → 激活本 Skill
2. Agent 确定分析任务（表名、指标、分组维度、关联条件）
3. Agent 用 query_data 分别查各库（聚合下推）
4. Agent 在内存中合并/对比/关联
5. Agent 基于结果生成结论和建议

## 示例：跨库订单对比

用户："对比库 A 和库 B 的订单量趋势"

Agent 执行：
```
1. query_data(connId="A", sql="SELECT DATE(create_time) AS d, COUNT(*) AS cnt FROM orders GROUP BY d")
2. query_data(connId="B", sql="SELECT DATE(create_time) AS d, COUNT(*) AS cnt FROM orders GROUP BY d")
3. 内存对齐：按日期 join 两个结果集
4. 计算差异：每日 cnt_B - cnt_A，百分比变化
5. 输出 Markdown 对比表 + 趋势结论
```

## 与旧版差异

旧版（v2.0）由 Python 脚本直连数据库，存在安全隐患（DSN 传递）且绕过权限审计。
新版由 Agent 用 query_data 工具取数，所有查询经 PermissionMiddleware + audit。
