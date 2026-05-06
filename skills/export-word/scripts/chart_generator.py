#!/usr/bin/env python3
"""
专业图表生成器 — WebSQL AI 平台
基于 matplotlib 生成符合中国商务审美的数据可视化图表。
"""

import json
import os
import sys

import matplotlib
matplotlib.use("Agg")

import matplotlib.pyplot as plt
import matplotlib.ticker as mticker
import numpy as np

BRAND_COLORS = [
    "#1A3C6D", "#C0392B", "#8B6914", "#2E7D32",
    "#1565C0", "#E65100", "#6A1B9A", "#00838F",
    "#AD1457", "#4E342E", "#37474F", "#BF360C",
]

BG_COLOR = "#FAFAFA"
GRID_COLOR = "#E0E4E8"
TEXT_COLOR = "#424242"
TITLE_COLOR = "#1A3C6D"
ACCENT_COLOR = "#C0392B"


def configure_matplotlib():
    plt.rcParams.update({
        "font.family": "sans-serif",
        "font.sans-serif": ["Microsoft YaHei", "SimHei", "DejaVu Sans"],
        "axes.unicode_minus": False,
        "axes.facecolor": "white",
        "axes.edgecolor": GRID_COLOR,
        "axes.grid": True,
        "axes.grid.axis": "y",
        "grid.color": GRID_COLOR,
        "grid.linewidth": 0.6,
        "grid.alpha": 0.8,
        "figure.facecolor": BG_COLOR,
        "figure.dpi": 150,
        "savefig.dpi": 150,
        "savefig.bbox": "tight",
        "savefig.pad_inches": 0.3,
        "text.color": TEXT_COLOR,
        "xtick.color": "#757575",
        "ytick.color": "#757575",
    })


def format_number(num):
    if abs(num) >= 1e8:
        return f"{num/1e8:.2f}\u4ebf"
    if abs(num) >= 1e4:
        return f"{num/1e4:.1f}\u4e07"
    if num == int(num):
        return str(int(num))
    return f"{num:.1f}"


def generate_line_chart(series_list, title, file_path):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(BG_COLOR)

    for i, series in enumerate(series_list):
        color = BRAND_COLORS[i % len(BRAND_COLORS)]
        label = series.get("name", f"\u7cfb\u5217 {i+1}")

        x_vals = list(range(len(series["yValues"])))
        y_vals = series["yValues"]
        x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])

        ax.plot(x_vals, y_vals, color=color, linewidth=2.8, marker="o",
                markersize=5, markerfacecolor="white", markeredgewidth=2,
                markeredgecolor=color, label=label, zorder=5)

        for j, (xv, yv) in enumerate(zip(x_vals, y_vals)):
            if j % max(1, len(y_vals) // 8) == 0:
                ax.annotate(format_number(yv), (xv, yv),
                            textcoords="offset points", xytext=(0, 12),
                            fontsize=8, color=color, ha="center",
                            fontweight="bold")

    num_labels = min(len(x_labels), 12)
    step = max(1, len(x_labels) // num_labels)
    tick_positions = list(range(0, len(x_labels), step))
    tick_labels = [x_labels[i][:12] for i in tick_positions]
    ax.set_xticks(tick_positions)
    ax.set_xticklabels(tick_labels, rotation=30, ha="right", fontsize=9)

    ax.yaxis.set_major_formatter(mticker.FuncFormatter(
        lambda x, p: format_number(x)))

    ax.set_title(title, fontsize=18, fontweight="bold", color=TITLE_COLOR,
                 pad=20, loc="left")
    ax.spines["top"].set_visible(False)
    ax.spines["right"].set_visible(False)
    ax.spines["left"].set_color(GRID_COLOR)
    ax.spines["bottom"].set_color(GRID_COLOR)
    ax.tick_params(axis="both", length=0)

    if len(series_list) > 1:
        legend = ax.legend(frameon=True, fontsize=10, loc="upper right",
                           framealpha=0.9, edgecolor=GRID_COLOR,
                           facecolor="white")
        legend.get_frame().set_linewidth(0.5)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=BG_COLOR)
    plt.close(fig)


def generate_bar_chart(series_list, title, file_path):
    fig, ax = plt.subplots(figsize=(11, 6))
    fig.patch.set_facecolor(BG_COLOR)

    if len(series_list) == 1:
        series = series_list[0]
        y_vals = series["yValues"]
        x_labels = series.get("xLabels", [str(j) for j in range(len(y_vals))])

        colors = [BRAND_COLORS[j % len(BRAND_COLORS)] for j in range(len(y_vals))]
        bars = ax.bar(range(len(y_vals)), y_vals, color=colors, width=0.65,
                      edgecolor="white", linewidth=0.8, zorder=3)

        for bar, val in zip(bars, y_vals):
            ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height(),
                    format_number(val), ha="center", va="bottom",
                    fontsize=9, fontweight="bold", color=TEXT_COLOR)

        num_labels = min(len(x_labels), 15)
        step = max(1, len(x_labels) // num_labels)
        tick_positions = list(range(0, len(x_labels), step))
        tick_labels = [x_labels[i][:12] for i in tick_positions]
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels, rotation=35, ha="right", fontsize=9)
    else:
        n_groups = len(series_list[0]["yValues"])
        n_series = len(series_list)
        bar_width = 0.7 / n_series
        x = np.arange(n_groups)

        for i, series in enumerate(series_list):
            color = BRAND_COLORS[i % len(BRAND_COLORS)]
            label = series.get("name", f"\u7cfb\u5217 {i+1}")
            offset = (i - n_series / 2 + 0.5) * bar_width
            ax.bar(x + offset, series["yValues"], bar_width * 0.9,
                   color=color, label=label, edgecolor="white",
                   linewidth=0.5, zorder=3)

        x_labels = series_list[0].get("xLabels",
                                       [str(j) for j in range(n_groups)])
        num_labels = min(len(x_labels), 12)
        step = max(1, len(x_labels) // num_labels)
        tick_positions = list(range(0, len(x_labels), step))
        tick_labels = [x_labels[i][:12] for i in tick_positions]
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels, rotation=35, ha="right", fontsize=9)

        legend = ax.legend(frameon=True, fontsize=10, loc="upper right",
                           framealpha=0.9, edgecolor=GRID_COLOR,
                           facecolor="white")

    ax.yaxis.set_major_formatter(mticker.FuncFormatter(
        lambda x, p: format_number(x)))

    ax.set_title(title, fontsize=18, fontweight="bold", color=TITLE_COLOR,
                 pad=20, loc="left")
    ax.spines["top"].set_visible(False)
    ax.spines["right"].set_visible(False)
    ax.spines["left"].set_color(GRID_COLOR)
    ax.spines["bottom"].set_color(GRID_COLOR)
    ax.tick_params(axis="both", length=0)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=BG_COLOR)
    plt.close(fig)


