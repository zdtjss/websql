---
name: export-ppt
description: 生成专业数据分析 PPT 演示文稿（.pptx）。Agent 负责用 query_data 取数并计算统计指标，本 Skill 的 Python 脚本负责渲染成带封面、目录、图表页的科技感深色主题 PPT。当用户需要 PPT/幻灯片时使用。
version: "1.2.0"
min_agent_version: "1.0.0"
dependencies:
  - type: context
    name: connection_id
    description: 需要 query_data 已建立数据库连接
  - type: skill
    name: query_data
    description: 导出前需先用 query_data 取数
error_hints:
  - pattern: "ModuleNotFoundError"
    hint: "Python 依赖缺失。请先用 execute 工具运行 pip install -r requirements.txt 安装依赖后重试"
    suggestion: "或回退到 export_ppt 工具（Go 原生兜底）"
  - pattern: "UnicodeEncodeError"
    hint: "Python 编码错误（Windows 常见 gbk 问题）。建议设置 PYTHONIOENCODING=utf-8"
    suggestion: "或回退到 export_ppt 工具"
  - pattern: "PermissionError"
    hint: "文件写入权限不足。请确认 outputPath 以 /exports/ 开头"
    suggestion: "检查输出目录是否存在，或联系管理员"
  - pattern: "timeout"
    hint: "脚本执行超时。可能是数据量过大导致图表生成缓慢"
    suggestion: "减少数据量或减少 chartPages 数量"
  - pattern: "SyntaxError"
    hint: "Python 脚本语法错误。可能是 Python 版本不兼容"
    suggestion: "回退到 export_ppt 工具（Go 原生兜底）"
command_blacklist:
  - DROP DATABASE
  - DROP SCHEMA
  - TRUNCATE
  - SHUTDOWN
---

# PPT 演示文稿生成 Skill

本 Skill 将结构化数据渲染为科技感深色主题 PPT。**Agent 负责取数与统计计算，Python 脚本只负责幻灯片渲染**。

## 工作流（Agent 必须按序执行）

1. **取数**：用 `query_data` 工具执行用户的 SELECT SQL，获得 `columns` 和 `data`
2. **计算统计**：Agent 自行计算 summary 和 highlights（规则见下文）
3. **组装 JSON**：按"输入数据契约"组装 stdin JSON
4. **执行脚本**：用 `execute` 工具（Eino Filesystem Middleware 提供）运行：
   ```
   python <本 SKILL 目录>/scripts/export_ppt.py
   ```
   JSON 通过 stdin 传入
5. **解析输出**：脚本 stdout 返回 `{"success":true,"outputPath":"...","slideCount":15}` 或 `{"success":false,"error":"..."}`
6. **返回链接**：把 outputPath 转成下载链接 `/exports/<文件名>.pptx` 返回用户

## 依赖安装（首次执行前）

若 Python 脚本报 `ModuleNotFoundError`，先用 `execute` 工具安装依赖：
```
pip install -r <本 SKILL 目录>/scripts/requirements.txt
```
安装完成后重试脚本执行。

## 输入数据契约（stdin JSON）

### data 模式（从 SQL 取数）

```json
{
  "mode": "data",
  "title": "PPT 标题",
  "columns": ["month", "revenue", "cost"],
  "data": [{"month": "2026-01", "revenue": 10000, "cost": 8000}],
  "summary": {
    "totalRows": 12,
    "totalCols": 3,
    "columns": ["month", "revenue", "cost"],
    "stats": {
      "revenue": {"min": 8000, "max": 15000, "avg": 11000},
      "cost": {"min": 6000, "max": 9000, "avg": 7500}
    }
  },
  "numericColumns": ["revenue", "cost"],
  "chartPaths": ["/exports/report_ppt_chart.png"],
  "highlights": ["revenue — 平均: 11000, 峰值: 15000", "cost — 平均: 7500, 峰值: 9000"],
  "outputPath": "/exports/slides_20260619.pptx"
}
```

### content 模式（从 Markdown 文本生成）

```json
{
  "mode": "content",
  "title": "PPT 标题",
  "sections": [
    {"title": "章节标题", "blocks": [{"type": "paragraph", "content": "要点1"}]}
  ],
  "outputPath": "/exports/slides_20260619.pptx"
}
```

## 统计字段计算规则（Agent 自行计算）

- **numericColumns**：首行值可转为 float 的列名列表
- **summary**：
  - `totalRows` / `totalCols`：数据行列数
  - `stats`：对每个 numericColumn 计算 min/max/avg
- **highlights**：基于 stats 生成 5-8 条亮点，格式 `"列名 — 平均: X, 峰值: Y"`
- **chartPaths**：预留字段，当前 export_ppt.py 已内置 matplotlib 图表生成，无需单独生成 PNG

## 图表生成（内置）

export_ppt.py 已内置 matplotlib 图表生成能力，无需单独执行图表脚本。
在 `chartPages` 中指定图表类型和数据，脚本会自动生成图表并嵌入 PPT。
支持的图表类型：line / bar / horizontal_bar / pie / donut / scatter / radar / heatmap / area / stacked_bar

## 失败处理

- 脚本返回 `success: false` → 告知用户失败原因，建议改用 `export_html` 工具
- Python 不可用 → 直接回退到 `export_ppt` 工具（Go 原生 PPT 生成）

## 依赖

`pip install python-pptx matplotlib numpy Pillow`

## 输出路径规则

- outputPath 必须以 `/exports/` 开头，文件名含时间戳
- 示例：`/exports/slides_20260619_120000.pptx`

## 典型 PPT 结构（12-18 页）

封面 → 目录 → 背景 → KPI 概览 → 趋势分析 → 对比分析 → 构成分析 → 分布分析 → 洞察 → 风险 → 建议 → 总结 → 致谢
