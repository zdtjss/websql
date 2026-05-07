"""
统一图表生成器 — WebSQL AI 平台
基于 matplotlib 生成符合中国商务审美的高质量数据可视化图表。
支持折线图、柱状图、饼图、环形图、雷达图、散点图、面积图、热力图等。
"""

import json
import os
import sys

import matplotlib
matplotlib.use("Agg")

import matplotlib.pyplot as plt
import matplotlib.ticker as mticker
import numpy as np

from .colors import ColorPalette
from .fonts import FontManager
from .utils import format_number_cn, clean_surrogates, ensure_output_dir
from .exceptions import ChartGenerationError, ValidationError

CHART_GENERATORS = {}


def register(chart_type):
    def decorator(func):
        CHART_GENERATORS[chart_type] = func
        return func
    return decorator


def configure_matplotlib(scheme_name="chinese_business"):
    scheme = ColorPalette.get(scheme_name)
    plt.rcParams.update({
        "font.family": "sans-serif",
        "font.sans-serif": FontManager.FALLBACK_CN + FontManager.FALLBACK_EN,
        "axes.unicode_minus": False,
        "axes.facecolor": "white",
        "axes.edgecolor": "#E0E4E8",
        "axes.grid": True,
        "axes.grid.axis": "y",
        "grid.color": "#E0E4E8",
        "grid.linewidth": 0.6,
        "grid.alpha": 0.8,
        "figure.facecolor": "#FAFAFA",
        "figure.dpi": 150,
        "savefig.dpi": 150,
        "savefig.bbox": "tight",
        "savefig.pad_inches": 0.3,
        "text.color": "#424242",
        "xtick.color": "#757575",
        "ytick.color": "#757575",
    })
    return scheme


def _setup_spines(ax):
    ax.spines["top"].set_visible(False)
    ax.spines["right"].set_visible(False)
    ax.spines["left"].set_color("#E0E4E8")
    ax.spines["bottom"].set_color("#E0E4E8")
    ax.tick_params(axis="both", length=0)


@register("line")
def generate_line_chart(series_list, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    for i, series in enumerate(series_list):
        color = chart_colors[i % len(chart_colors)]
        label = series.get("name", f"系列 {i + 1}")
        x_vals = list(range(len(series["yValues"])))
        y_vals = series["yValues"]
        x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])

        ax.plot(x_vals, y_vals, color=color, linewidth=2.8, marker="o",
                markersize=5, markerfacecolor="white", markeredgewidth=2,
                markeredgecolor=color, label=label, zorder=5)

        for j, (xv, yv) in enumerate(zip(x_vals, y_vals)):
            if j % max(1, len(y_vals) // 8) == 0:
                ax.annotate(format_number_cn(yv), (xv, yv),
                            textcoords="offset points", xytext=(0, 12),
                            fontsize=8, color=color, ha="center",
                            fontweight="bold")

    num_labels = min(len(x_labels), 12)
    step = max(1, len(x_labels) // num_labels)
    tick_positions = list(range(0, len(x_labels), step))
    tick_labels = [clean_surrogates(x_labels[i][:12]) for i in tick_positions]
    ax.set_xticks(tick_positions)
    ax.set_xticklabels(tick_labels, rotation=30, ha="right", fontsize=9)
    ax.yaxis.set_major_formatter(mticker.FuncFormatter(lambda x, p: format_number_cn(x)))

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=20, loc="left")
    _setup_spines(ax)

    if len(series_list) > 1:
        legend = ax.legend(frameon=True, fontsize=10, loc="upper right",
                           framealpha=0.9, edgecolor="#E0E4E8", facecolor="white")
        legend.get_frame().set_linewidth(0.5)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("bar")
def generate_bar_chart(series_list, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    if len(series_list) == 1:
        series = series_list[0]
        y_vals = series["yValues"]
        x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])
        colors = [chart_colors[j % len(chart_colors)] for j in range(len(y_vals))]

        bars = ax.bar(range(len(y_vals)), y_vals, color=colors, width=0.65,
                      edgecolor="white", linewidth=0.8, zorder=3)

        for bar, val in zip(bars, y_vals):
            ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height(),
                    format_number_cn(val), ha="center", va="bottom",
                    fontsize=9, fontweight="bold", color="#424242")

        num_labels = min(len(x_labels), 15)
        step = max(1, len(x_labels) // num_labels)
        tick_positions = list(range(0, len(x_labels), step))
        tick_labels = [clean_surrogates(x_labels[i][:12]) for i in tick_positions]
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels, rotation=35, ha="right", fontsize=9)
    else:
        n_groups = len(series_list[0]["yValues"])
        n_series = len(series_list)
        bar_width = 0.7 / n_series
        x = np.arange(n_groups)

        for i, series in enumerate(series_list):
            color = chart_colors[i % len(chart_colors)]
            label = series.get("name", f"系列 {i + 1}")
            offset = (i - n_series / 2 + 0.5) * bar_width
            ax.bar(x + offset, series["yValues"], bar_width * 0.9,
                   color=color, label=label, edgecolor="white",
                   linewidth=0.5, zorder=3)

        x_labels = series_list[0].get("xLabels", [str(j) for j in range(n_groups)])
        num_labels = min(len(x_labels), 12)
        step = max(1, len(x_labels) // num_labels)
        tick_positions = list(range(0, len(x_labels), step))
        tick_labels = [clean_surrogates(x_labels[i][:12]) for i in tick_positions]
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels, rotation=35, ha="right", fontsize=9)
        ax.legend(frameon=True, fontsize=10, loc="upper right",
                  framealpha=0.9, edgecolor="#E0E4E8", facecolor="white")

    ax.yaxis.set_major_formatter(mticker.FuncFormatter(lambda x, p: format_number_cn(x)))
    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=20, loc="left")
    _setup_spines(ax)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("pie")
