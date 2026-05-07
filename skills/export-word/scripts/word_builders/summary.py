"""
报告摘要构建器
生成包含报告摘要、核心指标速览表等内容的章节。
"""

from docx.shared import Pt
from docx.enum.text import WD_ALIGN_PARAGRAPH


class SummaryBuilder:
    """报告摘要构建器"""

    def __init__(self, template):
        self.template = template
        self.scheme = template.scheme

    def build(self, doc, rows_count, cols_count, numeric_cols, numeric_stats):
        tpl = self.template
        tpl._heading(doc, 1, 1, "报告摘要")

        tpl._body_para(doc,
                       f"本报告基于对数据库查询结果的系统分析，共涉及 {cols_count} 个数据维度、{rows_count} 条业务记录。"
                       f"报告从数据概览、统计特征、趋势变化、分布规律等角度进行了全方位剖析，"
                       f"旨在为业务决策提供数据支撑与参考依据。")

        if numeric_cols:
            cols_str = "、".join(numeric_cols[:5])
            suffix = "等" if len(numeric_cols) > 5 else ""
            tpl._body_para(doc,
                           f"经自动识别，数据集中包含 {len(numeric_cols)} 个数值型字段（{cols_str}{suffix}），"
                           f"已对其进行了描述性统计分析与可视化呈现。")

        if numeric_stats:
            tpl._heading(doc, 2, None, "核心指标速览")
            headers = ["指标名称", "样本量", "最小值", "最大值", "均值"]
            rows_data = []
            for s in numeric_stats[:tpl.config.get("limits", "word_max_stats_rows")]:
                rows_data.append([
                    s.get("column", ""),
                    str(s.get("count", 0)),
                    f"{s.get('min', 0):,.2f}",
                    f"{s.get('max', 0):,.2f}",
                    f"{s.get('avg', 0):,.2f}",
                ])
            tpl._create_table(doc, headers, rows_data)
            doc.add_paragraph()

        tpl._divider(doc, self.scheme["accent"])
