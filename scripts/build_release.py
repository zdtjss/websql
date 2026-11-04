#!/usr/bin/env python3
"""
WebSQL Web 版跨平台打包脚本

在任意平台交叉编译并打包 Windows / Linux / macOS 发行版，
每个平台生成一个独立可运行的 zip 压缩包。

用法:
  python scripts/build_release.py                          # 交互式选择平台
  python scripts/build_release.py --platform windows       # 仅打包 Windows
  python scripts/build_release.py --platform linux         # 仅打包 Linux
  python scripts/build_release.py --platform macos         # 仅打包 macOS (amd64+arm64)
  python scripts/build_release.py --platform all           # 打包全部平台
  python scripts/build_release.py --python-mode external   # 外置模式：包内附自动下载安装脚本
  python scripts/build_release.py --python-mode bundle     # 内置模式：把 Python 运行时打进包（仅 Windows）
  python scripts/build_release.py --skip-frontend          # 跳过前端构建
  python scripts/build_release.py --skip-build             # 跳过 Go 编译（使用已有二进制）
  python scripts/build_release.py --skip-db                # 跳过全新数据库创建

  --python-mode 不传（或传 ask）时交互式询问，默认推荐外置自动下载模式。

  构建过程中会交互式询问是否携带 skills 目录（默认携带）。

两种 Python 运行时模式:
  external  不内置 Python；包内附 python_bootstrap.py 与 setup-python.*，
            首次启动自动检测/下载运行时、安装依赖，并写入 python-runtime.json
  bundle    随包内置 Python 运行时（当前由本地 embeddable 包构建，仅 Windows），
            开箱即用、无需联网安装

产物:
  dist-pack/websql-web-{platform}.zip — 可独立发行、运行的 zip 包
  包内容: 二进制 + static/ + config.json + nway.sqlite3.db +
           migrations/ + db_migrate.py + startup 脚本 +
           可选 skills/（默认携带）+ PYTHON-RUNTIME.md（两种模式说明）+
           内置模式: python/；外置模式: python_bootstrap.py + setup-python.*
"""

import argparse
import io
import os
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import zipfile
from datetime import datetime

# 复用桌面版打包脚本的 Python 运行时收集逻辑（embeddable 解压 + pip 交叉安装 + 验证），
# 避免两套脚本维护同一份依赖清单与安装步骤。
_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
if _SCRIPT_DIR not in sys.path:
    sys.path.insert(0, _SCRIPT_DIR)
try:
    from build_desktop import (
        BUNDLE_PYTHON_DIR,
        cleanup_python_bundle,
        collect_python_runtime,
    )
    PY_BUNDLE_AVAILABLE = True
except ImportError:  # 脚本被单独拷贝运行时退化为不支持捆绑
    BUNDLE_PYTHON_DIR = ""
    PY_BUNDLE_AVAILABLE = False

# 外置模式所需资源（跟随本项目分发，不依赖第三方下载器）
from python_bootstrap import (
    MIN_PYTHON_VERSION_STR,
    PIP_DEPS,
    PYTHON_FULL_VERSION,
    REQUIRED_MODULES,
    STANDALONE_RELEASE,
)

PYTHON_BOOTSTRAP_FILE = os.path.join(_SCRIPT_DIR, "python_bootstrap.py")
SETUP_BAT_FILE = os.path.join(_SCRIPT_DIR, "setup-python.bat")
SETUP_PS1_FILE = os.path.join(_SCRIPT_DIR, "setup-python.ps1")
SETUP_SH_FILE = os.path.join(_SCRIPT_DIR, "setup-python.sh")

PYTHON_MODE_ASK = "ask"
PYTHON_MODE_BUNDLE = "bundle"
PYTHON_MODE_EXTERNAL = "external"
PYTHON_MODES = (PYTHON_MODE_ASK, PYTHON_MODE_BUNDLE, PYTHON_MODE_EXTERNAL)


def _configure_stdout():
    """仅在作为脚本运行时统一输出编码（作为模块导入时不改动调用方 stdout）。"""
    if sys.stdout.encoding != "utf-8":
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")


PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DIST_PACK_DIR = os.path.join(PROJECT_ROOT, "dist-pack")
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")
SKILLS_DIR = os.path.join(PROJECT_ROOT, "skills")
CONFIG_FILE = os.path.join(PROJECT_ROOT, "config.json")
DB_MIGRATE_SCRIPT = os.path.join(PROJECT_ROOT, "scripts", "db_migrate.py")
MIGRATIONS_DIR = os.path.join(PROJECT_ROOT, "migrations")
SQLITE_FULL_SQL = os.path.join(MIGRATIONS_DIR, "full", "sqlite_full.sql")
MYSQL_FULL_SQL = os.path.join(MIGRATIONS_DIR, "full", "mysql_full.sql")

APP_NAME = "WebSql"
DB_NAME = "nway.sqlite3.db"
VERSION = datetime.now().strftime("%Y%m%d%H%M")

EXCLUDE_DIRS = {"__pycache__", ".git", "node_modules"}

# 平台配置
PLATFORMS = {
    "windows-amd64": {"goos": "windows", "goarch": "amd64", "ext": ".exe"},
    "linux-amd64":   {"goos": "linux",   "goarch": "amd64", "ext": ""},
    "macos-amd64":   {"goos": "darwin",  "goarch": "amd64", "ext": ""},
    "macos-arm64":   {"goos": "darwin",  "goarch": "arm64", "ext": ""},
}

# 交互式选项到平台列表的映射
PLATFORM_GROUPS = {
    "windows": ["windows-amd64"],
    "linux":   ["linux-amd64"],
    "macos":   ["macos-amd64", "macos-arm64"],
    "all":     list(PLATFORMS.keys()),
}


def startup_bat_content(python_mode):
    """Windows 启动脚本：外置模式下先确保 Python 运行时可用。"""
    lines = [
        "@ECHO OFF",
        'cd /d "%~dp0"',
    ]
    if python_mode == PYTHON_MODE_EXTERNAL:
        lines.append('call "%~dp0setup-python.bat" --ensure')
    lines.append(
        '%1 start mshta vbscript:createobject("wscript.shell").run("""%~0"" ::",0)(window.close)&&exit')
    if python_mode == PYTHON_MODE_EXTERNAL:
        lines.append('if exist "%~dp0python-env.bat" call "%~dp0python-env.bat"')
    lines.extend([
        'tskill WebSql >nul 2>&1',
        'start /b "" WebSql.exe',
    ])
    # .bat 必须使用 CRLF：仅 LF 换行的批处理在部分 Windows 版本上会被整段误解析
    return "\r\n".join(lines) + "\r\n"


def startup_sh_content(python_mode):
    """Linux / macOS 启动脚本：外置模式下先确保 Python 运行时可用。"""
    lines = [
        "#!/bin/bash",
        'cd "$(dirname "$0")"',
    ]
    if python_mode == PYTHON_MODE_EXTERNAL:
        lines.extend([
            'chmod +x ./setup-python.sh 2>/dev/null || true',
            './setup-python.sh --ensure || echo "[WebSQL][WARN] Python 运行时检查未通过，应用将以降级模式运行"',
            'if [ -f "./python-env.sh" ]; then . ./python-env.sh; fi',
        ])
    lines.extend([
        'pkill -f WebSql 2>/dev/null || true',
        'sleep 1',
        'chmod +x WebSql',
        'nohup ./WebSql > websql.log 2>&1 &',
        'echo "WebSql started (PID: $!)"',
        'echo "Log file: websql.log"',
    ])
    return "\n".join(lines) + "\n"


