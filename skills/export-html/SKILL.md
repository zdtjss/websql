---
name: export-html
description: 生成交互式 HTML 数据分析报告。支持 Markdown 渲染、Mermaid 图表（可缩放/全屏）、代码高亮、数学公式。Agent 负责组织 Markdown 内容，调用 export_html 工具生成。当用户需要 HTML 报告或可交互文档时使用。
version: "1.1.0"
min_agent_version: "1.0.0"
dependencies:
  - type: context
    name: connection_id
    description: 若使用 sql 模式需已建立数据库连接
error_hints:
  - pattern: "syntax error"
    hint: "Markdown 语法错误。请检查表格格式、代码块闭合、Mermaid 语法"
    suggestion: "简化 Markdown 内容后重试"
  - pattern: "template"
    hint: "HTML 模板渲染失败。可能是 Markdown 中包含特殊字符"
    suggestion: "移除 Markdown 中的 < > & 等特殊字符或用反引号包裹"
  - pattern: "memory"
    hint: "内存不足。可能是 Markdown 内容过大"
    suggestion: "减少内容量或拆分为多个报告"
command_blacklist:
  - DROP DATABASE
  - DROP SCHEMA
  - SHUTDOWN
---

# HTML 报告生成 Skill

Agent 负责组织 Markdown 内容，export_html 工具负责渲染为交互式 HTML。

## 工作流

### 场景 A：用户提供了分析文本（content 模式）

1. 把分析结论整理成 Markdown 文本
2. 调用 `export_html` 工具：
   ```json
   { "content": "<Markdown>", "fileName": "report", "title": "报告标题" }
   ```

### 场景 B：用户提供了 SQL（sql 模式）

1. 调用 `export_html` 工具，传入 SQL：
   ```json
   { "sql": "<SELECT SQL>", "fileName": "report", "title": "报告标题" }
   ```
   工具自动查询并生成统计摘要 + 数据明细表格

### 场景 C：需要深度分析（推荐）

1. 用 `query_data` 取数
2. Agent 基于数据生成 Markdown 分析报告（含 mermaid 流程图、表格、结论）
3. 调用 `export_html` 工具，content 模式传入分析 Markdown

## 渲染能力

- **Mermaid 图表**：自动渲染为可交互 SVG（缩放/全屏/拖拽/查看源码）
- **数学公式**：KaTeX（`$...$` 行内，`$$...$$` 块级）
- **代码高亮**：highlight.js
- **GFM 扩展**：表格、任务列表、引用、图片

## 与其他 Skill 对比

| Skill | 输出格式 | 优势 | 适用场景 |
|-------|---------|------|---------|
| export-html | .html | 交互式、支持 mermaid 缩放、无需 Python | 在线浏览、复杂图表 |
| export-word | .docx | 可编辑、正式 | 正式报告、打印 |
| export-ppt | .pptx | 演示导向 | 汇报、演讲 |
