#!/usr/bin/env python3
"""
专业 Word 数据分析报告导出 — WebSQL AI 平台
深度利用 python-docx 能力，生成符合中国商务审美的排版精美、层次分明的报告。
"""

import json
import logging
import os
import sys
from datetime import datetime

from docx import Document
from docx.shared import Pt, Inches, Cm, RGBColor, Emu
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_LINE_SPACING
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.section import WD_ORIENT
from docx.oxml.ns import qn, nsdecls
from docx.oxml import parse_xml, OxmlElement

logger = logging.getLogger("websql.skill.export_word")
logger.setLevel(logging.INFO)

_log_dir = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
_log_file = os.path.join(_log_dir, "skill_export_word.log")
try:
    file_handler = logging.FileHandler(_log_file, encoding="utf-8")
    file_handler.setFormatter(logging.Formatter("%(asctime)s %(name)s: %(message)s", datefmt="%Y/%m/%d %H:%M:%S"))
    logger.addHandler(file_handler)
except Exception:
    pass

# 品牌色系 — 中国商务审美
C_PRIMARY = "1A3C6D"
C_ACCENT = "C0392B"
C_GOLD = "8B6914"
C_TITLE = "0D2137"
C_TEXT = "2C2C2C"
C_MUTED = "666666"
C_LIGHT_MUTED = "999999"
C_BG_SECTION = "F0F3F7"
C_TABLE_HEADER = "1A3C6D"
C_TABLE_STRIPE = "F4F6FA"
C_BORDER = "CFD8DC"
C_DIVIDER = "C0392B"
C_COVER_BG = "0D2137"

FONT_CN = "Microsoft YaHei"
FONT_EN = "Calibri"


def _clean_surrogates(text):
    if not isinstance(text, str):
        return str(text)
    return "".join(ch for ch in text if not (0xD800 <= ord(ch) <= 0xDFFF))


def rgb(h):
    h = h.lstrip("#")
    return RGBColor(int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))


def _cell_shading(cell, color):
    shading = parse_xml(f'<w:shd {nsdecls("w")} w:fill="{color}" w:val="clear"/>')
    cell._tc.get_or_add_tcPr().append(shading)


def _set_border(cell, **edges):
    tc = cell._tc
    tcPr = tc.get_or_add_tcPr()
    tcBorders = OxmlElement("w:tcBorders")
    for edge, val in edges.items():
        element = OxmlElement(f"w:{edge}")
        element.set(qn("w:val"), val.get("val", "single"))
        element.set(qn("w:sz"), val.get("sz", "4"))
        element.set(qn("w:color"), val.get("color", C_BORDER))
        element.set(qn("w:space"), "0")
        tcBorders.append(element)
    tcPr.append(tcBorders)


def _para_format(p, before=0, after=0, spacing=1.25, align=None, indent_first=None):
    pf = p.paragraph_format
    pf.space_before = Pt(before)
    pf.space_after = Pt(after)
    pf.line_spacing = spacing
    if align is not None:
        p.alignment = align
    if indent_first is not None:
        pf.first_line_indent = Cm(indent_first)


def _run(p, text, font=FONT_CN, size=11, bold=False, color=None, italic=False):
    r = p.add_run()
    r.font.name = font
    r._element.rPr.rFonts.set(qn("w:eastAsia"), font)
    r.font.size = Pt(size)
    r.bold = bold
    r.italic = italic
    if color:
        r.font.color.rgb = rgb(color)
    r.text = _clean_surrogates(text)
    return r


def _heading(doc, num, text, level=1):
    """层次分明的标题，一级带章节编号和装饰线"""
    if level == 1:
        p = doc.add_paragraph()
        _para_format(p, before=28, after=10, spacing=1.2)
        # 装饰色块
        _run(p, "\u2588", size=18, bold=True, color=C_ACCENT)
        _run(p, "  ", size=8)
        if num:
            _run(p, f"第{_num_cn(num)}章  ", size=22, bold=True, color=C_PRIMARY)
        _run(p, text, size=22, bold=True, color=C_TITLE)
        _divider(doc, C_ACCENT)
    elif level == 2:
        p = doc.add_paragraph()
        _para_format(p, before=20, after=6, spacing=1.2)
        _run(p, "\u2503 ", size=16, bold=True, color=C_PRIMARY)
        _run(p, text, size=16, bold=True, color=C_PRIMARY)
    else:
        p = doc.add_paragraph()
        _para_format(p, before=14, after=4, spacing=1.2)
        _run(p, "\u2502 ", size=14, bold=True, color=C_MUTED)
        _run(p, text, size=14, bold=True, color=C_TEXT)


