"""
封面构建器
生成带品牌标识、报告标题、副标题、元信息的专业封面页。
"""


class CoverBuilder:
    """封面构建器"""

    def __init__(self, template):
        self.template = template

    def build(self, doc, title, subtitle, dept=""):
        self.template.build_cover(doc, title, subtitle, dept)
