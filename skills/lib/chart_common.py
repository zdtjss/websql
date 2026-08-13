"""
chart_common.py — Word/PPT 导出脚本共享的 matplotlib 图表生成模块。

背景：export_ppt.py 与 word_generator.py 原各自内嵌约 200 行几乎相同的图表代码，
仅深色/浅色主题参数不同，维护需双改。本模块统一收敛图表生成逻辑，
通过 ChartStyle 参数化主题差异，两个脚本按需实例化对应 Style。

用法：
    from lib.chart_common import DarkChartStyle, LightChartStyle, create_chart
    fig = create_chart(DarkChartStyle(), "bar", {
        "title": "...", "categories": [...],
        "series": [{"name": "...", "values": [...]}],
    })
"""

import matplotlib
import matplotlib.pyplot as plt
import numpy as np

# 中文字体支持（Windows 优先微软雅黑/黑体，缺失时回退默认字体）
matplotlib.rcParams['font.sans-serif'] = [
    'Microsoft YaHei', 'SimHei', 'Arial Unicode MS', 'DejaVu Sans']
matplotlib.rcParams['axes.unicode_minus'] = False

# 两种主题共享的图表调色板（与原两个脚本保持一致）
CHART_COLORS = ['#00A8FF', '#00F5D4', '#7B61FF', '#FF6B35', '#E63946', '#FFD700', '#00E696']


class ChartStyle:
    """图表样式参数集。子类覆盖字段以适配不同主题。"""

    wide_figsize = (8, 5)          # 常规图表尺寸
    pie_figsize = (6, 6)           # 饼图/环形图尺寸
    radar_figsize = (6, 6)         # 雷达图尺寸
    heatmap_figsize = (8, 5)       # 热力图尺寸

    title_color = 'white'          # 标题颜色；None 表示使用默认文字色
    title_fontsize = 14
    legend_alpha = 0.3             # 图例背景透明度
    grid_alpha = 0.2               # 网格透明度
    bar_alpha = 0.9                # 柱状图填充透明度
    hbar_alpha = 0.9               # 水平柱状图填充透明度
    area_alpha = 0.3               # 面积图填充透明度
    radar_alpha = 0.15             # 雷达图填充透明度
    stacked_alpha = 0.9            # 堆叠柱状图填充透明度
    bar_value_color = '#00F5D4'    # 柱状图数值标签颜色
    bar_value_fontsize = 9
    bar_value_offset = 1           # 数值标签相对柱顶的偏移
    scatter_size = 60              # 散点大小
    pie_textprops = {'color': 'white', 'fontsize': 11}


class DarkChartStyle(ChartStyle):
    """深色主题（PPT）：原 export_ppt.py 默认参数。"""
    pass


class LightChartStyle(ChartStyle):
    """浅色主题（Word）：原 word_generator.py 默认参数。"""

    wide_figsize = (8, 4.5)
    pie_figsize = (6, 5)
    radar_figsize = (6, 5)
    heatmap_figsize = (8, 4.5)

    title_color = None
    title_fontsize = 13
    legend_alpha = 0.8
    grid_alpha = 0.3
    bar_alpha = 0.85
    hbar_alpha = 0.85
    area_alpha = 0.25
    radar_alpha = 0.1
    stacked_alpha = 0.85
    bar_value_color = '#333'
    bar_value_fontsize = 8
    bar_value_offset = 0.5
    scatter_size = 50
    pie_textprops = {'fontsize': 10}


def create_chart(style, chart_type, data):
    """按类型生成 matplotlib Figure。未知类型回退为柱状图。

    data 契约（与两脚本原约定一致）：
    - line/bar/area/stacked_bar：{title, categories, series: [{name, values}]}
    - horizontal_bar/radar：同 series 格式（取第一个 series 或全部 series）
    - pie/donut：{title, labels, values}
    - scatter：{title, x, y, x_label, y_label}
    - heatmap：{title, values(二维数组), x_labels, y_labels}
    """
    creators = {
        'line': _chart_line,
        'bar': _chart_bar,
        'horizontal_bar': _chart_hbar,
        'pie': _chart_pie,
        'donut': _chart_donut,
        'scatter': _chart_scatter,
        'radar': _chart_radar,
        'heatmap': _chart_heatmap,
        'area': _chart_area,
        'stacked_bar': _chart_stacked_bar,
    }
    return creators.get(chart_type, _chart_bar)(style, data)


def _series_count(data):
    """返回 series 数量；空数据回退为 1 防止除零。"""
    n = len(data.get('series') or [])
    return n if n > 0 else 1


def _set_title(ax, data, style, pad=10):
    """设置标题。title_color 为 None 时不传 color（matplotlib 3.10+ 拒绝 None）。"""
    kwargs = {'fontsize': style.title_fontsize, 'pad': pad}
    if style.title_color is not None:
        kwargs['color'] = style.title_color
    ax.set_title(data.get('title', ''), **kwargs)