def _divider(doc, color=C_DIVIDER):
    p = doc.add_paragraph()
    _para_format(p, before=4, after=4)
    border = parse_xml(
        f'<w:pBdr {nsdecls("w")}>'
        f'<w:bottom w:val="single" w:sz="4" w:space="4" w:color="{color}"/>'
        f'</w:pBdr>'
    )
    p._p.get_or_add_pPr().append(border)


def _body_para(doc, text, first_indent=True):
    """正文段落 — 首行缩进两字符"""
    p = doc.add_paragraph()
    indent = 0.74 if first_indent else 0
    _para_format(p, before=2, after=4, spacing=1.6, indent_first=indent)
    _run(p, text, size=11, color=C_TEXT)
    return p


def _num_cn(n):
    nums = ["一", "二", "三", "四", "五", "六", "七", "八", "九", "十"]
    if 1 <= n <= 10:
        return nums[n - 1]
    return str(n)


# ─── 封面 ───────────────────────────────────────────
def _cover(doc, title, subtitle, dept=""):
    section = doc.sections[0]
    section.top_margin = Cm(2.0)
    section.bottom_margin = Cm(1.5)
    section.left_margin = Cm(2.5)
    section.right_margin = Cm(2.5)

    # 顶部品牌栏
    hdr_para = doc.add_paragraph()
    _para_format(hdr_para, before=0, after=0, align=WD_ALIGN_PARAGRAPH.RIGHT)
    _run(hdr_para, "WebSQL AI · 智能数据分析平台", size=9, color=C_LIGHT_MUTED)

    for _ in range(5):
        doc.add_paragraph()

    _divider(doc, C_ACCENT)

    p = doc.add_paragraph()
    _para_format(p, before=24, after=8, align=WD_ALIGN_PARAGRAPH.CENTER, spacing=1.1)
    _run(p, title, size=30, bold=True, color=C_COVER_BG, font=FONT_EN)
    # 中文标题
    p2 = doc.add_paragraph()
    _para_format(p2, before=0, after=18, align=WD_ALIGN_PARAGRAPH.CENTER, spacing=1.1)
    _run(p2, title, size=30, bold=True, color=C_COVER_BG)

    _divider(doc, C_ACCENT)

    p3 = doc.add_paragraph()
    _para_format(p3, before=20, after=4, align=WD_ALIGN_PARAGRAPH.CENTER)
    _run(p3, subtitle, size=15, color=C_MUTED)

    for _ in range(6):
        doc.add_paragraph()

    info_lines = [
        f"编制单位：{dept}" if dept else "编制单位：WebSQL AI 数据分析中心",
        f"编制日期：{datetime.now().strftime('%Y年%m月%d日')}",
        f"报告编号：WS-RPT-{datetime.now().strftime('%Y%m%d')}-{os.urandom(2).hex().upper()}",
        "密级：内部资料",
    ]
    for line in info_lines:
        p = doc.add_paragraph()
        _para_format(p, before=1, after=1, align=WD_ALIGN_PARAGRAPH.CENTER)
        _run(p, line, size=10, color=C_LIGHT_MUTED)

    doc.add_page_break()


