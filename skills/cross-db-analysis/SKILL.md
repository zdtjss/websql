---
name: cross-db-analysis
description: 跨数据库/多 Schema 大数据量分析。支持连接多个数据库，在数据库端完成聚合计算，仅返回分析结论，避免将海量数据加载到大模型上下文中。适用于跨库关联、对比统计、复杂聚合等数据量可能超过 10 万行的场景。当用户涉及多个 schema 或数据库的分析、对比、统计时必须使用此技能。
---

# 跨库大数据量分析技能 v1.0

专门用于处理跨多个 schema/数据库的大数据量分析任务。核心设计原则是将聚合计算下沉到数据库端，仅返回统计结论，保护大模型上下文窗口不溢出。

## 架构概览

```
cross-db-analysis/
├── SKILL.md
└── scripts/
    ├── analyze.py          # 主入口 — CrossDBAnalyzer 类
    └── requirements.txt    # Python 依赖
```

## 核心能力

1. **多数据源连接** — 支持同时连接多个 MySQL/PostgreSQL/Oracle/SQLite 数据库
2. **SQL 聚合下推** — 在数据库端执行 COUNT/SUM/AVG/GROUP BY 等聚合，仅返回统计结果
3. **跨库对比** — 对不同 schema 的同名表执行对比分析，自动计算差异
4. **分块处理** — 对极大表自动分块处理，防止数据库连接超时
5. **JSON 输出** — 返回结构化的 JSON 分析结果，便于大模型解读

## 使用方式

```bash
python scripts/analyze.py \
  --config '{"sources":[{"connId":"1","schema":"public","dbType":"postgresql","dsn":"host=localhost dbname=db1 user=root password=xxx"},{"connId":"2","schema":"analytics","dbType":"mysql","dsn":"root:xxx@tcp(localhost:3306)/db2"}]}' \
  --task '{"type":"compare","tables":["orders","users"],"metrics":["count","sum:amount","avg:amount"],"groupBy":"date"}' \
  --output result.json
```

## 任务类型

| 类型 | 说明 |
|------|------|
| `aggregate` | 单表聚合统计（支持 GROUP BY、多指标） |
| `compare` | 多源同名表对比分析（自动计算差异百分比） |
| `join` | 跨库关联查询（使用数据库原生跨库 JOIN 语法） |
| `custom` | 自定义 SQL（直接传入 SQL 语句列表） |

## 大模型协作流程

1. 大模型识别到跨库大数据量分析需求
2. 大模型确定分析任务（表名、指标、分组维度）
3. 调用此技能执行数据库端聚合
4. 脚本返回紧凑 JSON 结果
5. 大模型基于结果进行解读和建议
