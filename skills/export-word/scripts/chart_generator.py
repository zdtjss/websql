"""
向后兼容包装器 — 代理到共享图表生成器
用法与之前相同：python chart_generator.py < input.json
"""

import sys
import os

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
_SKILLS_DIR = os.path.dirname(os.path.dirname(_SCRIPT_DIR))
sys.path.insert(0, _SKILLS_DIR)

from shared.chart_generator import main

if __name__ == "__main__":
    main()