def generate_pie_chart(labels, values, title, file_path):
    fig, ax = plt.subplots(figsize=(9, 7))
    fig.patch.set_facecolor(BG_COLOR)

    colors = [BRAND_COLORS[i % len(BRAND_COLORS)] for i in range(len(values))]
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
        label_text = labels[i][:20] if len(labels[i]) > 20 else labels[i]
        ax.annotate(f"{label_text}\n{format_number(val)} ({pct:.1f}%)",
                    xy=(x * 0.7, y * 0.7), fontsize=9,
                    color=BRAND_COLORS[i % len(BRAND_COLORS)],
                    ha="center", va="center", fontweight="bold",
                    bbox=dict(boxstyle="round,pad=0.3", facecolor="white",
                              edgecolor=GRID_COLOR, alpha=0.85))

    ax.set_title(title, fontsize=18, fontweight="bold", color=TITLE_COLOR,
                 pad=25, loc="left")

    fig.tight_layout()
    fig.savefig(file_path, facecolor=BG_COLOR)
    plt.close(fig)


def generate_radar_chart(series_list, title, file_path):
    categories = series_list[0].get("xLabels", [])
    n = len(categories)
    angles = np.linspace(0, 2 * np.pi, n, endpoint=False).tolist()
    angles += angles[:1]

    fig, ax = plt.subplots(figsize=(9, 8), subplot_kw={"polar": True})
    fig.patch.set_facecolor(BG_COLOR)
    ax.set_facecolor("white")

    for i, series in enumerate(series_list):
        color = BRAND_COLORS[i % len(BRAND_COLORS)]
        label = series.get("name", f"\u7cfb\u5217 {i+1}")
        values = series["yValues"] + [series["yValues"][0]]
        ax.fill(angles, values, color=color, alpha=0.15)
        ax.plot(angles, values, color=color, linewidth=2, marker="o",
                markersize=6, label=label)
        for j, val in enumerate(values[:-1]):
            ax.annotate(format_number(val), (angles[j], val),
                        fontsize=8, color=color, fontweight="bold")

    ax.set_xticks(angles[:-1])
    ax.set_xticklabels([c[:10] for c in categories], fontsize=10, color=TEXT_COLOR)
    ax.set_yticklabels([])
    ax.spines["polar"].set_color(GRID_COLOR)
    ax.grid(color=GRID_COLOR, linewidth=0.6)
    ax.set_title(title, fontsize=18, fontweight="bold", color=TITLE_COLOR,
                 pad=30, loc="left")

    legend = ax.legend(loc="upper right", bbox_to_anchor=(1.3, 1.1),
                       fontsize=10, frameon=True, edgecolor=GRID_COLOR)
    legend.get_frame().set_alpha(0.9)

    fig.tight_layout()
    fig.savefig(file_path, facecolor=BG_COLOR)
    plt.close(fig)


def main():
    configure_matplotlib()

    input_data = json.loads(sys.stdin.read())
    chart_type = input_data.get("chartType", "line")
    title = input_data.get("title", "\u6570\u636e\u56fe\u8868")
    output_path = input_data.get("outputPath", "chart.png")
    series_list = input_data.get("series", [])
    labels = input_data.get("labels", [])
    values = input_data.get("values", [])

    if chart_type in ("pie", "doughnut"):
        if labels and values:
            generate_pie_chart(labels, values, title, output_path)
        elif series_list:
            first = series_list[0]
            generate_pie_chart(
                first.get("xLabels", []),
                first["yValues"],
                title, output_path
            )
        else:
            raise ValueError("\u997c\u56fe\u9700\u8981\u63d0\u4f9b labels \u548c values")
    elif chart_type == "radar":
        generate_radar_chart(series_list, title, output_path)
    elif chart_type == "bar":
        generate_bar_chart(series_list, title, output_path)
    else:
        generate_line_chart(series_list, title, output_path)

    print(json.dumps({"success": True, "path": output_path}))


if __name__ == "__main__":
    main()
