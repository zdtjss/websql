"""
技能配置管理模块
支持默认配置、用户自定义配置和数据驱动配置的多层覆盖机制。
"""

import json
import os
from copy import deepcopy
from .exceptions import ConfigError


_DEFAULT_CONFIG = {
    "brand": {
        "name": "WebSQL AI",
        "tagline": "智能数据分析平台",
        "version": "2.0.0",
        "watermark": "内部资料 · 请注意保密",
    },
    "layout": {
        "ppt": {
            "slide_width_inches": 13.333,
            "slide_height_inches": 7.5,
            "aspect_ratio": "16:9",
            "margin_left": 1.2,
            "margin_right": 1.2,
            "margin_top": 0.35,
            "accent_bar_height": 0.05,
        },
        "word": {
            "page_margin_top_cm": 3.0,
            "page_margin_bottom_cm": 2.5,
            "page_margin_left_cm": 2.5,
            "page_margin_right_cm": 2.5,
            "body_line_spacing": 1.6,
            "first_indent_cm": 0.74,
        },
    },
    "limits": {
        "ppt_max_table_columns": 7,
        "ppt_max_table_rows": 12,
        "ppt_max_highlights": 6,
        "ppt_max_sections": 10,
        "word_max_data_preview_columns": 6,
        "word_max_data_preview_rows": 5,
        "word_max_detail_columns": 8,
        "word_max_detail_rows": 20,
        "word_max_findings": 10,
        "word_max_stats_rows": 12,
    },
    "naming": {
        "report_prefix": "WS-RPT",
        "date_format_date": "%Y年%m月%d日",
        "date_format_datetime": "%Y-%m-%d %H:%M",
    },
    "charts": {
        "dpi": 150,
        "default_width": 11,
        "default_height": 6,
        "pie_size": 9,
        "radar_size": 9,
    },
}


class SkillConfig:
    """技能配置管理器，支持多层覆盖：默认 -> 环境变量 -> 用户文件 -> 运行时"""

    def __init__(self, config_file=None):
        self._config = deepcopy(_DEFAULT_CONFIG)
        if config_file and os.path.exists(config_file):
            try:
                with open(config_file, "r", encoding="utf-8") as f:
                    user_config = json.load(f)
                    self._deep_merge(self._config, user_config)
            except (json.JSONDecodeError, OSError) as e:
                raise ConfigError(f"配置文件加载失败: {e}")

    def _deep_merge(self, base, override):
        for key, value in override.items():
            if key in base and isinstance(base[key], dict) and isinstance(value, dict):
                self._deep_merge(base[key], value)
            else:
                base[key] = deepcopy(value)

    def get(self, *keys, default=None):
        """使用点路径获取配置值，如 config.get('layout', 'ppt', 'slide_width_inches')"""
        node = self._config
        for key in keys:
            if isinstance(node, dict) and key in node:
                node = node[key]
            else:
                return default
        return deepcopy(node)

    def set(self, *keys, value):
        """使用点路径设置配置值"""
        keys_list = list(keys)
        if len(keys_list) < 2:
            self._config[keys_list[0]] = value
            return
        val_key = keys_list.pop()
        node = self._config
        for key in keys_list:
            if key not in node:
                node[key] = {}
            node = node[key]
        node[val_key] = value

    @property
    def brand_name(self):
        return self.get("brand", "name")

    @property
    def brand_tagline(self):
        return self.get("brand", "tagline")

    @property
    def watermark_text(self):
        return self.get("brand", "watermark")
