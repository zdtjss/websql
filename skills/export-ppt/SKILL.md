---
name: export-ppt
description: 使用 Python 生成科技感数据分析 PPT，支持图文混排、多种图表类型，深色主题配色
inclusion: manual
-----------------

# PPT 生成器 Skill

科技感数据分析 PPT 生成，基于 python-pptx + matplotlib，支持图文混排。

## 使用前提

依赖：`pip install python-pptx matplotlib numpy Pillow`

## 工作流程

1. 运行本目录下的 `pscripts/export_ppt.py` 作为模块导入
2. 调用脚本中 `sys.path/scripts` 指向本 skill 目录

## 调用模板

```python
import sys, os
# 指向本 skill 目录（AI 执行时替换为实际路径）
SKILL_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), '.kiro', 'skills', 'ppt-generator')
sys.path.insert(0, SKILL_DIR)
from ppt_generator import PPTBuilder

builder = PPTBuilder()
builder.add_cover(title="标题", subtitle="副标题", date="日期", author="作者")
builder.add_toc(["章节1", "章节2"])
builder.add_section_divider("章节标题", "描述")
builder.add_kpi_page("指标概览", [
    {"label": "指标名", "value": "数值", "change": "+X%", "trend": "up"},
])
builder.add_chart_page("图表标题", "line", data, insights=["要点"], layout="left_chart")
builder.add_dual_chart_page("双图标题", {"type":"bar","data":{...}}, {"type":"pie","data":{...}})
builder.add_text_page("标题", ["要点1", "要点2"], highlight_indices=[0])
builder.add_comparison_page("对比", "左标题", ["左项"], "右标题", ["右项"])
builder.add_summary_page("总结", ["结论1", "结论2"])
builder.add_thank_you("感谢聆听", "联系方式")
builder.save("output.pptx")
```

## 图表类型

line, bar, horizontal\_bar, pie, donut, scatter, radar, heatmap, area, stacked\_bar

## 数据格式

```python
# 折线/柱状/面积/堆叠
{"title":"", "categories":[...], "series":[{"name":"", "values":[...]}]}
# 饼图/环形
{"title":"", "labels":[...], "values":[...]}
# 散点
{"title":"", "x":[...], "y":[...], "x_label":"", "y_label":""}
# 雷达
{"title":"", "categories":[...], "series":[{"name":"", "values":[...]}]}
# 热力图
{"title":"", "x_labels":[...], "y_labels":[...], "values":[[...]]}
```

## 布局选项

left\_chart（左图右文，默认）| full\_chart（全幅）| top\_chart（上图下文）

## 内容要求

- 每页有分析观点，不能只放图不解读
- 使用具体数字，逻辑递进：现状→趋势→原因→建议
- 页数 12-18 页，内容丰富
- 典型结构：封面→目录→背景→KPI→趋势→对比→构成→分布→洞察→风险→建议→总结→致谢

## 工具脚本参考

\#\[\[file:scripts/export\_ppt.py]]