def python_runtime_doc(platform_key, python_mode):
    """生成发行包内的 Python 运行时模式说明（两种模式的差异/场景/注意事项）。"""
    cfg = PLATFORMS[platform_key]
    goos = cfg["goos"]
    is_bundle = python_mode == PYTHON_MODE_BUNDLE
    current = "内置 Python 运行时（bundle）" if is_bundle else "外置自动下载（external）"
    startup = "startup.bat" if goos == "windows" else "startup.sh"
    setup_cmd = "setup-python.bat --ensure" if goos == "windows" else "./setup-python.sh --ensure"
    dep_list = "\n".join(f"- `{item}`" for item in PIP_DEPS)
    module_list = ", ".join(REQUIRED_MODULES)

    bundle_section = """\
### 内置模式（bundle，本包{flag}）
- 发行包内含 `python/` 独立运行时（Python {py_version}），解压即可用，**无需联网**。
- 包体通常增加数百 MB；当前仅 Windows 目标由本地 embeddable 包构建，Linux/macOS 请使用外置模式。
- 适合内网、离线、无外网出口的生产环境，或希望“拷贝即用”的部署方式。
""".format(py_version=PYTHON_FULL_VERSION, flag="采用" if is_bundle else "未采用")

    external_section = """\
### 外置模式（external，本包{flag}）
- 发行包不含 Python 运行时，包体小；首次启动会自动检测、下载运行时并安装依赖。
- 需要目标机器可访问 python-build-standalone GitHub Releases（或通过
  `WEBSQL_PYTHON_MIRROR` 指向内网镜像），并具备安装目录的写权限。
- 适合可联网环境、希望减小安装包体积或统一由运维预置 Python 的场景。
- 离线部署：在可联网机器执行一次安装脚本生成 `python/` 目录与
  `python-runtime.json`，再整体拷贝到目标机即可；目标机不会再次下载。
""".format(flag="采用" if not is_bundle else "未采用")

    if is_bundle:
        maintenance = """\
内置模式已随包提供运行时，无需执行安装命令。
如需重建：删除 `python/` 后重新解压发行包即可；`python-env.*` 仅在外置模式下由安装脚本生成。
"""
    else:
        maintenance = """\
```bash
# 检查运行时与依赖状态（不下载、不安装）
{check_cmd}

# 安装/修复（幂等；依赖缺失时自动补齐）
{ensure_cmd}

# 强制重新下载运行时
{force_cmd}
```

Windows 使用 `setup-python.bat <参数>`；Linux/macOS 使用 `./setup-python.sh <参数>`。
""".format(
            check_cmd="setup-python.bat --check" if goos == "windows" else "./setup-python.sh --check",
            ensure_cmd=setup_cmd,
            force_cmd="setup-python.bat --force" if goos == "windows" else "./setup-python.sh --force",
        )

    return """\
# WebSQL Python 运行时说明（{platform}）

本发行包采用的模式：**{current}**

## 一、两种模式对比

| 维度 | 内置模式（bundle） | 外置模式（external） |
| --- | --- | --- |
| 包体大小 | 大（+数百 MB） | 小 |
| 首次启动 | 开箱即用 | 自动下载运行时并安装依赖（需联网） |
| 网络要求 | 无需联网 | 首次启动需访问下载源/镜像 |
| 运行时版本 | 固定 {py_version} | 固定 {py_version}（下载同一版本，版本一致） |
| 适用平台 | 当前仅 Windows | Windows / Linux / macOS 全平台 |
| 适用场景 | 内网、离线、拷贝即用 | 公网部署、体积敏感、运维统一预置 |

## 二、本包的行为

- 启动入口：`{startup}`{startup_note}
- 运行时定位顺序（应用启动时自动判断）：
  1. 环境变量 `WEBSQL_PYTHON`（显式指定解释器绝对路径）
  2. `python-runtime.json`（安装脚本写入的解释器清单）
  3. 随包 `python/` 目录（内置模式或外置模式自动下载后的产物）
  4. 系统 `PATH` 中的 `python3` / `python`
- 都找不到时应用仍可启动，但 Skill 导出与 docx/pdf 解析会降级（其余功能不受影响）。

## 三、安装与维护

{maintenance}

## 四、依赖清单

运行时最低版本：Python {min_version}+；安装脚本会安装以下固定版本依赖：

{deps}

安装完成后会逐个验证模块：`{modules}`。

## 五、环境变量

| 变量 | 作用 |
| --- | --- |
| `WEBSQL_PYTHON` | 指定解释器路径，优先级最高（也可跳过自动安装） |
| `WEBSQL_PYTHON_MIRROR` | 独立运行时下载镜像基地址（内网镜像） |
| `WEBSQL_PIP_INDEX_URL` | pip 索引地址（内网 PyPI 镜像） |

## 六、注意事项

1. 外置模式首次启动需要网络；若下载失败，设置 `WEBSQL_PYTHON_MIRROR` 或改用离线预置方式。
2. 系统 Python 版本低于 {min_version} 时，脚本会自动下载托管运行时，不会修改系统 Python。
3. 使用系统 Python 且无写入权限时，脚本自动回退 `pip install --user`。
4. 删除 `python/` 与 `python-runtime.json` 可回到“未安装”状态，重新执行安装命令即可重建。
5. 内置模式的 `python/` 目录由打包机产出，请勿手工替换为不同版本的解释器。

{bundle_section}
{external_section}
""".format(
        platform=platform_key,
        current=current,
        py_version=PYTHON_FULL_VERSION,
        startup=startup,
        startup_note="（外置模式会自动调用安装脚本）" if not is_bundle else "（直接启动，无需安装步骤）",
        maintenance=maintenance,
        min_version=MIN_PYTHON_VERSION_STR,
        deps=dep_list,
        modules=module_list,
        bundle_section=bundle_section,
        external_section=external_section,
    )


