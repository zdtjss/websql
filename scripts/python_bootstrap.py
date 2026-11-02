#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""WebSQL Python 运行时自动安装脚本（Web 版「外置自动下载」模式）

职责（一次执行完成全部步骤，无需用户手工操作）：
  1. 检测当前机器可用的 Python（托管目录 / WEBSQL_PYTHON / 运行时清单 / 系统 PATH）
  2. 版本过低或未安装时，自动下载与本项目锁定的 Python 独立运行时（python-build-standalone）
  3. 安装 skills 导出与 docx/pdf 解析所需的第三方依赖
  4. 校验依赖可用，写入 python-runtime.json 与 python-env.*，供应用启动时精确复用

用法:
  python python_bootstrap.py                # 安装/修复（幂等，已就绪时秒级返回）
  python python_bootstrap.py --ensure       # 同默认行为，供 startup 脚本调用
  python python_bootstrap.py --check        # 仅检测并输出状态，不下载、不安装
  python python_bootstrap.py --force        # 删除托管运行时后重新下载安装
  python python_bootstrap.py --offline      # 只使用本机已有资源（离线部署）

可选环境变量:
  WEBSQL_PYTHON              指定解释器绝对路径（应用与脚本共同识别，优先级最高）
  WEBSQL_PYTHON_MIRROR       独立运行时下载镜像基地址（默认 GitHub Releases）
  WEBSQL_PIP_INDEX_URL       pip 索引地址（内网镜像，等价 --index-url）

注意: 本文件是运行时版本、平台三元组与依赖清单的唯一定义处，
      scripts/build_desktop.py（内置模式）与测试直接引用，不要再重复维护。
