#!/usr/bin/env python3
"""WebSQL Desktop Build Script
使用 Wails v3 构建原生桌面应用，产出独立可发行 zip 包。
必须在目标平台上运行(Wails 用 CGO,无法交叉编译)。

前置条件:
  - Go 1.21+
  - Node.js 18+
  - Wails v3 CLI: go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  - Windows: WebView2 Runtime(Win11 自带)
  - macOS: Xcode Command Line Tools
  - Linux: libgtk-3-dev libwebkit2gtk-4.1-dev

用法:
  python scripts/build_desktop.py                          # 当前平台完整构建
  python scripts/build_desktop.py --skip-frontend          # 跳过前端构建
  python scripts/build_desktop.py --package                # 调用 wails3 build 完成构建与打包
  python scripts/build_desktop.py --check                  # 仅检查环境

产物:
  dist-pack/websql-desktop-{platform}.zip  — 可独立发行、运行的 zip 包
  包内容: 单个可执行文件（前端 + 配置 + 迁移脚本均通过 go:embed 嵌入）

注意:
  桌面版使用 CGO（Wails 依赖），必须在目标平台上构建，无法交叉编译。
  脚本会自动检测当前平台作为默认值，但仍允许手动指定（用于 CI 等场景）。
"""
from datetime import datetime
import argparse
import os
import shutil
import subprocess
import sys
import zipfile

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DIST_PACK_DIR = os.path.join(PROJECT_ROOT, "dist-pack")
WAILS3_CLI = "wails3"
DESKTOP_DIR = os.path.join(PROJECT_ROOT, "cmd", "desktop")
EMBED_STATIC_DIR = os.path.join(DESKTOP_DIR, "static")
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")
MIGRATIONS_DIR = os.path.join(PROJECT_ROOT, "migrations")

# 平台配置：key 用于 --platform 参数和 zip 命名
DESKTOP_PLATFORMS = {
    "windows-amd64": {"goos": "windows", "goarch": "amd64", "ext": ".exe",
                      "ldflags_extra": ["-H=windowsgui"], "syso": True},
    "macos-amd64":   {"goos": "darwin",  "goarch": "amd64", "ext": "",
                      "ldflags_extra": [], "syso": False},
    "macos-arm64":   {"goos": "darwin",  "goarch": "arm64", "ext": "",
                      "ldflags_extra": [], "syso": False},
    "linux-amd64":   {"goos": "linux",   "goarch": "amd64", "ext": "",
                      "ldflags_extra": [], "syso": False},
}

# 当前运行平台自动检测
def _detect_macos_arch():
    try:
        return "macos-arm64" if os.uname().machine == "arm64" else "macos-amd64"
    except AttributeError:
        return "macos-arm64"


CURRENT_PLATFORM_MAP = {
    "win32": "windows-amd64",
    "darwin": _detect_macos_arch(),
    "linux": "linux-amd64",
}


def run(cmd, cwd=None):
    print(f"> {cmd}")
    result = subprocess.run(cmd, shell=True, cwd=cwd)
    if result.returncode != 0:
        print(f"[FAIL] 命令失败 (exit={result.returncode}): {cmd}")
        sys.exit(1)


def check_command(cmd):
    try:
        subprocess.run(cmd, shell=True, check=True, capture_output=True)
        return True
    except (subprocess.CalledProcessError, FileNotFoundError):
        return False


def check_env():
    print("[1/3] 检查 Go...")
    if not check_command("go version"):
        print("[FAIL] 未检测到 Go,请安装 Go 1.21+")
        sys.exit(1)
    subprocess.run("go version", shell=True)

    print("[2/3] 检查 Node.js...")
    if not check_command("node --version"):
        print("[FAIL] 未检测到 Node.js,请安装 Node.js 18+")
        sys.exit(1)
    subprocess.run("node --version", shell=True)

    print("[3/3] 检查 Wails v3 CLI...")
    if not check_command(f"{WAILS3_CLI} version"):
        print(f"[FAIL] 未检测到 {WAILS3_CLI} CLI,请先安装:")
        print("    go install github.com/wailsapp/wails/v3/cmd/wails3@latest")
        sys.exit(1)
    subprocess.run(f"{WAILS3_CLI} version", shell=True)

    print("[OK] 环境检查通过")