# ─── 报告摘要 ───────────────────────────────────────
def _executive_summary(doc, rows, cols, numeric_cols, stats):
    _heading(doc, 1, "报告摘要")

    _body_para(doc, f"本报告基于对数据库查询结果的系统分析，共涉及 {cols} 个数据维度、{rows} 条业务记录。"
               f"报告从数据概览、统计特征、趋势变化、分布规律等角度进行了全方位剖析，"
               f"旨在为业务决策提供数据支撑与参考依据。")

    if numeric_cols:
        _body_para(doc, f"经自动识别，数据集中包含 {len(numeric_cols)} 个数值型字段"
                   f"（{'、'.join(numeric_cols[:5])}{'等' if len(numeric_cols) > 5 else ''}），"
                   f"已对其进行了描述性统计分析与可视化呈现。")

    if stats:
        _heading(doc, 2, "核心指标速览")
        tbl = doc.add_table(rows=1, cols=5)
        tbl.alignment = WD_TABLE_ALIGNMENT.CENTER
        tbl.style = "Table Grid"

        hdrs = ["指标名称", "样本量", "最小值", "最大值", "均值"]
        for i, h in enumerate(hdrs):
            c = tbl.rows[0].cells[i]
            p = c.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            _run(p, h, size=9, bold=True, color="FFFFFF")
            _cell_shading(c, C_TABLE_HEADER)

        for j, s in enumerate(stats[:12]):
            row = tbl.add_row()
            bg = "FFFFFF" if j % 2 == 0 else C_TABLE_STRIPE
            vals = [
                s.get("column", ""), str(s.get("count", 0)),
                f"{s.get('min', 0):,.2f}", f"{s.get('max', 0):,.2f}",
                f"{s.get('avg', 0):,.2f}"
            ]
            for k, v in enumerate(vals):
                c = row.cells[k]
                p = c.paragraphs[0]
                p.alignment = WD_ALIGN_PARAGRAPH.CENTER
                _run(p, v, size=9, bold=(k == 0))
                _cell_shading(c, bg)

        doc.add_paragraph()

    _divider(doc, C_ACCENT)


# ─── 数据概览与质量评估 ─────────────────────────────
def _data_overview(doc, columns, rows_count, nrows=5):
    _heading(doc, 2, "数据概览与质量评估")

    _body_para(doc, f"本次分析共读取 {rows_count} 条数据记录，涉及 {len(columns)} 个字段。"
               f"以下为数据集的简要概览，涵盖核心字段及其数据样例。")

    display_cols = min(len(columns), 6)
    display_rows = min(rows_count, nrows)

    tbl = doc.add_table(rows=1, cols=display_cols)
    tbl.alignment = WD_TABLE_ALIGNMENT.CENTER
    tbl.style = "Table Grid"

    for i in range(display_cols):
        c = tbl.rows[0].cells[i]
        p = c.paragraphs[0]
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        _run(p, columns[i], size=9, bold=True, color="FFFFFF")
        _cell_shading(c, C_TABLE_HEADER)

    for j in range(display_rows):
        row = tbl.add_row()
        bg = "FFFFFF" if j % 2 == 0 else C_TABLE_STRIPE
        for k in range(display_cols):
            c = row.cells[k]
            val = str(rows_count.get(columns[k], "")) if isinstance(rows_count, dict) else ""
            # 实际调用时 data_rows 是 list
            if hasattr(rows_count, '__len__') and not isinstance(rows_count, dict):
                val = str(rows_count[j].get(columns[k], "")) if j < len(rows_count) else ""
            if len(val) > 30:
                val = val[:30] + "\u2026"
            p = c.paragraphs[0]
            _run(p, val, size=9)
            _cell_shading(c, bg)

    doc.add_paragraph()
    _divider(doc)


# ─── 统计分析与核心指标 ────────────────────────────
def _statistical_analysis(doc, numeric_stats, numeric_cols, data_rows):
    _heading(doc, 2, "统计分析与核心指标")

    if not numeric_stats:
        _body_para(doc, "当前数据集无可用于统计分析的数值型字段。")
        return

    _body_para(doc, "针对数据集中的数值型字段，已进行全面的描述性统计分析，涵盖集中趋势、"
               "离散程度及分布特征等维度。各指标详见表 1。")

    p = doc.add_paragraph()
    _para_format(p, before=4, after=8, align=WD_ALIGN_PARAGRAPH.CENTER)
    _run(p, "表 1  数值字段描述性统计汇总", size=10, bold=True, color=C_PRIMARY)

    tbl = doc.add_table(rows=1, cols=6)
    tbl.alignment = WD_TABLE_ALIGNMENT.CENTER
    tbl.style = "Table Grid"
    hdrs = ["字段名称", "有效样本", "最小值", "最大值", "均值", "标准差"]
    for i, h in enumerate(hdrs):
        c = tbl.rows[0].cells[i]
        p = c.paragraphs[0]
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        _run(p, h, size=9, bold=True, color="FFFFFF")
        _cell_shading(c, C_TABLE_HEADER)

    for j, s in enumerate(numeric_stats):
        row = tbl.add_row()
        bg = "FFFFFF" if j % 2 == 0 else C_TABLE_STRIPE
        vals = [
            s.get("column", ""), str(s.get("count", 0)),
            f"{s.get('min', 0):,.2f}", f"{s.get('max', 0):,.2f}",
            f"{s.get('avg', 0):,.2f}", f"{s.get('stddev', 0):,.2f}"
        ]
        for k, v in enumerate(vals):
            c = row.cells[k]
            p = c.paragraphs[0]
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
            _run(p, v, size=9, bold=(k == 0))
            _cell_shading(c, bg)

    doc.add_paragraph()

    # 统计解读
    _heading(doc, None, "数据特征解读", 3)
    max_std_col = max(numeric_stats, key=lambda x: x.get("stddev", 0)) if numeric_stats else None
    max_avg_col = max(numeric_stats, key=lambda x: x.get("avg", 0)) if numeric_stats else None
    if max_std_col:
        _body_para(doc, f"从离散程度来看，\"{max_std_col['column']}\"的标准差最大"
                   f"（{max_std_col['stddev']:.2f}），表明该指标在不同记录间波动较为显著，"
                   f"建议重点关注其变化规律及影响因素。")
    if max_avg_col:
        _body_para(doc, f"从集中趋势来看，\"{max_avg_col['column']}\"的均值最高"
                   f"（{max_avg_col['avg']:.2f}），是当前数据集中的核心贡献指标。")

    _divider(doc, C_ACCENT)


