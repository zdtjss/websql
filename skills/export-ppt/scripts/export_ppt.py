#!/usr/bin/env python3
"""
专业 PPT 演示文稿导出 — WebSQL AI 平台 v2.0
模块化架构：共享基础设施 + 模板系统 + 构建器模式
支持数据模式、内容模式和演示模式，多种配色主题。
"""

import json
import os
import sys

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
_SKILL_DIR = os.path.dirname(_SCRIPT_DIR)
_SKILLS_DIR = os.path.dirname(_SKILL_DIR)
sys.path.insert(0, _SCRIPT_DIR)
sys.path.insert(0, _SKILLS_DIR)

from shared.config import SkillConfig
from shared.colors import ColorPalette
from shared.logger import SkillLogger
from shared.utils import (
    clean_surrogates, format_date_cn, generate_report_id, ensure_output_dir, safe_json_dumps
)
from shared.exceptions import SkillError, ValidationError, FileGenerationError

from ppt_templates.chinese_business import ChineseBusinessTemplate
from ppt_templates.tech_modern import TechModernTemplate

from ppt_builders.cover import CoverBuilder
from ppt_builders.toc import TOCBuilder
from ppt_builders.section import SectionBuilder
from ppt_builders.data import DataBuilder
from ppt_builders.chart import ChartBuilder
from ppt_builders.insight import InsightBuilder
from ppt_builders.ending import EndingBuilder

TEMPLATE_MAP = {
    "chinese_business": ChineseBusinessTemplate,
    "tech_modern": TechModernTemplate,
}

logger = SkillLogger("websql.skill.export_ppt")


