"""
过渡页构建器
生成 PART 编号 + 第N部分 + 章节标题的过渡页，分隔不同章节内容。
"""

from pptx.util import Inches, Pt
from pptx.enum.text import PP_ALIGN


class SectionBuilder:
    """过渡页构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, prs, num, title, slide_num, total):
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        self.template.build_section(slide, num, title, slide_num, total)
        return slide