# ─── 数据可视化 ─────────────────────────────────────
def _visualization_section(doc, chart_paths):
    if not chart_paths:
        return
    _heading(doc, 3, "数据可视化分析")

    _body_para(doc, "为直观呈现数据特征与变化趋势，以下图表对各关键指标进行了可视化展示。"
               "图表采用统一配色方案，便于横向对比分析。")

    for i, path in enumerate(chart_paths):
        if os.path.exists(path):
            p = doc.add_paragraph()
            _para_format(p, before=14, after=2, align=WD_ALIGN_PARAGRAPH.CENTER)
            _run(p, f"图 {i+1}  关键指标可视化", size=10, bold=True, color=C_PRIMARY)
            doc.add_picture(path, width=Inches(5.6))
            p2 = doc.add_paragraph()
            _para_format(p2, before=2, after=6, align=WD_ALIGN_PARAGRAPH.CENTER)
            _run(p2, f"数据来源：实时数据库查询  |  生成时间：{datetime.now().strftime('%Y-%m-%d %H:%M')}",
                 size=8, italic=True, color=C_LIGHT_MUTED)

    _divider(doc, C_ACCENT)


# ─── 关键发现与建议 ─────────────────────────────────
def _findings(doc, findings_list):
    _heading(doc, 4, "关键发现与建议")

    if not findings_list:
        _body_para(doc, "本次分析未识别出显著的数据特征或异常模式。")
        return

    _body_para(doc, "基于上述统计分析及可视化结果，提炼出以下关键发现及对应建议：")

    for i, f in enumerate(findings_list[:10], 1):
        p = doc.add_paragraph()
        _para_format(p, before=6, after=3, spacing=1.4)
        _run(p, f"发现 {i}：", size=11, bold=True, color=C_ACCENT)
        _run(p, f, size=11, color=C_TEXT)

    doc.add_paragraph()
    _divider(doc, C_ACCENT)


# ─── 数据明细 ───────────────────────────────────────
def _data_table(doc, columns, data_rows, max_rows=20):
    _heading(doc, 5, "附录：数据明细")

    _body_para(doc, f"以下为本次分析所依据的原始数据明细（共 {len(data_rows)} 条记录），"
               f"供核查与参考。")

    display_cols = min(len(columns), 8)
    display_rows = min(len(data_rows), max_rows)

    tbl = doc.add_table(rows=1, cols=display_cols)
    tbl.alignment = WD_TABLE_ALIGNMENT.CENTER
    tbl.style = "Table Grid"

    for i in range(display_cols):
        c = tbl.rows[0].cells[i]
        p = c.paragraphs[0]
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        _run(p, columns[i], size=9, bold=True, color="FFFFFF")
        _cell_shading(c, C_TABLE_HEADER)

    for j, row_data in enumerate(data_rows[:display_rows]):
        row = tbl.add_row()
        bg = "FFFFFF" if j % 2 == 0 else C_TABLE_STRIPE
        for k in range(display_cols):
            c = row.cells[k]
            val = str(row_data.get(columns[k], ""))
            if len(val) > 32:
                val = val[:32] + "\u2026"
            p = c.paragraphs[0]
            _run(p, val, size=9)
            _cell_shading(c, bg)

    if len(data_rows) > display_rows:
        p = doc.add_paragraph()
        _para_format(p, before=4, after=2, align=WD_ALIGN_PARAGRAPH.CENTER)
        _run(p, f"\u203B 限于篇幅，仅展示前 {display_rows} 条记录，完整数据共 {len(data_rows)} 条",
             size=9, italic=True, color=C_LIGHT_MUTED)

    doc.add_paragraph()


