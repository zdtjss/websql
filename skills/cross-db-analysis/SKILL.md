---
name: cross-db-analysis
description: 跨数据库/多 Schema 数据分析与对比。当查询结果可能超过单次 500 行上限，或需要跨库/跨 Schema 关联、对比统计时使用。Agent 用 query_data 取数（聚合下推）、用 cross_db_join 完成跨库服务端关联，仅将紧凑统计结果写入上下文。用户涉及多个 schema 或数据库的分析、对比、统计且数据量较大时必须使用此技能。
version: "2.6.0"
min_agent_version: "1.0.0"
dependencies:
  - type: context
    name: connection_id
    description: 跨库分析需要至少一个数据库连接
  - type: context
    name: schema
    description: 跨库分析需要 schema 信息用于路由查询
error_hints:
  - pattern: "db conn not found"
    hint: "connId 无效。connId 支持 Schema 名（自动路由）或连接 ID（直接使用）"
    suggestion: "先用 list_tables / 上下文确认可用 schema 名，再用其作为 connId"
  - pattern: "query_data only supports"
    hint: "query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH，写操作被拒绝"
    suggestion: "改用 exec_sql 执行写操作（跨库分析场景应避免写操作）"
  - pattern: "timeout"
    hint: "查询超时（内置 60s）。可能是数据量过大或缺少索引"
    suggestion: "添加 WHERE 条件缩小范围，或使用聚合下推（GROUP BY）减少返回行数"
  - pattern: "syntax error"
    hint: "SQL 语法错误。注意跨库查询时各库方言可能不同"
    suggestion: "确认目标库类型，使用兼容的 SQL 语法（如 Oracle 用 FETCH FIRST n ROWS ONLY 而非 LIMIT）"
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

本 Skill 指导 Agent 完成跨数据库/多 Schema 的大数据量分析。**Agent 用 `query_data` 取数、用 `cross_db_join` 做跨库服务端关联，在 SQL 层面下推聚合，仅将紧凑统计结果写入上下文**，不加载海量明细。

## 核心原则

1. **聚合下推**：在数据库端执行 COUNT/SUM/AVG/GROUP BY，只返回统计结果
2. **分库查询**：用 `query_data` 的 `connId` 参数分别查多个库
3. **服务端关联**：跨库 JOIN 由 `cross_db_join` 工具在服务端完成 Hash Join，只返回统计与少量样本
4. **结果精简**：未带 LIMIT 的查询自动追加 LIMIT 500；显式 LIMIT 可提高取数上限但需控制上下文占用

## 数据契约（query_data 工具）

- **参数**：`sql`（必填）、`connId`（可选）
  - `connId` 不填 = 默认连接
  - `connId` 填 **Schema 名** = 自动路由到对应连接
  - `connId` 填 **连接 ID** = 直接使用该连接
- **返回结构**：`{"columns": [...], "data": [{...}], "count": n}`，data 为对象数组（列名 → 值）
- **行数行为**：
  - 未带 LIMIT 的 SELECT/WITH 会被自动追加 `LIMIT 500`
  - **SQL 已自带 LIMIT 时不再追加**，可显式写 `LIMIT 2000` 等提高取数上限（注意上下文大小）
  - GROUP BY 分组数超过 500 也会被截断，需先用 COUNT 评估分组规模
- **只读**：仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH，多语句会被拒绝
- **超时**：内置 60s，超时报错

## 工作流（Agent 必须按序执行）

### 步骤 1：评估数据规模

对涉及的表先做计数或小样本探测：

```sql
SELECT COUNT(*) AS cnt FROM <schema>.<table>
```

判断标准：
- 预估返回行数 > 500 → **必须聚合下推**，禁止全量拉取
- 大表 COUNT 本身可能超时，可先 `SELECT ... LIMIT 1` 探活，或用 `WHERE <时间列> >= <近期时间>` 缩小计数范围做近似评估
- GROUP BY 场景：先确认分组数规模，超过 500 组需加 WHERE 缩小范围或按维度拆批

### 步骤 2：选择任务类型

