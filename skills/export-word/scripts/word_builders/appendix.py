"""
附录构建器
生成原始数据明细表，带截断提示。
"""

from docx.shared import Pt
from docx.enum.text import WD_ALIGN_PARAGRAPH


class AppendixBuilder:
    """附录构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, doc, columns, data_rows):
        tpl = self.template
        max_cols = tpl.config.get("limits", "word_max_detail_columns")
        max_rows = tpl.config.get("limits", "word_max_detail_rows")

        tpl._heading(doc, 2, None, "附录：数据明细")

        tpl._body_para(doc,
                       f"以下为本次分析所依据的原始数据明细（共 {len(data_rows)} 条记录），供核查与参考。")

        display_cols = min(len(columns), max_cols)
        display_rows = min(len(data_rows), max_rows)

        headers = [columns[i] for i in range(display_cols)]
        rows_data = []
        for j in range(display_rows):
            row_vals = []
            for k in range(display_cols):
                from shared.utils import truncate_text
                val = truncate_text(str(data_rows[j].get(columns[k], "")), 32)
                row_vals.append(val)
            rows_data.append(row_vals)

        tpl._create_table(doc, headers, rows_data)

        if len(data_rows) > display_rows:
            p = doc.add_paragraph()
            tpl._para_format(p, before=6, after=2, align=WD_ALIGN_PARAGRAPH.CENTER)
            tpl._run(p,
                     f"※ 限于篇幅，仅展示前 {display_rows} 条记录，完整数据共 {len(data_rows)} 条",
                     size=9, italic=True, color=self.scheme["text_muted"])

        doc.add_paragraph()
