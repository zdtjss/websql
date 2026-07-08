#!/usr/bin/env python3
"""
WebSQL Desktop Build Script (Wails v3)
Usage: python build_desktop.py [--skip-frontend]

跨平台构建桌面版 WebSQL（Windows / Linux / macOS），基于 Wails v3。
自动识别当前平台并生成对应可执行文件；Windows 额外内嵌图标/清单/版本信息。
单实例检测、免登录等行为由 main.go 保证，三平台一致。

产物目录结构 (release-desktop/):
  WebSQL[.exe]        主程序
  config.json         配置（isRemote=false）
  nway.sqlite3.db     数据库
  favicon.ico         图标
  static/             前端静态文件（Gin 从 ./static/ 提供服务）
    index.html
    assets/...
  skills/             AI 技能文件
  sqlite3-init.sql    初始化脚本
"""

import argparse
import io
import json
import os
import shutil
import sqlite3
import subprocess
import sys
from datetime import datetime

if sys.stdout.encoding != "utf-8":
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

PROJECT_ROOT = os.path.dirname(os.path.abspath(__file__))
WEB_SRC_DIR = os.path.join(PROJECT_ROOT, "web-src")
DIST_DIR = os.path.join(WEB_SRC_DIR, "dist")
SQLITE_INIT_SQL = os.path.join(PROJECT_ROOT, "sqlite3-init.sql")
CONFIG_FILE = os.path.join(PROJECT_ROOT, "config.json")
SKILLS_DIR = os.path.join(PROJECT_ROOT, "skills")
FAVICON = os.path.join(WEB_SRC_DIR, "public", "favicon.ico")

# 产物输出目录
OUTPUT_DIR = os.path.join(PROJECT_ROOT, "release-desktop")

APP_NAME = "WebSQL"
DB_NAME = "nway.sqlite3.db"
VERSION = datetime.now().strftime("%Y%m%d%H%M")

# 平台识别：windows / linux / darwin（按当前主机，CGO 交叉编译受限，故只构建本机平台）
PLATFORM = "windows" if sys.platform == "win32" else sys.platform
# 可执行文件名：Windows 带 .exe，Linux/macOS 无后缀
BINARY_NAME = APP_NAME + ".exe" if PLATFORM == "windows" else APP_NAME


def run(cmd, cwd=None, env=None):
    merged_env = {**os.environ, **(env or {})}
    print(f"  > {cmd}")
    proc = subprocess.run(cmd, shell=True, cwd=cwd, env=merged_env)
    if proc.returncode != 0:
        print(f"  [FAIL] 命令执行失败: {cmd}")
        sys.exit(1)


def build_frontend():
    print("\n[1/5] 构建前端...")
    if not os.path.isdir(os.path.join(WEB_SRC_DIR, "node_modules")):
        print("  安装 npm 依赖...")
        run("npm install", cwd=WEB_SRC_DIR)
    run("npm run build", cwd=WEB_SRC_DIR)
    if not os.path.isdir(DIST_DIR):
        print(f"  [FAIL] 前端构建失败，未找到 {DIST_DIR}")
        sys.exit(1)
    print("  [OK] 前端构建完成")


def copy_frontend_to_static():
    """将前端构建产物复制到产物目录的 static/ 下（Gin 从 ./static/ 提供服务）"""
    print("\n[2/5] 复制前端到 static/ ...")
    static_dir = os.path.join(OUTPUT_DIR, "static")
    if os.path.isdir(static_dir):
        shutil.rmtree(static_dir)
    shutil.copytree(DIST_DIR, static_dir)
    print(f"  [OK] 前端已复制到 {static_dir}")


def create_fresh_db():
    print("\n[3/5] 创建全新数据库...")
    if not os.path.isfile(SQLITE_INIT_SQL):
        print(f"  [FAIL] 未找到 {SQLITE_INIT_SQL}")
        sys.exit(1)

    db_path = os.path.join(OUTPUT_DIR, DB_NAME)
    if os.path.isfile(db_path):
        os.remove(db_path)

    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA synchronous=NORMAL")

    with open(SQLITE_INIT_SQL, "r", encoding="utf-8") as f:
        sql_script = f.read()

    conn.executescript(sql_script)
    conn.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    conn.execute("PRAGMA journal_mode=DELETE")
    conn.close()

    size_kb = os.path.getsize(db_path) / 1024
    print(f"  [OK] 数据库创建完成: {DB_NAME} ({size_kb:.1f} KB)")


