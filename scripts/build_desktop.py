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
  - Python 3.8+（打包脚本自身；Windows 捆绑运行时默认取自本机 Python）

用法:
  python scripts/build_desktop.py                          # 当前平台完整构建
  python scripts/build_desktop.py --skip-frontend          # 跳过前端构建
  python scripts/build_desktop.py --package                # 调用 wails3 build 完成构建与打包
  python scripts/build_desktop.py --check                  # 仅检查环境
  python scripts/build_desktop.py --skip-python            # 不捆绑 Python 运行时
  python scripts/build_desktop.py --rebuild-python         # 强制重新收集捆绑 Python

产物:
  dist-pack/websql-desktop-{platform}.zip  — 可独立发行、运行的 zip 包
  包内容: WebSQL 可执行文件 + skills/（skill 脚本与模板）+ python/（捆绑 Python 运行时）
  捆绑 Python 使桌面版 Skill 导出（Word/PPT 模板驱动渲染）开箱即用；
  即使无 Python，导出工具也会自动降级 Go 基础版，保证导出不失败。

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
SKILLS_DIR = os.path.join(PROJECT_ROOT, "skills")

# 捆绑 Python 运行时暂存目录（zip 内为 python/，与可执行文件同级）
BUNDLE_PYTHON_DIR = os.path.join(DIST_PACK_DIR, "python")

# 本地官方 embeddable Python 包（scripts/ 下，随仓库分发，无需联网下载）
EMBEDDABLE_ZIP = os.path.join(PROJECT_ROOT, "scripts", "python-3.14.7-embed-amd64.zip")
EMBEDDABLE_PY_VERSION = "3.14"
EMBEDDABLE_ABI = "cp314"
EMBEDDABLE_PLATFORM = "win_amd64"

# 复制本机 Python 时排除的大而无用的目录（保留标准库与 site-packages）
PYTHON_EXCLUDE_DIRS = {
    "__pycache__", "test", "tests", "tkinter", "idlelib", "turtledemo",
    "lib2to3", "ensurepip", "venv", "include", "share", "Doc", "Tools",
    "tcl", "__pypackages__",
}

# 用户 site-packages 中排除的无关包（pywin32 等，导出功能不需要）
USER_SITE_EXCLUDE = {
    "win32", "win32com", "win32comext", "Pythonwin", "pywin32_system32",
    "adodbapi", "isapi", "pythonwin", "servicemanager", "win32ctypes",
}

# 导出渲染所需的 Python 依赖（捆绑后验证用）
BUNDLE_REQUIRED_MODULES = ["pptx", "docx", "docxtpl", "matplotlib", "numpy", "PIL"]

# pip 安装的依赖清单（精确版本，与 skills/*/requirements.txt 保持一致）
BUNDLE_PIP_DEPS = [
    "python-pptx==1.0.2", "python-docx==1.2.0", "docxtpl==0.20.2",
    "matplotlib==3.11.1", "numpy==2.5.2", "Pillow==12.3.0",
]

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


def generate_agents_md_embed():
    """执行 go generate 将 AGENTS.md 复制到 agent 包供 go:embed 嵌入。"""
    print("\n[Build] 生成 AGENTS.md 嵌入文件...")
    agents_md_src = os.path.join(PROJECT_ROOT, "AGENTS.md")
    agents_md_dst = os.path.join(PROJECT_ROOT, "internal", "ai", "agent", "_embedded_agents.md")
    if not os.path.isfile(agents_md_src):
        print(f"  [WARN] 未找到 {agents_md_src}，跳过")
        return
    shutil.copy2(agents_md_src, agents_md_dst)
    print(f"  [OK] AGENTS.md → _embedded_agents.md ({os.path.getsize(agents_md_dst)} 字节)")


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


# ═══════════════════════════════════════════════════════════════
# 捆绑 Python 运行时（桌面版 Skill 导出开箱即用）
# ═══════════════════════════════════════════════════════════════

def _copy_tree_filtered(src, dst, exclude_dirs):
    """复制目录树，跳过 exclude_dirs 与 __pycache__/*.pyc。"""
    for root, dirs, files in os.walk(src):
        dirs[:] = [d for d in dirs
                   if d not in exclude_dirs and d != "__pycache__"]
        rel_root = os.path.relpath(root, src)
        target = dst if rel_root == "." else os.path.join(dst, rel_root)
        os.makedirs(target, exist_ok=True)
        for f in files:
            if f.endswith(".pyc"):
                continue
            shutil.copy2(os.path.join(root, f), os.path.join(target, f))


