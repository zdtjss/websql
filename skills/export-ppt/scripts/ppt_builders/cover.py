"""
封面页构建器
生成带渐变背景、中国红装饰条、标题、副标题和元信息的专业封面页。
"""

from pptx.util import Inches, Pt
from pptx.enum.text import PP_ALIGN


class CoverBuilder:
    """封面幻灯片构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, prs, title, subtitle, presenter=""):
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        self.template.build_cover(slide, title, subtitle, presenter)
        return slide