def build_desktop():
    print("\n[4/5] 编译桌面版...")

    binary_path = os.path.join(OUTPUT_DIR, BINARY_NAME)
    # Windows 用 windowsgui 隐藏控制台窗口；Linux/macOS 不需要
    if PLATFORM == "windows":
        ldflags = f"-s -w -H windowsgui -X main.version={VERSION}"
    else:
        ldflags = f"-s -w -X main.version={VERSION}"
    env = {"CGO_ENABLED": "1"}

    syso_path = os.path.join(PROJECT_ROOT, "cmd", "desktop", "desktop.syso")
    # 清理可能残留的上一轮 .syso，避免平台错配被 go build 误链接
    if os.path.isfile(syso_path):
        os.remove(syso_path)

    if PLATFORM == "windows":
        # 生成 Windows 资源（图标/清单/版本信息）为 .syso，go build 会自动链接
        run(
            'wails3 generate syso -arch amd64'
            ' -icon web-src/public/favicon.ico'
            ' -manifest cmd/desktop/build/windows/wails.exe.manifest'
            ' -info cmd/desktop/build/windows/info.json'
            ' -out cmd/desktop/desktop.syso',
            cwd=PROJECT_ROOT,
        )
        if not os.path.isfile(syso_path) or os.path.getsize(syso_path) == 0:
            print("  [FAIL] 生成 .syso 资源失败，二进制将不带图标")
            sys.exit(1)
        print("  [OK] 已生成 desktop.syso（图标/清单/版本信息）")
    else:
        # Linux/macOS 无 .syso 资源机制，窗口图标由 main.go 运行时读取 favicon.ico
        print(f"  [SKIP] {PLATFORM} 平台无需 .syso 资源")

    run(
        f'go build -ldflags "{ldflags}" -o "{binary_path}" ./cmd/desktop/',
        cwd=PROJECT_ROOT,
        env=env,
    )

    if not os.path.isfile(binary_path):
        print(f"  [FAIL] 编译失败，未找到 {binary_path}")
        sys.exit(1)

    size_mb = os.path.getsize(binary_path) / 1024 / 1024
    print(f"  [OK] 编译完成: {BINARY_NAME} ({size_mb:.1f} MB)")


def copy_resources():
    print("\n[5/5] 复制资源文件...")

    # config.json（强制本地模式）
    local_config = os.path.join(OUTPUT_DIR, "config.json")
    if os.path.isfile(CONFIG_FILE):
        with open(CONFIG_FILE, "r", encoding="utf-8") as f:
            cfg = json.load(f)
        cfg["isRemote"] = False
        with open(local_config, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=2)
        print("  [OK] config.json (isRemote=false)")

    # favicon.ico
    if os.path.isfile(FAVICON):
        shutil.copy2(FAVICON, os.path.join(OUTPUT_DIR, "favicon.ico"))
        print("  [OK] favicon.ico")

    # skills 目录
    if os.path.isdir(SKILLS_DIR):
        dest_skills = os.path.join(OUTPUT_DIR, "skills")
        if os.path.isdir(dest_skills):
            shutil.rmtree(dest_skills)
        shutil.copytree(SKILLS_DIR, dest_skills)
        print("  [OK] skills/")

    # SQL init files
    for sql_file in ["sqlite3-init.sql", "mysql-init.sql"]:
        src = os.path.join(PROJECT_ROOT, sql_file)
        if os.path.isfile(src):
            shutil.copy2(src, os.path.join(OUTPUT_DIR, sql_file))
            print(f"  [OK] {sql_file}")


def main():
    parser = argparse.ArgumentParser(description="WebSQL 桌面版构建脚本")
    parser.add_argument("--skip-frontend", action="store_true", help="跳过前端构建")
    args = parser.parse_args()

    print("=" * 50)
    print("  WebSQL Desktop Build Script (Wails v3)")
    print(f"  Version: {VERSION}")
    print("=" * 50)

    # 清理并创建产物目录
    if os.path.isdir(OUTPUT_DIR):
        shutil.rmtree(OUTPUT_DIR)
    os.makedirs(OUTPUT_DIR)

    if not args.skip_frontend:
        build_frontend()
    else:
        print("\n[1/5] 跳过前端构建")
        if not os.path.isdir(DIST_DIR):
            print(f"  [FAIL] 未找到前端构建产物 {DIST_DIR}")
            sys.exit(1)

    copy_frontend_to_static()
    copy_resources()
    create_fresh_db()
    build_desktop()

    print("\n" + "=" * 50)
    print("  [DONE] 桌面版构建完成！")
    print("=" * 50)
    print(f"  平台: {PLATFORM}")
    print(f"  输出目录: {OUTPUT_DIR}")
    print(f"  可执行文件: {os.path.join(OUTPUT_DIR, BINARY_NAME)}")
    print()
    if PLATFORM == "windows":
        print("  运行方式: 进入 release-desktop/ 目录，双击 WebSQL.exe")
    else:
        print(f"  运行方式: 进入 release-desktop/ 目录，执行 ./{BINARY_NAME}")
    print()


if __name__ == "__main__":
    main()
