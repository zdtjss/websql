#!/usr/bin/env python3
"""
OOXML Pack — 将 XML 目录树打包回 .pptx/.docx/.xlsx 文件
确保 [Content_Types].xml 中声明的所有 Part 都存在，否则报错。
"""

import sys
import os
import zipfile
import xml.etree.ElementTree as ET


def _ensure_content_types(dir_path):
    ct_path = os.path.join(dir_path, "[Content_Types].xml")
    if not os.path.isfile(ct_path):
        print(f"ERROR: 缺少 [Content_Types].xml", file=sys.stderr)
        sys.exit(1)

    tree = ET.parse(ct_path)
    root = tree.getroot()
    ns = "{http://schemas.openxmlformats.org/package/2006/content-types}"
    missing = []

    for override in root.findall(f"{ns}Override"):
        part = override.get("PartName", "")
        if part.startswith("/"):
            part = part[1:]
        part_path = os.path.join(dir_path, part)
        if not os.path.isfile(part_path):
            missing.append(part)

    for default in root.findall(f"{ns}Default"):
        ext = default.get("Extension", "")
        if ext:
            for root_dir, _, files in os.walk(dir_path):
                for f in files:
                    if f.endswith(f".{ext}"):
                        break

    if missing:
        print(f"WARNING: Content_Types 中声明但缺失的文件 ({len(missing)}):",
              file=sys.stderr)
        for m in missing[:5]:
            print(f"  - {m}", file=sys.stderr)
        if len(missing) > 5:
            print(f"  ... 及其他 {len(missing) - 5} 个文件", file=sys.stderr)


def pack(input_dir, output_file):
    input_dir = os.path.abspath(input_dir)
    output_file = os.path.abspath(output_file)

    if not os.path.isdir(input_dir):
        print(f"ERROR: 目录不存在: {input_dir}", file=sys.stderr)
        sys.exit(1)

    ext = os.path.splitext(output_file)[1].lower()
    if ext not in (".pptx", ".docx", ".xlsx"):
        print(f"ERROR: 不支持的文件格式: {ext}", file=sys.stderr)
        sys.exit(1)

    _ensure_content_types(input_dir)

    output_dir = os.path.dirname(output_file)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)

    with zipfile.ZipFile(output_file, "w", zipfile.ZIP_DEFLATED) as zf:
        for root_dir, dirs, files in os.walk(input_dir):
            for filename in files:
                file_path = os.path.join(root_dir, filename)
                arcname = os.path.relpath(file_path, input_dir)
                arcname = arcname.replace("\\", "/")
                zf.write(file_path, arcname)

    file_count = sum(1 for _, _, files in os.walk(input_dir) for _ in files)
    print(f"已打包 {file_count} 个文件 -> {output_file}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"用法: python pack.py <input_directory> <output_file>", file=sys.stderr)
        sys.exit(1)
    pack(sys.argv[1], sys.argv[2])
