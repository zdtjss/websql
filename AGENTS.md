# Agents.md — Agent 运行时参考规范

本文件由 agentsmd 中间件在每次模型调用前注入，内容为瞬态参考（不进入会话状态、不被 summarization 压缩）。

## 数据导入流程

数据导入已迁移至「表数据浏览」页面，AI 对话中不支持导入。
当用户要求导入时，告知：在左侧数据库树中找到目标表 → 右键「浏览数据」→ 工具栏「导入」按钮。支持 Excel/CSV/JSON，字段映射预览，新增/更新两种模式。

## 数据文件分析流程

用户上传文件后，意图由提问决定，不要默认当作导入：
1. **仅分析文件数据**：调用 read_file_data 读取（默认 100 行、最大 500 行）后分析
2. **结合数据库分析**：同时调 read_file_data + query_data 进行关联对比
3. **要求导入**：引导到表数据浏览页面（见上）
4. read_file_data 只读，可多次分页调用

## 数据可视化（Mermaid）

在回复中使用 ` + "```mermaid```" + ` 代码块，前端自动渲染 SVG。

**规则**：
1. 先文字结论后图表，图表是补充不是替代
2. 简洁优先，节点 ≤ 15 个
3. 选对类型：占比→pie，流程→flowchart，关系→erDiagram，趋势→xychart-beta，时序→sequenceDiagram
4. 确保语法正确，避免冷门语法
5. 导出含图表的报告用 export_html（支持 Mermaid 渲染）
6. **禁止**使用 `---` config front-matter 块（渲染器不支持，会导致解析失败）
7. **禁止**使用 `legend` 指令（mermaid 11.x 无此语法，会导致渲染失败）

**xychart-beta 必须遵守**：
- y-axis / x-axis 范围分隔符必须用 `-->` 箭头（如 `y-axis "标签" 0 --> 100`），**禁止**用 `--`
- 不要混入 dateFormat / axisFormat / numberFormat 等 gantt 语法
- title 含中文或特殊字符时必须加双引号：`title "含中文的标题"`
- 正确示例：
  ```
  xychart-beta
      title "月度销售额"
      x-axis ["1月", "2月", "3月"]
      y-axis "金额(万)" 0 --> 500
      bar [120, 300, 450]
  ```

**pie 必须遵守**：
- 数值只写纯数字，**禁止**加 `%` 符号（如 `"类别" : 45` 而非 `"类别" : 45%`）
- title 不加引号：`pie title 占比分布`

**sequenceDiagram 必须遵守**：
- participant/actor 的 as 别名不加引号：`participant A as 用户` 而非 `participant A as "用户"`

**flowchart / graph 必须遵守**：
- 节点文本含特殊字符时用引号包裹：`A["含(括号)的文本"]`
- 子图 subgraph 标题含中文时用引号：`subgraph "中文标题"`

**timeline 必须遵守**：
- 日期与事件用 ` : ` 分隔，日期部分不含时间（如 `2026-06-04 : 发起申请`）
- 若需带时间，将时间放在事件描述中：`2026-06-04 : 09:12 发起申请`

**通用禁止**：
- 禁止在 mermaid 代码块内再嵌套 ` + "```" + ` 标记
- 禁止使用 %%{init:...}%% 配置指令（渲染器使用预设主题，自定义配置会被忽略或导致错误）
- 禁止使用 mermaid 不支持的图表类型（如 funnel、treeview）

## 导出工具选择

**决策路径**：
- Word/PPT 报告 → export_analysis_docx / export_ppt（模板驱动专业版，Go 薄封装内部 fork Python 渲染器）；需要更细粒度自定义（sections/blocks）时用 skill:export-word/export-ppt
- HTML 报告 → export_html（直接用，支持 Markdown/Mermaid/KaTeX）
- Excel 数据 → export_excel
- Excel + 图表 → export_excel_with_chart
- 跨库深度分析 → skill:cross-db-analysis

**最佳实践**：
1. 优先 content 模式（直接传分析文本），避免重复查询
2. 先确认查询结果正确再导出
3. 导出内容应包含表格 + 分析结论 + 图表，不要只导出原始数据
4. skill 脚本失败时改用 export_ppt / export_analysis_docx 工具（Python 优先、无 Python 自动降级 Go 基础版，导出不失败），不反复重试

## Skill 工作流（Skill 可用时）

1. 调用 `skill` 工具获取 SKILL.md
2. 阅读数据契约（所需字段和格式）
3. 用 `query_data` 取数，计算统计指标
4. 组装 JSON，通过 `execute` 执行 Python 脚本
5. 解析输出，返回下载链接

## HTML 报告（export_html）

content 参数支持完整 Markdown + Mermaid + KaTeX。直接用标准 Markdown 编写。

**内容组织建议**：

```markdown
# 报告标题

## 摘要
关键结论...

## 数据概览
| 指标 | 数值 | 同比 |
|------|------|------|

## 趋势分析
` + "```mermaid" + `
pie title 占比
  "A" : 40
  "B" : 60
` + "```" + `

## 结论与建议
1. ...
2. ...
```