def _write_text_entry(zipf, arcname, content, executable=False):
    """写入文本文件；Unix 目标需要显式设置可执行权限位。"""
    info = zipfile.ZipInfo(arcname, date_time=datetime.now().timetuple()[:6])
    info.create_system = 3  # Unix，external_attr 才会被解压工具识别
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = (0o755 if executable else 0o644) << 16
    zipf.writestr(info, content)


def _read_script(path, crlf=False, bom=False):
    """读取脚本文本并统一换行/编码，避免受仓库 autocrlf 设置影响。

    - .bat 需要 CRLF；.ps1 需要 CRLF + UTF-8 BOM（PowerShell 5.1 才能正确解析中文）
    - .sh 需要 LF，保证 Linux/macOS 的 shebang 与解释器正常工作
    """
    with open(path, "r", encoding="utf-8-sig") as handle:
        text = handle.read().replace("\r\n", "\n").replace("\r", "\n")
    if crlf:
        text = text.replace("\n", "\r\n")
    if bom:
        text = "\ufeff" + text
    return text


def add_external_python_assets(zipf, goos):
    """外置模式资源：引导脚本 + 各平台一键安装脚本（缺一不可）。"""
    required = [PYTHON_BOOTSTRAP_FILE]
    required.append(SETUP_BAT_FILE if goos == "windows" else SETUP_SH_FILE)
    if goos == "windows":
        required.append(SETUP_PS1_FILE)
    missing = [path for path in required if not os.path.isfile(path)]
    if missing:
        print(f"  [FAIL] 缺少外置模式资源: {', '.join(missing)}")
        sys.exit(1)

    zipf.write(PYTHON_BOOTSTRAP_FILE, "python_bootstrap.py")
    if goos == "windows":
        _write_text_entry(zipf, "setup-python.bat",
                          _read_script(SETUP_BAT_FILE, crlf=True))
        _write_text_entry(zipf, "setup-python.ps1",
                          _read_script(SETUP_PS1_FILE, crlf=True, bom=True))
    else:
        _write_text_entry(zipf, "setup-python.sh",
                          _read_script(SETUP_SH_FILE), executable=True)


def run(cmd, cwd=None, env=None):
    merged_env = {**os.environ, **(env or {})}
    print(f"  > {cmd}")
    proc = subprocess.run(cmd, shell=True, cwd=cwd, env=merged_env)
    if proc.returncode != 0:
        print(f"  [FAIL] 命令执行失败: {cmd}")
        sys.exit(1)


def select_platforms(platform_arg):
    """解析平台参数，返回要构建的平台 key 列表。"""
    if platform_arg:
        # 精确匹配平台 key
        if platform_arg in PLATFORMS:
            return [platform_arg]
        # 匹配分组名
        if platform_arg in PLATFORM_GROUPS:
            return PLATFORM_GROUPS[platform_arg]
        print(f"[WARN] 未知平台: {platform_arg}")

    # 交互式选择
    print("\n请选择目标平台:")
    options = [
        ("1", "Windows (amd64)", "windows"),
        ("2", "Linux (amd64)", "linux"),
        ("3", "macOS (amd64 + arm64)", "macos"),
        ("4", "全部平台", "all"),
    ]
    for num, desc, _ in options:
        print(f"  {num}. {desc}")

    while True:
        try:
            choice = input("请输入选项 [1-4] (默认 4): ").strip()
            if not choice:
                choice = "4"
            for num, _, key in options:
                if choice == num:
                    return PLATFORM_GROUPS[key]
        except (ValueError, EOFError):
            pass
        print("  无效输入，请重试")