def find_local_python_root():
    """定位本机 Python 安装根目录（从当前解释器推导）。"""
    prefix = sys.prefix
    if os.path.isfile(os.path.join(prefix, "python.exe")):
        return prefix
    return None


def find_user_site_packages():
    """定位用户级 site-packages（pip install --user 的包）。"""
    import site
    p = site.getusersitepackages()
    if p and os.path.isdir(p):
        return p
    return None


def collect_python_runtime(rebuild=False):
    """收集捆绑 Python 运行时到 dist-pack/python/。

    主路径：本地官方 embeddable 包（scripts/python-3.14.7-embed-amd64.zip）
      - 解压 + 启用 site（修改 ._pth）+ pip 交叉安装 cp314 依赖闭包
      - 版本固定 3.14.7，产物可复现；embeddable zip 随仓库分发，离线可用
    回退路径：本机 Python 精简复制（pip 交叉安装失败/无 embeddable 包时）
    已存在且非强制重建时直接复用缓存。
    """
    if os.path.isdir(BUNDLE_PYTHON_DIR) and not rebuild:
        print(f"[Python] 复用已有捆绑运行时: {BUNDLE_PYTHON_DIR}")
        return True

    if os.path.isdir(BUNDLE_PYTHON_DIR):
        shutil.rmtree(BUNDLE_PYTHON_DIR, ignore_errors=True)

    # 主路径：本地 embeddable 包
    if os.path.isfile(EMBEDDABLE_ZIP):
        if _build_from_embeddable():
            return True
        print("[WARN] embeddable 构建失败，回退本机 Python 复制（版本跟随开发机）")
        if os.path.isdir(BUNDLE_PYTHON_DIR):
            shutil.rmtree(BUNDLE_PYTHON_DIR, ignore_errors=True)
    else:
        print(f"[WARN] 未找到 embeddable 包: {EMBEDDABLE_ZIP}，回退本机 Python 复制")

    # 回退路径：本机 Python 精简复制
    return _build_from_local()


def _build_from_embeddable():
    """从本地 embeddable zip 构建捆绑运行时（解压 + _pth + 交叉安装依赖）。"""
    print(f"\n[Python] 从 embeddable 包构建捆绑运行时 ({EMBEDDABLE_ZIP})...")

    # 1. 解压 embeddable
    with zipfile.ZipFile(EMBEDDABLE_ZIP) as z:
        z.extractall(BUNDLE_PYTHON_DIR)
    if not os.path.isfile(os.path.join(BUNDLE_PYTHON_DIR, "python.exe")):
        print("[FAIL] embeddable 包缺少 python.exe")
        return False

    # 2. 启用 site：embeddable 默认注释 import site，导致 site-packages 不加载
    import glob
    pth_files = glob.glob(os.path.join(BUNDLE_PYTHON_DIR, "python3*._pth"))
    if not pth_files:
        print("[FAIL] embeddable 包缺少 ._pth 文件")
        return False
    pth = pth_files[0]
    with open(pth, "r", encoding="utf-8") as f:
        content = f.read()
    # 注意：原始内容为 "#import site"（注释状态），"import site" 是其子串，
    # 必须匹配带 # 的形式才能正确启用
    if "#import site" in content:
        content = content.replace("#import site", "import site")
        with open(pth, "w", encoding="utf-8") as f:
            f.write(content)

    # 3. pip 交叉安装依赖（embeddable 无 pip，从打包机以 cp314 wheel 安装）
    dst_sp = os.path.join(BUNDLE_PYTHON_DIR, "Lib", "site-packages")
    os.makedirs(dst_sp, exist_ok=True)
    cross = (EMBEDDABLE_PLATFORM, EMBEDDABLE_PY_VERSION, EMBEDDABLE_ABI)
    if not _install_deps_via_pip(dst_sp, cross=cross):
        return False

    # 4. 验证
    return _verify_bundled_python()