def detect_current_platform():
    return CURRENT_PLATFORM_MAP.get(sys.platform, "unknown")


def build_frontend():
    print("\n[Build] 构建前端...")
    if not os.path.isdir(os.path.join(WEB_SRC_DIR, "node_modules")):
        print("  安装 npm 依赖...")
        run("npm install", cwd=WEB_SRC_DIR)
    run("npm run build", cwd=WEB_SRC_DIR)
    if not os.path.isdir(DIST_DIR):
        print(f"[FAIL] 前端构建失败,未找到 {DIST_DIR}")
        sys.exit(1)
    print("[OK] 前端构建完成")


def copy_frontend_to_embed():
    print("\n[Build] 复制前端产物到嵌入目录...")
    if not os.path.isdir(DIST_DIR):
        print(f"[FAIL] 前端产物目录不存在: {DIST_DIR}")
        sys.exit(1)
    if os.path.isdir(EMBED_STATIC_DIR):
        for entry in os.listdir(EMBED_STATIC_DIR):
            if entry == ".gitkeep":
                continue
            path = os.path.join(EMBED_STATIC_DIR, entry)
            if os.path.isdir(path):
                shutil.rmtree(path, ignore_errors=True)
            else:
                os.remove(path)
    os.makedirs(EMBED_STATIC_DIR, exist_ok=True)
    for entry in os.listdir(DIST_DIR):
        src = os.path.join(DIST_DIR, entry)
        dst = os.path.join(EMBED_STATIC_DIR, entry)
        if os.path.isdir(src):
            shutil.copytree(src, dst, dirs_exist_ok=True)
        else:
            shutil.copy2(src, dst)
    print(f"[OK] 前端产物已复制到 {EMBED_STATIC_DIR}")


def copy_syso_to_desktop():
    src_syso = os.path.join(PROJECT_ROOT, "wails.exe.syso")
    if os.path.isfile(src_syso):
        shutil.copy2(src_syso, os.path.join(DESKTOP_DIR, "wails.exe.syso"))
        print("[OK] 已复制 wails.exe.syso 到 cmd/desktop/")
    else:
        print("[WARN] 未找到 wails.exe.syso，可执行文件将无 Windows 图标")


def copy_migrations_to_desktop():
    """将迁移脚本复制到 cmd/desktop/ 供 //go:embed 嵌入。"""
    # 增量迁移脚本
    src_dir = os.path.join(MIGRATIONS_DIR, "sqlite")
    dst_dir = os.path.join(DESKTOP_DIR, "migrations", "sqlite")
    if not os.path.isdir(src_dir):
        print(f"[FAIL] 未找到迁移脚本目录: {src_dir}")
        sys.exit(1)
    os.makedirs(dst_dir, exist_ok=True)
    for f in os.listdir(dst_dir):
        os.remove(os.path.join(dst_dir, f))
    for f in os.listdir(src_dir):
        if f.endswith(".sql"):
            shutil.copy2(os.path.join(src_dir, f), os.path.join(dst_dir, f))
    print(f"[OK] 已复制增量迁移脚本到 {dst_dir}")

    # 全量初始化脚本
    full_src_dir = os.path.join(MIGRATIONS_DIR, "full")
    full_dst_dir = os.path.join(DESKTOP_DIR, "migrations", "full")
    if not os.path.isdir(full_src_dir):
        print(f"[FAIL] 未找到全量脚本目录: {full_src_dir}")
        sys.exit(1)
    os.makedirs(full_dst_dir, exist_ok=True)
    for f in os.listdir(full_dst_dir):
        os.remove(os.path.join(full_dst_dir, f))
    for f in os.listdir(full_src_dir):
        if f.endswith(".sql"):
            shutil.copy2(os.path.join(full_src_dir, f), os.path.join(full_dst_dir, f))
    print(f"[OK] 已复制全量初始化脚本到 {full_dst_dir}")


