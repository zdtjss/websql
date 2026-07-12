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
  python scripts/build_release.py --skip-frontend          # 跳过前端构建
  python scripts/build_release.py --skip-build             # 跳过 Go 编译（使用已有二进制）
  python scripts/build_release.py --skip-db                # 跳过全新数据库创建

产物:
  dist-pack/websql-web-{platform}.zip — 可独立发行、运行的 zip 包
  包内容: 二进制 + static/ + config.json + nway.sqlite3.db +
           migrations/ + db_migrate.py + skills/ + startup 脚本
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

if sys.stdout.encoding != "utf-8":
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DIST_PACK_DIR = os.path.join(PROJECT_ROOT, "dist-pack")
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")
SKILLS_DIR = os.path.join(PROJECT_ROOT, "skills")
CONFIG_FILE = os.path.join(PROJECT_ROOT, "config.json")
DB_MIGRATE_SCRIPT = os.path.join(PROJECT_ROOT, "db_migrate.py")
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

STARTUP_BAT_CONTENT = "\n".join([
    "@ECHO OFF",
    '%1 start mshta vbscript:createobject("wscript.shell").run("""%~0"" ::",0)(window.close)&&exit',
    'cd /d "%~dp0"',
    "tskill WebSql >nul 2>&1",
    'start /b "" WebSql.exe',
]) + "\n"

STARTUP_SH_CONTENT = """\
#!/bin/bash
cd "$(dirname "$0")"
pkill -f WebSql 2>/dev/null || true
sleep 1
chmod +x WebSql
nohup ./WebSql > websql.log 2>&1 &
echo "WebSql started (PID: $!)"
echo "Log file: websql.log"
"""


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
    conn.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    conn.execute("PRAGMA journal_mode=DELETE")
    conn.close()

    size_kb = os.path.getsize(db_path) / 1024
    print(f"  [OK] 数据库创建完成: {DB_NAME} ({size_kb:.1f} KB)")
    return db_path, tmp_dir


def cleanup_fresh_db(tmp_dir):
    if tmp_dir and os.path.isdir(tmp_dir):
        shutil.rmtree(tmp_dir, ignore_errors=True)


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


def create_package(platform_key, binary_path, db_path):
    """组装独立可运行的 zip 发行包。"""
    cfg = PLATFORMS[platform_key]
    goos, goarch, ext = cfg["goos"], cfg["goarch"], cfg["ext"]

    zip_name = f"websql-web-{platform_key}.zip"
    zip_path = os.path.join(DIST_PACK_DIR, zip_name)

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

        # skills 目录
        if os.path.isdir(SKILLS_DIR):
            for root, dirs, files in os.walk(SKILLS_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    arcname = os.path.join("skills", os.path.relpath(file_path, SKILLS_DIR))
                    zipf.write(file_path, arcname)

        # 启动脚本
        if goos == "windows":
            zipf.writestr("startup.bat", STARTUP_BAT_CONTENT)
        else:
            zipf.writestr("startup.sh", STARTUP_SH_CONTENT)

    size_mb = os.path.getsize(zip_path) / 1024 / 1024
    print(f"  [OK] 打包完成: {zip_name} ({size_mb:.1f} MB)")
    return zip_path


def main():
    parser = argparse.ArgumentParser(description="WebSQL Web 版跨平台打包脚本")
    parser.add_argument("--platform", default=None,
                        help="目标平台 (windows|linux|macos|windows-amd64|linux-amd64|macos-amd64|macos-arm64|all)")
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

        # 逐平台编译 + 打包
        print("\n[3/4] 交叉编译 Go 二进制...")
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
            zip_path = create_package(platform_key, binary_path, db_path)
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
        print()

    finally:
        cleanup_fresh_db(tmp_dir)


if __name__ == "__main__":
    main()