def build_frontend():
    print("\n[1/4] 构建前端...")
    if not os.path.isdir(os.path.join(WEB_SRC_DIR, "node_modules")):
        print("  安装 npm 依赖...")
        run("npm install", cwd=WEB_SRC_DIR)
    run("npm run build", cwd=WEB_SRC_DIR)
    if not os.path.isdir(DIST_DIR):
        print(f"  [FAIL] 前端构建失败，未找到 {DIST_DIR}")
        sys.exit(1)
    print("  [OK] 前端构建完成")


def get_sqlite_migration_versions():
    """读取 SQLite 增量迁移版本号（与程序内 Migrator 的 version 命名一致）。"""
    sqlite_dir = os.path.join(MIGRATIONS_DIR, "sqlite")
    if not os.path.isdir(sqlite_dir):
        return []
    return sorted(f[: -len(".sql")] for f in os.listdir(sqlite_dir) if f.endswith(".sql"))


def create_fresh_db():
    """从全量 SQL 脚本创建全新数据库，用于首次部署开箱即用。"""
    print("\n[2/4] 创建全新数据库...")
    if not os.path.isfile(SQLITE_FULL_SQL):
        print(f"  [FAIL] 未找到 {SQLITE_FULL_SQL}")
        sys.exit(1)

    tmp_dir = tempfile.mkdtemp(prefix="websql_build_")
    db_path = os.path.join(tmp_dir, DB_NAME)

    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA synchronous=NORMAL")

    with open(SQLITE_FULL_SQL, "r", encoding="utf-8") as f:
        sql_script = f.read()

    conn.executescript(sql_script)

    # 全量脚本已包含最新 schema，必须同时登记全部增量迁移版本，
    # 否则首次启动会被识别为「存量库」并重放增量脚本，触发 duplicate column 错误。
    versions = get_sqlite_migration_versions()
    if versions:
        conn.executemany(
            "INSERT OR IGNORE INTO t_schema_migration (version, description, checksum) VALUES (?, ?, ?)",
            [(v, "applied via full init", "") for v in versions],
        )
        conn.commit()

    conn.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    conn.execute("PRAGMA journal_mode=DELETE")
    conn.close()

    size_kb = os.path.getsize(db_path) / 1024
    print(f"  [OK] 数据库创建完成: {DB_NAME} ({size_kb:.1f} KB)，已登记 {len(versions)} 个迁移版本")
    return db_path, tmp_dir


def cleanup_fresh_db(tmp_dir):
    if tmp_dir and os.path.isdir(tmp_dir):
        shutil.rmtree(tmp_dir, ignore_errors=True)


def generate_agents_md_embed():
    """将 AGENTS.md 复制到 agent 包供 go:embed 嵌入。"""
    agents_md_src = os.path.join(PROJECT_ROOT, "AGENTS.md")
    agents_md_dst = os.path.join(PROJECT_ROOT, "internal", "ai", "agent", "_embedded_agents.md")
    if not os.path.isfile(agents_md_src):
        print(f"  [WARN] 未找到 {agents_md_src}，跳过 AGENTS.md 嵌入")
        return
    shutil.copy2(agents_md_src, agents_md_dst)
    print(f"  [OK] AGENTS.md 嵌入文件已生成 ({os.path.getsize(agents_md_dst)} 字节)")


