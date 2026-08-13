---
name: export-word
description: 生成专业数据分析 Word 报告（.docx，模板驱动）。Agent 负责用 query_data 取数并计算统计指标，本 Skill 的 Python 脚本将结构化数据渲染到 report_template.docx 模板（封面/目录/样式集/图表/表格）。当用户需要 Word/PDF 报告时使用。
version: "2.0.0"
min_agent_version: "1.0.0"
error_hints:
  - pattern: "ModuleNotFoundError"
    hint: "Python 依赖缺失。依赖应随部署预装（pip install -r scripts/requirements.txt，含 python-docx/docxtpl/matplotlib）"
    suggestion: "告知用户部署环境缺少 Python 依赖，不要反复尝试运行时安装"
  - pattern: "UnicodeEncodeError"
    hint: "Python 编码错误（Windows 常见 gbk 问题）"
    suggestion: "检查脚本是否以 UTF-8 输出（脚本已内置 reconfigure，若仍报错请反馈）"
  - pattern: "PermissionError"
    hint: "文件写入权限不足。请确认 outputPath 以 /exports/ 开头"
    suggestion: "检查输出目录是否存在，或联系管理员"
  - pattern: "SyntaxError"
    hint: "Python 脚本语法错误。可能是 Python 版本不兼容"
    suggestion: "重新加载 skill 后重试"
  - pattern: "ZeroDivisionError"
    hint: "脚本 bug 已修复，请确认使用的是最新版脚本"
    suggestion: "重新加载 skill"
command_blacklist:
  - DROP DATABASE
  - DROP SCHEMA
  - TRUNCATE
  - SHUTDOWN
---

# Word 报告生成 Skill（模板驱动）

本 Skill 将结构化数据渲染到**模板文件** `templates/report_template.docx`（docxtpl 模板：封面占位、目录域、样式集、正文骨架）。**Agent 负责取数与统计计算，Python 脚本负责模板填充与表格/图片插入。**

## 模板与换肤

- 模板位置：`skills/export-word/templates/report_template.docx`
- **换肤/换版式**：直接用 Word 编辑模板文件（改样式集、封面布局、配色），或修改 `skills/lib/build_templates.py` 后重新生成——渲染脚本零代码改动
- 样式集：Normal（微软雅黑 11pt）/ Heading 1-4 / List Bullet / 表格斑马纹 / 页眉页脚页码

## 工作流（Agent 必须按序执行）

1. **取数**：用 `query_data` 工具执行用户的 SELECT SQL，获得 `columns` 和 `data`
2. **计算统计**：Agent 自行计算以下字段（规则见下文）
3. **组装 JSON**：按"输入数据契约"组装 JSON
4. **执行脚本**：用 `execute` 工具（Eino Filesystem Middleware 提供）运行：
   ```
   python <skills 目录>/export-word/scripts/word_generator.py
   ```
   JSON 传入方式二选一（execute 工具不支持直接向子进程 stdin 写入）：
   - **重定向**：先用 `write_file` 把 JSON 写入临时文件（如 /tmp/word_input.json），再执行 `python <脚本路径> < /tmp/word_input.json`
   - **位置参数**：直接执行 `python <脚本路径> /tmp/word_input.json`（脚本自动读取首参数文件）
5. **解析输出**：脚本 stdout 返回 `{"success":true,"outputPath":"..."}` 或 `{"success":false,"error":"..."}`
6. **返回链接**：把 outputPath 转成下载链接 `/exports/<文件名>.docx` 返回用户

## 依赖说明

依赖（python-docx/docxtpl/matplotlib/numpy/Pillow，精确版本见 scripts/requirements.txt）应随部署预装。
若报 `ModuleNotFoundError`，说明部署不完整，告知用户补充安装依赖，不要反复尝试运行时安装。

## 输入数据契约（JSON）

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
  "markdown": "# 摘要\n\n正文...\n\n## 数据明细\n\n| 列A | 列B |\n|---|---|\n| 1 | 2 |\n",
  "outputPath": "/exports/report_20260619.docx"
}
```

`markdown` 字段（推荐）：脚本内置 Markdown 解析（skills/lib/md_blocks.py），支持标题分节、段落、列表、表格、代码块。
也可使用 `sections` 字段（兼容旧契约）：

```json
{
  "mode": "content",
  "title": "报告标题",
  "sections": [
    {"title": "章节标题", "level": 2, "blocks": [{"type": "paragraph", "content": "正文"}]}
  ],
  "outputPath": "/exports/report_20260619.docx"
}
```

#### 支持的 block 类型

| type | content 字段 | 说明 |
|------|-------------|------|
| `text` / `paragraph` | 字符串 | 普通段落 |
| `heading` | 字符串 | 子标题 |
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
- **chartPaths**：可选。指向已存在的 PNG 文件路径（配合 `includeCharts: true` 插入）

## 图表生成

- data 模式：脚本自动检测数值列生成柱状图（无需外部 PNG）
- content 模式：`chart` block 由内置 matplotlib 渲染（实现见 skills/lib/chart_common.py 共享模块）
- 外部 PNG：`chartPaths` 配合 `includeCharts: true` 插入

## 失败处理

- 脚本返回 `success: false` → 改用 export_analysis_docx 工具（Python 优先、无 Python 时自动降级 Go 基础版，导出不失败）
- Python 不可用 → 改用 export_analysis_docx 工具（工具内置 Go 基础版兜底，无 Python 依赖）

## 输出路径规则

- outputPath 必须以 `/exports/` 开头，文件名含时间戳
- 示例：`/exports/report_20260619_120000.docx`

## 典型报告结构

封面 → 目录 → 摘要 → 背景与目标 → 数据概览(KPI) → 详细分析(含图表) → 问题与风险 → 建议 → 结论
