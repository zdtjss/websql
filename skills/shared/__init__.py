"""
WebSQL AI Skills — 共享基础设施层
提供跨技能模块的统一配置、配色、字体、日志、异常处理、工具函数和国际化支持。
"""

from .config import SkillConfig
from .colors import ColorPalette
from .fonts import FontManager
from .logger import SkillLogger
from .exceptions import (
    SkillError,
    ConfigError,
    ValidationError,
    DataProcessingError,
    FileGenerationError,
    ChartGenerationError,
)
from .utils import (
    clean_surrogates,
    format_number_cn,
    format_date_cn,
    format_datetime_cn,
    generate_report_id,
    ensure_output_dir,
    safe_json_dumps,
)

__version__ = "2.0.0"
__all__ = [
    "SkillConfig",
    "ColorPalette",
    "FontManager",
    "SkillLogger",
    "SkillError",
    "ConfigError",
    "ValidationError",
    "DataProcessingError",
    "FileGenerationError",
    "ChartGenerationError",
    "clean_surrogates",
    "format_number_cn",
    "format_date_cn",
    "format_datetime_cn",
    "generate_report_id",
    "ensure_output_dir",
    "safe_json_dumps",
]
