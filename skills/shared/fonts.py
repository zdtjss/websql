"""
统一字体管理模块
处理跨平台字体回退、中英文混排和字号规范。
"""


class FontSet:
    """字体集合"""

    def __init__(self, cn, en, code="CN"):
        self.cn = cn
        self.en = en
        self.code = code


class FontManager:
    """字体管理器"""

    DEFAULT_CN = "Microsoft YaHei"
    FALLBACK_CN = ["Microsoft YaHei", "SimHei", "SimSun", "KaiTi", "FangSong"]
    DEFAULT_EN = "Calibri"
    FALLBACK_EN = ["Calibri", "Arial", "Helvetica", "Segoe UI"]

    PPT_TYPOGRAPHY = {
        "cover_title": {"size": 44, "bold": True},
        "cover_subtitle": {"size": 22, "bold": False},
        "cover_info": {"size": 13, "bold": False},
        "toc_title": {"size": 32, "bold": True},
        "toc_number": {"size": 28, "bold": True},
        "toc_section": {"size": 20, "bold": True},
        "toc_desc": {"size": 11, "bold": False},
        "section_part": {"size": 40, "bold": True},
        "section_label": {"size": 20, "bold": False},
        "section_title": {"size": 32, "bold": True},
        "content_title": {"size": 26, "bold": True},
        "content_title_large": {"size": 30, "bold": True},
        "content_body": {"size": 16, "bold": False},
        "content_body_large": {"size": 18, "bold": False},
        "table_header": {"size": 10, "bold": True},
        "table_body": {"size": 9.5, "bold": False},
        "page_number": {"size": 9, "bold": False},
        "ending_title": {"size": 48, "bold": True},
        "ending_subtitle": {"size": 20, "bold": False},
        "ending_info": {"size": 14, "bold": False},
        "section_location": {"size": 10, "bold": False},
    }

    WORD_TYPOGRAPHY = {
        "heading1_chapter": {"size": 22, "bold": True},
        "heading2_section": {"size": 16, "bold": True},
        "heading3_subsection": {"size": 14, "bold": True},
        "body_text": {"size": 11, "bold": False},
        "table_header": {"size": 9, "bold": True},
        "table_body": {"size": 9, "bold": False},
        "cover_title": {"size": 30, "bold": True},
        "cover_subtitle": {"size": 15, "bold": False},
        "cover_info": {"size": 10, "bold": False},
        "page_header": {"size": 8, "bold": False},
        "page_footer": {"size": 8, "bold": False},
        "chart_caption": {"size": 10, "bold": True},
        "chart_source": {"size": 8, "bold": False, "italic": True},
        "note_text": {"size": 9, "bold": False, "italic": True},
    }

    @classmethod
    def get_cn_font(cls):
        return cls.DEFAULT_CN

    @classmethod
    def get_en_font(cls):
        return cls.DEFAULT_EN

    @classmethod
    def get_ppt_typography(cls, element):
        return cls.PPT_TYPOGRAPHY.get(element, {"size": 16, "bold": False})

    @classmethod
    def get_word_typography(cls, element):
        return cls.WORD_TYPOGRAPHY.get(element, {"size": 11, "bold": False})

    @classmethod
    def matplotlib_font_config(cls):
        return {
            "font.family": "sans-serif",
            "font.sans-serif": cls.FALLBACK_CN + cls.FALLBACK_EN,
            "axes.unicode_minus": False,
        }
