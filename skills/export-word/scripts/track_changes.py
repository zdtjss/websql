#!/usr/bin/env python3
"""
DOCX Track Changes — 启用修订追踪 (Redlining)
在 OOXML 级别插入 track changes 标记，实现文档修订与批注。
基于 Anthropic docx skill 的 ooxml.js 思想。
"""

import sys
import os
import zipfile
import uuid
import tempfile
import shutil
from lxml import etree


_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(_SCRIPT_DIR))


NSMAP = {
    "w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
}


def enable_track_changes(input_path, output_path, author="WebSQL AI", date=None):
    """为文档启用修订追踪"""
    W = NSMAP["w"]

    if date is None:
        from datetime import datetime
        date = datetime.now().strftime("%Y-%m-%dT%H:%M:%SZ")

    rsid = uuid.uuid4().hex[:8].upper()
    rsid_root = uuid.uuid4().hex[:8].upper()

    tmpdir = tempfile.mkdtemp()
    try:
        with zipfile.ZipFile(input_path, "r") as zf:
            zf.extractall(tmpdir)

        settings_path = os.path.join(tmpdir, "word", "settings.xml")
        document_path = os.path.join(tmpdir, "word", "document.xml")

        # settings.xml
        if os.path.isfile(settings_path):
            stree = etree.parse(settings_path)
            sroot = stree.getroot()

            track_elem = etree.SubElement(sroot, f"{W}trackRevisions")
            rsid_elem = etree.SubElement(sroot, f"{W}rsids")
            rsid_root_attr = etree.SubElement(rsid_elem, f"{W}rsidRoot")
            rsid_root_attr.set(f"{W}val", rsid_root)
            rsid_item = etree.SubElement(rsid_elem, f"{W}rsid")
            rsid_item.set(f"{W}val", rsid)

            stree.write(settings_path, xml_declaration=True, encoding="UTF-8", standalone=True)
            print(f"修订追踪已启用 (RSID: {rsid})")
        else:
            print("WARNING: settings.xml 未找到")

        if os.path.isfile(document_path):
            dtree = etree.parse(document_path)
            droot = dtree.getroot()
            body = droot.find(f"./{W}body")
            if body is not None:
                sect_pr = body.find(f"./{W}sectPr")
                if sect_pr is None:
                    sect_pr = etree.SubElement(body, f"{W}sectPr")

                rsid_attr = sect_pr.find(f"{W}rsidR")
                if rsid_attr is None:
                    rsid_attr = etree.SubElement(sect_pr, f"{W}rsidR")
                rsid_attr.set(f"{W}val", rsid)

            dtree.write(document_path, xml_declaration=True, encoding="UTF-8", standalone=True)

        with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zfout:
            for root, _, files in os.walk(tmpdir):
                for fname in files:
                    fp = os.path.join(root, fname)
                    arc = os.path.relpath(fp, tmpdir).replace("\\", "/")
                    zfout.write(fp, arc)

        print(f"已导出 -> {output_path}")

    finally:
        shutil.rmtree(tmpdir)


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("用法: python track_changes.py <input.docx> <output.docx> [author]", file=sys.stderr)
        sys.exit(1)

    author = sys.argv[3] if len(sys.argv) > 3 else "WebSQL AI"
    enable_track_changes(sys.argv[1], sys.argv[2], author)