| 类型 | 适用场景 | 执行方式 |
|------|---------|---------|
| **aggregate** | 单表聚合统计 | 各库执行 GROUP BY 聚合 SQL |
| **compare** | 多源同名表对比 | 各库分别聚合，Agent 对齐对比 |
| **join** | 跨库/跨表关联 | 同库直接 SQL JOIN；跨库用 cross_db_join 工具（服务端 Hash Join） |
| **custom** | 自定义 SQL | 各库分别执行，Agent 合并结果 |

### 步骤 3：执行查询

用 `query_data` 工具，通过 `connId` 参数指定目标库：

```json
{"sql": "SELECT d, COUNT(*) AS cnt FROM orders GROUP BY d", "connId": "Schema_A"}
{"sql": "SELECT d, COUNT(*) AS cnt FROM orders GROUP BY d", "connId": "Schema_B"}
```

### 步骤 4：Agent 内存处理

#### aggregate 任务
直接汇总各库返回的聚合结果。注意：多库指标口径不一致时（时区、NULL 处理、币种），先归一化再对比。

#### compare 任务
1. 各库返回 `{columns, data, count}`，取出 data 中按维度对齐的指标
2. 以第一个数据源为基准，计算差异：
   - 绝对变化量：`metric_b - metric_a`
   - 百分比变化：`(metric_b - metric_a) / metric_a * 100`（基准为 0 时标注不可比）
3. 超过 2 个数据源时两两对比，不要只比前两个

#### join 任务

**同库 JOIN**（connId 相同，含同连接跨 schema）：直接写 SQL JOIN

```sql
SELECT a.id, b.name FROM schemaA.orders a JOIN schemaB.users b ON a.user_id = b.id
```

**跨库 JOIN**（connId 不同）：**优先调用 `cross_db_join` 工具**，服务端完成 Hash Join：

```json
{"leftConnId": "Schema_A", "leftTable": "orders", "leftKey": "user_id", "leftWhere": "status = 'ACTIVE' AND amount > 100", "leftSelect": ["id","user_id","amount"], "rightConnId": "Schema_B", "rightTable": "users", "rightKey": "id", "rightSelect": ["id","name"], "joinType": "inner", "metrics": [{"func":"sum","column":"left.amount"}]}
```

工具返回（紧凑结果，不占上下文）：
- `matchedRows` / `leftOnlyRows` / `rightOnlyRows`：各 join 类型的行数统计
- `metrics`：对匹配行的聚合指标（count/sum/avg/min/max，column 用 `left.`/`right.` 前缀）
- `sample`：匹配行的少量样本（默认 30 行，列加 `left_`/`right_` 前缀，长文本截断、数值保留 4 位）

参数能力：
- **复合 key**：`leftKeys`/`rightKeys` 数组支持多列联合匹配（如 `["user_id","order_date"]`），任一列为 NULL 不参与匹配
- **WHERE 过滤**：`leftWhere`/`rightWhere` 支持简单条件（列 + 比较符 + 字面量 + AND/OR/NOT/IN/BETWEEN/LIKE/IS NULL），**禁止函数调用、子查询、注释、多语句**，便于先缩小取数范围
- **schema 一致**：表名可带 schema 前缀（`schema.table`），但前缀 schema 必须与 connId 路由一致，否则工具会拒绝

要点：
- key 类型不一致（int vs str）时工具已自动归一化；NULL 不参与匹配（与 SQL JOIN 语义一致）
- 每侧取前 `limit` 行（默认 10000，最大 50000），**结果属抽样关联**，统计口径需向用户说明
- 仅当单侧数据量很小（≤ 500 行）且需要完整明细时，才退化为 query_data 分步拉数 + 内存关联

#### 异构库关联（Oracle + MySQL 混连）注意事项

