# Agents.md — Agent 运行时参考规范

本文件随系统提示词注入，不进入会话历史；修改后下次运行生效。

## 数据导入与文件分析

- 导入走「表数据浏览」：左侧树右键目标表 → 浏览数据 → 工具栏导入（Excel/CSV/JSON，支持字段映射与新增/更新）。
- 文件意图由提问决定：仅分析 → read_file_data 的 outline/profile/search 定位后按 unit/offset 精读；结合数据库 → read_file_data + query_data；要求导入 → 引导到上述页面。
- 以 totalRows/charCount/notice 为准，被截断时继续分页；read_file_data 只读，可多次调用。

## 数据可视化（Mermaid）

- 先结论后图；节点 ≤15；占比→pie、流程→flowchart、关系→erDiagram、趋势→xychart-beta、时序→sequenceDiagram。
- xychart-beta 轴范围用 `-->`，title 含中文加引号；pie 数值写纯数字；sequenceDiagram 的 as 别名不加引号；特殊字符节点加引号。
- 禁用 `---` front-matter、`legend`、`%%{init}%%` 与不支持的图表类型；报告优先 kpi/chart/table。

## 导出工具选择

- Word/PPT/HTML → export_analysis_docx / export_ppt / export_html（Go 原生渲染，优先 content 模式）。
- Excel → export_excel / export_excel_with_chart（纯文件模式不可用，需先选择连接）。
- 其他格式 → export_by_code（需：完整连接权限 + 允许代码执行与 AI 数据写 + Python + OS 隔离 + 无脱敏规则）。
- 跨库深度分析 → skill:cross-db-analysis。
- 报告必须图文并茂：摘要 + kpi + 至少一个 chart + 结论；导出前先确认数据，禁止编造 downloadUrl。

## 高级导出（export_by_code）

- 仅 Python；数据库只经 websql_db 只读查询；禁止写库/改结构、改删文件、联网、pip 安装、直接 import 驱动。
- 连接信息由 WEBSQL_DB_* 注入；表结构见 schema.json；产物写到 outputFile。
- 大数据先聚合/WHERE，避免拉取海量明细。

## 报告扩展语法（docx/ppt/html 通用）

```kpi
[{"label":"总销售额","value":"1,234万","change":"+12%","trend":"up"}]
```

```chart
{"type":"bar","title":"月度销售额","categories":["1月","2月"],"series":[{"name":"销售额","data":[120,300]}]}
```

- 图表也可给 sql（服务端按权限取数，需 x/y）；table 代码块传 {"sql":"SELECT ...","limit":20}；提示框 > [!NOTE]；分页 <!-- pagebreak -->。
- type：bar/hbar/stacked/line/area/pie/donut/scatter/radar。

## Skill 工作流（仅当工具可用）

1. skill 读取 SKILL.md；2. query_data 取数；3. execute 运行 skills/ 内脚本（禁止自写脚本）；4. 返回下载链接。