def generate_pie_chart(labels, values, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(9, 7))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    colors = [chart_colors[i % len(chart_colors)] for i in range(len(values))]
    total = sum(values) or 1

    wedges, texts, autotexts = ax.pie(
        values, labels=None, colors=colors, autopct="",
        startangle=90, pctdistance=0.75,
        wedgeprops={"edgecolor": "white", "linewidth": 2, "antialiased": True},
    )

    for i, (wedge, val) in enumerate(zip(wedges, values)):
        angle = (wedge.theta2 + wedge.theta1) / 2
        x = np.cos(np.radians(angle)) * 1.35
        y = np.sin(np.radians(angle)) * 1.35
        pct = val / total * 100
        label_text = clean_surrogates(labels[i][:20] if len(labels[i]) > 20 else labels[i])
        ax.annotate(f"{label_text}\n{format_number_cn(val)} ({pct:.1f}%)",
                    xy=(x * 0.7, y * 0.7), fontsize=9,
                    color=chart_colors[i % len(chart_colors)],
                    ha="center", va="center", fontweight="bold",
                    bbox=dict(boxstyle="round,pad=0.3", facecolor="white",
                              edgecolor="#E0E4E8", alpha=0.85))

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=25, loc="left")

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("doughnut")
def generate_doughnut_chart(labels, values, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(9, 7))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    colors = [chart_colors[i % len(chart_colors)] for i in range(len(values))]
    total = sum(values) or 1

    wedges, texts, autotexts = ax.pie(
        values, labels=None, colors=colors, autopct="%1.1f%%",
        startangle=90, pctdistance=0.85,
        wedgeprops={"edgecolor": "white", "linewidth": 2, "width": 0.4, "antialiased": True},
    )

    for autotext in autotexts:
        autotext.set_visible(False)

    centre_circle = plt.Circle((0, 0), 0.55, color="white", linewidth=0)
    ax.add_artist(centre_circle)

    ax.text(0, 0, f"总计\n{format_number_cn(total)}",
            ha="center", va="center", fontsize=16, fontweight="bold",
            color=scheme.get("primary", "#1A3C6D"))

    for i, (wedge, val) in enumerate(zip(wedges, values)):
        angle = (wedge.theta2 + wedge.theta1) / 2
        x = np.cos(np.radians(angle)) * 1.4
        y = np.sin(np.radians(angle)) * 1.4
        pct = val / total * 100
        label_text = clean_surrogates(labels[i][:18] if len(labels[i]) > 18 else labels[i])
        ax.annotate(f"{label_text}\n{format_number_cn(val)} ({pct:.1f}%)",
                    xy=(x * 0.75, y * 0.75), fontsize=8,
                    color=chart_colors[i % len(chart_colors)],
                    ha="center", va="center", fontweight="bold",
                    bbox=dict(boxstyle="round,pad=0.25", facecolor="white",
                              edgecolor="#E0E4E8", alpha=0.9))

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=25, loc="left")

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("radar")
def generate_radar_chart(series_list, title, file_path, scheme):
    categories = series_list[0].get("xLabels", [])
    n = len(categories)
    angles = np.linspace(0, 2 * np.pi, n, endpoint=False).tolist()
    angles += angles[:1]

    fig, ax = plt.subplots(figsize=(9, 8), subplot_kw={"polar": True})
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    ax.set_facecolor("white")
    chart_colors = scheme.get("chart_colors", [])

    all_vals = [v for s in series_list for v in s["yValues"]]
    max_val = max(all_vals) * 1.15 if all_vals else 1

    for i, series in enumerate(series_list):
        color = chart_colors[i % len(chart_colors)]
        label = series.get("name", f"系列 {i + 1}")
        values = series["yValues"] + [series["yValues"][0]]
        ax.fill(angles, values, color=color, alpha=0.15)
        ax.plot(angles, values, color=color, linewidth=2, marker="o",
                markersize=6, label=label)
        for j, val in enumerate(values[:-1]):
            ax.annotate(format_number_cn(val), (angles[j], val),
                        fontsize=8, color=color, fontweight="bold")

    ax.set_xticks(angles[:-1])
    ax.set_xticklabels([clean_surrogates(c[:10]) for c in categories], fontsize=10, color="#424242")
    ax.set_yticklabels([])
    ax.spines["polar"].set_color("#E0E4E8")
    ax.grid(color="#E0E4E8", linewidth=0.6)
    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=30, loc="left")

    if len(series_list) > 1:
        legend = ax.legend(loc="upper right", bbox_to_anchor=(1.3, 1.1),
                           fontsize=10, frameon=True, edgecolor="#E0E4E8")
        legend.get_frame().set_alpha(0.9)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("scatter")
