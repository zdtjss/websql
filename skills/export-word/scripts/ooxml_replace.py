#!/usr/bin/env python3
"""
DOCX OOXML Text Replace — 直接操作 OOXML XML 进行精确文字替换
保留格式（rPr），只替换文本内容（t 节点）。
基于 Anthropic docx skill 的 ooxml 编辑思想。
"""

import sys
import os
import zipfile
import json
import re
import tempfile
import shutil
from lxml import etree


_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(_SCRIPT_DIR))


NSMAP = {
    "w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "v": "urn:schemas-microsoft-com:vml",
}


def load_replacements(mapping):
    """加载替换映射: JSON 文件或 JSON 字符串"""
    if os.path.isfile(mapping):
        with open(mapping, "r", encoding="utf-8") as f:
            return json.load(f)
    return json.loads(mapping)


def ooxml_text_replace(input_path, replacements, output_path):
    """在 OOXML 级别进行文本替换：解包 -> 逐文件替换 -> 打包"""
    replacements_map = load_replacements(replacements) if isinstance(replacements, str) else replacements

    tmpdir = tempfile.mkdtemp()
    try:
        with zipfile.ZipFile(input_path, "r") as zf:
            zf.extractall(tmpdir)

        document_xml = os.path.join(tmpdir, "word", "document.xml")
        headers_footers_dir = os.path.join(tmpdir, "word")

        _replace_in_xml(document_xml, replacements_map, "document")

        for root, _, files in os.walk(headers_footers_dir):
            for f in files:
                if f in ("header1.xml", "header2.xml", "header3.xml",
                         "footer1.xml", "footer2.xml", "footer3.xml"):
                    fp = os.path.join(root, f)
                    if os.path.isfile(fp):
                        _replace_in_xml(fp, replacements_map, f)

        with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zfout:
            for root, _, files in os.walk(tmpdir):
                for fname in files:
                    fp = os.path.join(root, fname)
                    arc = os.path.relpath(fp, tmpdir).replace("\\", "/")
                    zfout.write(fp, arc)

        print(f"OOXML 替换完成 -> {output_path}")

    finally:
        shutil.rmtree(tmpdir)


def _replace_in_xml(xml_path, replacements, label):
    if not os.path.isfile(xml_path):
        return

    tree = etree.parse(xml_path)
    root = tree.getroot()

    W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"

    t_nodes = root.findall(f".//{W}t")
    replaced_count = 0

    for t_node in t_nodes:
        if t_node.text and t_node.get("{http://www.w3.org/XML/1998/namespace}space") != "preserve":
            original = t_node.text
            replaced = original
            for old, new in replacements_map.items():
                if old in replaced:
                    replaced = replaced.replace(old, new)

            if replaced != original:
                t_node.text = replaced
                replaced_count += 1

    if replaced_count > 0:
        tree.write(xml_path, xml_declaration=True, encoding="UTF-8", standalone=True)
        print(f"[{label}] 替换了 {replaced_count} 处文本")


if __name__ == "__main__":
    if len(sys.argv) < 4:
        print("用法: python ooxml_replace.py <input.docx> <replacements.json> <output.docx>", file=sys.stderr)
        sys.exit(1)

    ooxml_text_replace(sys.argv[1], sys.argv[2], sys.argv[3])
