---
name: export-word
description: 生成专业 Word（DOCX）数据分析报告。支持四种工作流：1) 数据驱动编程创建（数据/内容两种模式，含智能统计解读） 2) OOXML 直接文本替换 3) 模板组装多章节拼接 4) OOXML 拆包/编辑/打包+修订追踪。生成包含封面（编制单位/日期/编号/密级）、摘要与指标速览、数据概览、统计分析、可视化、发现与建议、附录及页眉页脚的完整报告。当用户请求生成 Word 文档或数据分析报告时必须使用此技能。
---

# 专业 Word 数据分析报告导出技能 v3.0

基于 Anthropic docx skill 架构思想重构，融合 OOXML 直接操作 + 模板组装 + 修订追踪 + 编程控制四层能力。

## 架构概览

```
export-word/
├── SKILL.md
└── scripts/
    ├── export_word.py         # 主入口 — WordExporter 类（2模式）
    ├── inventory.py           # 模板结构清单
    ├── ooxml_replace.py       # OOXML 文本精确替换
    ├── assemble.py            # 多章节模板组装
    ├── track_changes.py       # 修订追踪启用
    ├── chart_generator.py     # 图表生成（→ shared/）
    ├── word_templates/        # 模板
    └── word_builders/         # 8 个构建器

shared/
├── ooxml/scripts/
│   ├── unpack.py              # OOXML 解包
│   ├── pack.py                # OOXML 打包
│   └── validate.py            # OOXML 验证
```

## 四种工作流

### 工作流 1：数据驱动编程创建

```bash
python export_word.py < input.json
```

两种模式：
- **data** — 完整分析报告（封面→摘要→指标速览→数据概览→统计分析→可视化→发现与建议→附录→页眉页脚）
- **content** — 结构化文档（封面→自定义章节，支持 heading1-3/paragraph/list/table/image 块类型）

报告包含智能数据特征解读（3层分析：离散程度→集中趋势→稳定性评估）。

### 工作流 2：OOXML 精确文本替换

直接操作 OOXML XML，保留所有格式（rPr），仅替换文本：

```bash
python ooxml_replace.py template.docx replacements.json output.docx
```

replacements.json：
```json
{
  "{{TITLE}}": "2024年销售报告",
  "{{AUTHOR}}": "张明远",
  "{{DATE}}": "2024年12月31日"
}
```

自动处理 document.xml 和 header/footer XML。

### 工作流 3：模板组装多章节拼接

将多个独立章节 .docx 文件拼接为最终文档：

```bash
python assemble.py master.docx output.docx chapter1.docx chapter2.docx chapter3.docx
```

在章节之间自动添加分节符。

### 工作流 4：OOXML 拆包/编辑/打包 + 修订追踪

```bash
# 拆包
python shared/ooxml/scripts/unpack.py input.docx unpacked/

# 启用修订追踪
python track_changes.py input.docx tracked.docx "审核人姓名"

# 手动编辑 XML...

# 验证
python shared/ooxml/scripts/validate.py unpacked/ --original input.docx

# 打包
python shared/ooxml/scripts/pack.py unpacked/ output.docx
```

## 生成文档结构（data 模式）

### 封面页
- 品牌标识：`WebSQL AI · 智能数据分析平台`
- 中国红装饰分隔线（#C0392B）
- 双语标题（中英文 30pt 深蓝加粗）
- 元信息：编制单位、编制日期（`YYYY年MM月DD日`）、报告编号（`WS-RPT-YYYYMMDD-XXXX`）、密级"内部资料"

### 第一章 · 报告摘要
- █ + 章标题，中国红装饰块
- 数据全景描述（字段维度、记录条数）
- 核心指标速览表（指标名称/样本量/最小值/最大值/均值）

### 数据概览与质量评估
- 次级标题（┃ 前缀）
- 数据样例预览表（最大6列×5行）

### 统计分析与核心指标
- 表1：数值字段描述性统计汇总（字段/有效样本/最小值/最大值/均值/标准差）
- 智能解读（离散程度→集中趋势→稳定性 → 综合评语）

### 数据可视化分析
- 嵌入式图表（图1、图2…）+ 数据来源标注

### 关键发现与建议
- 发现N：中国红前缀 + └ 建议：
- 支持结构化发现与简单文本发现

### 附录：数据明细
- 原始数据表（最大8列×20行）+ 截断提示

### 页眉页脚
- 页眉：`WebSQL · 报告标题` 右对齐
- 页脚：`— 第 {PAGE} 页 —` Word域代码自动页码

## 模板结构清单

```bash
python inventory.py template.docx structure.json
```
提取段落样式（对齐、行距、字体信息）和表格结构。

## 图表生成

支持 8 种图表类型，与 PPT 技能共用 `shared/chart_generator.py`。

## 依赖

- python-docx>=1.0.0
- matplotlib>=3.7.0, numpy>=1.24.0
- lxml>=4.9.0
