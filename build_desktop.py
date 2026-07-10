#!/usr/bin/env python3
"""WebSQL Desktop Build Script
使用 Wails v3 构建原生桌面应用。
必须在目标平台上运行(Wails 用 CGO,无法交叉编译)。

前置条件:
  - Go 1.21+
  - Node.js 18+
  - Wails v3 CLI: go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  - Windows: WebView2 Runtime(Win11 自带)
  - macOS: Xcode Command Line Tools
  - Linux: libgtk-3-dev libwebkit2gtk-4.1-dev

用法:
  python build_desktop.py                # 完整构建(前端 + Go)
  python build_desktop.py --skip-frontend  # 跳过前端构建(仅 Go 编译)
  python build_desktop.py --package        # 构建并打包安装包
  python build_desktop.py --check         # 仅检查环境

产物形态说明:
  Wails v3 沿用 Go 静态编译传统,产出单个可执行文件。前端资源与 config.json 通过
  //go:embed 打入二进制,无需额外磁盘文件。WebView2 运行时是唯一外部依赖
  (Win11 自带)。如需重新生成 exe 图标资源(已在仓库内的 wails.exe.syso),运行:
    wails3 generate syso -icon build/windows/icon.ico \\
      -manifest build/windows/wails.exe.manifest \\
      -info build/windows/info.json -out wails.exe.syso -arch amd64

注意:
  Wails v3 与 v2 的 CLI 参数差异较大:
  - v3 build 不再有 -clean / -nsis / -skipbindings 参数
  - v3 通过 wails.json 的 go.buildtags 自动应用构建标签
  - v3 的 NSIS/DMG 打包通过 `wails3 package` 或 Taskfile 实现
"""
from datetime import datetime
import argparse
import os
import shutil
import subprocess
import sys

PROJECT_ROOT = os.path.dirname(os.path.abspath(__file__))
WAILS3_CLI = "wails3"
DESKTOP_DIR = os.path.join(PROJECT_ROOT, "cmd", "desktop")
EMBED_STATIC_DIR = os.path.join(DESKTOP_DIR, "static")
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")


def run(cmd, cwd=None):
    print(f"> {cmd}")
    result = subprocess.run(cmd, shell=True, cwd=cwd)
    if result.returncode != 0:
        print(f"[FAIL] 命令失败 (exit={result.returncode}): {cmd}")
        sys.exit(1)


def check_command(cmd):
    try:
        subprocess.run(cmd, shell=True, check=True,
                       capture_output=True)
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
    """将前端构建产物复制到 cmd/desktop/static/ 供 //go:embed 嵌入。"""
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
    """将 Windows 图标资源文件复制到 cmd/desktop/，Go 编译时自动嵌入可执行文件。"""
    src_syso = os.path.join(PROJECT_ROOT, "wails.exe.syso")
    if os.path.isfile(src_syso):
        shutil.copy2(src_syso, os.path.join(DESKTOP_DIR, "wails.exe.syso"))
        print("[OK] 已复制 wails.exe.syso 到 cmd/desktop/")
    else:
        print("[WARN] 未找到 wails.exe.syso，可执行文件将无 Windows 图标")


def copy_migrations_to_desktop():
    """将 migrations/sqlite/ 和 migrations/full/ 复制到 cmd/desktop/ 供 //go:embed 嵌入。"""
    # 复制增量迁移脚本
    src_dir = os.path.join(PROJECT_ROOT, "migrations", "sqlite")
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

    # 复制全量初始化脚本
    full_src_dir = os.path.join(PROJECT_ROOT, "migrations", "full")
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


def build_go():
    print("\n[Build] 构建 Go 桌面版二进制...")
    bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
    os.makedirs(bin_dir, exist_ok=True)
    ext = ".exe" if sys.platform == "win32" else ""
    output = os.path.join(bin_dir, f"WebSQL{ext}")
    version = datetime.now().strftime("%Y%m%d%H%M%S")
    ldflags_parts = [f"-X internal/version.Version={version}"]
    if sys.platform == "win32":
        ldflags_parts.append("-H=windowsgui")
    ldflags = " ".join(ldflags_parts)
    cmd = f"go build -tags=desktop -o \"{output}\" -ldflags \"{ldflags}\" ./cmd/desktop/"
    run(cmd, cwd=PROJECT_ROOT)
    print(f"[Build] 二进制产物: {output} (version={version})")


def build(skip_frontend, package):
    check_env()

    if not skip_frontend:
        build_frontend()
        copy_frontend_to_embed()
    else:
        print("[Build] 跳过前端构建")
        if not os.path.isdir(EMBED_STATIC_DIR) or \
                len([f for f in os.listdir(EMBED_STATIC_DIR) if f != ".gitkeep"]) == 0:
            print("[FAIL] 跳过前端构建但嵌入目录为空,请先运行完整构建")
            sys.exit(1)

    copy_syso_to_desktop()
    copy_migrations_to_desktop()

    if package:
        print("\n[Build] 调用 wails3 build 完成完整构建与打包...")
        run(f"{WAILS3_CLI} build", cwd=PROJECT_ROOT)
    else:
        build_go()

    bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
    print(f"\n[DONE] 桌面版构建完成,产物在 {bin_dir}")
    if os.path.isdir(bin_dir):
        for name in os.listdir(bin_dir):
            print(f"  - {name}")


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

    build(args.skip_frontend, args.package)


if __name__ == "__main__":
    main()
