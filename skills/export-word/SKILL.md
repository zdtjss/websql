---
name: Word 文档生成器
description: 使用 Python 生成专业数据分析 Word 报告，支持图表、表格、KPI 卡片，科技感配色
inclusion: manual
---

# Word 文档生成器 Skill

专业数据分析 Word 报告生成，基于 python-docx + matplotlib，支持图表、表格、KPI 卡片。

## 使用前提

依赖：`pip install python-docx matplotlib numpy Pillow`

## 工作流程

1. 运行本目录下的 `scripts/word_generator.py` 作为模块导入
2. 调用脚本中 `sys.path/scripts` 指向本 skill 目录

## 调用模板

```python
import sys, os
# 指向本 skill 目录（AI 执行时替换为实际路径）
SKILL_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), '.kiro', 'skills', 'word-generator')
sys.path.insert(0, SKILL_DIR)
from word_generator import WordBuilder

doc = WordBuilder()
doc.add_cover(title="标题", subtitle="副标题", date="日期", author="作者", org="机构")
doc.add_toc_placeholder()
doc.add_heading("一级标题", level=1)
doc.add_heading("二级标题", level=2)
doc.add_paragraph("正文内容", bold=False, indent=True)
doc.add_bullet_list(["要点1", "要点2"], highlight_indices=[0])
doc.add_numbered_list(["步骤1", "步骤2"])
doc.add_quote("引用文字")
doc.add_kpi_table([{"label":"指标", "value":"数值", "change":"+X%", "trend":"up"}])
doc.add_table(["列1","列2"], [["数据1","数据2"]], caption="表名")
doc.add_chart("chart_type", data_dict, caption="图表说明")
doc.add_page_break()
doc.save("output.docx")
```

## 图表类型

line, bar, horizontal\_bar, pie, donut, scatter, radar, heatmap, area, stacked\_bar

## 数据格式（与 PPT skill 一致）

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

## 可用 API

| 方法                                  | 用途              |
| ----------------------------------- | --------------- |
| `add_cover(...)`                    | 封面页             |
| `add_toc_placeholder()`             | 目录（Word 中更新域生成） |
| `add_heading(text, level)`          | 标题（1/2/3级）      |
| `add_paragraph(text, bold, indent)` | 正文段落            |
| `add_bullet_list(items)`            | 无序列表            |
| `add_numbered_list(items)`          | 有序列表            |
| `add_quote(text)`                   | 引用块             |
| `add_kpi_table(kpis)`               | KPI 指标卡片        |
| `add_table(headers, rows, caption)` | 数据表格            |
| `add_chart(type, data, caption)`    | 插入图表            |
| `add_page_break()`                  | 分页              |

## 内容要求

- 结构完整：封面→目录→摘要→正文→结论→附录
- 每章有分析观点，数据+解读结合
- 图表配文字说明，表格用于精确数据
- 正文首行缩进，段落间距适当
- 专业术语准确，逻辑递进

## 典型报告结构

封面 → 目录 → 摘要 → 背景与目标 → 数据概览(KPI) → 详细分析(多章节，含图表) → 问题与风险 → 建议 → 结论 → 附录/参考

## 工具脚本参考

\#\[\[file:scripts/word\_generator.py]]
