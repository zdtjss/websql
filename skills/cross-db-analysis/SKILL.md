---
name: cross-db-analysis
description: 跨数据库/多 Schema 大数据量分析。支持连接多个数据库，在数据库端完成聚合计算，仅返回分析结论，避免将海量数据加载到大模型上下文中。适用于跨库关联、对比统计、复杂聚合等数据量可能超过 10 万行的场景。当用户涉及多个 schema 或数据库的分析、对比、统计时必须使用此技能。
---

# 跨库大数据量分析技能 v2.0

专门用于处理跨多个 schema/数据库的大数据量分析任务。核心设计原则是将聚合计算下沉到数据库端，仅返回统计结论，保护大模型上下文窗口不溢出。

## 架构概览

```
cross-db-analysis/
├── SKILL.md
└── scripts/
    ├── analyze.py          # 主入口 — 连接池 + 多任务类型
    └── requirements.txt    # Python 依赖
```

## 核心能力

1. **多数据源连接** — 支持同时连接多个 MySQL/PostgreSQL/Oracle/SQLite 数据库
2. **SQL 聚合下推** — 在数据库端执行 COUNT/SUM/AVG/GROUP BY 等聚合，仅返回统计结果
3. **跨库对比** — 对不同 schema 的同名表执行多指标对比分析，自动计算差异百分比
4. **跨库关联** — 同连接直接 JOIN，跨连接 Python 侧 Hash Join
5. **连接池管理** — 自动复用数据库连接，避免频繁建连/断连
6. **查询超时** — 支持按任务设置查询超时，防止大表查询挂起
7. **结果大小控制** — 自动截断超大结果集，保护大模型上下文窗口
8. **JSON 输出** — 返回结构化的 JSON 分析结果，便于大模型解读

## 使用方式

### 方式一：stdin JSON（推荐）

```bash
echo '{"config":{"sources":[...]},"task":{"type":"compare",...}}' | python scripts/analyze.py
```

### 方式二：命令行参数（向后兼容）

```bash
python scripts/analyze.py \
  --config '{"sources":[{"connId":"1","schema":"public","dbType":"postgresql","dsn":"host=localhost dbname=db1 user=root password=xxx"},{"connId":"2","schema":"analytics","dbType":"mysql","dsn":"root:xxx@tcp(localhost:3306)/db2"}]}' \
  --task '{"type":"compare","tables":["orders","users"],"metrics":["count","sum:amount","avg:amount"],"groupBy":"date"}' \
  --output result.json
```

## 任务类型

| 类型 | 说明 |
|------|------|
| `aggregate` | 单表聚合统计（支持 GROUP BY、多指标、可配置 LIMIT） |
| `compare` | 多源同名表多指标对比分析（自动计算差异百分比） |
| `join` | 跨库关联查询（同连接直接 JOIN，跨连接 Hash Join） |
| `custom` | 自定义 SQL（直接传入 SQL 语句列表） |

## 任务参数详解

### aggregate

```json
{
  "type": "aggregate",
  "tables": ["orders", "users"],
  "metrics": ["count", "sum:amount", "avg:amount"],
  "groupBy": "date",
  "limit": 1000,
  "timeout": 120
}
```

### compare

```json
{
  "type": "compare",
  "tables": ["orders", "users"],
  "metrics": ["count", "sum:amount", "avg:amount"],
  "timeout": 120
}
```

compare 结果自动包含 `diff` 字段，计算第一个与第二个数据源的指标差异（绝对变化量 + 百分比变化）。

### join

```json
{
  "type": "join",
  "join": {
    "leftSource": "1",
    "rightSource": "2",
    "leftTable": "orders",
    "rightTable": "users",
    "leftKey": "user_id",
    "rightKey": "id",
    "joinType": "inner",
    "select": ["left_order_id", "right_name"],
    "limit": 5000
  },
  "timeout": 120
}
```

| 参数 | 说明 |
|------|------|
| `leftSource` / `rightSource` | 数据源 connId |
| `leftTable` / `rightTable` | 左/右表名 |
| `leftKey` / `rightKey` | JOIN 键列名 |
| `joinType` | `inner` / `left` / `right` / `full` |
| `select` | 可选，指定返回列（默认全部） |
| `limit` | 结果行数上限 |

**同连接 JOIN**：当 leftSource 和 rightSource 的 connId 相同时，直接生成 SQL JOIN 语句在数据库端执行。

**跨连接 Hash Join**：当 connId 不同时，分步查询两侧数据，Python 侧构建 Hash 索引完成关联，结果列自动加 `left_` / `right_` 前缀避免冲突。

### custom

```json
{
  "type": "custom",
  "queries": [
    {"sourceIndex": 0, "sql": "SELECT COUNT(*) AS cnt FROM orders WHERE status='active'"},
    {"sourceIndex": 1, "sql": "SELECT AVG(amount) AS avg_amt FROM payments"}
  ],
  "timeout": 120
}
```

### 通用参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `timeout` | 查询超时秒数，0 表示不限制 | 120 |
| `limit` | 结果行数上限（aggregate/join） | 5000 |

## 安全机制

- **标识符校验** — 表名、列名、Schema 名仅允许 `[a-zA-Z_][a-zA-Z0-9_]*`，防止 SQL 注入
- **查询超时** — PostgreSQL 使用 `statement_timeout`，MySQL 使用 `max_execution_time`
- **结果截断** — 最大 5000 行 / 512KB，超出自动截断并在返回中标记 `truncated: true`

## 输出格式

```json
{
  "success": true,
  "taskType": "compare",
  "executionTimeMs": 1234,
  "results": { ... },
  "error": null
}
```

## 大模型协作流程

1. 大模型识别到跨库大数据量分析需求
2. 大模型确定分析任务（表名、指标、分组维度、关联条件）
3. 调用此技能执行数据库端聚合
4. 脚本返回紧凑 JSON 结果（含执行耗时、截断标记）
5. 大模型基于结果进行解读和建议
