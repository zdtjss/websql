class SkillError(Exception):
    """所有技能异常的基类"""

    def __init__(self, message="", code=None, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}

    def to_dict(self):
        return {
            "error": True,
            "code": self.code or self.__class__.__name__,
            "message": str(self),
            "details": self.details,
        }


class ConfigError(SkillError):
    """配置相关错误"""


class ValidationError(SkillError):
    """输入数据验证错误"""


class DataProcessingError(SkillError):
    """数据处理错误"""


class FileGenerationError(SkillError):
    """文件生成错误"""


class ChartGenerationError(SkillError):
    """图表生成错误"""
