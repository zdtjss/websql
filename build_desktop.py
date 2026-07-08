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
  Wails v3 沿用 Go 静态编译传统,产出单个可执行文件,前端资源通过
  //go:embed 打入二进制。这是 Wails v3 最佳实践,不采用多 DLL 动态依赖库
  形态(那样会引入 DLL 地狱、版本管理复杂等问题)。WebView2 运行时是
  唯一外部依赖(Win11 自带)。如需重新生成 exe 图标资源(已在仓库内的
  wails.exe.syso),运行:
    wails3 generate syso -icon build/windows/icon.ico \\
      -manifest build/windows/wails.exe.manifest \\
      -info build/windows/info.json -out wails.exe.syso -arch amd64

注意:
  Wails v3 与 v2 的 CLI 参数差异较大:
  - v3 build 不再有 -clean / -nsis / -skipbindings 参数
  - v3 通过 wails.json 的 go.buildtags 自动应用构建标签
  - v3 的 NSIS/DMG 打包通过 `wails3 package` 或 Taskfile 实现
"""
import argparse
import os
import subprocess
import sys

PROJECT_ROOT = os.path.dirname(os.path.abspath(__file__))
WAILS3_CLI = "wails3"


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
    run("npm run build:desktop", cwd=os.path.join(PROJECT_ROOT, "web-src"))


def build_go():
    print("\n[Build] 构建 Go 桌面版二进制...")
    bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
    os.makedirs(bin_dir, exist_ok=True)
    ext = ".exe" if sys.platform == "win32" else ""
    output = os.path.join(bin_dir, f"websql{ext}")
    ldflags = "-H=windowsgui" if sys.platform == "win32" else ""
    cmd = f"go build -tags=desktop -o \"{output}\""
    if ldflags:
        cmd += f" -ldflags \"{ldflags}\""
    cmd += " ."
    run(cmd, cwd=PROJECT_ROOT)
    print(f"[Build] 二进制产物: {output}")


def build(skip_frontend, package):
    check_env()

    if not skip_frontend:
        build_frontend()
    else:
        print("[Build] 跳过前端构建")

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
