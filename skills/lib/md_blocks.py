# -*- coding: utf-8 -*-
"""md_blocks.py — 导出脚本共享的 Markdown → 结构块解析模块。

export_ppt.py 与 word_generator.py 的 content 模式共用此模块，
承接原 Go 端 markdown.go（ParseMarkdownBlocks / GenerateXxxFromContent）的职责，
使 Markdown 解析在 Python 渲染侧全局唯一。

块结构（kind 与 Word 模板 docxtpl 骨架对齐）：
    {kind: paragraph|subheading|bullets|code|table, content, items, lines,
     headers, rows}
"""

import re

_NUM_ITEM_RE = re.compile(r"^(\d+)[.)]\s+(.*)$")


def parse_markdown_table(md_text):
    """解析 Markdown 表格文本为 [headers] + rows 格式；非法返回 None。

    由 word_generator.py 原函数迁移至此，逻辑保持一致。
    """
    lines = [l.strip() for l in md_text.strip().split("\n") if l.strip()]
    if len(lines) < 2:
        return None

    def split_cells(line):
        return [c.strip() for c in line.strip("|").split("|")]

    headers = split_cells(lines[0])
    if not headers:
        return None

    data_start = 1
    # 跳过分隔行 (|---|---|)
    if data_start < len(lines):
        sep = split_cells(lines[data_start])
        if all(c.replace("-", "").replace(":", "").replace(" ", "") == "" for c in sep):
            data_start += 1

    rows = [split_cells(l) for l in lines[data_start:]]
    return [headers] + rows


def markdown_to_sections(md_text):
    """把 Markdown 文本解析为 sections 结构。

    返回 [{title, level, blocks}]：
      - `#`~`###` 行 → 新节（title/level）
      - `####` 及以上 → 节内 subheading block
      - `- ` / `* ` / 编号项 → bullets block（items 列表）
      - ``` 代码块 → code block（lines 列表）
      - `|` 表格块 → table block（headers/rows）
      - 其余连续行 → paragraph block（content 合并）
    """
    sections = []
    cur = {"title": "", "level": 0, "blocks": []}
    pending_paras = []
    pending_bullets = []

    def flush_paras():
        if pending_paras:
            cur["blocks"].append(_merged_block("paragraph", " ".join(pending_paras)))
            pending_paras.clear()

    def flush_bullets():
        if pending_bullets:
            cur["blocks"].append(_list_block("bullets", pending_bullets))
            pending_bullets.clear()

    lines = md_text.split("\n")
    i, n = 0, len(lines)
    while i < n:
        stripped = lines[i].rstrip().strip()

        # 代码块
        if stripped.startswith("```"):
            flush_paras()
            flush_bullets()
            code_lines = []
            i += 1
            while i < n and not lines[i].strip().startswith("```"):
                code_lines.append(lines[i])
                i += 1
            i += 1  # 跳过结束 ```
            cur["blocks"].append(_code_block(code_lines))
            continue

        # 标题
        if stripped.startswith("#"):
            level = len(stripped) - len(stripped.lstrip("#"))
            title = stripped.lstrip("#").strip()
            flush_paras()
            flush_bullets()
            if 1 <= level <= 3:
                if cur["title"] or cur["blocks"]:
                    sections.append(cur)
                cur = {"title": title, "level": level, "blocks": []}
            else:
                cur["blocks"].append(_heading_block(title))
            i += 1
            continue

        # 表格块（`|` 开头且下一行仍是 `|`）
        if stripped.startswith("|") and i + 1 < n and lines[i + 1].strip().startswith("|"):
            table_lines = []
            while i < n and lines[i].strip().startswith("|"):
                table_lines.append(lines[i].strip())
                i += 1
            flush_paras()
            flush_bullets()
            parsed = parse_markdown_table("\n".join(table_lines))
            if parsed:
                headers = [str(h) for h in parsed[0]]
                rows = _align_rows(parsed[1:], len(headers))
                if headers:
                    cur["blocks"].append(_table_block(headers, rows))
            continue

        # 无序列表
        if stripped.startswith(("- ", "* ", "+ ")):
            flush_paras()
            pending_bullets.append(stripped[2:].strip())
            i += 1
            continue

        # 编号列表
        m = _NUM_ITEM_RE.match(stripped)
        if m:
            flush_paras()
            pending_bullets.append(m.group(2))
            i += 1
            continue

        # 空行
        if not stripped:
            flush_paras()
            flush_bullets()
            i += 1
            continue

        # 普通段落
        flush_bullets()
        pending_paras.append(stripped)
        i += 1

    flush_paras()
    flush_bullets()
    if cur["title"] or cur["blocks"]:
        sections.append(cur)
    return sections