"""

import argparse
import datetime
import json
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request

# ── 锁定的运行时与依赖（与内置模式共用同一份定义） ──────────────────────
PYTHON_FULL_VERSION = "3.15.2"
STANDALONE_RELEASE = "20261026"
DOWNLOAD_BASE = "https://github.com/astral-sh/python-build-standalone/releases/download"
MIRROR_ENV = "WEBSQL_PYTHON_MIRROR"
INDEX_URL_ENV = "WEBSQL_PIP_INDEX_URL"
PYTHON_ENV_OVERRIDE = "WEBSQL_PYTHON"

MIN_PYTHON_VERSION = (3, 10)
MIN_PYTHON_VERSION_STR = "3.10"

# 与 skills/*/requirements.txt 及 build_desktop.py 的内置模式保持一致
PIP_DEPS = [
    "python-pptx==1.0.2", "python-docx==1.2.0", "docxtpl==0.20.2",
    "matplotlib==3.11.2", "numpy==2.5.2", "Pillow==12.3.0",
    "pdfplumber==0.11.7",
]

# 安装完成后必须能 import 的模块（导入名与包名不同，单独列出）
REQUIRED_MODULES = ["pptx", "docx", "docxtpl", "matplotlib", "numpy", "PIL", "pdfplumber"]

RUNTIME_DIR_NAME = "python"
MANIFEST_NAME = "python-runtime.json"
ENV_BAT_NAME = "python-env.bat"
ENV_SH_NAME = "python-env.sh"

# 平台三元组：Windows ARM64 使用 x64 运行时（由系统仿真执行，官方无 arm64 独立构建）
PLATFORM_TRIPLES = {
    ("windows", "x86_64"): "x86_64-pc-windows-msvc",
    ("windows", "aarch64"): "x86_64-pc-windows-msvc",
    ("linux", "x86_64"): "x86_64-unknown-linux-gnu",
    ("linux", "aarch64"): "aarch64-unknown-linux-gnu",
    ("darwin", "x86_64"): "x86_64-apple-darwin",
    ("darwin", "aarch64"): "aarch64-apple-darwin",
}

_MACHINE_ALIASES = {
    "amd64": "x86_64", "x86_64": "x86_64", "x64": "x86_64",
    "arm64": "aarch64", "aarch64": "aarch64",
}
_VERSION_RE = re.compile(r"(\d+)\.(\d+)(?:\.(\d+))?")


class BootstrapError(Exception):
    """可预期失败（下载失败、依赖安装失败、平台不支持等）。"""


# ══════════════════════════════════════════════════════════════════
# 平台 / 版本
# ══════════════════════════════════════════════════════════════════

def normalize_platform(system=None, machine=None):
    """归一化操作系统与架构标识；不支持的组合返回 None。"""
    system = (system or platform.system()).strip().lower()
    machine = (machine or platform.machine()).strip().lower()
    if system in ("win32", "windows", "cygwin"):
        system = "windows"
    elif system == "darwin":
        system = "darwin"
    elif system == "linux":
        system = "linux"
    else:
        return None
    machine = _MACHINE_ALIASES.get(machine, machine)
    if machine not in ("x86_64", "aarch64"):
        return None
    return system, machine


def target_triple(system=None, machine=None):
    """返回 python-build-standalone 的平台三元组；不支持时返回 None。"""
    key = normalize_platform(system, machine)
    if not key:
        return None
    return PLATFORM_TRIPLES.get(key)


def runtime_asset_name(version, release, triple):
    return "cpython-{}+{}-{}-install_only.tar.gz".format(version, release, triple)


def runtime_url(version, release, triple, base=None):
    base = (base or os.environ.get(MIRROR_ENV, "") or DOWNLOAD_BASE).rstrip("/")
    return "{}/{}/{}".format(base, release, runtime_asset_name(version, release, triple))


def parse_version(text):
    """从 “Python 3.15.2” / “3.15.2” 等文本中解析版本元组。"""
    if not text:
        return ()
    match = _VERSION_RE.search(str(text))
    if not match:
        return ()
    parts = [int(match.group(1)), int(match.group(2))]
    if match.group(3):
        parts.append(int(match.group(3)))
    return tuple(parts)


def version_supported(version_text):
    """运行时版本是否满足最低要求（默认 3.10+）。"""
    version = parse_version(version_text)
    return (len(version) >= 2 and version[0] == MIN_PYTHON_VERSION[0]
            and version[1] >= MIN_PYTHON_VERSION[1])


def runtime_executable(runtime_dir, system=None):
    """返回托管运行时目录内的解释器路径（按操作系统布局）。"""
    system = (system or platform.system()).strip().lower()
    if system in ("win32", "windows", "cygwin"):
        return os.path.join(runtime_dir, "python.exe")
    return os.path.join(runtime_dir, "bin", "python3")


# ══════════════════════════════════════════════════════════════════
# 子进程与探测
# ══════════════════════════════════════════════════════════════════

def _no_window_kwargs():
    if os.name == "nt" and hasattr(subprocess, "CREATE_NO_WINDOW"):
        return {"creationflags": subprocess.CREATE_NO_WINDOW}
    return {}


def _run(cmd, timeout, quiet=False):
    """执行子进程，返回 (returncode, 合并输出)。不抛异常，由调用方判断。"""
    if not quiet:
        print("  $ " + " ".join(shlex.quote(str(part)) for part in cmd))
    try:
        result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                text=True, timeout=timeout, **_no_window_kwargs())
        return result.returncode, result.stdout or ""
    except (OSError, subprocess.SubprocessError) as exc:
        return 1, str(exc)


def probe_runtime(exe, timeout=120):
    """读取解释器版本（如 “3.15.2”）；不可执行返回空字符串。"""
    if not exe or not os.path.isfile(exe):
        return ""
    code = "import sys;print('%d.%d.%d' % tuple(sys.version_info[:3]))"
    rc, out = _run([exe, "-c", code], timeout, quiet=True)
    return out.strip().splitlines()[-1].strip() if rc == 0 and out.strip() else ""


def missing_modules(exe, timeout=120):
    """单次子进程探测全部依赖模块，返回缺失的导入名列表（探测失败按全部缺失）。"""
    if not exe:
        return list(REQUIRED_MODULES)
    code = (
        "import json,sys\n"
        "missing=[]\n"
        "for name in sys.argv[1:]:\n"
        "    try:\n"
        "        __import__(name)\n"
        "    except Exception:\n"
        "        missing.append(name)\n"
        "print(json.dumps(missing))\n"
    )
    rc, out = _run([exe, "-c", code] + list(REQUIRED_MODULES), timeout, quiet=True)
    if rc != 0:
        return list(REQUIRED_MODULES)
    for line in reversed(out.strip().splitlines()):
        try:
            data = json.loads(line)
        except ValueError:
            continue
        if isinstance(data, list):
            return [str(item) for item in data]
    return list(REQUIRED_MODULES)


def _system_interpreters():
    """枚举系统可用解释器（去重），Windows 额外解析 py -3 的实际路径。"""
    seen = set()
    candidates = [sys.executable]

    for name in ("python3", "python"):
        found = shutil.which(name)
        if found:
            candidates.append(found)

    if os.name == "nt":
        launcher = shutil.which("py")
        if launcher:
            rc, out = _run([launcher, "-3", "-c", "import sys;print(sys.executable)"], 120)
            if rc == 0 and out.strip():
                candidates.append(out.strip().splitlines()[-1].strip())

    for candidate in candidates:
        if not candidate:
            continue
        try:
            real = os.path.realpath(candidate)
        except OSError:
            real = candidate
        if real in seen:
            continue
        seen.add(real)
        yield candidate


# ══════════════════════════════════════════════════════════════════
# 运行时清单与环境文件（应用启动时读取同一份数据）
# ══════════════════════════════════════════════════════════════════

def manifest_path(root):
    return os.path.join(root, MANIFEST_NAME)


def read_manifest_python(root):
    """读取清单中的解释器绝对路径；不存在/损坏返回空字符串。"""
    path = manifest_path(root)
    if not os.path.isfile(path):
        return ""
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
    except (OSError, ValueError):
        return ""
    exe = str(data.get("pythonPath") or "").strip()
    if not exe:
        return ""
    if not os.path.isabs(exe):
        exe = os.path.join(root, exe)
    return exe


def write_manifest(root, exe, version, source):
    """写入运行时清单（原子替换），供 WebSQL 启动时定位解释器。"""
    os.makedirs(root, exist_ok=True)
    payload = {
        "pythonPath": os.path.abspath(exe),
        "version": version,
        "source": source,
        "minimumVersion": MIN_PYTHON_VERSION_STR,
        "dependencies": list(PIP_DEPS),
        "requiredModules": list(REQUIRED_MODULES),
        "updatedAt": datetime.datetime.now().isoformat(timespec="seconds"),
    }
    destination = manifest_path(root)
    temporary = destination + ".tmp"
    with open(temporary, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    os.replace(temporary, destination)
    return destination


def _path_bin_dirs(exe):
    """返回需要加入 PATH 的目录（解释器目录 + Windows Scripts 目录）。"""
    bin_dir = os.path.dirname(os.path.abspath(exe))
    dirs = [bin_dir]
    if os.name == "nt":
        scripts = os.path.join(bin_dir, "Scripts")
        if os.path.isdir(scripts):
            dirs.insert(0, scripts)
    return dirs


def env_bat_content(exe, version):
    dirs = _path_bin_dirs(exe)
    path_value = ";".join(dirs + ["%PATH%"])
    # 批处理文件必须 CRLF，否则部分 Windows 版本会误解析整段脚本
    return "\r\n".join([
        "@ECHO OFF",
        "REM 由 python_bootstrap.py 自动生成（Python {}），请勿手工修改".format(version),
        'set "WEBSQL_PYTHON={}"'.format(os.path.abspath(exe)),
        'set "PATH={}"'.format(path_value),
        'set "PYTHONUTF8=1"',
        'set "PYTHONIOENCODING=utf-8"',
        "",
    ])


def env_sh_content(exe, version):
    dirs = _path_bin_dirs(exe)
    prefix = ":".join(dirs).replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")
    return "\n".join([
        "#!/bin/sh",
        "# 由 python_bootstrap.py 自动生成（Python {}），请勿手工修改".format(version),
        "export WEBSQL_PYTHON={}".format(shlex.quote(os.path.abspath(exe))),
        'export PATH="{}:${{PATH}}"'.format(prefix),
        "export PYTHONUTF8=1",
        "export PYTHONIOENCODING=utf-8",
        "",
    ])


def write_env_files(root, exe, version):
    """按当前操作系统写出可直接 source/call 的环境变量脚本。"""
    os.makedirs(root, exist_ok=True)
    written = []
    if os.name == "nt":
        path = os.path.join(root, ENV_BAT_NAME)
        # newline="\n" 禁用平台换行转换，确保 .bat 的 CRLF 不被写成 CRCRLF
        with open(path, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(env_bat_content(exe, version))
    else:
        path = os.path.join(root, ENV_SH_NAME)
        with open(path, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(env_sh_content(exe, version))
        try:
            os.chmod(path, 0o755)
        except OSError:
            pass
    written.append(path)
    return written


# ══════════════════════════════════════════════════════════════════
# 下载与解压
# ══════════════════════════════════════════════════════════════════

def _download(url, destination, timeout, quiet=False):
    print("[Python] 下载运行时: {}".format(url))
    request = urllib.request.Request(url, headers={"User-Agent": "WebSQL-Python-Bootstrap/1.0"})
    try:
        response = urllib.request.urlopen(request, timeout=timeout)
    except (urllib.error.URLError, OSError) as exc:
        raise BootstrapError("下载失败: {}（{}）".format(url, exc))

    with response:
        total = int(response.headers.get("Content-Length") or 0)
        received = 0
        next_report = 5
        with open(destination, "wb") as handle:
            while True:
                chunk = response.read(1024 * 256)
                if not chunk:
                    break
                handle.write(chunk)
                received += len(chunk)
                if total > 0:
                    percent = received * 100 // total
                    if percent >= next_report:
                        print("  已下载 {}% ({:.1f} MB / {:.1f} MB)".format(
                            percent, received / 1048576, total / 1048576))
                        next_report = percent - percent % 5 + 5
        if total and received < total:
            raise BootstrapError("下载不完整（{}/{} 字节）".format(received, total))
    print("  下载完成: {:.1f} MB".format(received / 1048576))


def _extract_archive(archive, destination):
    """安全解压 tar.gz（拒绝绝对路径/上级目录穿越）。"""
    base = os.path.abspath(destination)
    try:
        with tarfile.open(archive, "r:gz") as tar:
            for member in tar.getmembers():
                target = os.path.abspath(os.path.join(base, member.name))
                if os.path.commonpath([target, base]) != base:
                    raise BootstrapError("压缩包包含非法路径: {}".format(member.name))
            if hasattr(tarfile, "data_filter"):
                tar.extractall(destination, filter="data")
            else:
                tar.extractall(destination)
    except (tarfile.TarError, OSError, ValueError) as exc:
        raise BootstrapError("解压运行时失败: {}".format(exc))


def install_managed_runtime(root, managed_dir, args):
    """下载并解压独立运行时到 <root>/python，返回 (解释器路径, 版本)。"""
    triple = target_triple()
    if not triple:
        raise BootstrapError(
            "不支持的操作系统/架构: {}/{}".format(platform.system(), platform.machine()))
    if platform.system().lower() == "windows" and platform.machine().lower() in ("arm64", "aarch64"):
        print("[Python] Windows ARM64 将使用 x64 运行时（系统自动仿真）")

    url = runtime_url(args.python_version, args.release, triple, args.download_base)
    asset = runtime_asset_name(args.python_version, args.release, triple)
    work_dir = tempfile.mkdtemp(prefix="websql_python_")
    try:
        archive = os.path.join(work_dir, asset)
        _download(url, archive, args.timeout, quiet=args.quiet)
        staging = os.path.join(work_dir, "extract")
        os.makedirs(staging)
        _extract_archive(archive, staging)

        source_dir = os.path.join(staging, "python")
        if not os.path.isdir(source_dir):
            source_dir = staging
        extracted = runtime_executable(source_dir)
        version = probe_runtime(extracted, args.timeout)
        if not version or not version_supported(version):
            raise BootstrapError(
                "下载的运行时不可用（版本: {}），请检查镜像内容".format(version or "未知"))

        if os.path.isdir(managed_dir):
            shutil.rmtree(managed_dir)
        shutil.move(source_dir, managed_dir)
    finally:
        shutil.rmtree(work_dir, ignore_errors=True)

    exe = runtime_executable(managed_dir)
    version = probe_runtime(exe, args.timeout)
    if not version or not version_supported(version):
        raise BootstrapError("解压后的 Python 无法执行: {}".format(exe))
    print("[Python] 运行时安装完成: {} (Python {})".format(managed_dir, version))
    return exe, version


# ══════════════════════════════════════════════════════════════════
# 依赖安装
# ══════════════════════════════════════════════════════════════════

def _pip_ready(exe, timeout):
    rc, _ = _run([exe, "-m", "pip", "--version"], timeout, quiet=True)
    return rc == 0


def _permission_related(output):
    lowered = (output or "").lower()
    return ("permission denied" in lowered or "winerror 5" in lowered
            or "access is denied" in lowered or "errno 13" in lowered)


def install_dependencies(exe, args):
    """安装全部第三方依赖；系统 Python 权限不足时自动回退 --user。"""
    if not _pip_ready(exe, args.timeout):
        print("[Python] 当前解释器缺少 pip，尝试 ensurepip ...")
        rc, out = _run([exe, "-m", "ensurepip", "--upgrade"], args.timeout, quiet=args.quiet)
        if rc != 0:
            raise BootstrapError(
                "当前 Python 无 pip 且 ensurepip 不可用: {}\n"
                "  可先安装系统 pip（如 apt install python3-pip），"
                "或重新运行本脚本让自动下载模式接管".format(out.strip()[-500:]))

    index_url = (args.index_url or os.environ.get(INDEX_URL_ENV, "")).strip()
    base_cmd = [exe, "-m", "pip", "install", "--disable-pip-version-check",
                "--no-warn-script-location"]
    if index_url:
        base_cmd += ["--index-url", index_url]

    print("[Python] 安装依赖: {}".format(" ".join(PIP_DEPS)))
    rc, out = _run(base_cmd + list(PIP_DEPS), args.timeout, quiet=args.quiet)
    if rc != 0 and _permission_related(out):
        print("[Python] 系统 Python 无写入权限，改用 --user 安装到用户目录 ...")
        rc, out = _run(base_cmd + ["--user"] + list(PIP_DEPS), args.timeout, quiet=args.quiet)
    if rc != 0:
        raise BootstrapError("依赖安装失败（pip exit={}）:\n{}".format(
            rc, out.strip()[-800:]))


# ══════════════════════════════════════════════════════════════════
# 选择 / 检测 / 安装
# ══════════════════════════════════════════════════════════════════

def select_interpreter(root, args):
    """按优先级选出可用解释器，返回 (路径, 版本, 来源)；无可用返回 None。

    优先级与应用启动时的 Go 探测保持一致：WEBSQL_PYTHON > 托管目录 >
    运行时清单 > 系统 PATH。
    """
    override = os.environ.get(PYTHON_ENV_OVERRIDE, "").strip()
    if override:
        candidate = override
        if not os.path.isabs(candidate) and not os.path.isfile(candidate):
            found = shutil.which(candidate)
            if found:
                candidate = found
        version = probe_runtime(candidate, args.timeout)
        if version and version_supported(version):
            return candidate, version, "env"

    managed_exe = runtime_executable(os.path.join(root, RUNTIME_DIR_NAME))
    version = probe_runtime(managed_exe, args.timeout)
    if version and version_supported(version):
        return managed_exe, version, "managed"

    manifest_exe = read_manifest_python(root)
    if manifest_exe:
        version = probe_runtime(manifest_exe, args.timeout)
        if version and version_supported(version):
            return manifest_exe, version, "manifest"

    for candidate in _system_interpreters():
        version = probe_runtime(candidate, args.timeout)
        if version and version_supported(version):
            return candidate, version, "path"
    return None


def cmd_check(args):
    root = os.path.abspath(args.root or default_root())
    print("[Python] 检测结果（仅检查，不下载不安装）")
    print("  运行目录: {}".format(root))

    selected = select_interpreter(root, args)
    if not selected:
        print("  解释器 : 未找到满足要求的 Python（最低 {}）".format(MIN_PYTHON_VERSION_STR))
        print("  状态   : 未就绪（运行 python_bootstrap.py --ensure 自动安装）")
        return 1

    exe, version, source = selected
    print("  解释器 : {} (Python {}, 来源: {})".format(exe, version, source_label(source)))
    missing = missing_modules(exe, args.timeout)
    if missing:
        print("  依赖   : 缺失 {}".format(", ".join(missing)))
        print("  状态   : 未就绪（重新运行本脚本可自动补齐依赖）")
        return 1
    print("  依赖   : 完整（{}）".format(", ".join(REQUIRED_MODULES)))
    print("  状态   : 就绪")
    return 0


def source_label(source):
    return {
        "env": "环境变量 WEBSQL_PYTHON",
        "managed": "托管运行时目录 python/",
        "manifest": "运行时清单 python-runtime.json",
        "path": "系统 PATH",
        "downloaded": "自动下载",
    }.get(source, source)


def cmd_install(args):
    root = os.path.abspath(args.root or default_root())
    managed_dir = os.path.join(root, RUNTIME_DIR_NAME)
    os.makedirs(root, exist_ok=True)

    print("[Python] WebSQL 运行时自动安装（目标版本 Python {}+）".format(
        MIN_PYTHON_VERSION_STR))
    print("  运行目录: {}".format(root))

    if args.force and os.path.isdir(managed_dir):
        print("[Python] --force: 删除已有托管运行时 {}".format(managed_dir))
        shutil.rmtree(managed_dir, ignore_errors=True)

    selected = select_interpreter(root, args)
    if selected:
        exe, version, source = selected
        print("[Python] 使用已有解释器: {} (Python {}, 来源: {})".format(
            exe, version, source_label(source)))
    else:
        if args.offline:
            raise BootstrapError(
                "离线模式下未找到可用 Python（最低 {}）。请预先放置 python/ 运行时，"
                "或联网执行本脚本".format(MIN_PYTHON_VERSION_STR))
        print("[Python] 未找到可用 Python，开始自动下载独立运行时 ...")
        exe, version = install_managed_runtime(root, managed_dir, args)
        source = "downloaded"

    missing = missing_modules(exe, args.timeout)
    if missing:
        print("[Python] 检测到缺失依赖: {}".format(", ".join(missing)))
        try:
            install_dependencies(exe, args)
        except BootstrapError:
            # 清单/系统 PATH 的 Python 装不上依赖时，自动下载托管运行时再装。
            # WEBSQL_PYTHON 为运维显式指定，失败时直接报错（不静默切换解释器）。
            if source in ("manifest", "path"):
                if args.offline:
                    raise
                print("[Python][WARN] 系统 Python 依赖安装失败，改用托管运行时")
                exe, version = install_managed_runtime(root, managed_dir, args)
                source = "downloaded"
                install_dependencies(exe, args)
            else:
                raise

    missing = missing_modules(exe, args.timeout)
    if missing:
        raise BootstrapError("依赖校验未通过，仍缺失: {}".format(", ".join(missing)))

    manifest = write_manifest(root, exe, version, source)
    env_files = write_env_files(root, exe, version)

    print("[OK] Python 运行时已就绪: {} (Python {})".format(exe, version))
    print("     运行时清单: {}（应用启动时自动读取）".format(manifest))
    for item in env_files:
        print("     环境脚本  : {}（手动启动时可先执行以复用同一环境）".format(item))
    return 0


def default_root():
    return os.path.dirname(os.path.abspath(__file__))


def build_parser():
    parser = argparse.ArgumentParser(
        description="WebSQL Python 运行时自动下载与依赖安装（外置模式）")
    parser.add_argument("--root", default=None,
                        help="应用根目录（默认为脚本所在目录）")
    parser.add_argument("--check", action="store_true",
                        help="仅检测版本与依赖，不下载不安装")
    parser.add_argument("--ensure", action="store_true",
                        help="检查并按需安装（供启动脚本调用，行为同默认）")
    parser.add_argument("--force", action="store_true",
                        help="删除托管运行时后重新下载安装")
    parser.add_argument("--offline", action="store_true",
                        help="离线模式：只使用本机已有资源，禁止下载")
    parser.add_argument("--quiet", action="store_true",
                        help="减少命令输出（错误信息仍会打印）")
    parser.add_argument("--python-version", default=PYTHON_FULL_VERSION,
                        help="下载的 Python 完整版本（默认 {}）".format(PYTHON_FULL_VERSION))
    parser.add_argument("--release", default=STANDALONE_RELEASE,
                        help="python-build-standalone 发布标签（默认 {}）".format(STANDALONE_RELEASE))
    parser.add_argument("--download-base", default=None,
                        help="下载基地址（默认读取 {} 或内置地址）".format(MIRROR_ENV))
    parser.add_argument("--index-url", default=None,
                        help="pip 索引地址（默认读取 {}）".format(INDEX_URL_ENV))
    parser.add_argument("--timeout", type=int, default=900,
                        help="下载/安装超时秒数（默认 900）")
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        if args.check:
            return cmd_check(args)
        return cmd_install(args)
    except BootstrapError as exc:
        print("[Python][ERROR] {}".format(exc))
        print("  排查建议:")
        print("    1. 检查网络，或设置镜像: {} = <镜像基地址>".format(MIRROR_ENV))
        print("    2. 内网环境可预先准备 python/ 目录后使用 --offline")
        print("    3. 系统 Python 过旧（< {}）时会自动改用托管运行时".format(
            MIN_PYTHON_VERSION_STR))
        return 1
    except KeyboardInterrupt:
        print("\n[Python] 已取消")
        return 130


if __name__ == "__main__":
    sys.exit(main())
