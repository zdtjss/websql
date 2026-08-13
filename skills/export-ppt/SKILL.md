---
name: export-ppt
description: 生成专业数据分析 PPT 演示文稿（.pptx，模板驱动）。Agent 负责用 query_data 取数并计算统计指标，本 Skill 的 Python 脚本将内容填充到 slides_template.pptx 母版/版式（封面/目录/KPI/图表/表格页的科技感深色主题）。当用户需要 PPT/幻灯片时使用。
version: "2.0.0"
min_agent_version: "1.0.0"
error_hints:
  - pattern: "ModuleNotFoundError"
    hint: "Python 依赖缺失。依赖应随部署预装（pip install -r scripts/requirements.txt，含 python-pptx/matplotlib）"
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
  - pattern: "name 'headers' is not defined"
    hint: "脚本 bug 已修复，请确认使用的是最新版脚本"
    suggestion: "重新加载 skill"
command_blacklist:
  - DROP DATABASE
  - DROP SCHEMA
  - TRUNCATE
  - SHUTDOWN
---

# PPT 演示文稿生成 Skill（模板驱动）

本 Skill 将内容填充到**模板文件** `templates/slides_template.pptx`（16:9 深色主题母版 + 版式）。**Agent 负责取数与统计计算，Python 脚本负责按版式填充占位符与布局自适应。**

## 模板与换肤

- 模板位置：`skills/export-ppt/templates/slides_template.pptx`
- **换肤/换版式**：直接用 PowerPoint/WPS 编辑模板文件（改母版配色、版式占位符位置），或修改 `skills/lib/build_templates.py` 后重新生成——渲染脚本零代码改动
- 版式索引约定（与脚本对齐）：0=封面 1=内容/目录/表格/KPI 2=章节分隔 5=图表 6=致谢

## 工作流（Agent 必须按序执行）

1. **取数**：用 `query_data` 工具执行用户的 SELECT SQL，获得 `columns` 和 `data`
2. **计算统计**：Agent 自行计算 highlights（规则见下文）
3. **组装 JSON**：按"输入数据契约"组装 JSON
4. **执行脚本**：用 `execute` 工具（Eino Filesystem Middleware 提供）运行：
   ```
   python <skills 目录>/export-ppt/scripts/export_ppt.py
   ```
   JSON 传入方式二选一（execute 工具不支持直接向子进程 stdin 写入）：
   - **重定向**：先用 `write_file` 把 JSON 写入临时文件（如 /tmp/ppt_input.json），再执行 `python <脚本路径> < /tmp/ppt_input.json`
   - **位置参数**：直接执行 `python <脚本路径> /tmp/ppt_input.json`（脚本自动读取首参数文件）
5. **解析输出**：脚本 stdout 返回 `{"success":true,"outputPath":"...","slideCount":15}` 或 `{"success":false,"error":"..."}`
6. **返回链接**：把 outputPath 转成下载链接 `/exports/<文件名>.pptx` 返回用户

## 依赖说明

依赖（python-pptx/matplotlib/numpy/Pillow，精确版本见 scripts/requirements.txt）应随部署预装。
若报 `ModuleNotFoundError`，说明部署不完整，告知用户补充安装依赖，不要反复尝试运行时安装。

## 输入数据契约（JSON）

### data 模式（从 SQL 取数）

```json
{
  "mode": "data",
  "title": "PPT 标题",
  "columns": ["month", "revenue", "cost"],
  "data": [{"month": "2026-01", "revenue": 10000, "cost": 8000}],
  "highlights": ["revenue — 平均: 11000, 峰值: 15000", "cost — 平均: 7500, 峰值: 9000"],
  "outputPath": "/exports/slides_20260619.pptx"
}
```

说明：脚本只消费 `columns` / `data` / `highlights` / `chartPaths`（可选）。
`data` 含"表名/table_name/表名称"列时按表名分组生成表格页，否则生成数据概览图 + 明细页。

### content 模式（从 Markdown 文本生成）

```json
{
  "mode": "content",
  "title": "PPT 标题",
  "markdown": "# 摘要\n\n- 要点1\n- 要点2\n\n## 数据明细\n\n| 列A | 列B |\n|---|---|\n| 1 | 2 |\n",
  "outputPath": "/exports/slides_20260619.pptx"
}
```

`markdown` 字段（推荐）：脚本内置 Markdown 解析（skills/lib/md_blocks.py），`#` 标题分节、列表、表格、代码块自动映射为对应版式页面。
也可使用 `sections` 字段（兼容旧契约）：

```json
{
  "mode": "content",
  "title": "PPT 标题",
  "sections": [
    {"title": "章节标题", "level": 2, "blocks": [{"type": "paragraph", "content": "要点1"}]}
  ],
  "outputPath": "/exports/slides_20260619.pptx"
}
```

## 统计字段计算规则（Agent 自行计算）

- **highlights**：基于数据生成 5-8 条亮点，格式 `"列名 — 平均: X, 峰值: Y"`
- **chartPaths**：可选。指向已存在的 PNG 文件路径（由其他工具预生成），脚本会插入前 3 张

## 图表生成（内置）

export_ppt.py 内置 matplotlib 图表生成能力（实现见 skills/lib/chart_common.py 共享模块）：
- data 模式自动生成"数据分布"柱状图
- content 模式 `chart` block 按 `chartType` 渲染
- 自定义图表可用 Go 原生 `export_excel_with_chart` 预生成 PNG

## 失败处理

- 脚本返回 `success: false` → 改用 export_ppt 工具（Python 优先、无 Python 时自动降级 Go 基础版，导出不失败）
- Python 不可用 → 改用 export_ppt 工具（工具内置 Go 基础版兜底，无 Python 依赖）

## 输出路径规则

- outputPath 必须以 `/exports/` 开头，文件名含时间戳
- 示例：`/exports/slides_20260619_120000.pptx`

## 典型 PPT 结构（12-18 页）

封面 → 目录 → 背景 → KPI 概览 → 趋势分析 → 对比分析 → 构成分析 → 分布分析 → 洞察 → 风险 → 建议 → 总结 → 致谢