def compile_go(platform_key):
    cfg = PLATFORMS[platform_key]
    goos, goarch, ext = cfg["goos"], cfg["goarch"], cfg["ext"]
    binary_name = APP_NAME + ext

    env = {"CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch}
    ldflags = f"-s -w -X internal/version.Version={VERSION}"

    print(f"  编译 {goos}/{goarch}...")
    run(f'go build -ldflags "{ldflags}" -o {binary_name} main.go', cwd=PROJECT_ROOT, env=env)

    binary_path = os.path.join(PROJECT_ROOT, binary_name)
    if not os.path.isfile(binary_path):
        print(f"  [FAIL] 编译失败，未找到 {binary_path}")
        sys.exit(1)

    size_mb = os.path.getsize(binary_path) / 1024 / 1024
    print(f"  [OK] 编译完成: {binary_name} ({size_mb:.1f} MB)")
    return binary_path


def _zip_compress(file_path):
    """大文件（dll/pyd/模板）不压缩，加速打包并避免体积膨胀。"""
    return zipfile.ZIP_STORED if os.path.getsize(file_path) > 1 * 1024 * 1024 else zipfile.ZIP_DEFLATED


def create_package(platform_key, binary_path, db_path, include_skills=True,
                   python_mode=PYTHON_MODE_EXTERNAL):
    """组装独立可运行的 zip 发行包（按 python_mode 写入不同运行时资源）。"""
    cfg = PLATFORMS[platform_key]
    goos, goarch, ext = cfg["goos"], cfg["goarch"], cfg["ext"]

    effective_mode = python_mode
    has_python = False
    if python_mode == PYTHON_MODE_BUNDLE:
        has_python = bool(BUNDLE_PYTHON_DIR) and os.path.isdir(BUNDLE_PYTHON_DIR)
        if not has_python:
            print("  [WARN] 内置模式未找到 python/ 运行时，退化为外置自动下载模式")
            effective_mode = PYTHON_MODE_EXTERNAL

    zip_name = f"websql-web-{platform_key}.zip"
    zip_path = os.path.join(DIST_PACK_DIR, zip_name)
    has_skills = include_skills and os.path.isdir(SKILLS_DIR)

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zipf:
        # 二进制
        zipf.write(binary_path, APP_NAME + ext)

        # 全新数据库（首次部署开箱即用）
        if db_path and os.path.isfile(db_path):
            zipf.write(db_path, DB_NAME)

        # 前端静态文件
        if os.path.isdir(DIST_DIR):
            for root, dirs, files in os.walk(DIST_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    arcname = os.path.join("static", os.path.relpath(file_path, DIST_DIR))
                    zipf.write(file_path, arcname)

        # 迁移脚本
        sqlite_dir = os.path.join(MIGRATIONS_DIR, "sqlite")
        if os.path.isdir(sqlite_dir):
            for f in os.listdir(sqlite_dir):
                if f.endswith(".sql"):
                    zipf.write(os.path.join(sqlite_dir, f), os.path.join("migrations", "sqlite", f))

        if os.path.isfile(SQLITE_FULL_SQL):
            zipf.write(SQLITE_FULL_SQL, "migrations/full/sqlite_full.sql")
        if os.path.isfile(MYSQL_FULL_SQL):
            zipf.write(MYSQL_FULL_SQL, "migrations/full/mysql_full.sql")

        # 配置文件
        if os.path.isfile(CONFIG_FILE):
            zipf.write(CONFIG_FILE, "config.json")

        # 数据库迁移工具
        if os.path.isfile(DB_MIGRATE_SCRIPT):
            zipf.write(DB_MIGRATE_SCRIPT, "db_migrate.py")

        # skills 目录（Skill 脚本与模板，可选）
        if has_skills:
            for root, dirs, files in os.walk(SKILLS_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    arcname = os.path.join("skills", os.path.relpath(file_path, SKILLS_DIR))
                    zipf.write(file_path, arcname, compress_type=_zip_compress(file_path))

        # 内置模式：捆绑 Python 运行时（解压后 python/ 与可执行文件同级，启动时自动发现）
        if has_python:
            for root, dirs, files in os.walk(BUNDLE_PYTHON_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    rel = os.path.relpath(file_path, BUNDLE_PYTHON_DIR).replace(os.sep, "/")
                    zipf.write(file_path, "python/" + rel, compress_type=_zip_compress(file_path))

        # 外置模式：自动下载/安装脚本（全平台适配）
        if effective_mode == PYTHON_MODE_EXTERNAL:
            add_external_python_assets(zipf, goos)

        # Python 运行时模式说明（两种模式的差异、适用场景与注意事项）
        _write_text_entry(zipf, "PYTHON-RUNTIME.md",
                          python_runtime_doc(platform_key, effective_mode))

        # AGENTS.md（运行时可热更新覆盖嵌入内容）
        agents_md = os.path.join(PROJECT_ROOT, "AGENTS.md")
        if os.path.isfile(agents_md):
            zipf.write(agents_md, "AGENTS.md")

        # 启动脚本（外置模式下自动确保 Python 运行时可用）
        if goos == "windows":
            _write_text_entry(zipf, "startup.bat", startup_bat_content(effective_mode))
        else:
            _write_text_entry(zipf, "startup.sh", startup_sh_content(effective_mode),
                              executable=True)

    size_mb = os.path.getsize(zip_path) / 1024 / 1024
    parts = ["skills" if has_skills else "无 skills"]
    if has_python:
        parts.append("内置 Python {}".format(PYTHON_FULL_VERSION))
    elif effective_mode == PYTHON_MODE_EXTERNAL:
        parts.append("外置自动下载 Python")
    parts.append("模式说明 PYTHON-RUNTIME.md")
    print(f"  [OK] 打包完成: {zip_name} ({size_mb:.1f} MB, 含: {' + '.join(parts)})")
    return zip_path


def ask_include_skills():
    """交互式询问是否携带 skills 目录（默认携带；非交互环境回退为携带）。"""
    prompt = "[Package] 是否携带 skills 目录（Skill 脚本与模板，导出功能依赖）? [Y/n]: "
    try:
        answer = input(prompt)
    except EOFError:
        print("  -> 非交互环境，默认携带 skills")
        return True
    return answer.strip().lower() not in ("n", "no")


def python_bundle_targets(targets):
    """返回可捆绑 Python 运行时的目标平台。

    内置模式依赖随仓库分发的 Windows embeddable 运行时（win_amd64/cp315），
    因此当前仅 windows-amd64 支持内置；其余平台由外置自动下载模式覆盖。
    """
    return [key for key in targets if key == "windows-amd64"]


def resolve_python_mode(requested):
    """解析 Python 打包模式：显式参数优先，否则交互式询问（默认外置）。"""
    if requested in (PYTHON_MODE_BUNDLE, PYTHON_MODE_EXTERNAL):
        return requested

    print("\n请选择 Python 运行时打包模式:")
    print("  1. 外置自动下载（推荐：包体小，首次启动自动下载并安装依赖）")
    print("  2. 内置运行时（开箱即用、无需联网；包体 +数百 MB，当前仅 Windows）")
    while True:
        try:
            choice = input("请输入选项 [1-2] (默认 1): ").strip() or "1"
        except EOFError:
            print("  -> 非交互环境，默认外置自动下载模式")
            return PYTHON_MODE_EXTERNAL
        if choice in ("1", "external", "e"):
            return PYTHON_MODE_EXTERNAL
        if choice in ("2", "bundle", "b"):
            return PYTHON_MODE_BUNDLE
        print("  无效输入，请重试")


def platform_python_mode(platform_key, requested_mode, bundle_ready):
    """单个目标平台最终采用的模式（内置不可用时自动退化为外置）。"""
    if requested_mode == PYTHON_MODE_BUNDLE and bundle_ready \
            and platform_key in python_bundle_targets([platform_key]):
        return PYTHON_MODE_BUNDLE
    return PYTHON_MODE_EXTERNAL


def main():
    _configure_stdout()
    parser = argparse.ArgumentParser(description="WebSQL Web 版跨平台打包脚本")
    parser.add_argument("--platform", default=None,
                        help="目标平台 (windows|linux|macos|windows-amd64|linux-amd64|macos-amd64|macos-arm64|all)")
    parser.add_argument("--python-mode", choices=PYTHON_MODES, default=PYTHON_MODE_ASK,
                        help="Python 运行时模式: ask=交互式询问(默认，非交互回退 external), "
                             "external=外置自动下载, bundle=内置运行时(仅 Windows)")
    parser.add_argument("--skip-frontend", action="store_true", help="跳过前端构建")
    parser.add_argument("--skip-build", action="store_true", help="跳过 Go 编译（使用已有二进制）")
    parser.add_argument("--skip-db", action="store_true", help="跳过全新数据库创建")
    args = parser.parse_args()

    targets = select_platforms(args.platform)

    print()
    print("=" * 55)
    print("  WebSQL Web 版打包脚本")
    print(f"  Version: {VERSION}")
    print(f"  目标平台: {', '.join(targets)}")
    print(f"  产物目录: {DIST_PACK_DIR}")
    print("=" * 55)

    # 打包内容选项（默认携带 skills、外置自动下载 Python；非交互环境回退默认值）
    include_skills = ask_include_skills()
    python_mode = resolve_python_mode(args.python_mode)
    if python_mode == PYTHON_MODE_BUNDLE and not PY_BUNDLE_AVAILABLE:
        print("[WARN] 当前脚本未加载 Python 捆绑模块，改用外置自动下载模式")
        python_mode = PYTHON_MODE_EXTERNAL
    print(f"[Python] 运行时打包模式: {python_mode}")

    # 前端
    if not args.skip_frontend:
        build_frontend()
    else:
        print("\n[1/4] 跳过前端构建")
        if not os.path.isdir(DIST_DIR):
            print(f"  [FAIL] 未找到前端构建产物 {DIST_DIR}，请先构建前端或去掉 --skip-frontend")
            sys.exit(1)

    # 全新数据库
    db_path = None
    tmp_dir = None
    if not args.skip_db:
        db_path, tmp_dir = create_fresh_db()
    else:
        print("\n[2/4] 跳过全新数据库创建")

    try:
        os.makedirs(DIST_PACK_DIR, exist_ok=True)

        # 内置模式需要先收集运行时；失败时整体退化为外置模式（保证包可用）
        bundle_ready = False
        if python_mode == PYTHON_MODE_BUNDLE:
            supported = python_bundle_targets(targets)
            if not supported:
                print("[Python] 目标平台不包含 windows-amd64，内置模式不可用，"
                      "全部目标改用外置自动下载模式")
                python_mode = PYTHON_MODE_EXTERNAL
            else:
                unsupported = [key for key in targets if key not in supported]
                if unsupported:
                    print("[Python] 内置模式当前仅支持 windows-amd64，"
                          f"{', '.join(unsupported)} 将使用外置自动下载模式")
                if collect_python_runtime():
                    bundle_ready = True
                else:
                    print("[WARN] 内置 Python 运行时构建失败，全部目标改用外置自动下载模式")
                    python_mode = PYTHON_MODE_EXTERNAL

        # 逐平台编译 + 打包
        print("\n[3/4] 交叉编译 Go 二进制...")
        generate_agents_md_embed()
        packages = []
        for platform_key in targets:
            cfg = PLATFORMS[platform_key]

            if args.skip_build:
                binary_path = os.path.join(PROJECT_ROOT, APP_NAME + cfg["ext"])
                if not os.path.isfile(binary_path):
                    print(f"  [FAIL] 未找到已有二进制: {binary_path}")
                    sys.exit(1)
                print(f"  使用已有二进制: {APP_NAME + cfg['ext']}")
            else:
                binary_path = compile_go(platform_key)

            print(f"\n[4/4] 打包 {platform_key} ...")
            zip_path = create_package(
                platform_key, binary_path, db_path,
                include_skills=include_skills,
                python_mode=platform_python_mode(platform_key, python_mode, bundle_ready))
            packages.append(zip_path)

            if not args.skip_build and os.path.isfile(binary_path):
                os.remove(binary_path)

        print("\n" + "=" * 55)
        print("  打包完成! 发行包列表:")
        print("=" * 55)
        for zp in packages:
            size_mb = os.path.getsize(zp) / 1024 / 1024
            print(f"    {os.path.basename(zp):40s} {size_mb:6.1f} MB")
        print(f"\n  输出目录: {DIST_PACK_DIR}")
        print("  说明: 每个包内 PYTHON-RUNTIME.md 标注了当前模式与两种模式差异")
        print()

    finally:
        cleanup_fresh_db(tmp_dir)
        if PY_BUNDLE_AVAILABLE:
            cleanup_python_bundle()


if __name__ == "__main__":
    main()