cross_db_join 两侧可为任意库类型组合，服务端按各库方言分别取数后内存匹配。已知行为边界：
- **大整数 key（> 2^53）**：工具自动规范化 `s:` 保护标记，MySQL BIGINT/DOUBLE 与 Oracle NUMBER 可正确匹配；但 DOUBLE 存储超长 ID 本身丢精度（15-17 位有效数字），建议 key 用整数/字符串列
- **日期 key**：两侧统一为 `2006-01-02 15:04:05` 格式；一侧 DATE（00:00:00）与另一侧 DATETIME（有时分秒）语义不等，不会匹配（合理）
- **布尔 key**：MySQL BOOL（true/false）与 Oracle NUMBER(1)（0/1）**不会互相匹配**，请用整数/字符串列作为布尔 key
- **Oracle 标识符大小写**：Oracle 双引号区分大小写，表名/列名需与库中存储一致（通常大写），不确定时先 list_tables 确认
- **Oracle 版本**：取数使用 12c+ 的 FETCH FIRST 语法，Oracle 11g 及以下不支持（与 query_data 行为一致）
- **WHERE 过滤方言中立**：白名单仅允许「列 + 操作符 + 字面量」，两侧语法通用，无需区分方言

### 步骤 5：组织结论

把分析结果整理成 Markdown 表格 + 文字结论，返回给用户。标注数据来源（"来自 Schema_A 的数据..."）。可选调用 `export_html` 生成报告。

## 安全机制

- **语句白名单**：query_data 仅允许 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH，写操作走 exec_sql
- **多语句防护**：分号分隔的多条 SQL 会被拒绝
- **方言预检**：执行前检测不兼容语法（如 MySQL 上的 Oracle 函数），按提示改写
- **查询超时**：内置 60s
- **结果截断**：所有 SELECT/WITH 自动追加 LIMIT 500（含聚合查询，GROUP BY 超 500 组被截断）
- **权限与审计**：所有查询经 PermissionMiddleware 校验并记录 audit

## 失败处理

| 报错 | 处理方式 |
|------|---------|
| `db conn not found` | connId 无效，确认 Schema 名/连接 ID |
| 表不存在 | 先调 `list_tables` 确认表名和 schema 前缀 |
| 方言/语法错误 | 按错误提示改写为目标库兼容语法 |
| 超时 | 加 WHERE 缩小范围或聚合下推；大表 COUNT 改用小样本探测 |
| 数据不一致 | 多库指标口径不同（时区/NULL/类型）时，先归一化再对比，并在结论中说明 |

## 示例：跨库订单对比

用户："对比库 A 和库 B 的订单量趋势"

Agent 执行：

```
1. query_data(connId="Schema_A", sql="SELECT DATE(create_time) AS d, COUNT(*) AS cnt FROM orders GROUP BY d")
2. query_data(connId="Schema_B", sql="SELECT DATE(create_time) AS d, COUNT(*) AS cnt FROM orders GROUP BY d")
3. 内存对齐：按日期 join 两个结果集
4. 计算差异：每日 cnt_B - cnt_A，百分比变化
5. 输出 Markdown 对比表 + 趋势结论
```

## 变更记录

- **v2.6**：修正行数上限表述（工具仅对未带 LIMIT 的查询追加 500，显式 LIMIT 不被覆盖）；修正大表 COUNT 建议（唯一索引>0 会漏计 0/负值，改用 LIMIT 1 探活或时间列近似评估）
- **v2.5**：异构库支持加固——大整数 key 规范化（MySQL s:%f 与 Oracle s:%d 匹配）、FETCH 关键字入 where 黑名单（防行数限制绕过）；补充异构库注意事项
- **v2.4**：cross_db_join 支持复合 key（leftKeys/rightKeys 多列联合匹配）、两侧 WHERE 过滤（白名单校验防注入）、schema-connId 一致性校验
- **v2.3**：新增 cross_db_join 服务端跨库关联工具（服务端 Hash Join + 紧凑输出），跨库 join 不再依赖 Agent 内存关联
- **v2.2**：移除直连数据库的 analyze.py 脚本（安全风险、方言兼容缺陷），本 Skill 完全基于 query_data 工具编排；补充数据契约、行数限制、失败处理章节
- **v2.1**：由 Python 脚本直连改为 Agent 用 query_data 取数（此前版本存在 DSN 传递安全隐患且绕过权限审计）