def _chart_line(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    for i, s in enumerate(data.get('series') or []):
        ax.plot(data['categories'], s['values'], marker='o', linewidth=2,
                color=CHART_COLORS[i % len(CHART_COLORS)], label=s['name'])
    _set_title(ax, data, style)
    ax.legend(loc='upper left', framealpha=style.legend_alpha)
    ax.grid(True, alpha=style.grid_alpha)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    fig.tight_layout()
    return fig


def _chart_bar(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    cats = data['categories']
    n_series = _series_count(data)
    width = 0.7 / n_series
    x = np.arange(len(cats))
    for i, s in enumerate(data.get('series') or []):
        offset = (i - n_series / 2 + 0.5) * width
        bars = ax.bar(x + offset, s['values'], width, label=s['name'],
                      color=CHART_COLORS[i % len(CHART_COLORS)], alpha=style.bar_alpha)
        for bar, val in zip(bars, s['values']):
            ax.text(bar.get_x() + bar.get_width() / 2,
                    bar.get_height() + style.bar_value_offset,
                    str(val), ha='center', va='bottom',
                    fontsize=style.bar_value_fontsize, color=style.bar_value_color)
    ax.set_xticks(x)
    ax.set_xticklabels(cats)
    _set_title(ax, data, style)
    if n_series > 1:
        ax.legend(framealpha=style.legend_alpha)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    ax.grid(axis='y', alpha=style.grid_alpha)
    fig.tight_layout()
    return fig


def _chart_hbar(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    cats = data['categories']
    series = data.get('series') or []
    if not series:
        return fig
    values = series[0]['values']
    colors = [CHART_COLORS[i % len(CHART_COLORS)] for i in range(len(cats))]
    ax.barh(cats, values, color=colors, alpha=style.hbar_alpha)
    _set_title(ax, data, style)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    fig.tight_layout()
    return fig


def _chart_pie(style, data):
    fig, ax = plt.subplots(figsize=style.pie_figsize)
    labels = data.get('labels') or []
    values = data.get('values') or []
    if not labels or not values:
        return fig
    colors = CHART_COLORS[:len(labels)]
    ax.pie(values, labels=labels, colors=colors, autopct='%1.1f%%',
           textprops=style.pie_textprops, startangle=90)
    _set_title(ax, data, style)
    fig.tight_layout()
    return fig


def _chart_donut(style, data):
    fig, ax = plt.subplots(figsize=style.pie_figsize)
    labels = data.get('labels') or []
    values = data.get('values') or []
    if not labels or not values:
        return fig
    colors = CHART_COLORS[:len(labels)]
    ax.pie(values, labels=labels, colors=colors, autopct='%1.1f%%',
           textprops=style.pie_textprops, startangle=90,
           pctdistance=0.8, wedgeprops={'width': 0.4})
    _set_title(ax, data, style)
    fig.tight_layout()
    return fig


def _chart_scatter(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    ax.scatter(data['x'], data['y'], c=CHART_COLORS[0], alpha=0.7,
               s=style.scatter_size, edgecolors='white', linewidth=0.5)
    _set_title(ax, data, style)
    ax.set_xlabel(data.get('x_label', ''))
    ax.set_ylabel(data.get('y_label', ''))
    ax.grid(True, alpha=style.grid_alpha)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    fig.tight_layout()
    return fig


def _chart_radar(style, data):
    fig, ax = plt.subplots(figsize=style.radar_figsize, subplot_kw=dict(polar=True))
    cats = data['categories']
    n = len(cats)
    angles = np.linspace(0, 2 * np.pi, n, endpoint=False).tolist()
    angles += angles[:1]
    for i, s in enumerate(data.get('series') or []):
        vals = s['values'] + s['values'][:1]
        ax.plot(angles, vals, 'o-', linewidth=2,
                color=CHART_COLORS[i % len(CHART_COLORS)], label=s['name'])
        ax.fill(angles, vals, alpha=style.radar_alpha,
                color=CHART_COLORS[i % len(CHART_COLORS)])
    ax.set_xticks(angles[:-1])
    ax.set_xticklabels(cats, fontsize=10)
    _set_title(ax, data, style, pad=20)
    ax.legend(loc='upper right', framealpha=style.legend_alpha)
    fig.tight_layout()
    return fig


def _chart_heatmap(style, data):
    fig, ax = plt.subplots(figsize=style.heatmap_figsize)
    values = np.array(data['values'])
    im = ax.imshow(values, cmap='YlOrRd', aspect='auto')
    ax.set_xticks(range(len(data['x_labels'])))
    ax.set_xticklabels(data['x_labels'], fontsize=9)
    ax.set_yticks(range(len(data['y_labels'])))
    ax.set_yticklabels(data['y_labels'], fontsize=9)
    fig.colorbar(im, ax=ax, shrink=0.8)
    _set_title(ax, data, style)
    fig.tight_layout()
    return fig


def _chart_area(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    for i, s in enumerate(data.get('series') or []):
        ax.fill_between(data['categories'], s['values'], alpha=style.area_alpha,
                        color=CHART_COLORS[i % len(CHART_COLORS)])
        ax.plot(data['categories'], s['values'], linewidth=2,
                color=CHART_COLORS[i % len(CHART_COLORS)], label=s['name'])
    _set_title(ax, data, style)
    ax.legend(framealpha=style.legend_alpha)
    ax.grid(True, alpha=style.grid_alpha)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    fig.tight_layout()
    return fig


def _chart_stacked_bar(style, data):
    fig, ax = plt.subplots(figsize=style.wide_figsize)
    cats = data['categories']
    x = np.arange(len(cats))
    bottom = np.zeros(len(cats))
    for i, s in enumerate(data.get('series') or []):
        ax.bar(x, s['values'], bottom=bottom, label=s['name'],
               color=CHART_COLORS[i % len(CHART_COLORS)], alpha=style.stacked_alpha)
        bottom += np.array(s['values'])
    ax.set_xticks(x)
    ax.set_xticklabels(cats)
    _set_title(ax, data, style)
    ax.legend(framealpha=style.legend_alpha)
    ax.spines['top'].set_visible(False)
    ax.spines['right'].set_visible(False)
    fig.tight_layout()
    return fig
