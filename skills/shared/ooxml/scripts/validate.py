#!/usr/bin/env python3
"""
OOXML Validate -- 基础结构验证
检查 [Content_Types].xml、_rels 关系链和 XML 良构性。
"""

import sys
import os
import xml.etree.ElementTree as ET

_NS = {
    "ct": "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "pr": "http://schemas.openxmlformats.org/package/2006/relationships",
    "dc": "http://purl.org/dc/elements/1.1/",
    "cp": "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
    "dct": "http://purl.org/dc/terms/",
    "xsi": "http://www.w3.org/2001/XMLSchema-instance",
}


def _validate_xml(file_path):
    try:
        tree = ET.parse(file_path)
        return True, None
    except ET.ParseError as e:
        return False, str(e)


def validate(dir_path, original_file=None):
    dir_path = os.path.abspath(dir_path)
    errors = []

    ct_path = os.path.join(dir_path, "[Content_Types].xml")
    if not os.path.isfile(ct_path):
        errors.append("MISSING: [Content_Types].xml")
    else:
        ok, err = _validate_xml(ct_path)
        if not ok:
            errors.append(f"INVALID XML: [Content_Types].xml — {err}")

    rels_dir = os.path.join(dir_path, "_rels")
    if not os.path.isdir(rels_dir):
        errors.append("MISSING: _rels/ 目录")
    else:
        for root, _, files in os.walk(dir_path):
            for f in files:
                if f.endswith(".xml"):
                    fp = os.path.join(root, f)
                    ok, err = _validate_xml(fp)
                    if not ok:
                        short = os.path.relpath(fp, dir_path)
                        errors.append(f"INVALID XML: {short} — {err}")

    # 检查主要部件
    main_parts = {
        ".pptx": ["ppt/presentation.xml"],
        ".docx": ["word/document.xml"],
        ".xlsx": ["xl/workbook.xml"],
    }

    if original_file:
        ext = os.path.splitext(original_file)[1].lower()
        for part in main_parts.get(ext, []):
            part_path = os.path.join(dir_path, part)
            if not os.path.isfile(part_path):
                errors.append(f"MISSING: {part}")
            else:
                ok, e = _validate_xml(part_path)
                if not ok:
                    errors.append(f"INVALID XML: {part} — {e}")

    if errors:
        print(f"验证失败 — {len(errors)} 个错误:", file=sys.stderr)
        for e in errors[:20]:
            print(f"  ✗ {e}", file=sys.stderr)
        if len(errors) > 20:
            print(f"  ... 及其他 {len(errors) - 20} 个错误", file=sys.stderr)
        return False, errors
    else:
        xml_count = sum(1 for _, _, files in os.walk(dir_path)
                        for f in files if f.endswith(".xml"))
        print(f"验证通过 — {xml_count} 个 XML 文件均良构")
        return True, []


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("用法: python validate.py <directory> [--original <file>]", file=sys.stderr)
        sys.exit(1)

    dir_path = sys.argv[1]
    original = None
    if "--original" in sys.argv:
        idx = sys.argv.index("--original")
        if idx + 1 < len(sys.argv):
            original = sys.argv[idx + 1]

    ok, _ = validate(dir_path, original)
    sys.exit(0 if ok else 1)