# ─── 页眉页脚 ───────────────────────────────────────
def _setup_header_footer(doc, title):
    for section in doc.sections:
        # 页眉
        header = section.header
        header.is_linked_to_previous = False
        hp = header.paragraphs[0]
        hp.alignment = WD_ALIGN_PARAGRAPH.RIGHT
        _run(hp, f"WebSQL · {title}", size=8, color=C_LIGHT_MUTED)
        # 页眉下划线
        border = parse_xml(
            f'<w:pBdr {nsdecls("w")}>'
            f'<w:bottom w:val="single" w:sz="2" w:space="1" w:color="{C_BORDER}"/>'
            f'</w:pBdr>'
        )
        hp._p.get_or_add_pPr().append(border)

        # 页脚
        footer = section.footer
        footer.is_linked_to_previous = False
        fp = footer.paragraphs[0]
        fp.alignment = WD_ALIGN_PARAGRAPH.CENTER
        _run(fp, "\u2500 ", size=8, color=C_LIGHT_MUTED)
        # 页码域代码
        run = fp.add_run()
        fldChar1 = OxmlElement("w:fldChar")
        fldChar1.set(qn("w:fldCharType"), "begin")
        run._r.append(fldChar1)
        instrText = OxmlElement("w:instrText")
        instrText.set(qn("xml:space"), "preserve")
        instrText.text = " PAGE "
        run._r.append(instrText)
        fldChar2 = OxmlElement("w:fldChar")
        fldChar2.set(qn("w:fldCharType"), "end")
        run._r.append(fldChar2)
        _run(fp, " \u2500", size=8, color=C_LIGHT_MUTED)


# ─── 主入口 ─────────────────────────────────────────
def main():
    logger.info("[export_word] 开始生成 Word 报告")
    try:
        d = json.loads(sys.stdin.read())
    except json.JSONDecodeError as e:
        logger.error(f"[export_word] JSON 解析失败: {e}")
        sys.exit(1)

    title = d.get("title", "数据分析报告")
    subtitle = d.get("subtitle", "专业数据分析报告")
    dept = d.get("dept", "")
    columns = d.get("columns", [])
    data_rows = d.get("data", [])
    chart_paths = d.get("chartPaths", [])
    findings_list = d.get("findings", [])
    output_path = d.get("outputPath", "exports/report.docx")
    include_charts = d.get("includeCharts", True)
    numeric_cols = d.get("numericColumns", [])
    numeric_stats = d.get("numericStats", [])

    logger.info(f"[export_word] 标题: {title}, 记录数: {len(data_rows)}, 图表数: {len(chart_paths)}")

    try:
        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    except OSError as e:
        logger.error(f"[export_word] 创建目录失败: {e}")
        sys.exit(1)

    doc = Document()

    style = doc.styles["Normal"]
    style.font.name = FONT_CN
    style.font.size = Pt(11)
    style._element.rPr.rFonts.set(qn("w:eastAsia"), FONT_CN)

    # ── 封面 ──
    _cover(doc, title, subtitle, dept)

    # ── 报告正文 ──
    _executive_summary(doc, len(data_rows), len(columns), numeric_cols, numeric_stats)

    _data_overview(doc, columns, data_rows)
    _statistical_analysis(doc, numeric_stats, numeric_cols, data_rows)

    if include_charts and chart_paths:
        _visualization_section(doc, chart_paths)

    _findings(doc, findings_list)
    _data_table(doc, columns, data_rows)

    # ── 页眉页脚 ──
    _setup_header_footer(doc, title)

    try:
        doc.save(output_path)
        logger.info(f"[export_word] Word 报告已生成: {output_path}")
        print(json.dumps({
            "success": True,
            "path": output_path,
            "message": f"Word 报告已生成，共 {len(data_rows)} 条记录"
        }))
    except Exception as e:
        logger.error(f"[export_word] 保存文件失败: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