def generate_scatter_chart(series_list, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    for i, series in enumerate(series_list):
        color = chart_colors[i % len(chart_colors)]
        label = series.get("name", f"系列 {i + 1}")
        x_vals = series.get("xValues", list(range(len(series["yValues"]))))
        y_vals = series["yValues"]

        sizes = series.get("sizes", [60] * len(y_vals))
        ax.scatter(x_vals, y_vals, s=sizes, c=color, alpha=0.7,
                   edgecolors="white", linewidth=0.8, label=label, zorder=5)

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=20, loc="left")
    _setup_spines(ax)

    if len(series_list) > 1:
        ax.legend(frameon=True, fontsize=10, loc="upper right",
                  framealpha=0.9, edgecolor="#E0E4E8", facecolor="white")

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("area")
def generate_area_chart(series_list, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    for i, series in enumerate(series_list):
        color = chart_colors[i % len(chart_colors)]
        label = series.get("name", f"系列 {i + 1}")
        x_vals = list(range(len(series["yValues"])))
        y_vals = series["yValues"]
        x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])

        ax.fill_between(x_vals, y_vals, color=color, alpha=0.2, zorder=4)
        ax.plot(x_vals, y_vals, color=color, linewidth=2.5, marker="o",
                markersize=4, label=label, zorder=5)

    num_labels = min(len(x_labels), 12)
    step = max(1, len(x_labels) // num_labels)
    tick_positions = list(range(0, len(x_labels), step))
    tick_labels = [clean_surrogates(x_labels[i][:12]) for i in tick_positions]
    ax.set_xticks(tick_positions)
    ax.set_xticklabels(tick_labels, rotation=30, ha="right", fontsize=9)
    ax.yaxis.set_major_formatter(mticker.FuncFormatter(lambda x, p: format_number_cn(x)))

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=20, loc="left")
    _setup_spines(ax)

    if len(series_list) > 1:
        ax.legend(frameon=True, fontsize=10, loc="upper right",
                  framealpha=0.9, edgecolor="#E0E4E8", facecolor="white")

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