def build_go(platform_key):
    cfg = DESKTOP_PLATFORMS[platform_key]
    ext = cfg["ext"]
    goos, goarch = cfg["goos"], cfg["goarch"]

    print(f"\n[Build] 构建 Go 桌面版二进制 ({goos}/{goarch})...")
    bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
    os.makedirs(bin_dir, exist_ok=True)
    output = os.path.join(bin_dir, f"WebSQL{ext}")
    version = datetime.now().strftime("%Y%m%d%H%M%S")
    ldflags_parts = [f"-X internal/version.Version={version}"]
    ldflags_parts.extend(cfg["ldflags_extra"])
    ldflags = " ".join(ldflags_parts)

    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "1"}
    cmd = f'go build -tags=desktop -o "{output}" -ldflags "{ldflags}" ./cmd/desktop/'
    print(f"> {cmd}")
    result = subprocess.run(cmd, shell=True, cwd=PROJECT_ROOT, env=env)
    if result.returncode != 0:
        print(f"[FAIL] 构建 {platform_key} 失败 (Wails 需要 CGO，必须在目标平台运行)")
        sys.exit(1)
    print(f"[Build] 二进制产物: {output} (version={version})")
    return output


def create_release_zip(binary_path, platform_key):
    """将桌面版二进制打包为独立可发行 zip 包。"""
    cfg = DESKTOP_PLATFORMS[platform_key]
    ext = cfg["ext"]
    zip_name = f"websql-desktop-{platform_key}.zip"
    zip_path = os.path.join(DIST_PACK_DIR, zip_name)

    os.makedirs(DIST_PACK_DIR, exist_ok=True)

    with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as zipf:
        zipf.write(binary_path, f"WebSQL{ext}")

    zip_size = os.path.getsize(zip_path)
    print(f"[OK] {zip_name} ({zip_size / 1024 / 1024:.2f} MB)")
    return zip_path


def build_platform(platform_key, skip_frontend, package):
    cfg = DESKTOP_PLATFORMS[platform_key]

    if not skip_frontend:
        build_frontend()
        copy_frontend_to_embed()
    else:
        print("[Build] 跳过前端构建")
        if not os.path.isdir(EMBED_STATIC_DIR) or \
                len([f for f in os.listdir(EMBED_STATIC_DIR) if f != ".gitkeep"]) == 0:
            print("[FAIL] 跳过前端构建但嵌入目录为空,请先运行完整构建")
            sys.exit(1)

    # Windows 需要 syso 图标资源
    if cfg["syso"]:
        copy_syso_to_desktop()

    copy_migrations_to_desktop()

    if package:
        print("\n[Build] 调用 wails3 build 完成完整构建与打包...")
        run(f"{WAILS3_CLI} build", cwd=PROJECT_ROOT)
        bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
        if os.path.isdir(bin_dir):
            for name in os.listdir(bin_dir):
                if os.path.isfile(os.path.join(bin_dir, name)) and not name.endswith(".zip"):
                    create_release_zip(os.path.join(bin_dir, name), platform_key)
                    break
    else:
        binary_path = build_go(platform_key)
        print("\n[Build] 打包 zip ...")
        create_release_zip(binary_path, platform_key)


def main():
    parser = argparse.ArgumentParser(description="WebSQL Desktop Build Script (Wails v3)")
    parser.add_argument("--skip-frontend", action="store_true",
                        help="跳过前端构建,仅编译 Go 二进制")
    parser.add_argument("--package", action="store_true",
                        help="调用 wails3 build 完成完整构建(含打包)")
    parser.add_argument("--check", action="store_true",
                        help="仅检查环境,不构建")
    args = parser.parse_args()

    if args.check:
        check_env()
        return

    platform = detect_current_platform()
    if platform not in DESKTOP_PLATFORMS:
        print(f"[FAIL] 不支持的平台: {platform}")
        sys.exit(1)

    print()
    print("=" * 55)
    print("  WebSQL Desktop Build")
    print(f"  目标平台: {platform}")
    print(f"  产物目录: {DIST_PACK_DIR}")
    print("=" * 55)

    check_env()

    build_platform(platform, args.skip_frontend, args.package)

    print(f"\n{'=' * 55}")
    print("  桌面版构建完成!")
    print(f"  产物目录: {DIST_PACK_DIR}")
    if os.path.isdir(DIST_PACK_DIR):
        for name in sorted(os.listdir(DIST_PACK_DIR)):
            if name.startswith("websql-desktop-"):
                size_mb = os.path.getsize(os.path.join(DIST_PACK_DIR, name)) / 1024 / 1024
                print(f"  - {name} ({size_mb:.2f} MB)")
    print(f"{'=' * 55}")


if __name__ == "__main__":
    main()
