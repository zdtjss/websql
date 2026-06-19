---
name: export-word
description: 生成专业数据分析 Word 报告（.docx）。Agent 负责用 query_data 取数并计算统计指标，本 Skill 的 Python 脚本负责渲染成带封面、目录、图表、表格的科技感 Word 文档。当用户需要 Word/PDF 报告时使用。
---

# Word 报告生成 Skill

本 Skill 将结构化数据渲染为专业 Word 文档。**Agent 负责取数与统计计算，Python 脚本只负责文档渲染**。

## 工作流（Agent 必须按序执行）

1. **取数**：用 `query_data` 工具执行用户的 SELECT SQL，获得 `columns` 和 `data`
2. **计算统计**：Agent 自行计算以下字段（规则见下文）
3. **组装 JSON**：按"输入数据契约"组装 stdin JSON
4. **执行脚本**：用 `execute` 工具（Eino Filesystem Middleware 提供）运行：
   ```
   python <本 SKILL 目录>/scripts/word_generator.py
   ```
   JSON 通过 stdin 传入（execute 工具支持 stdin）
5. **解析输出**：脚本 stdout 返回 `{"success":true,"outputPath":"..."}` 或 `{"success":false,"error":"..."}`
6. **返回链接**：把 outputPath 转成下载链接 `/exports/<文件名>.docx` 返回用户

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
  "title": "报告标题",
  "columns": ["id", "name", "amount"],
  "data": [{"id": 1, "name": "foo", "amount": 100}],
  "numericColumns": ["amount"],
  "numericStats": [
    {"column": "amount", "count": 100, "min": 10.0, "max": 9999.0, "avg": 1234.56, "stddev": 15.0}
  ],
  "findings": ["amount 平均值 1234.56，峰值 9999.0，波动较大", "数据质量良好"],
  "chartPaths": ["/exports/report_chart.png"],
  "outputPath": "/exports/report_20260619.docx",
  "includeCharts": true
}
```

### content 模式（从 Markdown 文本生成）

```json
{
  "mode": "content",
  "title": "报告标题",
  "sections": [
    {"title": "章节标题", "blocks": [{"type": "paragraph", "content": "正文"}]}
  ],
  "outputPath": "/exports/report_20260619.docx"
}
```

#### 支持的 block 类型

| type | content 字段 | 说明 |
|------|-------------|------|
| `text` / `paragraph` | 字符串 | 普通段落 |
| `heading` | 字符串 | 标题，需额外提供 `level`（1-4） |
| `h1` / `h2` / `h3` | 字符串 | 快捷标题（h1→1级, h2→2级, h3→3级） |
| `bullet` / `list` | 字符串（`\n` 分隔） | 无序列表 |
| `table` | 字符串（Markdown 表格）或 list（`[[表头...], [行...]]`） | 数据表格 |
| `chart` | 无需 content | 图表，需提供 `chartType`（bar/pie/horizontal_bar/line）、`title`、`data: {labels:[], values:[]}` |
| `code` | 字符串 | 代码块 |

## 统计字段计算规则（Agent 自行计算）

- **numericColumns**：首行值可转为 float 的列名列表
- **numericStats**：对每个 numericColumn 计算：
  - `count`：有效数值个数
  - `min` / `max` / `avg`：最小/最大/平均值
  - `stddev`：样本标准差（除以 n-1）
- **findings**：基于 numericStats 生成 3-5 条洞察，例如：
  - "amount 平均值 1234.56，峰值 9999.0，波动较大"
  - "数据分布右偏，存在极端高值"
- **chartPaths**：预留字段，当前 word_generator.py 已内置 matplotlib 图表生成，无需单独生成 PNG。如需图表，在 `includeCharts: true` 时脚本会自动根据数据生成

## 图表生成（内置）

word_generator.py 已内置 matplotlib 图表生成能力，无需单独执行图表脚本。
在输入 JSON 中设置 `includeCharts: true`，脚本会根据数据自动生成合适的图表并嵌入报告。
支持的图表类型：line / bar / horizontal_bar / pie / donut / scatter / radar / heatmap / area / stacked_bar

## 失败处理

- 脚本返回 `success: false` → 告知用户失败原因，建议改用 `export_html` 工具（Go 原生 HTML 报告）
- Python 不可用 → 直接回退到 `export_analysis_docx` 工具（Go 原生 Word 生成）

## 依赖

`pip install python-docx matplotlib numpy Pillow`

## 输出路径规则

- outputPath 必须以 `/exports/` 开头，文件名含时间戳
- 示例：`/exports/report_20260619_120000.docx`

## 典型报告结构

封面 → 目录 → 摘要 → 背景与目标 → 数据概览(KPI) → 详细分析(含图表) → 问题与风险 → 建议 → 结论
