#!/usr/bin/env python3
"""
专业 PPT 演示文稿导出 — WebSQL AI 平台
深度利用 python-pptx 能力，生成符合中国商务审美的演示文稿。
"""

import json
import logging
import os
import sys
from datetime import datetime

from pptx import Presentation
from pptx.util import Inches, Pt, Cm, Emu
from pptx.dml.color import RGBColor
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pptx.oxml.ns import qn

logger = logging.getLogger("websql.skill.export_ppt")
logger.setLevel(logging.INFO)

_log_dir = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
_log_file = os.path.join(_log_dir, "skill_export_ppt.log")
try:
    file_handler = logging.FileHandler(_log_file, encoding="utf-8")
    file_handler.setFormatter(logging.Formatter("%(asctime)s %(name)s: %(message)s", datefmt="%Y/%m/%d %H:%M:%S"))
    logger.addHandler(file_handler)
except Exception:
    pass

# 品牌色 — 中国商务审美
C_PRIMARY = RGBColor(0x1A, 0x3C, 0x6D)
C_ACCENT = RGBColor(0xC0, 0x39, 0x2B)
C_GOLD = RGBColor(0x8B, 0x69, 0x14)
C_BG_DARK = RGBColor(0x0D, 0x21, 0x37)
C_BG_MID = RGBColor(0x1A, 0x3C, 0x6D)
C_BG_LIGHT = RGBColor(0x28, 0x35, 0x93)
C_WHITE = RGBColor(0xFF, 0xFF, 0xFF)
C_TEXT = RGBColor(0x2C, 0x2C, 0x2C)
C_MUTED = RGBColor(0x66, 0x66, 0x66)
C_LIGHT_MUTED = RGBColor(0x99, 0x99, 0x99)
C_SLIDE_BG = RGBColor(0xF7, 0xF8, 0xFA)
C_TABLE_HEADER = RGBColor(0x1A, 0x3C, 0x6D)
C_TABLE_STRIPE = RGBColor(0xF2, 0xF4, 0xF8)
C_ACCENT_LIGHT = RGBColor(0xE8, 0xC5, 0xC4)

FONT_CN = "Microsoft YaHei"
FONT_EN = "Calibri"
W = Inches(13.333)
H = Inches(7.5)


def _clean_surrogates(text):
    if not isinstance(text, str):
        return str(text)
    return "".join(ch for ch in text if not (0xD800 <= ord(ch) <= 0xDFFF))


def _gradient_bg(slide, c1, c2, c3):
    fill = slide.background.fill
    fill.gradient()
    fill.gradient_angle = 90.0

    from lxml import etree
    gsLst = fill._fill._gradFill.get_or_add_gsLst()
    gs_ns = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
    while len(gsLst) < 3:
        new_gs = etree.SubElement(gsLst, f"{gs_ns}gs")
        new_gs.set("pos", "100000")
        srgb = etree.SubElement(new_gs, f"{gs_ns}srgbClr")
        srgb.set("val", "000000")

    fill.gradient_stops[0].position = 0.0
    fill.gradient_stops[0].color.rgb = c1
    fill.gradient_stops[1].position = 0.5
    fill.gradient_stops[1].color.rgb = c2
    fill.gradient_stops[2].position = 1.0
    fill.gradient_stops[2].color.rgb = c3


def _solid_bg(slide, color):
    fill = slide.background.fill
    fill.solid()
    fill.fore_color.rgb = color


def _font(run, name=FONT_CN, size=18, bold=False, color=None):
    run.font.name = name
    el = run.font._element
    el.get_or_add_latin().set("typeface", name)
    ea = el.makeelement("{http://schemas.openxmlformats.org/drawingml/2006/main}ea")
    ea.set("typeface", name)
    el.append(ea)
    run.font.size = Pt(size)
    run.font.bold = bold
    if color:
        run.font.color.rgb = color


def _accent_bar(slide, top=0, height=0.06, color=C_ACCENT):
    shape = slide.shapes.add_shape(1, Inches(0), Inches(top), W, Inches(height))
    shape.fill.solid()
    shape.fill.fore_color.rgb = color
    shape.line.fill.background()


def _left_bar(slide, left=0.8, top=1.2, height=5.6, width=0.06, color=C_ACCENT):
    shape = slide.shapes.add_shape(1, Inches(left), Inches(top),
                                   Inches(width), Inches(height))
    shape.fill.solid()
    shape.fill.fore_color.rgb = color
    shape.line.fill.background()