class PPTExporter:
    """PPT 演示文稿导出器"""

    def __init__(self, template_name="chinese_business", config=None):
        self.config = config or SkillConfig()
        scheme = ColorPalette.get(template_name)
        template_cls = TEMPLATE_MAP.get(template_name, ChineseBusinessTemplate)
        self.template = template_cls(scheme=scheme)
        self._init_builders()

    def _init_builders(self):
        self.cover = CoverBuilder(self.template)
        self.toc_builder = TOCBuilder(self.template)
        self.section_builder = SectionBuilder(self.template)
        self.data = DataBuilder(self.template)
        self.chart = ChartBuilder(self.template)
        self.insight = InsightBuilder(self.template)
        self.ending = EndingBuilder(self.template)

    def _validate_data_mode(self, d):
        if not d.get("title"):
            raise ValidationError("缺少必要字段: title")
        if "data" in d and not isinstance(d["data"], list):
            raise ValidationError("data 字段必须是数组")

    def _validate_content_mode(self, d):
        if not d.get("title"):
            raise ValidationError("缺少必要字段: title")
        sections = d.get("sections", [])
        if not isinstance(sections, list):
            raise ValidationError("sections 字段必须是数组")

    def export_data_mode(self, d):
        self._validate_data_mode(d)
        prs = self.template.create_presentation()

        title = d.get("title", "数据分析报告")
        subtitle = d.get("subtitle", "专业数据分析报告")
        presenter = d.get("presenter", "")
        output_path = d.get("outputPath", "exports/slides.pptx")

        columns = d.get("columns", [])
        data_rows = d.get("data", [])
        qr_summary = d.get("summary", {})
        numeric_cols = d.get("numericColumns", [])
        chart_paths = d.get("chartPaths", [])
        highlights = d.get("highlights", [])

        has_charts = len(chart_paths) > 0
        has_data_table = len(data_rows) > 0

        total = 3
        if has_charts:
            total += len(chart_paths)
        if has_data_table:
            total += 1
        if len(data_rows) > 5:
            total += 1

        sn = 1
        logger.info(f"生成封面 [sn={sn}]")
        self.cover.build(prs, title, subtitle, presenter)

        if has_charts:
            for cp in chart_paths:
                sn += 1
                logger.info(f"生成图表页 [sn={sn}] path={cp}")
                fn = os.path.basename(cp)
                chart_title = fn.rsplit(".", 1)[0].replace("_", " ")
                self.chart.build_single(prs, chart_title, cp, sn, total)

        sn += 1
        logger.info(f"生成数据全景 [sn={sn}]")
        self.data.build_summary(prs, qr_summary, numeric_cols, sn, total)

        if has_data_table:
            sn += 1
            logger.info(f"生成数据明细表 [sn={sn}] rows={len(data_rows)}")
            self.data.build_table(prs, "数据明细 · 原始记录", columns, data_rows, sn, total)

        if total > sn:
            sn += 1
            logger.info(f"生成核心发现 [sn={sn}]")
            self.insight.build(prs, highlights, sn, total)

        logger.info(f"生成结束页 [sn={total}]")
        self.ending.build(prs, title, total, total)

        return prs, output_path

    def export_content_mode(self, d):
        self._validate_content_mode(d)
        prs = self.template.create_presentation()

        title = d.get("title", "演示文稿")
        subtitle = d.get("subtitle", "")
        presenter = d.get("presenter", "")
        output_path = d.get("outputPath", "exports/slides.pptx")
        sections = d.get("sections", [])

        if not sections:
            raise ValidationError("内容模式下 sections 不能为空")

        toc_items = [{"title": s["title"], "desc": s.get("desc", "")} for s in sections]

        total = 1 + 1 + len(sections) * 2 + 1

        self.cover.build(prs, title, subtitle, presenter)

        self.toc_builder.build(prs, toc_items, 2, total)

        sn = 3
        for i, sec in enumerate(sections):
            self.section_builder.build(prs, i + 1, sec["title"], sn, total)
            sn += 1
            blocks = sec.get("blocks", [])
            content_text = self._render_blocks(blocks)
            content_slide = prs.slides.add_slide(prs.slide_layouts[6])
            self.template.build_content(content_slide, sec["title"], content_text, sn, total)
            sn += 1

        self.ending.build(prs, title, total, total)

        return prs, output_path

    def export_demo_mode(self, d):
        """演示模式 — 适合产品介绍、方案展示等"""
        prs = self.template.create_presentation()
        title = d.get("title", "产品演示")
        subtitle = d.get("subtitle", "")
        presenter = d.get("presenter", "")
        output_path = d.get("outputPath", "exports/demo.pptx")
        flow = d.get("flow", [])

        total = 2 + len(flow) + 1

        self.cover.build(prs, title, subtitle, presenter)

        sn = 2
        for i, step in enumerate(flow):
            slide = prs.slides.add_slide(prs.slide_layouts[6])
            self.template._solid_bg(slide, self.template.scheme["bg_slide"])
            self.template._accent_bar(slide, 0, 0.05, self.template.scheme["accent"])

            step_title = step.get("title", f"Step {i + 1}")
            step_desc = step.get("desc", "")
            step_icon = step.get("icon", "▸")

            self.template._textbox(slide, 1.2, 0.35, 10.0, 0.8, step_title,
                                   size=28, bold=True, color=self.template.scheme["primary"])
            self.template._textbox(slide, 1.2, 1.6, 10.5, 5.0,
                                   f"{step_icon}  {clean_surrogates(step_desc)}",
                                   size=18, color=self.template.scheme["text_primary"])

            if step.get("chartPath") and os.path.exists(step["chartPath"]):
                slide.shapes.add_picture(step["chartPath"],
                                        Inches(1.2), Inches(3.5),
                                        Inches(10.5), Inches(3.0))

            self.template._page_num(slide, sn, total)
            sn += 1

        self.ending.build(prs, title, total, total)

        return prs, output_path

    def _render_blocks(self, blocks):
        from pptx.enum.text import PP_ALIGN

        text_lines = []
        for b in blocks:
            t = b.get("type", "")
            c = b.get("content", "")
            c_clean = clean_surrogates(c)
            if t == "h1":
                text_lines.append(f"■  {c_clean}")
            elif t == "h2":
                text_lines.append(f"▌  {c_clean}")
            elif t == "h3":
                text_lines.append(f"▸  {c_clean}")
            elif t == "paragraph":
                text_lines.append(c_clean)
            elif t == "quote":
                text_lines.append(f"「 {c_clean} 」")
            elif t == "highlight":
                text_lines.append(f"★  {c_clean}")
            elif t == "list":
                for item in c.split("\n"):
                    clean = item.strip("-  ").strip()
                    if clean:
                        text_lines.append(f"•  {clean_surrogates(clean)}")
            elif t == "numbered":
                for j, item in enumerate(c.split("\n")):
                    clean = item.strip("-  ").strip()
                    if clean:
                        text_lines.append(f"{j+1}.  {clean_surrogates(clean)}")
            elif t in ("code", "mermaid", "sql"):
                lines = c.split("\n")
                text_lines.append(f"[{t.upper()}]")
                for line in lines[:3]:
                    text_lines.append(f"    {clean_surrogates(line[:60])}")
                if len(lines) > 3:
                    text_lines.append(f"    …({len(lines)}行)")
            elif t == "table":
                text_lines.append(f"[表格数据] {c_clean.split(chr(10))[0][:50]}…")
            elif t == "kpi":
                label = b.get("label", "")
                value = b.get("value", "")
                trend = b.get("trend", "")
                line = f"{c_clean}"
                if label:
                    line = f"{label}：{value}" if value else f"{label}"
                if trend:
                    line += f"  {trend}"
                text_lines.append(line)
            else:
                text_lines.append(c_clean)
        return "\n\n".join(text_lines) if text_lines else ""


def main():
    logger.info("[export_ppt] 开始生成 PPT 演示文稿 v2.0")
    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    try:
        raw = sys.stdin.read()
        d = json.loads(raw)
    except json.JSONDecodeError as e:
        logger.error(f"[export_ppt] JSON 解析失败: {e}")
        print(json.dumps({"success": False, "error": f"JSON解析失败: {e}"}))
        sys.exit(1)

    mode = d.get("mode", "data")
    template_name = d.get("template", "chinese_business")
    output_path = d.get("outputPath", "exports/slides.pptx")

    try:
        exporter = PPTExporter(template_name=template_name)

        if mode == "content":
            prs, output_path = exporter.export_content_mode(d)
        elif mode == "demo":
            prs, output_path = exporter.export_demo_mode(d)
        else:
            prs, output_path = exporter.export_data_mode(d)

        ensure_output_dir(output_path)
        prs.save(output_path)

        result = {
            "success": True,
            "path": output_path,
            "slideCount": len(prs.slides),
            "message": f"PPT 演示文稿已生成，共 {len(prs.slides)} 页",
            "template": template_name,
        }
        logger.info(f"[export_ppt] 完成: {output_path} ({len(prs.slides)}页)")
        print(json.dumps(result, ensure_ascii=False))
    except SkillError as e:
        logger.error(f"[export_ppt] {e}")
        print(json.dumps(e.to_dict(), ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        logger.exception(f"[export_ppt] 未预期的错误")
        print(json.dumps({"success": False, "error": str(e)}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
