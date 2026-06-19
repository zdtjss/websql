"""
Word 文档生成器 - 科技感数据分析风格
依赖: pip install python-docx matplotlib numpy Pillow
用法: from tools.word_generator import WordBuilder
"""

import os
import tempfile
from pathlib import Path

import matplotlib
import matplotlib.pyplot as plt
import numpy as np
from docx import Document
from docx.shared import Inches, Pt, Cm, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.style import WD_STYLE_TYPE
from docx.oxml.ns import qn

# 中文字体配置
matplotlib.rcParams['font.sans-serif'] = ['Microsoft YaHei', 'SimHei', 'DejaVu Sans']
matplotlib.rcParams['axes.unicode_minus'] = False


# ═══════════════════════════════════════════════════════════════
# 配色主题（与 PPT 保持一致的科技感）
# ═══════════════════════════════════════════════════════════════
class Theme:
    # Word 文档颜色（深色系用于强调，浅色系用于正文）
    PRIMARY = RGBColor(0x00, 0xA8, 0xFF)       # 科技蓝 - 标题
    SECONDARY = RGBColor(0x0A, 0x16, 0x28)     # 深空蓝 - 正文
    ACCENT = RGBColor(0x00, 0xF5, 0xD4)        # 电光青 - 强调
    HEADING1 = RGBColor(0x0A, 0x16, 0x28)      # 一级标题
    HEADING2 = RGBColor(0x00, 0xA8, 0xFF)      # 二级标题
    HEADING3 = RGBColor(0x1E, 0x4D, 0x8C)      # 三级标题
    BODY = RGBColor(0x2C, 0x2C, 0x2C)          # 正文
    GRAY = RGBColor(0x66, 0x66, 0x66)          # 辅助文字
    TABLE_HEADER_BG = '0A1628'                  # 表头背景
    TABLE_HEADER_FG = 'FFFFFF'                  # 表头文字
    TABLE_ROW_ALT = 'F0F7FF'                    # 表格交替行

    # matplotlib 图表配色（Word 用浅色背景）
    CHART_COLORS = ['#00A8FF', '#00F5D4', '#7B61FF', '#FF6B35', '#E63946', '#FFD700', '#00E696']
    CHART_BG = '#FFFFFF'
    CHART_FACE = '#FFFFFF'
    CHART_GRID = '#E0E0E0'


