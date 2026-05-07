"""
结束页构建器
生成"感谢聆听"/"Thank You"等结束页，带标题回顾和品牌信息。
"""

from pptx.util import Inches, Pt
from pptx.enum.text import PP_ALIGN


class EndingBuilder:
    """结束页构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, prs, title, slide_num, total):
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        self.template.build_ending(slide, title, slide_num, total)
        return slide
