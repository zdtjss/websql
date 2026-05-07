#!/usr/bin/env python3
"""
DOCX Template Assembly — 从多个模板文件中组合章节生成最终文档
支持从多个 .docx 文件按顺序拼接内容到新文档。
基于 Anthropic docx skill 的模板组装思想。
"""

import sys
import os
import zipfile
import tempfile
import shutil
import copy
from lxml import etree


_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(_SCRIPT_DIR))


NSMAP = {
    "w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "pkg": "http://schemas.openxmlformats.org/package/2006/relationships",
}


def assemble(master_path, section_paths, output_path, section_breaks=True):
    """将多个 section docx 文件追加到 master 模板后面"""
    W = NSMAP["w"]
    R = NSMAP["r"]

    prs = zipfile.ZipFile(master_path, "r")
    master_dir = tempfile.mkdtemp()
    prs.extractall(master_dir)
    prs.close()

    sections_to_clean = []

    for i, section_path in enumerate(section_paths):
        if not os.path.isfile(section_path):
            print(f"WARNING: 章节文件不存在: {section_path}", file=sys.stderr)
            continue

        section_dir = tempfile.mkdtemp()
        sections_to_clean.append(section_dir)

        with zipfile.ZipFile(section_path, "r") as szf:
            szf.extractall(section_dir)

        src_doc_path = os.path.join(section_dir, "word", "document.xml")
        if not os.path.isfile(src_doc_path):
            print(f"WARNING: 章节缺少 word/document.xml", file=sys.stderr)
            continue

        dest_doc_path = os.path.join(master_dir, "word", "document.xml")
        src_tree = etree.parse(src_doc_path)
        dest_tree = etree.parse(dest_doc_path)

        src_body = src_tree.find(f"./{W}body")
        dest_body = dest_tree.find(f"./{W}body")

        if src_body is None or dest_body is None:
            continue

        if section_breaks and i > 0:
            sect_pr = etree.SubElement(dest_body, f"{W}p")
            pPr = etree.SubElement(sect_pr, f"{W}pPr")
            sectPr = etree.SubElement(pPr, f"{W}sectPr")

        for child in list(src_body):
            tag_local = child.tag.split("}")[-1] if "}" in child.tag else child.tag
            if tag_local == "sectPr":
                continue
            dest_body.append(copy.deepcopy(child))

        dest_tree.write(dest_doc_path, xml_declaration=True, encoding="UTF-8", standalone=True)

    with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zfout:
        for root, _, files in os.walk(master_dir):
            for fname in files:
                fp = os.path.join(root, fname)
                arc = os.path.relpath(fp, master_dir).replace("\\", "/")
                zfout.write(fp, arc)

    shutil.rmtree(master_dir)
    for sd in sections_to_clean:
        shutil.rmtree(sd)

    print(f"文档组装完成: {len(section_paths)} 章节 -> {output_path}")


if __name__ == "__main__":
    if len(sys.argv) < 4:
        print("用法: python assemble.py <master.docx> <output.docx> <section1.docx> [section2.docx ...]", file=sys.stderr)
        sys.exit(1)

    master = sys.argv[1]
    output = sys.argv[2]
    sections = sys.argv[3:]
    assemble(master, output, sections)
