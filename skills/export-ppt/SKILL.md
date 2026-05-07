---
name: export-ppt
description: 生成专业 PowerPoint（PPTX）演示文稿。支持四种工作流：1) HTML-to-PPTX 快速转换 2) 模板目录提取+重排+替换 3) 数据驱动编程创建（数据/内容/演示三种模式） 4) OOXML 级别拆包编辑打包。配备中国商务经典+科技现代两套模板，支持 8 种图表类型。当用户请求生成 PPT、演示文稿或 PowerPoint 导出时必须使用此技能。
---

# 专业 PPT 演示文稿导出技能 v3.0

基于 Anthropic pptx skill 架构思想重构，融合 OOXML 直接操作 + HTML 快速生成 + 编程控制三层能力。

## 架构概览

```
export-ppt/
├── SKILL.md
└── scripts/
    ├── export_ppt.py          # 主入口 — PPTExporter 类（3模式）
    ├── html2pptx.py           # HTML → PPTX 快速转换
    ├── inventory.py           # 模板结构提取
    ├── rearrange.py           # 模板重排
    ├── replace.py             # 模板文字替换
    ├── thumbnail.py           # 缩略图生成
    ├── chart_generator.py     # 图表生成（→ shared/）
    ├── ppt_templates/         # 3 套模板
    └── ppt_builders/          # 7 个构建器

shared/
├── ooxml/scripts/
│   ├── unpack.py              # OOXML 解包
│   ├── pack.py                # OOXML 打包
│   └── validate.py            # OOXML 验证
```

## 四种工作流

### 工作流 1：HTML-to-PPTX 快速转换
将 HTML 内容直接转为 PPTX，适合 AI 生成幻灯片。

```bash
python html2pptx.py --stdin -o output.pptx < slides.html
```

HTML 结构示例：
```html
<section class="cover">
  <h1>2024年度销售运营分析报告</h1>
  <h2>基于全渠道业务数据的综合分析</h2>
</section>

<section>
  <h1>市场概览</h1>
  <p>全年累计销售额达到 3.8亿元，同比增长 23.5%。</p>
  <div class="kpi" data-label="年度销售额" data-value="3.8亿" data-trend="↑ 23.5%"></div>
  <table>
    <tr><th>季度</th><th>销售额</th></tr>
    <tr><td>Q1</td><td>0.85亿</td></tr>
    <tr><td>Q2</td><td>0.92亿</td></tr>
  </table>
</section>

<section class="ending">
  <h1>感谢聆听</h1>
</section>
```

支持的幻灯片类型：`cover` / `section` / `ending` / 普通内容页
支持的内容元素：h1-h3, p, ul/li, img, table, KPI div

### 工作流 2：模板工作流（inventory → rearrange → replace）

**Step 1: 提取模板结构**
```bash
python inventory.py template.pptx inventory.json
```
输出每张幻灯片中所有形状的位置、字体、占位符类型等。

**Step 2: 重排幻灯片**
```bash
python rearrange.py template.pptx output.pptx 0,3,3,5
```
按索引选取幻灯片生成新文件（支持重复）。

**Step 3: 替换文字**
```bash
python replace.py template.pptx replacement.json output.pptx
```
输入 JSON 格式的替换映射，保留原有格式。

### 工作流 3：数据驱动编程创建

```bash
python export_ppt.py < input.json
```

三种模式：
- **data** — 数据报告流（封面→图表→数据全景→明细→核心发现→结束页）
- **content** — 结构化内容流（封面→目录→过渡页+内容页循环→结束页），14种块类型
- **demo** — 产品演示流（封面→步骤页（支持嵌入图表）→结束页）

### 工作流 4：OOXML 级别操作

```bash
# 解包
python shared/ooxml/scripts/unpack.py input.pptx unpacked/

# 手动编辑 XML...

# 验证
python shared/ooxml/scripts/validate.py unpacked/ --original input.pptx

# 打包
python shared/ooxml/scripts/pack.py unpacked/ output.pptx
```

适合需要精确控制 OOXML 的高级场景。

## 图表生成

支持 8 种图表类型，通过 stdin JSON：

```bash
python chart_generator.py < chart_input.json
```

| 类型 | chartType | 说明 |
|------|-----------|------|
| 折线图 | `line` | 数据趋势（带数据标签） |
| 柱状图 | `bar` | 分类对比（支持分组） |
| 饼图 | `pie` | 占比分布 |
| 环形图 | `doughnut` | 带总计中心 |
| 雷达图 | `radar` | 多维度综合评估 |
| 散点图 | `scatter` | 相关性分析 |
| 面积图 | `area` | 累积趋势 |
| 横向柱状图 | `hbar` | 长标签排行 |

## 缩略图预览

```bash
python thumbnail.py presentation.pptx thumbnails --cols 4
```
生成拼接缩略图网格（需 LibreOffice + poppler-utils）。

## 配色方案

| 方案 | 主色 | 强调色 | 场景 |
|------|------|--------|------|
| chinese_business | #1A3C6D | #C0392B | 政府/国企/正式场合 |
| tech_modern | #1565C0 | #00ACC1 | 互联网/科技企业 |
| warm_elegant | #8B4513 | #B22222 | 文化/教育/艺术 |

## 幻灯片尺寸

- 16:9 宽屏：13.333 × 7.5 英寸

## 依赖

- python-pptx>=0.6.21
- matplotlib>=3.7.0, numpy>=1.24.0
- lxml>=4.9.0, Pillow>=9.0.0
- LibreOffice + poppler-utils (thumbnail 可选)