def _build_from_local():
    """回退路径：本机 Python 精简复制 + 合并本机 site-packages。"""
    src = find_local_python_root()
    if not src:
        print("[FAIL] 未找到本机 Python 安装目录，无法捆绑运行时")
        return False

    print(f"\n[Python] 收集捆绑运行时 (回退路径，来源: {src})...")
    exclude = set(PYTHON_EXCLUDE_DIRS)
    exclude.add("site-packages")
    _copy_tree_filtered(src, BUNDLE_PYTHON_DIR, exclude)

    dst_sp = os.path.join(BUNDLE_PYTHON_DIR, "Lib", "site-packages")
    os.makedirs(dst_sp, exist_ok=True)
    if not _install_deps_via_pip(dst_sp):
        print("[WARN] pip 安装依赖失败，回退复制本机 site-packages（体积较大）")
        _copy_tree_filtered(os.path.join(src, "Lib", "site-packages"),
                            dst_sp, {"pip", "setuptools", "pkg_resources"})
        user_sp = find_user_site_packages()
        if user_sp:
            _copy_tree_filtered(user_sp, dst_sp, USER_SITE_EXCLUDE)
    return _verify_bundled_python()


def _verify_bundled_python():
    """验证捆绑运行时可用性（python.exe + 全部导出依赖 import）。"""
    exe = os.path.join(BUNDLE_PYTHON_DIR, "python.exe")
    if not os.path.isfile(exe):
        print(f"[FAIL] 捆绑运行时缺少 python.exe: {exe}")
        return False
    try:
        out = subprocess.run(
            [exe, "-c", "import sys; print(sys.version.split()[0])"],
            capture_output=True, text=True, timeout=120)
        version = out.stdout.strip()
        missing = []
        for mod in BUNDLE_REQUIRED_MODULES:
            r = subprocess.run([exe, "-c", f"import {mod}"],
                               capture_output=True, timeout=120)
            if r.returncode != 0:
                missing.append(mod)
        if missing:
            print(f"[WARN] 捆绑运行时缺少依赖 {missing}，将不打包 python/")
            shutil.rmtree(BUNDLE_PYTHON_DIR, ignore_errors=True)
            return False
        size_mb = _dir_size_mb(BUNDLE_PYTHON_DIR)
        print(f"[OK] 捆绑运行时可用 (Python {version}, {size_mb:.1f} MB, 依赖完整)")
        return True
    except Exception as e:
        print(f"[FAIL] 捆绑运行时验证失败: {e}")
        shutil.rmtree(BUNDLE_PYTHON_DIR, ignore_errors=True)
        return False


def _install_deps_via_pip(dst_sp, cross=None):
    """用 pip 安装导出渲染依赖闭包到指定目录。

    cross: (platform, python_version, abi) 元组——embeddable 无 pip 且目标
    解释器与打包机不同（如 cp314 vs cp313），用 pip 交叉安装参数拉取
    目标 ABI 的 wheel；None 表示按当前解释器安装。
    """
    print(f"[Python] pip 安装依赖: {' '.join(BUNDLE_PIP_DEPS)}")
    cmd = [sys.executable, "-m", "pip", "install", "--target", dst_sp,
           "--no-compile", "--disable-pip-version-check"]
    if cross:
        platform, pyver, abi = cross
        cmd += ["--platform", platform, "--python-version", pyver,
                "--implementation", "cp", "--abi", abi,
                "--only-binary=:all:"]
    cmd += BUNDLE_PIP_DEPS
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
        if r.returncode != 0:
            print(f"  pip 输出尾部: {r.stderr[-800:]}")
            return False
        return True
    except Exception as e:
        print(f"  pip 执行失败: {e}")
        return False


