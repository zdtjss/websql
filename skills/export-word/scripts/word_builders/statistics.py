"""
统计分析构建器
生成描述性统计汇总表、数据特征解读和智能评语。
"""

from docx.shared import Pt
from docx.enum.text import WD_ALIGN_PARAGRAPH


class StatisticsBuilder:
    """统计分析构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, doc, numeric_stats, data_rows):
        tpl = self.template

        tpl._heading(doc, 2, None, "统计分析与核心指标")

        if not numeric_stats:
            tpl._body_para(doc, "当前数据集无可用于统计分析的数值型字段。")
            return

        tpl._body_para(doc,
                       "针对数据集中的数值型字段，已进行全面的描述性统计分析，涵盖集中趋势、"
                       "离散程度及分布特征等维度。各指标详见表 1。")

        p = doc.add_paragraph()
        tpl._para_format(p, before=4, after=8, align=WD_ALIGN_PARAGRAPH.CENTER)
        tpl._run(p, "表 1  数值字段描述性统计汇总", size=10, bold=True, color=self.scheme["primary"])

        headers = ["字段名称", "有效样本", "最小值", "最大值", "均值", "标准差"]
        rows_data = []
        for s in numeric_stats:
            rows_data.append([
                s.get("column", ""),
                str(s.get("count", 0)),
                f"{s.get('min', 0):,.2f}",
                f"{s.get('max', 0):,.2f}",
                f"{s.get('avg', 0):,.2f}",
                f"{s.get('stddev', 0):,.2f}",
            ])
        tpl._create_table(doc, headers, rows_data)

        doc.add_paragraph()

        tpl._heading(doc, 3, None, "数据特征解读")

        if numeric_stats:
            max_std = max(numeric_stats, key=lambda x: x.get("stddev", 0))
            max_avg = max(numeric_stats, key=lambda x: x.get("avg", 0))

            col_max_std = max_std.get("column", "")
            col_max_avg = max_avg.get("column", "")

            tpl._body_para(doc,
                           f"从离散程度来看，\u201c{col_max_std}\u201d的标准差最大"
                           f"\uff08{max_std.get('stddev', 0):.2f}\uff09，表明该指标在不同记录间波动较为显著，"
                           f"建议重点关注其变化规律及影响因素。")

            tpl._body_para(doc,
                           f"从集中趋势来看，\u201c{col_max_avg}\u201d的均值最高"
                           f"\uff08{max_avg.get('avg', 0):.2f}\uff09，是当前数据集中的核心贡献指标。")

            min_std = min(numeric_stats, key=lambda x: x.get("stddev", 0))
            col_min_std = min_std.get("column", "")
            if col_min_std != col_max_std:
                tpl._body_para(doc,
                               f" \u201c{col_min_std}\u201d的标准差最小"
                               f"\uff08{min_std.get('stddev', 0):.2f}\uff09，"
                               f"数据分布最为集中，稳定性较高。")

            count = len(data_rows) if isinstance(data_rows, list) else 0
            if count > 0:
                tpl._body_para(doc,
                               f"综合来看，数据集有效记录为 {count} 条，字段完整性良好。"
                               f"建议结合业务背景，对波动较大的指标进行归因分析，"
                               f"以识别数据背后的业务驱动因素。")

        tpl._divider(doc, self.scheme["accent"])
