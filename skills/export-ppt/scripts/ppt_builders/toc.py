"""
目录页构建器
生成带分区编号、章节标题和简要说明的目录幻灯片。
"""

from pptx.util import Inches, Pt
from pptx.enum.text import PP_ALIGN


class TOCBuilder:
    """目录幻灯片构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, prs, sections, slide_num, total):
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        self.template.build_toc(slide, sections, slide_num, total)
        return slide
