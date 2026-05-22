#!/usr/bin/env python3
"""
WebSQL Linux Cross-Compile and Deploy Script (Optimized)
Usage: python deploy-linux.py [--host user@ip] [--path /opt/WebSQL2] [--password xxx]

密码优先级: 命令行参数 > 环境变量 DEPLOY_PASSWORD > 交互输入
"""

import argparse
import getpass
import os
import subprocess
import sys
import zipfile
import tempfile
import shutil

try:
    import paramiko
    from scp import SCPClient
except ImportError:
    print("需要安装依赖: pip install paramiko scp")
    sys.exit(1)

# ── 默认配置 ──
DEFAULT_HOST = "root@180.184.30.223"
DEFAULT_PATH = "/opt/WebSQL2"

# ── 可在此处直接填写密码（仅限本地开发，不要提交到仓库） ──
DEPLOY_PASSWORD = ""


def parse_args():
    parser = argparse.ArgumentParser(description="WebSQL Linux 部署脚本")
    parser.add_argument("--host", default=DEFAULT_HOST, help=f"远程主机 (默认: {DEFAULT_HOST})")
    parser.add_argument("--path", default=DEFAULT_PATH, help=f"远程路径 (默认: {DEFAULT_PATH})")
    parser.add_argument("--password", default="", help="SSH 密码 (也可通过环境变量 DEPLOY_PASSWORD 设置)")
    return parser.parse_args()


def get_password(cli_password):
    """按优先级获取密码: 命令行 > 脚本变量 > 环境变量 > 交互输入"""
    if cli_password:
        return cli_password
    if DEPLOY_PASSWORD:
        return DEPLOY_PASSWORD
    env_pwd = os.environ.get("DEPLOY_PASSWORD", "")
    if env_pwd:
        return env_pwd
    return getpass.getpass("请输入 SSH 密码: ")


def run_local(cmd, cwd=None, env=None):
    """执行本地命令，实时输出"""
    merged_env = {**os.environ, **(env or {})}
    proc = subprocess.run(cmd, shell=True, cwd=cwd, env=merged_env)
    if proc.returncode != 0:
        print(f"  ✗ 命令失败: {cmd}")
        sys.exit(1)


def create_ssh_client(host, password):
    """创建 SSH 连接（全程复用）"""
    if "@" in host:
        user, hostname = host.split("@", 1)
    else:
        user, hostname = "root", host

    # 解析端口
    port = 22
    if ":" in hostname:
        hostname, port_str = hostname.rsplit(":", 1)
        port = int(port_str)

    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(hostname, port=port, username=user, password=password, timeout=15)
    return client


def ssh_exec(client, cmd, check=True):
    """通过已有连接执行远程命令"""
    print(f"  $ {cmd}")
    _, stdout, stderr = client.exec_command(cmd)
    exit_code = stdout.channel.recv_exit_status()
    out = stdout.read().decode().strip()
    err = stderr.read().decode().strip()
    if out:
        print(f"    {out}")
    if err and exit_code != 0:
        print(f"    [stderr] {err}")
    if check and exit_code != 0:
        print(f"  ✗ 远程命令失败 (exit {exit_code})")
        sys.exit(1)
    return exit_code, out


def create_deploy_zip(binary_path, dist_dir, skills_dir=None):
    """创建部署包 zip 文件"""
    zip_path = os.path.join(tempfile.gettempdir(), "websql-deploy.zip")
    
    with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as zipf:
        # 添加二进制文件
        zipf.write(binary_path, "WebSql")
        print(f"  添加 WebSql 二进制文件")
        
        # 添加前端文件（去掉 dist 前缀）
        for root, dirs, files in os.walk(dist_dir):
            for file in files:
                file_path = os.path.join(root, file)
                arcname = os.path.join("static", os.path.relpath(file_path, dist_dir))
                zipf.write(file_path, arcname)
        print(f"  添加前端文件目录 (static/)")
        
        # 添加 skills 目录（如果存在）
        if skills_dir and os.path.isdir(skills_dir):
            for root, dirs, files in os.walk(skills_dir):
                for file in files:
                    file_path = os.path.join(root, file)
                    arcname = os.path.join("skills", os.path.relpath(file_path, skills_dir))
                    zipf.write(file_path, arcname)
            print(f"  添加 skills 目录")
    
    return zip_path


