#!/usr/bin/env python3
"""
WebSQL Cross-Platform Build & Package Script
Usage: python build_release.py [--skip-frontend] [--skip-build]

在 Windows 上交叉编译并打包 Windows / Linux / macOS 三平台发行版，
每个平台生成一个独立 zip 压缩包。
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

PROJECT_ROOT = os.path.dirname(os.path.abspath(__file__))
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")
SKILLS_DIR = os.path.join(PROJECT_ROOT, "skills")
CONFIG_FILE = os.path.join(PROJECT_ROOT, "config.json")
SQLITE_FULL_SQL = os.path.join(PROJECT_ROOT, "migrations", "full", "sqlite_full.sql")
MYSQL_FULL_SQL = os.path.join(PROJECT_ROOT, "migrations", "full", "mysql_full.sql")
OUTPUT_DIR = os.path.join(PROJECT_ROOT, "release")

APP_NAME = "WebSql"
DB_NAME = "nway.sqlite3.db"
VERSION = datetime.now().strftime("%Y%m%d%H%M")

BUILD_TARGETS = [
    {"goos": "windows", "goarch": "amd64", "ext": ".exe"},
    {"goos": "linux", "goarch": "amd64", "ext": ""},
    {"goos": "darwin", "goarch": "amd64", "ext": ""},
    {"goos": "darwin", "goarch": "arm64", "ext": ""},
]

EXCLUDE_DIRS = {"__pycache__", ".git", "node_modules"}

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
        print("  [OK] 已清理临时数据库文件")


def compile_go(target):
    goos = target["goos"]
    goarch = target["goarch"]
    ext = target["ext"]
    binary_name = APP_NAME + ext

    env = {
        "CGO_ENABLED": "0",
        "GOOS": goos,
        "GOARCH": goarch,
    }

    ldflags = f"-s -w -X internal/version.Version={VERSION}"

    print(f"  编译 {goos}/{goarch}...")
    run(f"go build -ldflags \"{ldflags}\" -o {binary_name} main.go", cwd=PROJECT_ROOT, env=env)

    binary_path = os.path.join(PROJECT_ROOT, binary_name)
    if not os.path.isfile(binary_path):
        print(f"  [FAIL] 编译失败，未找到 {binary_path}")
        sys.exit(1)

    size_mb = os.path.getsize(binary_path) / 1024 / 1024
    print(f"  [OK] 编译完成: {binary_name} ({size_mb:.1f} MB)")

    return binary_path


def create_package(target, binary_path, db_path):
    goos = target["goos"]
    goarch = target["goarch"]
    ext = target["ext"]

    zip_name = f"{APP_NAME}-{goos}-{goarch}.zip"
    zip_path = os.path.join(OUTPUT_DIR, zip_name)

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zipf:
        zipf.write(binary_path, APP_NAME + ext)

        zipf.write(db_path, DB_NAME)

        if os.path.isfile(SQLITE_FULL_SQL):
            zipf.write(SQLITE_FULL_SQL, "migrations/full/sqlite_full.sql")
        if os.path.isfile(MYSQL_FULL_SQL):
            zipf.write(MYSQL_FULL_SQL, "migrations/full/mysql_full.sql")

        if os.path.isdir(DIST_DIR):
            for root, dirs, files in os.walk(DIST_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    arcname = os.path.join("static", os.path.relpath(file_path, DIST_DIR))
                    zipf.write(file_path, arcname)

        if os.path.isdir(SKILLS_DIR):
            for root, dirs, files in os.walk(SKILLS_DIR):
                dirs[:] = [d for d in dirs if d not in EXCLUDE_DIRS]
                for f in files:
                    file_path = os.path.join(root, f)
                    arcname = os.path.join("skills", os.path.relpath(file_path, SKILLS_DIR))
                    zipf.write(file_path, arcname)

        if os.path.isfile(CONFIG_FILE):
            zipf.write(CONFIG_FILE, "config.json")

        if goos == "windows":
            zipf.writestr("startup.bat", STARTUP_BAT_CONTENT)
        else:
            zipf.writestr("startup.sh", STARTUP_SH_CONTENT)

    size_mb = os.path.getsize(zip_path) / 1024 / 1024
    print(f"  [OK] 打包完成: {zip_name} ({size_mb:.1f} MB)")

    return zip_path


def main():
    parser = argparse.ArgumentParser(description="WebSQL 跨平台编译打包脚本")
    parser.add_argument("--skip-frontend", action="store_true", help="跳过前端构建")
    parser.add_argument("--skip-build", action="store_true", help="跳过Go编译（使用已有二进制）")
    args = parser.parse_args()

    print("=" * 50)
    print("  WebSQL Cross-Platform Build Script")
    print(f"  Version: {VERSION}")
    print("=" * 50)

    if not args.skip_frontend:
        build_frontend()
    else:
        print("\n[1/4] 跳过前端构建")
        if not os.path.isdir(DIST_DIR):
            print(f"  [FAIL] 未找到前端构建产物 {DIST_DIR}，请先构建前端或去掉 --skip-frontend")
            sys.exit(1)

    db_path, tmp_dir = create_fresh_db()

    try:
        if os.path.isdir(OUTPUT_DIR):
            shutil.rmtree(OUTPUT_DIR)
        os.makedirs(OUTPUT_DIR)

        print("\n[3/4] 交叉编译 Go 二进制...")
        packages = []
        for target in BUILD_TARGETS:
            if args.skip_build:
                ext = target["ext"]
                binary_path = os.path.join(PROJECT_ROOT, APP_NAME + ext)
                if not os.path.isfile(binary_path):
                    print(f"  [FAIL] 未找到已有二进制: {binary_path}")
                    sys.exit(1)
                print(f"  使用已有二进制: {APP_NAME + ext}")
            else:
                binary_path = compile_go(target)

            print("\n[4/4] 打包发行文件...")
            zip_path = create_package(target, binary_path, db_path)
            packages.append(zip_path)

            if not args.skip_build:
                os.remove(binary_path)

        print("\n" + "=" * 50)
        print("  [DONE] 全部完成！发行包列表：")
        print("=" * 50)
        for zp in packages:
            size_mb = os.path.getsize(zp) / 1024 / 1024
            print(f"    {os.path.basename(zp):40s} {size_mb:6.1f} MB")
        print(f"\n  输出目录: {OUTPUT_DIR}")
        print()
    finally:
        cleanup_fresh_db(tmp_dir)


if __name__ == "__main__":
    main()
