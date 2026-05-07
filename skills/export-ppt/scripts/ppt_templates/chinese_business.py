"""
中国商务经典模板
深蓝渐变背景 + 中国红装饰 + 金色点缀，符合国内企业商务路演审美。
"""

from pptx.util import Pt
from pptx.enum.text import PP_ALIGN
from pptx.oxml.ns import qn
from pptx.oxml import parse_xml

from .base import PPTTemplate


class ChineseBusinessTemplate(PPTTemplate):
    name = "chinese_business"
    label = "中国商务经典"

    COLOR_MAP = {
        "cover_gradient": ("primary_dark", "primary", "primary_light"),
        "cover_text": "bg_white",
        "accent_bar": "accent",
        "content_bg": "bg_slide",
        "title_text": "primary",
        "body_text": "text_primary",
    }

    def build_cover(self, slide, title, subtitle, presenter=""):
        from shared.utils import clean_surrogates, format_date_cn

        self._gradient_bg(slide,
                           self.scheme["primary_dark"],
                           self.scheme["primary"],
                           self.scheme["primary_light"])
        self._accent_bar(slide, 2.6, 0.05, self.scheme["accent"])

        self._textbox(slide, 1.8, 1.8, 9.5, 2.4, clean_surrogates(title),
                      size=44, bold=True, color=self.scheme["bg_white"], align=PP_ALIGN.LEFT)
        self._textbox(slide, 1.8, 3.9, 9.5, 0.7, clean_surrogates(subtitle),
                      size=22, color=self.scheme["accent_light"], align=PP_ALIGN.LEFT)

        now = format_date_cn()
        info = f"◆  {now}"
        if presenter:
            info += f"  |  汇报人：{clean_surrogates(presenter)}"
        info += f"  |  {self.config.brand_name} · {self.config.brand_tagline}"
        self._textbox(slide, 1.8, 4.7, 9.5, 0.5, info, size=13, color=self.scheme["text_muted"])
        self._textbox(slide, 1.8, 6.2, 9.5, 0.4, self.config.watermark_text,
                      size=10, color=self.scheme["text_muted"])

    def build_toc(self, slide, sections, slide_num, total):
        self._solid_bg(slide, self.scheme["bg_slide"])
        self._accent_bar(slide, 0, 0.05, self.scheme["accent"])
        self._textbox(slide, 1.2, 0.4, 10.0, 0.9, "目  录",
                      size=32, bold=True, color=self.scheme["primary"])
        self._left_bar(slide, 1.8, 1.6, 5.2, 0.04, self.scheme["primary"])

        y = 1.8
        for i, sec in enumerate(sections[:10]):
            num_str = f"0{i + 1}" if i < 9 else str(i + 1)
            from shared.utils import clean_surrogates
            self._textbox(slide, 2.2, y, 1.2, 0.5, num_str,
                          size=28, bold=True, color=self.scheme["accent"], font_cn="Calibri")
            self._textbox(slide, 3.4, y + 0.05, 8.0, 0.5, clean_surrogates(sec["title"]),
                          size=20, bold=True, color=self.scheme["text_primary"])
            if sec.get("desc"):
                self._textbox(slide, 3.4, y + 0.45, 8.0, 0.3, clean_surrogates(sec["desc"]),
                              size=11, color=self.scheme["text_secondary"])
            y += 0.9 if sec.get("desc") else 0.65
        self._page_num(slide, slide_num, total)

    def build_section(self, slide, num, title, slide_num, total):
        from shared.utils import num_to_cn
        self._gradient_bg(slide,
                           self.scheme["primary_dark"],
                           self.scheme["primary"],
                           self.scheme["primary_light"])
        self._textbox(slide, 1.5, 2.2, 2.0, 1.2,
                      f"PART 0{num}" if num < 10 else f"PART {num}",
                      size=40, bold=True, color=self.scheme["accent"], font_cn="Calibri")
        self._textbox(slide, 1.5, 3.6, 10.0, 0.8,
                      f"第{num_to_cn(num)}部分", size=20, color=self.scheme["text_muted"])
        from shared.utils import clean_surrogates
        self._textbox(slide, 1.5, 4.1, 10.0, 1.0, clean_surrogates(title),
                      size=32, bold=True, color=self.scheme["bg_white"])
        self._page_num(slide, slide_num, total)

    def build_content(self, slide, title, content_text, slide_num, total):
        from shared.utils import clean_surrogates
        self._solid_bg(slide, self.scheme["bg_slide"])
        self._accent_bar(slide, 0, 0.05, self.scheme["accent"])
        self._textbox(slide, 1.2, 0.35, 10.0, 0.8, clean_surrogates(title),
                      size=26, bold=True, color=self.scheme["primary"])
        self._textbox(slide, 1.2, 1.5, 10.5, 5.2, clean_surrogates(content_text),
                      size=16, color=self.scheme["text_primary"])
        self._page_num(slide, slide_num, total)

    def build_ending(self, slide, title, slide_num, total):
        from shared.utils import clean_surrogates, format_date_cn
        self._gradient_bg(slide,
                           self.scheme["primary_dark"],
                           self.scheme["primary"],
                           self.scheme["primary_light"])
        self._textbox(slide, 2.0, 2.5, 9.5, 1.2, "感谢聆听",
                      size=48, bold=True, color=self.scheme["bg_white"], align=PP_ALIGN.CENTER)
        self._accent_bar(slide, 3.6, 0.04, self.scheme["accent"])
        self._textbox(slide, 2.0, 3.9, 9.5, 0.8, clean_surrogates(title),
                      size=20, color=self.scheme["text_muted"], align=PP_ALIGN.CENTER)
        self._textbox(slide, 2.0, 5.0, 9.5, 0.6,
                      f"{self.config.brand_name} · {self.config.brand_tagline}  |  {format_date_cn()}",
                      size=14, color=self.scheme["text_muted"], align=PP_ALIGN.CENTER)
        self._page_num(slide, slide_num, total)