# ═══════════════════════════════════════════════════════════════
# WordBuilder 主类
# ═══════════════════════════════════════════════════════════════
class WordBuilder:
    def __init__(self, title="数据分析报告"):
        self.doc = Document()
        self._temp_files = []
        self._title = title
        self._setup_styles()
        self._setup_matplotlib()

    def _setup_matplotlib(self):
        plt.rcParams.update({
            'figure.facecolor': Theme.CHART_FACE,
            'axes.facecolor': Theme.CHART_BG,
            'text.color': '#2C2C2C',
            'axes.labelcolor': '#2C2C2C',
            'xtick.color': '#2C2C2C',
            'ytick.color': '#2C2C2C',
            'axes.edgecolor': Theme.CHART_GRID,
            'grid.color': Theme.CHART_GRID,
            'grid.alpha': 0.5,
        })

    def _setup_styles(self):
        """配置文档默认样式"""
        style = self.doc.styles['Normal']
        style.font.name = '微软雅黑'
        style.font.size = Pt(11)
        style.font.color.rgb = Theme.BODY
        style.element.rPr.rFonts.set(qn('w:eastAsia'), '微软雅黑')

        # 设置页边距
        sections = self.doc.sections
        for section in sections:
            section.top_margin = Cm(2.5)
            section.bottom_margin = Cm(2.5)
            section.left_margin = Cm(2.8)
            section.right_margin = Cm(2.8)

    def _set_heading_style(self, paragraph, level, color):
        """设置标题样式"""
        run = paragraph.runs[0] if paragraph.runs else paragraph.add_run()
        run.font.color.rgb = color
        run.font.bold = True
        if level == 1:
            run.font.size = Pt(22)
        elif level == 2:
            run.font.size = Pt(16)
        elif level == 3:
            run.font.size = Pt(13)

    def _save_chart(self, fig):
        """保存图表为临时文件"""
        path = os.path.join(tempfile.gettempdir(), f'word_chart_{len(self._temp_files)}.png')
        fig.savefig(path, dpi=150, bbox_inches='tight', facecolor='white', edgecolor='none')
        plt.close(fig)
        self._temp_files.append(path)
        return path

    # ─── 文档结构 ───────────────────────────────────────────────

    def add_cover(self, title, subtitle="", date="", author="", org=""):
        """封面页"""
        # 空行留白
        for _ in range(4):
            self.doc.add_paragraph()

        # 主标题
        p = self.doc.add_paragraph()
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        run = p.add_run(title)
        run.font.size = Pt(28)
        run.font.bold = True
        run.font.color.rgb = Theme.HEADING1

        # 副标题
        if subtitle:
            p = self.doc.add_paragraph()
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            run = p.add_run(subtitle)
            run.font.size = Pt(14)
            run.font.color.rgb = Theme.PRIMARY

        # 空行
        self.doc.add_paragraph()
        self.doc.add_paragraph()

        # 作者/机构/日期
        info_lines = []
        if org:
            info_lines.append(org)
        if author:
            info_lines.append(author)
        if date:
            info_lines.append(date)
        for line in info_lines:
            p = self.doc.add_paragraph()
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            run = p.add_run(line)
            run.font.size = Pt(12)
            run.font.color.rgb = Theme.GRAY

        # 分页
        self.doc.add_page_break()

    def add_toc_placeholder(self):
        """目录占位（Word 需手动更新域）"""
        p = self.doc.add_paragraph()
        run = p.add_run("目  录")
        run.font.size = Pt(18)
        run.font.bold = True
        run.font.color.rgb = Theme.HEADING1
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER

        self.doc.add_paragraph()
        p = self.doc.add_paragraph()
        run = p.add_run("（请在 Word 中右键此处 → 更新域 以生成目录）")
        run.font.size = Pt(10)
        run.font.color.rgb = Theme.GRAY
        run.font.italic = True

        # 插入 TOC 域代码
        paragraph = self.doc.add_paragraph()
        run = paragraph.add_run()
        fldChar1 = run._r.makeelement(qn('w:fldChar'), {qn('w:fldCharType'): 'begin'})
        run._r.append(fldChar1)
        run2 = paragraph.add_run()
        instrText = run2._r.makeelement(qn('w:instrText'), {})
        instrText.text = ' TOC \\o "1-3" \\h \\z \\u '
        run2._r.append(instrText)
        run3 = paragraph.add_run()
        fldChar2 = run3._r.makeelement(qn('w:fldChar'), {qn('w:fldCharType'): 'end'})
        run3._r.append(fldChar2)

        self.doc.add_page_break()

    def add_heading(self, text, level=1):
        """添加标题"""
        p = self.doc.add_heading(text, level=level)
        colors = {1: Theme.HEADING1, 2: Theme.HEADING2, 3: Theme.HEADING3}
        color = colors.get(level, Theme.BODY)
        for run in p.runs:
            run.font.color.rgb = color
            run.font.name = '微软雅黑'
            run._element.rPr.rFonts.set(qn('w:eastAsia'), '微软雅黑')
        return p

    def add_paragraph(self, text, bold=False, color=None, indent=False):
        """添加正文段落"""
        p = self.doc.add_paragraph()
        if indent:
            p.paragraph_format.first_line_indent = Cm(0.7)
        run = p.add_run(text)
        run.font.size = Pt(11)
        run.font.bold = bold
        run.font.color.rgb = color or Theme.BODY
        return p

    def add_bullet_list(self, items, highlight_indices=None):
        """添加要点列表"""
        highlight_indices = highlight_indices or []
        for i, item in enumerate(items):
            p = self.doc.add_paragraph(style='List Bullet')
            run = p.add_run(item)
            run.font.size = Pt(11)
            run.font.color.rgb = Theme.PRIMARY if i in highlight_indices else Theme.BODY

    def add_numbered_list(self, items):
        """添加编号列表"""
        for item in items:
            p = self.doc.add_paragraph(style='List Number')
            run = p.add_run(item)
            run.font.size = Pt(11)
            run.font.color.rgb = Theme.BODY

    def add_quote(self, text):
        """添加引用块"""
        p = self.doc.add_paragraph()
        p.paragraph_format.left_indent = Cm(1.5)
        run = p.add_run(f"「{text}」")
        run.font.size = Pt(11)
        run.font.italic = True
        run.font.color.rgb = Theme.GRAY

    def add_code_block(self, code):
        """添加代码块"""
        for line in code.split("\n"):
            p = self.doc.add_paragraph()
            pPr = p._element.get_or_add_pPr()
            shd = pPr.makeelement(qn('w:shd'), {
                qn('w:val'): 'clear', qn('w:color'): 'auto', qn('w:fill'): 'F5F5F5'
            })
            pPr.append(shd)
            ind = pPr.makeelement(qn('w:ind'), {qn('w:left'): '360'})
            pPr.append(ind)
            spacing = pPr.makeelement(qn('w:spacing'), {
                qn('w:before'): '20', qn('w:after'): '20'
            })
            pPr.append(spacing)
            run = p.add_run(line)
            run.font.size = Pt(9)
            run.font.name = 'Courier New'
            run._element.rPr.rFonts.set(qn('w:eastAsia'), '微软雅黑')

    def add_kpi_table(self, kpis):
        """添加 KPI 指标表格，kpis: list of {label, value, change, trend}"""
        n = len(kpis)
        table = self.doc.add_table(rows=2, cols=n)
        table.alignment = WD_TABLE_ALIGNMENT.CENTER
        table.style = 'Table Grid'

        tbl = table._tbl
        tblPr = tbl.tblPr if tbl.tblPr is not None else tbl._add_tblPr()
        existing = tblPr.find(qn('w:tblLayout'))
        if existing is not None:
            tblPr.remove(existing)
        tblLayout = tblPr.makeelement(qn('w:tblLayout'), {qn('w:type'): 'autofit'})
        tblPr.append(tblLayout)

        # 表头行（指标名）
        for i, kpi in enumerate(kpis):
            cell = table.rows[0].cells[i]
            cell.text = ''
            p = cell.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            run = p.add_run(kpi['label'])
            run.font.size = Pt(10)
            run.font.color.rgb = Theme.GRAY
            # 背景色
            shading = cell._element.makeelement(qn('w:shd'), {
                qn('w:val'): 'clear', qn('w:color'): 'auto', qn('w:fill'): Theme.TABLE_HEADER_BG
            })
            cell._element.get_or_add_tcPr().append(shading)
            for r in p.runs:
                r.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)

        # 数据行
        for i, kpi in enumerate(kpis):
            cell = table.rows[1].cells[i]
            cell.text = ''
            p = cell.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            # 大数字
            run = p.add_run(kpi['value'])
            run.font.size = Pt(16)
            run.font.bold = True
            run.font.color.rgb = Theme.HEADING1
            # 换行 + 变化
            p.add_run('\n')
            arrow = "▲" if kpi.get('trend') == 'up' else "▼" if kpi.get('trend') == 'down' else "─"
            trend_color = RGBColor(0x00, 0xB8, 0x5C) if kpi.get('trend') == 'up' else RGBColor(0xE6, 0x39, 0x46) if kpi.get('trend') == 'down' else Theme.GRAY
            run2 = p.add_run(f"{arrow} {kpi.get('change', '')}")
            run2.font.size = Pt(10)
            run2.font.color.rgb = trend_color

        self.doc.add_paragraph()  # 间距

    def add_table(self, headers, rows, caption=""):
        """添加数据表格"""
        if caption:
            p = self.doc.add_paragraph()
            run = p.add_run(caption)
            run.font.size = Pt(10)
            run.font.bold = True
            run.font.color.rgb = Theme.GRAY

        table = self.doc.add_table(rows=1 + len(rows), cols=len(headers))
        table.alignment = WD_TABLE_ALIGNMENT.CENTER
        table.style = 'Table Grid'

        tbl = table._tbl
        tblPr = tbl.tblPr if tbl.tblPr is not None else tbl._add_tblPr()
        existing = tblPr.find(qn('w:tblLayout'))
        if existing is not None:
            tblPr.remove(existing)
        tblLayout = tblPr.makeelement(qn('w:tblLayout'), {qn('w:type'): 'autofit'})
        tblPr.append(tblLayout)

        for i, h in enumerate(headers):
            cell = table.rows[0].cells[i]
            cell.text = ''
            p = cell.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            run = p.add_run(h)
            run.font.size = Pt(10)
            run.font.bold = True
            run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
            run.font.name = '微软雅黑'
            run._element.rPr.rFonts.set(qn('w:eastAsia'), '微软雅黑')
            shading = cell._element.makeelement(qn('w:shd'), {
                qn('w:val'): 'clear', qn('w:color'): 'auto', qn('w:fill'): Theme.TABLE_HEADER_BG
            })
            cell._element.get_or_add_tcPr().append(shading)

        for r_idx, row in enumerate(rows):
            for c_idx, val in enumerate(row):
                cell = table.rows[r_idx + 1].cells[c_idx]
                cell.text = ''
                p = cell.paragraphs[0]
                p.alignment = WD_ALIGN_PARAGRAPH.CENTER
                run = p.add_run(str(val))
                run.font.size = Pt(10)
                run.font.name = '微软雅黑'
                run._element.rPr.rFonts.set(qn('w:eastAsia'), '微软雅黑')
                if r_idx % 2 == 1:
                    shading = cell._element.makeelement(qn('w:shd'), {
                        qn('w:val'): 'clear', qn('w:color'): 'auto', qn('w:fill'): Theme.TABLE_ROW_ALT
                    })
                    cell._element.get_or_add_tcPr().append(shading)

        self.doc.add_paragraph()

    def add_chart(self, chart_type, data, width=Inches(5.5), caption=""):
        """插入图表"""
        fig = self._create_chart(chart_type, data)
        img_path = self._save_chart(fig)
        if caption:
            p = self.doc.add_paragraph()
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            run = p.add_run(caption)
            run.font.size = Pt(9)
            run.font.color.rgb = Theme.GRAY
            run.font.italic = True
        p = self.doc.add_paragraph()
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        run = p.add_run()
        run.add_picture(img_path, width=width)
        self.doc.add_paragraph()

    def add_page_break(self):
        """分页"""
        self.doc.add_page_break()

    # ─── 图表生成（与 PPT 共用逻辑，白色背景版）─────────────────

    def _create_chart(self, chart_type, data):
        creators = {
            'line': self._chart_line,
            'bar': self._chart_bar,
            'horizontal_bar': self._chart_hbar,
            'pie': self._chart_pie,
            'donut': self._chart_donut,
            'scatter': self._chart_scatter,
            'radar': self._chart_radar,
            'heatmap': self._chart_heatmap,
            'area': self._chart_area,
            'stacked_bar': self._chart_stacked_bar,
        }
        fn = creators.get(chart_type, self._chart_bar)
        return fn(data)

    def _chart_line(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        for i, s in enumerate(data['series']):
            ax.plot(data['categories'], s['values'], marker='o', linewidth=2,
                    color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)], label=s['name'])
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        ax.legend(loc='upper left', framealpha=0.8)
        ax.grid(True, alpha=0.3)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        fig.tight_layout()
        return fig

    def _chart_bar(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        cats = data['categories']
        n_series = len(data['series'])
        width = 0.7 / n_series
        x = np.arange(len(cats))
        for i, s in enumerate(data['series']):
            offset = (i - n_series/2 + 0.5) * width
            bars = ax.bar(x + offset, s['values'], width, label=s['name'],
                          color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)], alpha=0.85)
            for bar, val in zip(bars, s['values']):
                ax.text(bar.get_x() + bar.get_width()/2, bar.get_height() + 0.5,
                        str(val), ha='center', va='bottom', fontsize=8, color='#333')
        ax.set_xticks(x)
        ax.set_xticklabels(cats)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        if n_series > 1:
            ax.legend(framealpha=0.8)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        ax.grid(axis='y', alpha=0.3)
        fig.tight_layout()
        return fig

    def _chart_hbar(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        cats = data['categories']
        values = data['series'][0]['values']
        colors = [Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)] for i in range(len(cats))]
        ax.barh(cats, values, color=colors, alpha=0.85)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        fig.tight_layout()
        return fig

    def _chart_pie(self, data):
        fig, ax = plt.subplots(figsize=(6, 5))
        colors = Theme.CHART_COLORS[:len(data['labels'])]
        ax.pie(data['values'], labels=data['labels'], colors=colors, autopct='%1.1f%%',
               textprops={'fontsize': 10}, startangle=90)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        fig.tight_layout()
        return fig

    def _chart_donut(self, data):
        fig, ax = plt.subplots(figsize=(6, 5))
        colors = Theme.CHART_COLORS[:len(data['labels'])]
        ax.pie(data['values'], labels=data['labels'], colors=colors, autopct='%1.1f%%',
               textprops={'fontsize': 10}, startangle=90, pctdistance=0.8,
               wedgeprops={'width': 0.4})
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        fig.tight_layout()
        return fig

    def _chart_scatter(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        ax.scatter(data['x'], data['y'], c=Theme.CHART_COLORS[0], alpha=0.7, s=50, edgecolors='white', linewidth=0.5)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        ax.set_xlabel(data.get('x_label', ''))
        ax.set_ylabel(data.get('y_label', ''))
        ax.grid(True, alpha=0.3)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        fig.tight_layout()
        return fig

    def _chart_radar(self, data):
        fig, ax = plt.subplots(figsize=(6, 5), subplot_kw=dict(polar=True))
        cats = data['categories']
        n = len(cats)
        angles = np.linspace(0, 2 * np.pi, n, endpoint=False).tolist()
        angles += angles[:1]
        for i, s in enumerate(data['series']):
            vals = s['values'] + s['values'][:1]
            ax.plot(angles, vals, 'o-', linewidth=2,
                    color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)], label=s['name'])
            ax.fill(angles, vals, alpha=0.1, color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)])
        ax.set_xticks(angles[:-1])
        ax.set_xticklabels(cats, fontsize=9)
        ax.set_title(data.get('title', ''), fontsize=13, pad=20)
        ax.legend(loc='upper right', framealpha=0.8)
        fig.tight_layout()
        return fig

    def _chart_heatmap(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        values = np.array(data['values'])
        im = ax.imshow(values, cmap='YlOrRd', aspect='auto')
        ax.set_xticks(range(len(data['x_labels'])))
        ax.set_xticklabels(data['x_labels'], fontsize=9)
        ax.set_yticks(range(len(data['y_labels'])))
        ax.set_yticklabels(data['y_labels'], fontsize=9)
        fig.colorbar(im, ax=ax, shrink=0.8)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        fig.tight_layout()
        return fig

    def _chart_area(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        for i, s in enumerate(data['series']):
            ax.fill_between(data['categories'], s['values'], alpha=0.25,
                            color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)])
            ax.plot(data['categories'], s['values'], linewidth=2,
                    color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)], label=s['name'])
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        ax.legend(framealpha=0.8)
        ax.grid(True, alpha=0.3)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        fig.tight_layout()
        return fig

    def _chart_stacked_bar(self, data):
        fig, ax = plt.subplots(figsize=(8, 4.5))
        cats = data['categories']
        x = np.arange(len(cats))
        bottom = np.zeros(len(cats))
        for i, s in enumerate(data['series']):
            ax.bar(x, s['values'], bottom=bottom, label=s['name'],
                   color=Theme.CHART_COLORS[i % len(Theme.CHART_COLORS)], alpha=0.85)
            bottom += np.array(s['values'])
        ax.set_xticks(x)
        ax.set_xticklabels(cats)
        ax.set_title(data.get('title', ''), fontsize=13, pad=10)
        ax.legend(framealpha=0.8)
        ax.spines['top'].set_visible(False)
        ax.spines['right'].set_visible(False)
        fig.tight_layout()
        return fig

    # ─── 保存 ──────────────────────────────────────────────────

    def save(self, filepath):
        """保存文档并清理临时文件"""
        Path(filepath).parent.mkdir(parents=True, exist_ok=True)
        self.doc.save(filepath)
        for f in self._temp_files:
            try:
                os.remove(f)
            except OSError:
                pass
        self._temp_files.clear()