@register("hbar")
def generate_hbar_chart(series_list, title, file_path, scheme):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(scheme.get("bg_section", "#FAFAFA"))
    chart_colors = scheme.get("chart_colors", [])

    series = series_list[0]
    y_vals = series["yValues"]
    x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])

    y_pos = range(len(y_vals))
    colors = [chart_colors[j % len(chart_colors)] for j in range(len(y_vals))]

    bars = ax.barh(y_pos, y_vals, color=colors, height=0.6,
                   edgecolor="white", linewidth=0.8, zorder=3)

    for bar, val in zip(bars, y_vals):
        ax.text(bar.get_width() + max(y_vals) * 0.01 if max(y_vals) else 0.01,
                bar.get_y() + bar.get_height() / 2,
                format_number_cn(val), ha="left", va="center",
                fontsize=9, fontweight="bold", color="#424242")

    ax.set_yticks(y_pos)
    ax.set_yticklabels([clean_surrogates(l[:14]) for l in x_labels], fontsize=10)
    ax.invert_yaxis()
    ax.xaxis.set_major_formatter(mticker.FuncFormatter(lambda x, p: format_number_cn(x)))

    ax.set_title(clean_surrogates(title), fontsize=18, fontweight="bold",
                 color=scheme.get("primary", "#1A3C6D"), pad=20, loc="left")
    ax.spines["top"].set_visible(False)
    ax.spines["right"].set_visible(False)
    ax.spines["left"].set_visible(False)
    ax.spines["bottom"].set_color("#E0E4E8")
    ax.tick_params(axis="both", length=0)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=scheme.get("bg_section", "#FAFAFA"))
    plt.close(fig)


def generate_chart(chart_type, data, title, output_path, scheme_name="chinese_business", labels=None, values=None):
    """统一图表生成入口"""
    scheme = configure_matplotlib(scheme_name)
    ensure_output_dir(output_path)

    if chart_type in ("pie", "doughnut"):
        if labels is not None and values is not None:
            generator = CHART_GENERATORS[chart_type]
            generator(labels, values, title, output_path, scheme)
        elif isinstance(data, list) and len(data) > 0:
            first = data[0]
            generator = CHART_GENERATORS[chart_type]
            generator(first.get("xLabels", []), first["yValues"], title, output_path, scheme)
        else:
            raise ChartGenerationError("饼图/环形图需要提供 labels/values 或有效的 series 数据")
    else:
        generator = CHART_GENERATORS.get(chart_type)
        if not generator:
            raise ChartGenerationError(f"不支持的图表类型: {chart_type}",
                                        details={"supported": list(CHART_GENERATORS.keys())})
        generator(data, title, output_path, scheme)

    return output_path


def main():
    configure_matplotlib()
    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    try:
        input_data = json.loads(sys.stdin.read())
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "error": f"JSON解析失败: {e}"}))
        sys.exit(1)

    chart_type = input_data.get("chartType", "line")
    title = input_data.get("title", "数据图表")
    output_path = input_data.get("outputPath", "chart.png")
    scheme_name = input_data.get("scheme", "chinese_business")
    series_list = input_data.get("series", [])
    labels = input_data.get("labels", [])
    values = input_data.get("values", [])

    scheme = configure_matplotlib(scheme_name)
    ensure_output_dir(output_path)

    try:
        if chart_type in ("pie", "doughnut"):
            if labels and values:
                CHART_GENERATORS[chart_type](labels, values, title, output_path, scheme)
            elif series_list:
                first = series_list[0]
                CHART_GENERATORS[chart_type](
                    first.get("xLabels", []), first["yValues"],
                    title, output_path, scheme
                )
            else:
                raise ValidationError("饼图/环形图需要提供 labels 和 values 或有效的 series")
        else:
            generator = CHART_GENERATORS.get(chart_type)
            if not generator:
                generator = CHART_GENERATORS["line"]
            generator(series_list, title, output_path, scheme)

        print(json.dumps({"success": True, "path": output_path}))
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}))
        sys.exit(1)


if __name__ == "__main__":
    main()
