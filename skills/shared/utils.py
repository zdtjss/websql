"""
通用工具函数
提供字符清洗、数字格式化、日期格式化、文件操作等跨模块共用的工具函数。
"""

import json
import os
import random
import string
from datetime import datetime


def clean_surrogates(text):
    """清洗 Unicode 代理对字符，防止 OpenXML 解析失败"""
    if not isinstance(text, str):
        return str(text)
    return "".join(ch for ch in text if not (0xD800 <= ord(ch) <= 0xDFFF))


def format_number_cn(num):
    """将数字格式化为中文习惯格式（万、亿）"""
    if num is None:
        return "N/A"
    if abs(num) >= 1e8:
        return f"{num / 1e8:,.2f}亿"
    if abs(num) >= 1e4:
        return f"{num / 1e4:,.1f}万"
    if isinstance(num, float) and num == int(num):
        return f"{int(num):,}"
    if isinstance(num, float):
        return f"{num:,.2f}"
    return f"{num:,}"


def format_date_cn(dt=None):
    """格式化为中文日期：2024年01月15日"""
    dt = dt or datetime.now()
    return dt.strftime("%Y年%m月%d日")


def format_datetime_cn(dt=None):
    """格式化为中文日期时间：2024-01-15 14:30"""
    dt = dt or datetime.now()
    return dt.strftime("%Y-%m-%d %H:%M")


def generate_report_id(prefix="WS-RPT"):
    """生成唯一的报告编号"""
    now = datetime.now()
    suffix = "".join(random.choices(string.ascii_uppercase + string.digits, k=4))
    return f"{prefix}-{now.strftime('%Y%m%d')}-{suffix}"


def ensure_output_dir(file_path):
    """确保输出文件的目录存在"""
    dir_path = os.path.dirname(file_path)
    if dir_path:
        os.makedirs(dir_path, exist_ok=True)
    return file_path


def safe_json_dumps(obj, **kwargs):
    """安全的 JSON 序列化，处理不可序列化对象"""

    def default_handler(o):
        if isinstance(o, datetime):
            return o.isoformat()
        if hasattr(o, "__dict__"):
            return str(o)
        return str(o)

    return json.dumps(obj, default=default_handler, ensure_ascii=False, **kwargs)


def truncate_text(text, max_len=30, suffix="…"):
    """截断文本，超过最大长度添加省略号"""
    if not isinstance(text, str):
        text = str(text)
    if len(text) <= max_len:
        return text
    return text[:max_len] + suffix


def safe_div(a, b, default=0):
    """安全除法，避免除零错误"""
    return a / b if b else default


def num_to_cn(n):
    """数字转中文数字（1-10）"""
    nums = ["一", "二", "三", "四", "五", "六", "七", "八", "九", "十"]
    if 1 <= n <= 10:
        return nums[n - 1]
    return str(n)


import re

_RE_MD_BOLD = re.compile(r'\*\*(.+?)\*\*')
_RE_MD_ITALIC = re.compile(r'\*(.+?)\*')
_RE_MD_CODE = re.compile(r'`(.+?)`')
_RE_MD_LINK = re.compile(r'\[(.+?)\]\(.+?\)')
_RE_BLOCKQUOTE = re.compile(r'^\s*>\s?', re.MULTILINE)
_RE_HR = re.compile(r'^[-*_]{3,}\s*$', re.MULTILINE)

_RE_LATEX_INLINE = re.compile(r'\$(.+?)\$')
_RE_LATEX_DISPLAY = re.compile(r'\$\$(.+?)\$\$')
_RE_LATEX_ARROW_R = re.compile(r'\\to\b|\\rightarrow')
_RE_LATEX_ARROW_L = re.compile(r'\\leftarrow')
_RE_LATEX_ARROW_R2 = re.compile(r'\\Rightarrow')
_RE_LATEX_ARROW_L2 = re.compile(r'\\Leftarrow')

_RE_LATEX_GREEK = re.compile(r'\\(alpha|beta|gamma|delta|epsilon|theta|pi|phi|omega)\b')
_RE_LATEX_REL = re.compile(r'\\(ge|geq|gt|le|leq|lt|ne|neq)\b')
_RE_LATEX_OP = re.compile(r'\\(sum|prod|ldots|cdots)\b')
_RE_LATEX_BRACES = re.compile(r'[\{\}]')
_RE_LATEX_CMD = re.compile(r'\\(frac|hat|bar|tilde|mbox|text)\{')

_LATEX_GREEK_MAP = {
    'alpha': 'α', 'beta': 'β', 'gamma': 'γ', 'delta': 'δ',
    'epsilon': 'ε', 'theta': 'θ', 'pi': 'π', 'phi': 'φ', 'omega': 'ω',
}
_LATEX_REL_MAP = {
    'ge': '≥', 'geq': '≥', 'gt': '>', 'le': '≤', 'leq': '≤', 'lt': '<', 'ne': '≠', 'neq': '≠',
}


def _replace_latex_greek(m):
    return _LATEX_GREEK_MAP.get(m.group(1), m.group(0))


def _replace_latex_rel(m):
    return _LATEX_REL_MAP.get(m.group(1), m.group(0))


def _replace_arrow_right(m):
    return '→' if 'to' in m.group(0) else '→'


def strip_markdown(text):
    if not isinstance(text, str):
        return str(text)
    s = text
    s = _RE_LATEX_DISPLAY.sub(r'\1', s)
    s = _RE_LATEX_INLINE.sub(r'\1', s)
    s = _RE_LATEX_ARROW_R.sub('→', s)
    s = _RE_LATEX_ARROW_L.sub('←', s)
    s = _RE_LATEX_ARROW_R2.sub('⇒', s)
    s = _RE_LATEX_ARROW_L2.sub('⇐', s)
    s = _RE_LATEX_GREEK.sub(_replace_latex_greek, s)
    s = _RE_LATEX_REL.sub(_replace_latex_rel, s)
    s = _RE_LATEX_OP.sub('…', s)
    s = _RE_LATEX_BRACES.sub('', s)
    s = _RE_LATEX_CMD.sub('', s)
    s = _RE_MD_BOLD.sub(r'\1', s)
    s = _RE_MD_ITALIC.sub(r'\1', s)
    s = _RE_MD_CODE.sub(r'\1', s)
    s = _RE_MD_LINK.sub(r'\1', s)
    s = _RE_BLOCKQUOTE.sub('', s)
    s = _RE_HR.sub('', s)
    return s