def _textbox(slide, left, top, width, height, text, size=18, bold=False,
             color=C_TEXT, align=PP_ALIGN.LEFT, font=FONT_CN):
    text = _clean_surrogates(text)
    tb = slide.shapes.add_textbox(Inches(left), Inches(top),
                                  Inches(width), Inches(height))
    tb.text_frame.word_wrap = True
    tf = tb.text_frame
    for i, line in enumerate(text.split("\n")):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        p.alignment = align
        r = p.add_run()
        r.text = line
        _font(r, font, size, bold, color)
    return tb


def _page_num(slide, n, total):
    _textbox(slide, 11.8, 7.08, 1.3, 0.3, f"{n} / {total}",
             size=9, color=C_LIGHT_MUTED, align=PP_ALIGN.RIGHT)


def _section_num(slide, num, subtitle):
    _textbox(slide, 0.55, 6.6, 2.0, 0.5, f"\u2500\u2500 {subtitle}",
             size=10, color=C_LIGHT_MUTED, align=PP_ALIGN.LEFT)


# ═══════ 封面 ═══════
def _title_slide(prs, title, subtitle, presenter=""):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _gradient_bg(slide, C_BG_DARK, C_BG_MID, C_BG_LIGHT)

    _accent_bar(slide, 2.6, 0.05, C_ACCENT)

    _textbox(slide, 1.8, 1.8, 9.5, 2.4, _clean_surrogates(title),
             size=44, bold=True, color=C_WHITE, align=PP_ALIGN.LEFT)

    _textbox(slide, 1.8, 3.9, 9.5, 0.7, _clean_surrogates(subtitle),
             size=22, color=C_ACCENT_LIGHT, align=PP_ALIGN.LEFT)

    now = datetime.now().strftime("%Y年%m月%d日")
    info = f"◆  {now}"
    if presenter:
        info += f"  |  汇报人：{_clean_surrogates(presenter)}"
    info += f"  |  WebSQL AI 平台"
    _textbox(slide, 1.8, 4.7, 9.5, 0.5, info, size=13, color=C_LIGHT_MUTED)

    _textbox(slide, 1.8, 6.2, 9.5, 0.4, "内部资料 · 请注意保密",
             size=10, color=C_LIGHT_MUTED)


