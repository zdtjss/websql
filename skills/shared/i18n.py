"""
国际化支持模块
为技能提供多语言文案支持，默认使用简体中文。
"""


_I18N_DATA = {
    "zh-CN": {
        "skill": {
            "ppt_name": "专业PPT演示文稿导出",
            "ppt_desc": "生成符合中国商务审美的专业PowerPoint演示文稿",
            "word_name": "专业Word报告导出",
            "word_desc": "生成符合中国商务标准的专业数据分析报告",
        },
        "common": {
            "loading": "正在加载配置…",
            "generating": "正在生成文件…",
            "success": "文件生成成功",
            "failed": "文件生成失败",
            "save_path": "文件已保存至",
            "pages": "页",
            "records": "条记录",
            "dimensions": "个维度",
        },
        "ppt": {
            "cover_title_default": "数据分析报告",
            "cover_subtitle_default": "专业数据分析报告",
            "toc_title": "目  录",
            "data_insight": "数据洞察",
            "data_panorama": "数据全景",
            "data_detail": "数据明细",
            "core_findings": "核心发现与建议",
            "thank_you": "感谢聆听",
            "part_label": "第{n}部分",
            "data_scale": "数据规模",
            "core_metrics": "核心指标",
            "metric_stats": "指标统计",
            "no_significant_findings": "本次分析未发现显著异常模式。",
            "showing_records": "展示 {shown}/{total} 条",
        },
        "word": {
            "cover_title_default": "数据分析报告",
            "cover_subtitle_default": "专业数据分析报告",
            "dept_default": "WebSQL AI 数据分析中心",
            "report_summary": "报告摘要",
            "key_metrics_overview": "核心指标速览",
            "data_overview_quality": "数据概览与质量评估",
            "statistical_analysis": "统计分析与核心指标",
            "stat_table_caption": "表 {n}  数值字段描述性统计汇总",
            "data_visualization": "数据可视化分析",
            "chart_caption": "图 {n}  关键指标可视化",
            "chart_source": "数据来源：实时数据库查询  |  生成时间：{time}",
            "key_findings": "关键发现与建议",
            "finding_label": "发现 {n}",
            "data_appendix": "附录：数据明细",
            "data_truncated": "※ 限于篇幅，仅展示前 {n} 条记录，完整数据共 {total} 条",
            "no_findings": "本次分析未识别出显著的数据特征或异常模式。",
            "density_internal": "内部资料",
        },
    },
}


def get_text(lang="zh-CN", *keys, **kwargs):
    """
    获取国际化文本
    用法: get_text("zh-CN", "ppt", "thank_you")
          get_text("zh-CN", "ppt", "showing_records", shown=5, total=20)
    """
    node = _I18N_DATA.get(lang, _I18N_DATA["zh-CN"])
    for key in keys:
        if isinstance(node, dict) and key in node:
            node = node[key]
        else:
            return keys[-1]
    if isinstance(node, str) and kwargs:
        return node.format(**kwargs)
    return node
