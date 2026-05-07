"""
数据概览构建器
生成数据集概览、数据质量评估和预览样例表格。
"""

from docx.shared import Pt
from docx.enum.text import WD_ALIGN_PARAGRAPH


class OverviewBuilder:
    """数据概览构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, doc, columns, data_rows):
        tpl = self.template
        tpl._heading(doc, 2, None, "数据概览与质量评估")

        tpl._body_para(doc,
                       f"本次分析共读取 {len(data_rows)} 条数据记录，涉及 {len(columns)} 个字段。"
                       f"以下为数据集的简要概览，涵盖核心字段及其数据样例。")

        max_cols = tpl.config.get("limits", "word_max_data_preview_columns")
        max_rows = tpl.config.get("limits", "word_max_data_preview_rows")

        display_cols = min(len(columns), max_cols)
        display_rows = min(len(data_rows), max_rows)

        headers = [columns[i] for i in range(display_cols)]
        rows_data = []
        for j in range(display_rows):
            row_vals = []
            for k in range(display_cols):
                from shared.utils import truncate_text
                val = truncate_text(str(data_rows[j].get(columns[k], "")), 30)
                row_vals.append(val)
            rows_data.append(row_vals)

        tpl._create_table(doc, headers, rows_data)
        doc.add_paragraph()
        tpl._divider(doc)