def parse_markdown_table(md_text):
    """解析 Markdown 表格文本为 [headers, ...rows] 格式"""
    lines = [l.strip() for l in md_text.strip().split("\n") if l.strip()]
    if len(lines) < 2:
        return None

    def split_cells(line):
        cells = [c.strip() for c in line.strip("|").split("|")]
        return cells

    headers = split_cells(lines[0])
    if not headers:
        return None

    data_start = 1
    # 跳过分隔行 (|---|---|)
    if data_start < len(lines):
        sep = split_cells(lines[data_start])
        if all(c.replace("-", "").replace(":", "").replace(" ", "") == "" for c in sep):
            data_start += 1

    rows = []
    for line in lines[data_start:]:
        cells = split_cells(line)
        rows.append(cells)

    return [headers] + rows


if __name__ == "__main__":
    import json
    import sys
    from datetime import datetime

    # Windows 下默认 stdin/stdout 编码可能为 GBK（cp936），导致中文路径/内容乱码或
    # surrogate 编码异常。显式重配置为 UTF-8，与 Go 端 chcp 65001 + PYTHONIOENCODING=utf-8 对齐。
    try:
        sys.stdin.reconfigure(encoding='utf-8')
        sys.stdout.reconfigure(encoding='utf-8')
    except (AttributeError, ValueError):
        pass  # Python < 3.7 或已重配置，忽略

    try:
        raw = sys.stdin.read()
        if not raw:
            print(json.dumps({"success": False, "error": "stdin 为空"}, ensure_ascii=False))
            sys.exit(1)
        data = json.loads(raw)
    except Exception as e:
        print(json.dumps({"success": False, "error": f"解析输入失败: {e}"}, ensure_ascii=False))
        sys.exit(1)

    try:
        mode = data.get("mode", "data")
        title = data.get("title", "数据分析报告")
        output_path = data.get("outputPath", "exports/output.docx")
        # 规范化输出路径：去掉前导 / 使其成为相对路径，确保 Windows 下保存到项目 exports/ 目录
        # （/exports/x.docx 在 Windows 下会解析到驱动器根 D:\exports\，而非项目目录）
        if output_path.startswith("/"):
            output_path = output_path.lstrip("/")
        builder = WordBuilder(title=title)

        builder.add_cover(title, date=datetime.now().strftime("%Y-%m-%d"))
        builder.add_toc_placeholder()

        if mode == "content":
            sections = data.get("sections", [])
            for sec in sections:
                sec_title = sec.get("title", "")
                builder.add_heading(sec_title, level=2)
                blocks = sec.get("blocks", [])
                for b in blocks:
                    content = b.get("content", "")
                    block_type = b.get("type", "text")
                    if block_type == "text" or block_type == "paragraph":
                        builder.add_paragraph(content)
                    elif block_type == "heading":
                        level = b.get("level", 2)
                        builder.add_heading(content, level=min(max(int(level), 1), 4))
                    elif block_type == "bullet":
                        items = [item for item in content.split("\n") if item.strip()]
                        builder.add_bullet_list(items)
                    elif block_type == "list":
                        items = [item for item in content.split("\n") if item.strip()]
                        builder.add_bullet_list(items)
                    elif block_type == "h1" or block_type == "h2":
                        builder.add_heading(content, level=2 if block_type == "h2" else 1)
                    elif block_type == "h3":
                        builder.add_heading(content, level=3)
                    elif block_type == "table":
                        # 支持两种格式：markdown 字符串 或 list-of-lists（[[header...], [row...], ...]）
                        if isinstance(content, list):
                            rows_data = content if content else None
                        else:
                            rows_data = parse_markdown_table(content)
                        if rows_data and len(rows_data) >= 1:
                            headers = [str(h) for h in rows_data[0]]
                            body = [[str(c) for c in row] for row in rows_data[1:]]
                            builder.add_table(headers, body, caption="")
                    elif block_type == "chart":
                        # content 模式图表：支持 {labels, values} 简单格式
                        chart_type = b.get("chartType", "bar")
                        chart_title = b.get("title", "")
                        chart_data = b.get("data", {})
                        labels = chart_data.get("labels", [])
                        values = chart_data.get("values", [])
                        if labels and values:
                            internal_data = {
                                "categories": [str(l) for l in labels],
                                "series": [{"name": chart_title, "values": list(values)}],
                                "title": chart_title,
                            }
                            try:
                                builder.add_chart(chart_type, internal_data, caption=chart_title)
                            except Exception:
                                builder.add_paragraph(f"[图表渲染失败: {chart_title}]")
                    elif block_type == "code":
                        builder.add_code_block(content)
                    else:
                        builder.add_paragraph(content)
        else:
            columns = data.get("columns", [])
            rows = data.get("data", [])
            findings = data.get("findings", [])
            numeric_stats = data.get("numericStats", [])
            chart_paths = data.get("chartPaths", [])
            include_charts = data.get("includeCharts", False)

            builder.add_heading("数据概览", level=1)
            builder.add_paragraph(f"本次分析共返回 {len(rows)} 条记录，包含 {len(columns)} 个字段。")

            if numeric_stats:
                builder.add_heading("关键指标", level=2)
                for ns in numeric_stats:
                    col_name = ns.get("column", "")
                    builder.add_paragraph(
                        f"{col_name}: 计数={ns.get('count', 0)}, "
                        f"均值={ns.get('avg', 0):.2f}, "
                        f"最小={ns.get('min', 0):.2f}, "
                        f"最大={ns.get('max', 0):.2f}, "
                        f"标准差={ns.get('stddev', 0):.2f}"
                    )

            if findings:
                builder.add_heading("分析洞察", level=2)
                builder.add_bullet_list(findings)

            if columns and rows:
                GROUP_COLUMNS = {"表名", "table_name", "TABLE_NAME", "表名称"}
                group_col = None
                for c in columns:
                    if c in GROUP_COLUMNS:
                        group_col = c
                        break

                if group_col:
                    from collections import OrderedDict
                    groups = OrderedDict()
                    for row in rows:
                        gk = str(row.get(group_col, ""))
                        groups.setdefault(gk, []).append(row)

                    builder.add_heading("数据明细", level=2)
                    builder.add_paragraph(
                        f"共 {len(rows)} 条记录，按「{group_col}」分为 {len(groups)} 组展示。"
                    )

                    other_cols = [c for c in columns if c != group_col]
                    for gk, gk_rows in groups.items():
                        builder.add_heading(f"{group_col}: {gk}（{len(gk_rows)} 条）", level=3)
                        display_rows = []
                        for row in gk_rows:
                            display_rows.append([str(row.get(c, "")) for c in other_cols])
                        if other_cols and display_rows:
                            builder.add_table(other_cols, display_rows, caption="")
                else:
                    builder.add_heading("数据明细", level=2)
                    headers = columns
                    display_rows = []
                    for row in rows:
                        display_rows.append([str(row.get(c, "")) for c in columns])
                    max_display = 8000
                    if len(display_rows) > max_display:
                        display_rows = display_rows[:max_display]
                        builder.add_paragraph(f"（仅展示前 {max_display} 条记录，共 {len(rows)} 条）")
                    if headers and display_rows:
                        builder.add_table(headers, display_rows, caption="数据明细表")

            if include_charts and chart_paths:
                builder.add_heading("图表分析", level=2)
                for cp in chart_paths:
                    if os.path.exists(cp):
                        builder.add_chart("bar", {
                            "title": "",
                            "categories": [],
                            "series": []
                        }, caption=os.path.basename(cp))
                        # 实际图表通过 Go 端生成的图片文件插入
                        p = builder.doc.add_paragraph()
                        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
                        run = p.add_run()
                        run.add_picture(cp, width=Inches(5.5))
                        builder.doc.add_paragraph()

        builder.save(output_path)
        # 返回 URL 路径（/exports/...），供前端下载链接使用
        url_path = "/" + output_path.replace("\\", "/").lstrip("/")
        print(json.dumps({"success": True, "outputPath": url_path}, ensure_ascii=False))
    except Exception as e:
        # 异常消息可能含 surrogate 字符（如 matplotlib 字体错误），需清理后再输出
        safe_err = str(e).encode('utf-8', errors='replace').decode('utf-8')
        print(json.dumps({"success": False, "error": safe_err}, ensure_ascii=False))
        sys.exit(1)
