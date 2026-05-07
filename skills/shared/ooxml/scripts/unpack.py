#!/usr/bin/env python3
"""
OOXML Unpack — 将 .pptx/.docx/.xlsx 文件拆解为原始 XML 目录树
支持 Office Open XML 格式的完整解包，保留 [Content_Types].xml 和 _rels 结构。
"""

import sys
import os
import zipfile
import shutil


def unpack(office_file, output_dir):
    office_file = os.path.abspath(office_file)
    output_dir = os.path.abspath(output_dir)

    if not os.path.isfile(office_file):
        print(f"ERROR: 文件不存在: {office_file}", file=sys.stderr)
        sys.exit(1)

    ext = os.path.splitext(office_file)[1].lower()
    if ext not in (".pptx", ".docx", ".xlsx"):
        print(f"ERROR: 不支持的文件格式: {ext}", file=sys.stderr)
        sys.exit(1)

    if os.path.exists(output_dir):
        shutil.rmtree(output_dir)

    os.makedirs(output_dir, exist_ok=True)

    with zipfile.ZipFile(office_file, "r") as zf:
        zf.extractall(output_dir)

    file_count = sum(1 for _, _, files in os.walk(output_dir) for _ in files)
    print(f"已解包 {file_count} 个文件到: {output_dir}")

    if ext == ".docx":
        import uuid
        rsid = uuid.uuid4().hex[:8].upper()
        print(f"建议 RSID: {rsid} (用于修订追踪)")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"用法: python unpack.py <office_file> <output_directory>", file=sys.stderr)
        sys.exit(1)
    unpack(sys.argv[1], sys.argv[2])
