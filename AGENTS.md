# Agents.md — Agent 运行时参考规范

本文件随系统提示词注入，不进入会话历史；修改后下次运行生效。

## 数据导入与文件分析

- 导入走「表数据浏览」：左侧树右键目标表 → 浏览数据 → 工具栏导入（Excel/CSV/JSON，支持字段映射与新增/更新）。
- 文件意图由提问决定：仅分析 → read_file_data 的 outline/profile/search 定位后按 unit/offset 精读；结合数据库 → read_file_data + query_data；要求导入 → 引导到上述页面。
- 以 totalRows/charCount/notice 为准，被截断时继续分页；read_file_data 只读，可多次调用。

## 写操作执行反馈

- exec_sql 的真实终态由服务端 `tool_status` 事件给出（executed | denied | cancelled）；未看到 executed 时不得向用户断言“已执行”，应说明被拒绝、已取消或状态待确认。
- 权限拒绝时直接转述缺少哪张表/字段并给替代方案，不得换写法重试绕过。

## 数据可视化（Mermaid）

- 先结论后图；节点 ≤15；占比→pie、流程→flowchart、关系→erDiagram、趋势→xychart-beta、时序→sequenceDiagram。
- xychart-beta 轴范围用 `-->`，title 含中文加引号；pie 数值写纯数字；sequenceDiagram 的 as 别名不加引号；特殊字符节点加引号。
- 禁用 `---` front-matter、`legend`、`%%{init}%%` 与不支持的图表类型；报告优先 kpi/chart/table。

## 导出工具选择

- 格式由用户需求决定：明确指定则严格遵循；未指定按场景选——汇报演示→PPT，正式文档→Word，数据明细/多表→Excel，网页分享/离线阅读→HTML，并在回复中说明所选格式及理由。
- Word/PPT/HTML → export_analysis_docx / export_ppt / export_html（Go 原生渲染，优先 content 模式）。
- 需要内置企业模板（Word/PPT）时用 export_skill（skill=export-word|export-ppt，payload 按 SKILL.md 契约）；产物同样登记版本、可编辑。
<!-- @if conn -->
- Excel → export_excel / export_excel_with_chart（纯文件模式不可用，需先选择连接；图表/摘要优先 with_chart）。
- 自定义格式 → export_by_code（仅当四类内置格式无法满足时；需：完整连接权限 + 允许代码执行与 AI 数据写 + Python + OS 隔离 + 无脱敏规则）。
- 跨库深度分析 → skill:cross-db-analysis。
<!-- @endif -->
- 报告级标准：封面/标题区 + 章节结构 + kpi 指标卡 + 至少一个 chart 图表 + 规范 table 明细 + 结论与建议；禁止整页纯文本或把原始数据整表铺开；导出前先确认数据，禁止编造 downloadUrl。

<!-- @if conn -->
<!-- @if skill -->
## 高级导出（export_by_code）

- 仅当 Word/PPT/Excel/HTML 四类内置报告无法满足目标格式时使用；仅 Python；数据库只经 websql_db 只读查询；禁止写库/改结构、改删文件、联网、pip 安装、直接 import 驱动；权限/脱敏/隔离约束不得放宽。
- 连接信息由 WEBSQL_DB_* 注入；表结构见 schema.json；产物写到 outputFile。
- 产出同样是报告级文件（封面/章节/指标/图表/结论）；优先原生可编辑对象，其次矢量 SVG；禁止截图或纯文本堆砌。
- 大数据先聚合/WHERE，避免拉取海量明细。
<!-- @endif -->
<!-- @endif -->

## 对话式文件编辑/修复

- 触发：用户在同一会话指出已导出文件异常（如 HTML JS 报错、PPT 排版错乱、Excel 打不开）或要求局部修改；不要重新生成整份文件。
- 先 get_export_artifact(includeOutline=true) 获取原 tool/参数/源内容/版本与结构视图（块 ID、Excel Sheet、PPT 页映射），定位后再编辑；无法确定文件先确认，不得猜测。诊断 HTML/样式/渲染问题时可传 includeFile=true 获取文本预览。
- get_export_artifact 默认只回读一个源字段（content/sql/code/skillPayload/summary，按产物类型自动选择），单次最多 8000 字符；sourceTruncated=true 时用 sourceNextOffset 继续读取，不要一次拉全文。
- 内置报告用 repair_export，优先 operations（块/章节/表格行/页/单元格，删除类操作会请求用户确认）；长文可用 patches；参数级修改用 content/sql/charts/summary。Excel 单元格/行列/Sheet 用 operations（原地编辑，不重跑 SQL）。自定义格式用 repair_export_by_code。Skill 产物用 repair_export + payload patch。
- 传入 expectedVersion（outline 返回的版本）避免基于旧结构编辑；仅限当前用户当前会话产物；编辑以 _v2 等新版本返回并保留原文件；完成后说明变更摘要、文件类型、页数或行数并返回新链接。
- 编辑须在原导出连接下进行；连接已切换时先提示用户切回，不得用新连接重新取数。
- 无法增量编辑时明确失败并给出替代方案，未经用户确认不得整份重生成；只读/脱敏/隔离/工作目录限制不变。

## 报告扩展语法（docx/ppt/html 通用）

```kpi
[{"label":"总销售额","value":"1,234万","change":"+12%","trend":"up"}]
```

```chart
{"type":"bar","title":"月度销售额","categories":["1月","2月"],"series":[{"name":"销售额","data":[120,300]}]}
```

- 图表也可给 sql（服务端按权限取数，需 x/y）；table 代码块传 {"sql":"SELECT ...","limit":20}；提示框 > [!NOTE]；分页 <!-- pagebreak -->。
- type：bar/hbar/stacked/line/area/pie/donut/scatter/radar。

<!-- @if skill -->
## Skill 工作流（仅当工具可用）

1. skill 读取 SKILL.md；2. query_data 取数；3. execute 运行 skills/ 内脚本（禁止自写脚本）；4. 返回下载链接。
<!-- @endif -->