# ═══════ 目录 ═══════
def _toc_slide(prs, sections, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _solid_bg(slide, C_SLIDE_BG)
    _accent_bar(slide)

    _textbox(slide, 1.2, 0.4, 10.0, 0.9, "目  录",
             size=32, bold=True, color=C_PRIMARY)

    _left_bar(slide, 1.8, 1.6, 5.2, 0.04, C_PRIMARY)

    y = 1.8
    nums_cn = ["一", "二", "三", "四", "五", "六", "七", "八", "九", "十"]
    for i, sec in enumerate(sections[:10]):
        num_str = f"0{i+1}" if i < 9 else str(i+1)
        _textbox(slide, 2.2, y, 1.2, 0.5, num_str,
                 size=28, bold=True, color=C_ACCENT, font=FONT_EN)
        _textbox(slide, 3.4, y + 0.05, 8.0, 0.5, _clean_surrogates(sec["title"]),
                 size=20, bold=True, color=C_TEXT)
        if sec.get("desc"):
            _textbox(slide, 3.4, y + 0.45, 8.0, 0.3, _clean_surrogates(sec["desc"]),
                     size=11, color=C_MUTED)
        y += 0.9 if sec.get("desc") else 0.65

    _page_num(slide, slide_num, total)


# ═══════ 过渡页 ═══════
def _section_slide(prs, num, title, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _gradient_bg(slide, C_BG_DARK, C_BG_MID, C_BG_LIGHT)

    nums_cn = ["一", "二", "三", "四", "五", "六", "七", "八", "九", "十"]
    _textbox(slide, 1.5, 2.2, 2.0, 1.2, f"PART 0{num}" if num < 10 else f"PART {num}",
             size=40, bold=True, color=C_ACCENT, font=FONT_EN)
    _textbox(slide, 1.5, 3.6, 10.0, 0.8, f"第{nums_cn[num-1] if num <= 10 else num}部分",
             size=20, color=C_LIGHT_MUTED)
    _textbox(slide, 1.5, 4.1, 10.0, 1.0, _clean_surrogates(title),
             size=32, bold=True, color=C_WHITE)

    _page_num(slide, slide_num, total)


# ═══════ 数据汇总 ═══════
def _summary_slide(prs, qr, numeric_cols, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _solid_bg(slide, C_SLIDE_BG)
    _accent_bar(slide)

    _textbox(slide, 1.2, 0.35, 10.0, 0.8, "数据全景",
             size=30, bold=True, color=C_PRIMARY)

    total_rows = qr.get("totalRows", 0)
    total_cols = qr.get("totalCols", 0)
    lines = [f"▸  数据规模：{total_rows} 条记录  ·  {total_cols} 个维度"]
    if numeric_cols:
        cols_text = _clean_surrogates('、'.join(numeric_cols[:6]))
        lines.append(f"▸  核心指标：{cols_text}")

    stats = qr.get("stats", {})
    stat_lines = []
    for col, info in list(stats.items())[:8]:
        col_clean = _clean_surrogates(col)
        stat_lines.append(f"\u25E6  {col_clean}  |  均值 {info['avg']:,.2f}  |  区间 [{info['min']:,.2f}, {info['max']:,.2f}]")

    if stat_lines:
        lines.append("")
        lines.append("\u25B8  指标统计：")
        lines.extend(stat_lines)

    _textbox(slide, 1.2, 1.6, 10.5, 5.0, "\n".join(lines),
             size=16, color=C_TEXT)

    _page_num(slide, slide_num, total)


# ═══════ 统计表 ═══════
def _table_slide(prs, title, columns, data_rows, slide_num, total, max_rows=12):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _solid_bg(slide, C_SLIDE_BG)
    _accent_bar(slide)

    _textbox(slide, 1.2, 0.3, 10.0, 0.7, title,
             size=26, bold=True, color=C_PRIMARY)

    dc = min(len(columns), 7)
    dr = min(len(data_rows), max_rows)
    rows_total = dr + 1
    row_h = 0.35

    tbl = slide.shapes.add_table(rows_total, dc,
                                 Inches(1.2), Inches(1.2),
                                 Inches(10.5), Inches(row_h * rows_total)).table

    for c in range(dc):
        cell = tbl.cell(0, c)
        col_name = _clean_surrogates(columns[c][:16])
        cell.text = col_name
        for p in cell.text_frame.paragraphs:
            p.alignment = PP_ALIGN.CENTER
            for run in p.runs:
                _font(run, size=10, bold=True, color=C_WHITE)
        cell.fill.solid()
        cell.fill.fore_color.rgb = C_TABLE_HEADER

    for r in range(dr):
        bg = C_WHITE if r % 2 == 0 else C_TABLE_STRIPE
        for c in range(dc):
            cell = tbl.cell(r + 1, c)
            val = _clean_surrogates(str(data_rows[r].get(columns[c], "")))
            if len(val) > 24:
                val = val[:24] + "…"
            cell.text = val
            for p in cell.text_frame.paragraphs:
                for run in p.runs:
                    _font(run, size=9.5)
            cell.fill.solid()
            cell.fill.fore_color.rgb = bg

    if len(data_rows) > dr:
        _textbox(slide, 1.2, 1.2 + row_h * rows_total + 0.15, 10.5, 0.3,
                 f"\u203B 展示 {dr}/{len(data_rows)} 条", size=11, color=C_LIGHT_MUTED)

    _page_num(slide, slide_num, total)


# ═══════ 图表页 ═══════
def _chart_slide(prs, chart_title, chart_path, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _solid_bg(slide, C_SLIDE_BG)
    _accent_bar(slide)

    _textbox(slide, 1.2, 0.35, 10.5, 0.7, _clean_surrogates(chart_title),
             size=26, bold=True, color=C_PRIMARY)

    if os.path.exists(chart_path):
        slide.shapes.add_picture(chart_path, Inches(1.2), Inches(1.4),
                                 Inches(10.5), Inches(5.2))

    _page_num(slide, slide_num, total)


# ═══════ 亮点总结 ═══════
def _highlights_slide(prs, highlights, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _solid_bg(slide, C_SLIDE_BG)
    _accent_bar(slide)

    _textbox(slide, 1.2, 0.35, 10.0, 0.8, "核心发现与建议",
             size=30, bold=True, color=C_PRIMARY)

    if not highlights:
        highlights = ["本次分析未发现显著异常模式。"]

    text = "\n\n".join(f"\u25B6  {_clean_surrogates(h)}" for h in highlights[:6])
    _textbox(slide, 1.2, 1.6, 10.5, 5.2, text, size=18, color=C_TEXT)

    _page_num(slide, slide_num, total)


# ═══════ 结束页 ═══════
def _ending_slide(prs, title, slide_num, total):
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    _gradient_bg(slide, C_BG_DARK, C_BG_MID, C_BG_LIGHT)

    _textbox(slide, 2.0, 2.5, 9.5, 1.2, "感谢聆听",
             size=48, bold=True, color=C_WHITE, align=PP_ALIGN.CENTER)

    _accent_bar(slide, 3.6, 0.04, C_ACCENT)

    _textbox(slide, 2.0, 3.9, 9.5, 0.8, _clean_surrogates(title),
             size=20, color=C_LIGHT_MUTED, align=PP_ALIGN.CENTER)

    now = datetime.now().strftime("%Y年%m月%d日")
    _textbox(slide, 2.0, 5.0, 9.5, 0.6,
             f"WebSQL AI · 智能数据分析平台  |  {now}",
             size=14, color=C_LIGHT_MUTED, align=PP_ALIGN.CENTER)

    _page_num(slide, slide_num, total)


# ═══════ 主入口 ═══════
def main():
    logger.info("[export_ppt] 开始生成 PPT 演示文稿")
    try:
        d = json.loads(sys.stdin.read())
    except json.JSONDecodeError as e:
        logger.error(f"[export_ppt] JSON 解析失败: {e}")
        sys.exit(1)

    mode = d.get("mode", "data")
    title = d.get("title", "数据分析报告")
    subtitle = d.get("subtitle", "专业数据分析报告")
    presenter = d.get("presenter", "")
    output_path = d.get("outputPath", "exports/slides.pptx")

    logger.info(f"[export_ppt] 模式: {mode}, 标题: {title}")

    try:
        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    except OSError as e:
        logger.error(f"[export_ppt] 创建目录失败: {e}")
        sys.exit(1)

    prs = Presentation()
    prs.slide_width = W
    prs.slide_height = H

    if mode == "content":
        sections = d.get("sections", [])
        toc_items = [{"title": s["title"], "desc": s.get("desc", ""),
                      "blocks": s.get("blocks", [])} for s in sections]
        total = len(sections) + 3  # 封面 + 目录 + N内容 + 结束

        _title_slide(prs, title, subtitle, presenter)
        _toc_slide(prs, toc_items, 2, total)

        sn = 3
        for i, sec in enumerate(sections):
            _section_slide(prs, i + 1, sec["title"], sn, total)
            sn += 1
            blocks = sec.get("blocks", [])

            text_lines = []
            for b in blocks:
                t, c = b.get("type", ""), b.get("content", "")
                if t == "h3":
                    text_lines.append(f"\u25B8  {_clean_surrogates(c)}")
                elif t == "paragraph":
                    text_lines.append(_clean_surrogates(c))
                elif t == "list":
                    for item in c.split("\n"):
                        clean = item.strip("- ").strip()
                        if clean:
                            text_lines.append(f"\u2022  {_clean_surrogates(clean)}")
                elif t in ("code", "mermaid", "table", "sql"):
                    text_lines.append(f"[{t}]  {_clean_surrogates(c.split(chr(10))[0][:60])}...")
                else:
                    text_lines.append(_clean_surrogates(c))

            text = "\n\n".join(text_lines) if text_lines else _clean_surrogates(sec["title"])
            content_slide = prs.slides.add_slide(prs.slide_layouts[6])
            _solid_bg(content_slide, C_SLIDE_BG)
            _accent_bar(content_slide)
            _textbox(content_slide, 1.2, 0.35, 10.0, 0.8, _clean_surrogates(sec["title"]),
                     size=26, bold=True, color=C_PRIMARY)
            _textbox(content_slide, 1.2, 1.5, 10.5, 5.2, text, size=16, color=C_TEXT)
            _page_num(content_slide, sn, total)
            sn += 1

        _ending_slide(prs, title, total, total)
    else:
        columns = d.get("columns", [])
        data_rows = d.get("data", [])
        qr_summary = d.get("summary", {})
        numeric_cols = d.get("numericColumns", [])
        chart_paths = d.get("chartPaths", [])
        highlights = d.get("highlights", [])

        has_charts = len(chart_paths) > 0
        has_data_table = len(data_rows) > 0

        total = 3  # 封面 + 数据全景 + 结束
        if has_charts:
            total += 1
        if has_data_table:
            total += 1
        if len(data_rows) > 5:
            total += 1

        sn = 1
        _title_slide(prs, title, subtitle, presenter)

        if has_charts:
            sn += 1
            _chart_slide(prs, "数据洞察 \u00b7 关键指标趋势", chart_paths[0], sn, total)

        sn += 1
        _summary_slide(prs, qr_summary, numeric_cols, sn, total)

        if has_data_table:
            sn += 1
            _table_slide(prs, "数据明细 \u00b7 原始记录", columns, data_rows, sn, total)

        if total > sn:
            sn += 1
            _highlights_slide(prs, highlights, sn, total)

        _ending_slide(prs, title, total, total)

    try:
        prs.save(output_path)
        logger.info(f"[export_ppt] PPT 演示文稿已生成: {output_path} ({len(prs.slides)}页)")
        print(json.dumps({
            "success": True,
            "path": output_path,
            "slideCount": len(prs.slides),
            "message": f"PPT 演示文稿已生成，共 {len(prs.slides)} 页"
        }))
    except Exception as e:
        logger.error(f"[export_ppt] 保存文件失败: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
