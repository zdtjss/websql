#!/usr/bin/env python3
"""
PPT Thumbnail Generator — 生成幻灯片缩略图网格
将 PPTX 转换为图像网格，用于快速视觉审查。每行默认 4 列。
基于 Anthropic pptx skill 的 thumbnail.py 思想。
"""

import sys
import os
import subprocess
import tempfile
import shutil

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(_SCRIPT_DIR))

try:
    from PIL import Image
except ImportError:
    print("ERROR: 需要 Pillow 库: pip install Pillow", file=sys.stderr)
    sys.exit(1)


def create_thumbnails(pptx_path, output_prefix="thumbnails", cols=4):
    """两步转换: PPTX -> PDF (LibreOffice) -> JPEG (pdftoppm) -> 网格拼接"""
    pptx_path = os.path.abspath(pptx_path)
    if not os.path.exists(pptx_path):
        print(f"ERROR: 文件不存在: {pptx_path}", file=sys.stderr)
        sys.exit(1)

    tmpdir = tempfile.mkdtemp()

    try:
        pdf_path = os.path.join(tmpdir, "output.pdf")
        result = subprocess.run(
            ["soffice", "--headless", "--convert-to", "pdf", "--outdir", tmpdir, pptx_path],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"WARNING: LibreOffice 转换失败，尝试直接读取: {result.stderr}", file=sys.stderr)
            return

        for f in os.listdir(tmpdir):
            if f.endswith(".pdf"):
                pdf_path = os.path.join(tmpdir, f)
                break

        if not os.path.exists(pdf_path):
            print("ERROR: PDF 生成失败", file=sys.stderr)
            return

    except FileNotFoundError:
        print("WARNING: LibreOffice 未安装，尝试 pip 直接提取文本", file=sys.stderr)
        shutil.rmtree(tmpdir)
        return

    try:
        result = subprocess.run(
            ["pdftoppm", "-jpeg", "-r", "150", pdf_path,
             os.path.join(tmpdir, "slide")],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"WARNING: pdftoppm 转换失败: {result.stderr}", file=sys.stderr)
            return
    except FileNotFoundError:
        print("WARNING: poppler-utils 未安装 (pdftoppm)", file=sys.stderr)
        shutil.rmtree(tmpdir)
        return

    images = sorted([f for f in os.listdir(tmpdir) if f.startswith("slide-") and f.endswith(".jpg")])
    if not images:
        print("WARNING: 没有生成缩略图", file=sys.stderr)
        shutil.rmtree(tmpdir)
        return

    pil_images = []
    for img_file in images:
        img = Image.open(os.path.join(tmpdir, img_file))
        img.thumbnail((400, 300))
        pil_images.append(img)

    rows = (len(pil_images) + cols - 1) // cols
    thumb_w, thumb_h = pil_images[0].size
    grid = Image.new("RGB", (thumb_w * cols, thumb_h * rows), "white")

    for i, img in enumerate(pil_images):
        r, c = i // cols, i % cols
        grid.paste(img, (c * thumb_w, r * thumb_h))

    output_file = f"{output_prefix}.jpg"
    grid.save(output_file, quality=85)
    print(f"缩略图已生成 ({len(pil_images)} 张): {output_file}")

    shutil.rmtree(tmpdir)
    return output_file


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("pptx_file")
    parser.add_argument("output_prefix", nargs="?", default="thumbnails")
    parser.add_argument("--cols", type=int, default=4)
    args = parser.parse_args()
    create_thumbnails(args.pptx_file, args.output_prefix, args.cols)