def main():
    args = parse_args()
    project_root = os.path.dirname(os.path.abspath(__file__))
    web_src = os.path.join(project_root, "web-src")
    dist_dir = os.path.join(web_src, "dist")
    binary_path = os.path.join(project_root, "WebSql")
    skills_dir = os.path.join(project_root, "skills")

    print("=" * 40)
    print("  WebSQL Linux Deployment Script (Optimized)")
    print("=" * 40)
    print()

    # 获取密码（只输入一次）
    password = get_password(args.password)

    # Step 1: Build frontend
    print("[1/4] 构建前端...")
    if not os.path.isdir(os.path.join(web_src, "node_modules")):
        print("  安装 npm 依赖...")
        run_local("npm install", cwd=web_src)
    run_local("npm run build-only", cwd=web_src)
    print("  ✓ 前端构建完成")

    # Step 2: Cross-compile Go binary
    print()
    print("[2/4] 交叉编译 Go 二进制...")
    run_local("go build -o WebSql main.go", cwd=project_root, env={"GOOS": "linux", "GOARCH": "amd64"})
    print("  ✓ Go 编译完成")

    # Step 2.5: 打包成 zip
    print()
    print("[2.5/4] 打包部署文件...")
    zip_path = create_deploy_zip(binary_path, dist_dir, skills_dir)
    zip_size = os.path.getsize(zip_path)
    print(f"  ✓ 打包完成: {zip_path} ({zip_size / 1024 / 1024:.2f} MB)")

    # Step 3: Deploy via SSH (单次连接，优化目录操作)
    print()
    print(f"[3/4] 部署到远程服务器 ({args.host}:{args.path})...")
    client = create_ssh_client(args.host, password)

    try:
        # 1. 创建临时目录和解压
        remote_zip_path = f"/tmp/websql-deploy-{os.getpid()}.zip"
        
        # 2. 上传 zip 文件
        print("  上传部署包...")
        with SCPClient(client.get_transport()) as scp:
            scp.put(zip_path, remote_path=remote_zip_path)
        print("  ✓ 上传完成")

        # 3. 停止服务
        ssh_exec(client, "systemctl stop websql || true", check=False)

        # 4. 备份重要文件
        print("  备份数据库和配置文件...")
        backup_dir = f"/tmp/websql-backup-{os.getpid()}"
        ssh_exec(client, f"mkdir -p {backup_dir}", check=False)
        ssh_exec(client, f"cp -f {args.path}/nway.sqlite3.db {backup_dir}/ 2>/dev/null || true", check=False)
        ssh_exec(client, f"cp -f {args.path}/config.json {backup_dir}/ 2>/dev/null || true", check=False)

        # 5. 清理旧文件并创建目录
        ssh_exec(client, f"mkdir -p {args.path}", check=False)
        ssh_exec(client, f"rm -rf {args.path}/*", check=False)

        # 6. 解压到目标目录
        print("  解压部署包...")
        ssh_exec(client, f"unzip -o {remote_zip_path} -d {args.path}")
        
        # 6.5. 赋予执行权限
        ssh_exec(client, f"chmod +x {args.path}/WebSql")
        print("  ✓ 已赋予执行权限")
        
        # 7. 恢复备份文件
        print("  恢复数据库和配置文件...")
        ssh_exec(client, f"cp -f {backup_dir}/nway.sqlite3.db {args.path}/ 2>/dev/null || true", check=False)
        ssh_exec(client, f"cp -f {backup_dir}/config.json {args.path}/ 2>/dev/null || true", check=False)
        ssh_exec(client, f"rm -rf {backup_dir}", check=False)
        
        # 8. 清理临时 zip 文件
        ssh_exec(client, f"rm -f {remote_zip_path}", check=False)

        # 9. 数据库表结构自动对比与升级
        print("  同步数据库表结构...")
        local_sql = os.path.join(project_root, "sqlite3-init.sql")
        local_migrate = os.path.join(project_root, "db_migrate.py")
        if os.path.exists(local_sql):
            remote_sql = f"/tmp/websql-init-{os.getpid()}.sql"
            remote_migrate = f"/tmp/websql-migrate-{os.getpid()}.py"
            remote_backup_dir = f"{args.path}/db_backups"

            with SCPClient(client.get_transport()) as scp:
                scp.put(local_sql, remote_path=remote_sql)
                scp.put(local_migrate, remote_path=remote_migrate)

            ssh_exec(client, f"mkdir -p {remote_backup_dir}", check=False)
            exit_code, output = ssh_exec(
                client,
                f"python3 {remote_migrate} --db {args.path}/nway.sqlite3.db --sql {remote_sql} --backup-dir {remote_backup_dir}",
                check=False
            )

            ssh_exec(client, f"rm -f {remote_sql} {remote_migrate}", check=False)

            if exit_code != 0:
                print(f"  [警告] 数据库表结构同步未完全成功 (exit {exit_code})，请检查远程日志")
            else:
                print("  ✓ 数据库表结构同步完成")
        else:
            print(f"  [警告] 未找到 {local_sql}，跳过表结构同步")

        print("  ✓ 文件部署完成")

        # Step 4: Restart service
        print()
        print("[4/4] 重启远程服务...")
        ssh_exec(client, "systemctl restart websql")
        print("  ✓ 服务已重启")
    finally:
        client.close()

    # Cleanup local files
    if os.path.exists(binary_path):
        os.remove(binary_path)
    if os.path.exists(zip_path):
        os.remove(zip_path)

    print()
    print("=" * 40)
    print("  ✓ 部署完成!")
    print("=" * 40)


if __name__ == "__main__":
    main()