# ─── 块构造辅助 ─────────────────────────────────────────────────


def _merged_block(kind, content):
    return {"kind": kind, "content": content, "items": [], "lines": []}


def _heading_block(content):
    return {"kind": "subheading", "content": content, "items": [], "lines": []}


def _list_block(kind, items):
    return {"kind": kind, "content": "", "items": list(items), "lines": []}


def _code_block(lines):
    return {"kind": "code", "content": "\n".join(lines),
            "items": [], "lines": list(lines)}


def _table_block(headers, rows):
    return {"kind": "table", "content": "", "items": [], "lines": [],
            "headers": headers, "rows": rows}


def _align_rows(rows, n_cols):
    """补齐/截断每行单元格数，防止渲染时索引越界。"""
    aligned = []
    for row in rows:
        cells = [str(c) for c in row]
        if len(cells) < n_cols:
            cells += [""] * (n_cols - len(cells))
        elif len(cells) > n_cols:
            cells = cells[:n_cols]
        aligned.append(cells)
    return aligned


# ─── 旧契约 sections 规范化 ───────────────────────────────────

_KIND_ALIAS = {
    "text": "paragraph", "heading": "subheading",
    "h1": "subheading", "h2": "subheading", "h3": "subheading",
    "bullet": "bullets", "list": "bullets",
    "chart": "image",
}


def block_kind(block):
    """兼容旧契约 type 字段与新 kind 字段，统一返回 kind。"""
    kind = block.get("kind") or block.get("type") or "paragraph"
    return _KIND_ALIAS.get(kind, kind)


def block_items(block):
    """bullets 块取 items（兼容 content 换行字符串）。"""
    items = block.get("items")
    if items:
        return [str(x) for x in items]
    content = block.get("content", "")
    return [x for x in str(content).split("\n") if x.strip()]


def normalize_sections(raw):
    """把旧契约 sections（blocks 的 type 字段）规范化为统一结构。

    table block: content 可为 Markdown 表格字符串或 list-of-lists
    image block: 携带 chartType/title/data（labels/values 简单格式）
    """
    sections = []
    for sec in raw:
        title = sec.get("title", "")
        level = int(sec.get("level", 2))
        blocks = []
        for b in sec.get("blocks", []):
            kind = block_kind(b)
            nb = {"kind": kind, "content": str(b.get("content", "")),
                  "items": block_items(b), "lines": []}
            if kind == "table":
                content = b.get("content", "")
                if isinstance(content, list):
                    parsed = [[str(c) for c in row] for row in content]
                else:
                    parsed = parse_markdown_table(str(content))
                if parsed and parsed[0]:
                    nb["headers"] = [str(h) for h in parsed[0]]
                    nb["rows"] = _align_rows(parsed[1:], len(nb["headers"]))
                else:
                    nb["headers"] = []
                    nb["rows"] = []
            if kind == "image":
                nb["chart_type"] = b.get("chartType", "bar")
                nb["chart_title"] = b.get("title", "")
                chart_data = b.get("data", {}) or {}
                nb["labels"] = [str(l) for l in chart_data.get("labels", [])]
                nb["values"] = list(chart_data.get("values", []))
            if kind == "code":
                nb["lines"] = str(b.get("content", "")).split("\n")
            blocks.append(nb)
        sections.append({"title": title, "level": level, "blocks": blocks})
    return sections
