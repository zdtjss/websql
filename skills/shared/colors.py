"""
统一配色方案管理 — 中国商务审美体系
支持多种预设主题和自定义配色方案。
"""


class ColorScheme:
    """单个配色方案"""

    def __init__(self, name, **colors):
        self.name = name
        for key, value in colors.items():
            if isinstance(value, tuple):
                setattr(self, key, value)
            else:
                setattr(self, key, value)

    def hex_to_rgb(self, hex_color):
        h = hex_color.lstrip("#")
        return (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))

    def rgb_to_hex(self, r, g, b):
        return f"#{r:02X}{g:02X}{b:02X}"


class ColorPalette:
    """全局配色管理"""

    CHINESE_BUSINESS = {
        "name": "中国商务经典",
        "primary": "#1A3C6D",
        "primary_dark": "#0D2137",
        "primary_light": "#283593",
        "accent": "#C0392B",
        "accent_light": "#E8C5C4",
        "gold": "#8B6914",
        "gold_light": "#D4A853",
        "text_primary": "#2C2C2C",
        "text_secondary": "#666666",
        "text_muted": "#999999",
        "bg_white": "#FFFFFF",
        "bg_slide": "#F7F8FA",
        "bg_section": "#F0F3F7",
        "table_header": "#1A3C6D",
        "table_stripe": "#F2F4F8",
        "table_stripe_word": "#F4F6FA",
        "border": "#CFD8DC",
        "divider": "#C0392B",
        "chart_colors": [
            "#1A3C6D", "#C0392B", "#8B6914", "#2E7D32",
            "#1565C0", "#E65100", "#6A1B9A", "#00838F",
            "#AD1457", "#4E342E", "#37474F", "#BF360C",
        ],
    }

    TECH_MODERN = {
        "name": "科技现代",
        "primary": "#1565C0",
        "primary_dark": "#0D3B66",
        "primary_light": "#42A5F5",
        "accent": "#00ACC1",
        "accent_light": "#B2EBF2",
        "gold": "#FF6F00",
        "gold_light": "#FFB74D",
        "text_primary": "#212121",
        "text_secondary": "#616161",
        "text_muted": "#9E9E9E",
        "bg_white": "#FFFFFF",
        "bg_slide": "#F5F7FA",
        "bg_section": "#ECEFF1",
        "table_header": "#0D3B66",
        "table_stripe": "#F0F4F8",
        "table_stripe_word": "#F0F4F8",
        "border": "#B0BEC5",
        "divider": "#00ACC1",
        "chart_colors": [
            "#1565C0", "#00ACC1", "#FF6F00", "#43A047",
            "#7B1FA2", "#D32F2F", "#0097A7", "#E64A19",
            "#5C6BC0", "#00897B", "#8D6E63", "#C2185B",
        ],
    }

    WARM_ELEGANT = {
        "name": "暖调雅致",
        "primary": "#8B4513",
        "primary_dark": "#5D2E0C",
        "primary_light": "#A0522D",
        "accent": "#B22222",
        "accent_light": "#F5D6D6",
        "gold": "#DAA520",
        "gold_light": "#F0D68A",
        "text_primary": "#3E2723",
        "text_secondary": "#795548",
        "text_muted": "#A1887F",
        "bg_white": "#FFFFFF",
        "bg_slide": "#FDF8F5",
        "bg_section": "#F5EBE0",
        "table_header": "#5D2E0C",
        "table_stripe": "#FAF3ED",
        "table_stripe_word": "#FAF3ED",
        "border": "#D7CCC8",
        "divider": "#B22222",
        "chart_colors": [
            "#8B4513", "#B22222", "#DAA520", "#6B8E23",
            "#CD853F", "#A0522D", "#BC8F8F", "#D2691E",
            "#2E8B57", "#8FBC8F", "#DEB887", "#F4A460",
        ],
    }

    SCHEMES = {
        "chinese_business": CHINESE_BUSINESS,
        "tech_modern": TECH_MODERN,
        "warm_elegant": WARM_ELEGANT,
    }

    @classmethod
    def get(cls, scheme_name="chinese_business"):
        """获取指定的配色方案"""
        scheme = cls.SCHEMES.get(scheme_name, cls.CHINESE_BUSINESS)
        return dict(scheme)

    @classmethod
    def list_schemes(cls):
        """列出所有可用的配色方案"""
        return [(k, v["name"]) for k, v in cls.SCHEMES.items()]

    @classmethod
    def hex_to_rgb(cls, hex_color):
        h = hex_color.lstrip("#")
        return (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))

    @classmethod
    def get_chart_colors(cls, scheme_name="chinese_business", count=None):
        """获取图表配色列表"""
        scheme = cls.get(scheme_name)
        colors = scheme["chart_colors"]
        return colors[:count] if count else colors