def _dir_size_mb(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            try:
                total += os.path.getsize(os.path.join(root, f))
            except OSError:
                pass
    return total / 1024 / 1024


def create_release_zip(binary_path, platform_key, include_python=True):
    """将桌面版二进制 + skills + 捆绑 Python 打包为独立可发行 zip。"""
    cfg = DESKTOP_PLATFORMS[platform_key]
    ext = cfg["ext"]
    zip_name = f"websql-desktop-{platform_key}.zip"
    zip_path = os.path.join(DIST_PACK_DIR, zip_name)

    os.makedirs(DIST_PACK_DIR, exist_ok=True)

    has_python = include_python and os.path.isdir(BUNDLE_PYTHON_DIR)
    has_skills = os.path.isdir(SKILLS_DIR)

    # 大文件（二进制 pyd/dll/png）不压缩，加速打包并避免体积膨胀
    def _compress(full):
        size = os.path.getsize(full)
        return zipfile.ZIP_STORED if size > 1 * 1024 * 1024 else zipfile.ZIP_DEFLATED

    with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as zipf:
        zipf.write(binary_path, f"WebSQL{ext}")

        # skills 目录（skill 脚本 + 模板，Skill 导出依赖）
        if has_skills:
            for root, dirs, files in os.walk(SKILLS_DIR):
                dirs[:] = [d for d in dirs if d != "__pycache__"]
                for f in files:
                    full = os.path.join(root, f)
                    rel = os.path.relpath(full, PROJECT_ROOT).replace(os.sep, "/")
                    zipf.write(full, rel, compress_type=_compress(full))

        # 捆绑 Python 运行时（exe 同级 python/，detectPython 自动发现）
        if has_python:
            for root, dirs, files in os.walk(BUNDLE_PYTHON_DIR):
                for f in files:
                    full = os.path.join(root, f)
                    rel = "python/" + os.path.relpath(full, BUNDLE_PYTHON_DIR).replace(os.sep, "/")
                    zipf.write(full, rel, compress_type=_compress(full))

    zip_size = os.path.getsize(zip_path)
    parts = ["WebSQL 可执行文件"]
    if has_skills:
        parts.append("skills")
    if has_python:
        parts.append("捆绑 Python")
    else:
        parts.append("无捆绑 Python（导出将降级 Go 基础版）")
    print(f"[OK] {zip_name} ({zip_size / 1024 / 1024:.2f} MB, 含: {' + '.join(parts)})")
    return zip_path


def build_platform(platform_key, skip_frontend, package, skip_python=False, rebuild_python=False):
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

    # 生成 AGENTS.md 嵌入文件（确保 go:embed 能找到 _embedded_agents.md）
    generate_agents_md_embed()

    # 收集捆绑 Python 运行时（失败不阻塞打包：Go 基础版兜底保证导出不失败）
    include_python = True
    if skip_python:
        print("[Python] 已跳过捆绑（--skip-python），导出将使用系统 Python 或降级 Go 基础版")
        include_python = False
    else:
        if not collect_python_runtime(rebuild=rebuild_python):
            print("[WARN] 捆绑 Python 不可用，zip 将不含 python/（导出自动降级 Go 基础版）")
            include_python = False

    if package:
        print("\n[Build] 调用 wails3 build 完成完整构建与打包...")
        run(f"{WAILS3_CLI} build", cwd=PROJECT_ROOT)
        bin_dir = os.path.join(PROJECT_ROOT, "build", "bin")
        if os.path.isdir(bin_dir):
            for name in os.listdir(bin_dir):
                if os.path.isfile(os.path.join(bin_dir, name)) and not name.endswith(".zip"):
                    create_release_zip(os.path.join(bin_dir, name), platform_key,
                                       include_python=include_python)
                    break
    else:
        binary_path = build_go(platform_key)
        print("\n[Build] 打包 zip ...")
        create_release_zip(binary_path, platform_key, include_python=include_python)


def main():
    parser = argparse.ArgumentParser(description="WebSQL Desktop Build Script (Wails v3)")
    parser.add_argument("--skip-frontend", action="store_true",
                        help="跳过前端构建,仅编译 Go 二进制")
    parser.add_argument("--package", action="store_true",
                        help="调用 wails3 build 完成完整构建(含打包)")
    parser.add_argument("--check", action="store_true",
                        help="仅检查环境,不构建")
    parser.add_argument("--skip-python", action="store_true",
                        help="不捆绑 Python 运行时（导出将使用系统 Python 或降级 Go 基础版）")
    parser.add_argument("--rebuild-python", action="store_true",
                        help="强制重新收集捆绑 Python 运行时（默认复用 dist-pack/python 缓存）")
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

    build_platform(platform, args.skip_frontend, args.package,
                   skip_python=args.skip_python,
                   rebuild_python=args.rebuild_python)

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
