#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Web 版打包两种 Python 运行时模式与外置引导脚本的自动化测试

覆盖范围:
  1. 内置模式（bundle）：zip 内含 python/ 运行时、启动脚本不触发安装
  2. 外置模式（external）：zip 内含全平台安装脚本与模式说明文档
     - Windows: setup-python.bat + setup-python.ps1
     - Linux/macOS: setup-python.sh（可执行位）
  3. 内置不可用 / 非 Windows 目标时的降级逻辑
  4. python_bootstrap 的平台三元组、版本校验、清单与环境文件、--check 运行
  5. 安装脚本与引导脚本的版本号一致性（防止两处定义漂移）

运行方式:
  python -m unittest scripts.test_build_release -v
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
import zipfile
from unittest import mock

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(SCRIPT_DIR)
if SCRIPT_DIR not in sys.path:
    sys.path.insert(0, SCRIPT_DIR)

import build_release  # noqa: E402
import python_bootstrap  # noqa: E402

BOOTSTRAP_PATH = os.path.join(SCRIPT_DIR, "python_bootstrap.py")
SETUP_BAT_PATH = os.path.join(SCRIPT_DIR, "setup-python.bat")
SETUP_PS1_PATH = os.path.join(SCRIPT_DIR, "setup-python.ps1")
SETUP_SH_PATH = os.path.join(SCRIPT_DIR, "setup-python.sh")


class _PackageFixture(unittest.TestCase):
    """为 create_package 准备隔离的临时项目目录，并替换模块内的路径常量。"""

    PATCHED_NAMES = (
        "PROJECT_ROOT", "DIST_PACK_DIR", "DIST_DIR", "SKILLS_DIR",
        "CONFIG_FILE", "DB_MIGRATE_SCRIPT", "MIGRATIONS_DIR",
        "SQLITE_FULL_SQL", "MYSQL_FULL_SQL", "BUNDLE_PYTHON_DIR",
    )

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="websql_pkg_test_")
        self.addCleanup(self.tmp.cleanup)
        root = self.tmp.name
        self.project_root = os.path.join(root, "project")
        self.pack_dir = os.path.join(root, "dist-pack")
        self.static_dir = os.path.join(root, "static-build")
        os.makedirs(self.project_root)
        os.makedirs(self.pack_dir)
        os.makedirs(self.static_dir)

        self._write(self.project_root, "config.json", "{}")
        self._write(self.project_root, "db_migrate.py", "print('migrate')\n")
        self._write(self.project_root, "AGENTS.md", "# agents\n")
        self._write(self.project_root, "migrations/sqlite/0001_baseline.sql", "-- baseline\n")
        self._write(self.project_root, "migrations/full/sqlite_full.sql", "-- sqlite\n")
        self._write(self.project_root, "migrations/full/mysql_full.sql", "-- mysql\n")
        self._write(self.project_root, "skills/demo/SKILL.md", "# demo\n")
        self._write(self.static_dir, "index.html", "<html></html>\n")

        self.binary = os.path.join(root, "WebSql.exe")
        with open(self.binary, "wb") as handle:
            handle.write(b"stub-binary")
        self.db_path = os.path.join(root, "nway.sqlite3.db")
        with open(self.db_path, "wb") as handle:
            handle.write(b"stub-db")

        self.bundle_dir = os.path.join(root, "bundle-python")
        self._saved = {}
        replacements = {
            "PROJECT_ROOT": self.project_root,
            "DIST_PACK_DIR": self.pack_dir,
            "DIST_DIR": self.static_dir,
            "SKILLS_DIR": os.path.join(self.project_root, "skills"),
            "CONFIG_FILE": os.path.join(self.project_root, "config.json"),
            "DB_MIGRATE_SCRIPT": os.path.join(self.project_root, "db_migrate.py"),
            "MIGRATIONS_DIR": os.path.join(self.project_root, "migrations"),
            "SQLITE_FULL_SQL": os.path.join(self.project_root, "migrations", "full", "sqlite_full.sql"),
            "MYSQL_FULL_SQL": os.path.join(self.project_root, "migrations", "full", "mysql_full.sql"),
            "BUNDLE_PYTHON_DIR": self.bundle_dir,
        }
        for name, value in replacements.items():
            self._saved[name] = getattr(build_release, name)
            setattr(build_release, name, value)

    def tearDown(self):
        for name, value in self._saved.items():
            setattr(build_release, name, value)

    def _write(self, base, relative, content):
        path = os.path.join(base, relative)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(content)
        return path

    def _package(self, platform_key, python_mode):
        return build_release.create_package(
            platform_key, self.binary, self.db_path,
            include_skills=True, python_mode=python_mode)

    def _create_bundle_runtime(self):
        os.makedirs(self.bundle_dir, exist_ok=True)
        self._write(self.bundle_dir, "python.exe", "stub-runtime")
        self._write(self.bundle_dir, "Lib/site-packages/pptx/__init__.py", "")


class ExternalModePackageTest(_PackageFixture):
    def test_windows_package_contains_installer_not_runtime(self):
        zip_path = self._package("windows-amd64", build_release.PYTHON_MODE_EXTERNAL)
        with zipfile.ZipFile(zip_path) as archive:
            names = set(archive.namelist())
            self.assertIn("python_bootstrap.py", names)
            self.assertIn("setup-python.bat", names)
            self.assertIn("setup-python.ps1", names)
            self.assertIn("PYTHON-RUNTIME.md", names)
            self.assertNotIn("python/python.exe", names)
            self.assertFalse([name for name in names if name.startswith("python/")])

            startup = archive.read("startup.bat").decode("utf-8")
            self.assertIn("setup-python.bat\" --ensure", startup)
            self.assertIn("python-env.bat", startup)

            startup_raw = archive.read("startup.bat")
            self.assertIn(b"\r\n", startup_raw, "startup.bat 必须使用 CRLF 换行")
            self.assertIn(b"\r\n", archive.read("setup-python.bat"),
                          "setup-python.bat 必须使用 CRLF 换行")
            self.assertTrue(archive.read("setup-python.ps1").startswith(b"\xef\xbb\xbf"),
                            "setup-python.ps1 必须带 UTF-8 BOM")

            doc = archive.read("PYTHON-RUNTIME.md").decode("utf-8")
            self.assertIn("内置模式（bundle", doc)
            self.assertIn("外置模式（external", doc)
            self.assertIn(python_bootstrap.PYTHON_FULL_VERSION, doc)

    def test_unix_package_contains_shell_installer_with_exec_bit(self):
        zip_path = self._package("macos-arm64", build_release.PYTHON_MODE_EXTERNAL)
        with zipfile.ZipFile(zip_path) as archive:
            names = set(archive.namelist())
            self.assertIn("python_bootstrap.py", names)
            self.assertIn("setup-python.sh", names)
            self.assertIn("startup.sh", names)
            self.assertNotIn("setup-python.bat", names)
            self.assertNotIn("setup-python.ps1", names)

            info = archive.getinfo("setup-python.sh")
            mode = (info.external_attr >> 16) & 0o777
            self.assertTrue(mode & 0o111, "setup-python.sh 缺少可执行权限位")
            self.assertNotIn(b"\r\n", archive.read("setup-python.sh"),
                             "setup-python.sh 必须使用 LF 换行")

            startup = archive.read("startup.sh").decode("utf-8")
            self.assertIn("./setup-python.sh --ensure", startup)
            self.assertIn("python-env.sh", startup)

    def test_linux_package_uses_shell_installer(self):
        zip_path = self._package("linux-amd64", build_release.PYTHON_MODE_EXTERNAL)
        with zipfile.ZipFile(zip_path) as archive:
            names = set(archive.namelist())
            self.assertIn("setup-python.sh", names)
            self.assertIn("startup.sh", names)
            self.assertNotIn("setup-python.bat", names)
            doc = archive.read("PYTHON-RUNTIME.md").decode("utf-8")
            self.assertIn("Linux / macOS 全平台", doc)

    def test_bundle_mode_falls_back_to_external_when_runtime_missing(self):
        zip_path = self._package("windows-amd64", build_release.PYTHON_MODE_BUNDLE)
        with zipfile.ZipFile(zip_path) as archive:
            names = set(archive.namelist())
            self.assertIn("setup-python.bat", names)
            self.assertNotIn("python/python.exe", names)
            doc = archive.read("PYTHON-RUNTIME.md").decode("utf-8")
            self.assertIn("外置自动下载（external）", doc)
            startup = archive.read("startup.bat").decode("utf-8")
            self.assertIn("--ensure", startup)


class BundleModePackageTest(_PackageFixture):
    def test_windows_package_contains_runtime_and_skips_installer(self):
        self._create_bundle_runtime()
        zip_path = self._package("windows-amd64", build_release.PYTHON_MODE_BUNDLE)
        with zipfile.ZipFile(zip_path) as archive:
            names = set(archive.namelist())
            self.assertIn("python/python.exe", names)
            self.assertIn("python/Lib/site-packages/pptx/__init__.py", names)
            self.assertNotIn("setup-python.bat", names)
            self.assertNotIn("python_bootstrap.py", names)

            startup = archive.read("startup.bat").decode("utf-8")
            self.assertNotIn("--ensure", startup)

            doc = archive.read("PYTHON-RUNTIME.md").decode("utf-8")
            self.assertIn("内置 Python 运行时（bundle）", doc)
            self.assertIn("外置模式（external", doc)


class PackagedRuntimeFlowTest(_PackageFixture):
    """在解压后的真实发行包目录内执行检测/启动脚本，覆盖“安装包 → 启动”链路。"""

    def _current_platform_key(self):
        import platform as platform_module
        if sys.platform == "win32":
            return "windows-amd64"
        if sys.platform == "darwin":
            machine = platform_module.machine().lower()
            return "macos-arm64" if machine in ("arm64", "aarch64") else "macos-amd64"
        return "linux-amd64"

    def test_extracted_external_package_runs_runtime_check(self):
        platform_key = self._current_platform_key()
        zip_path = self._package(platform_key, build_release.PYTHON_MODE_EXTERNAL)
        extract_dir = os.path.join(self.tmp.name, "extracted")
        os.makedirs(extract_dir)
        with zipfile.ZipFile(zip_path) as archive:
            archive.extractall(extract_dir)

        result = subprocess.run(
            [sys.executable, os.path.join(extract_dir, "python_bootstrap.py"),
             "--check", "--root", extract_dir],
            capture_output=True, text=True, timeout=180,
            env={**os.environ, "PYTHONIOENCODING": "utf-8"})
        self.assertIn(result.returncode, (0, 1),
                      f"发行包内引导脚本执行异常: {result.stderr}")
        self.assertIn("状态", result.stdout)

        # 启动脚本必须引用包内实际存在的安装脚本（无路径错误）
        if platform_key == "windows-amd64":
            launcher = os.path.join(extract_dir, "setup-python.bat")
            startup = os.path.join(extract_dir, "startup.bat")
            launcher_cmd = [os.environ.get("COMSPEC", "cmd.exe"), "/c", launcher, "--check"]
        else:
            launcher = os.path.join(extract_dir, "setup-python.sh")
            startup = os.path.join(extract_dir, "startup.sh")
            launcher_cmd = ["sh", launcher, "--check"]
        self.assertTrue(os.path.isfile(launcher), "发行包缺少安装脚本")
        self.assertTrue(os.path.isfile(startup), "发行包缺少启动脚本")
        with open(startup, "r", encoding="utf-8") as handle:
            content = handle.read()
        self.assertIn(os.path.basename(launcher), content)

        # 直接执行包内的平台安装脚本（--check 不下载），验证脚本语法与路径
        launcher_result = subprocess.run(
            launcher_cmd, capture_output=True, text=True, timeout=180,
            env={**os.environ, "PYTHONIOENCODING": "utf-8"})
        self.assertIn(launcher_result.returncode, (0, 1),
                      f"包内安装脚本执行异常: {launcher_result.stderr}")

    def test_extracted_bundle_package_contains_self_contained_runtime_layout(self):
        self._create_bundle_runtime()
        zip_path = self._package("windows-amd64", build_release.PYTHON_MODE_BUNDLE)
        extract_dir = os.path.join(self.tmp.name, "extracted-bundle")
        os.makedirs(extract_dir)
        with zipfile.ZipFile(zip_path) as archive:
            archive.extractall(extract_dir)
        # 内置模式的运行时位于应用根目录 python/ 下，与应用启动探测约定一致
        runtime_exe = os.path.join(extract_dir, "python", "python.exe")
        self.assertTrue(os.path.isfile(runtime_exe))
        self.assertFalse(os.path.exists(os.path.join(extract_dir, "setup-python.bat")))


class PythonModeSelectionTest(unittest.TestCase):
    def test_explicit_mode_wins(self):
        self.assertEqual(build_release.resolve_python_mode("bundle"), "bundle")
        self.assertEqual(build_release.resolve_python_mode("external"), "external")

    def test_interactive_defaults_to_external_without_tty(self):
        with mock.patch("builtins.input", side_effect=EOFError):
            self.assertEqual(build_release.resolve_python_mode("ask"), "external")

    def test_interactive_choice_bundle(self):
        with mock.patch("builtins.input", return_value="2"):
            self.assertEqual(build_release.resolve_python_mode("ask"), "bundle")

    def test_per_platform_mode_matches_bundle_support(self):
        self.assertEqual(
            build_release.platform_python_mode("windows-amd64", "bundle", True), "bundle")
        self.assertEqual(
            build_release.platform_python_mode("linux-amd64", "bundle", True), "external")
        self.assertEqual(
            build_release.platform_python_mode("windows-amd64", "bundle", False), "external")
        self.assertEqual(
            build_release.platform_python_mode("windows-amd64", "external", True), "external")

    def test_bundle_targets_only_windows(self):
        targets = list(build_release.PLATFORMS.keys())
        self.assertEqual(build_release.python_bundle_targets(targets), ["windows-amd64"])


class PythonBootstrapUnitTest(unittest.TestCase):
    def test_target_triples_all_supported_platforms(self):
        cases = [
            ("Windows", "AMD64", "x86_64-pc-windows-msvc"),
            ("Windows", "ARM64", "x86_64-pc-windows-msvc"),
            ("Linux", "x86_64", "x86_64-unknown-linux-gnu"),
            ("Linux", "aarch64", "aarch64-unknown-linux-gnu"),
            ("Darwin", "x86_64", "x86_64-apple-darwin"),
            ("Darwin", "arm64", "aarch64-apple-darwin"),
        ]
        for system, machine, expected in cases:
            self.assertEqual(python_bootstrap.target_triple(system, machine), expected,
                             f"{system}/{machine} 三元组不符")
        self.assertIsNone(python_bootstrap.target_triple("FreeBSD", "x86_64"))
        self.assertIsNone(python_bootstrap.target_triple("Linux", "i386"))

    def test_runtime_asset_and_url(self):
        triple = "x86_64-pc-windows-msvc"
        asset = python_bootstrap.runtime_asset_name(
            python_bootstrap.PYTHON_FULL_VERSION,
            python_bootstrap.STANDALONE_RELEASE, triple)
        self.assertEqual(
            asset,
            "cpython-3.15.2+20261026-x86_64-pc-windows-msvc-install_only.tar.gz")
        url = python_bootstrap.runtime_url(
            python_bootstrap.PYTHON_FULL_VERSION,
            python_bootstrap.STANDALONE_RELEASE, triple)
        self.assertTrue(url.startswith(python_bootstrap.DOWNLOAD_BASE + "/20261026/"))
        self.assertTrue(url.endswith(asset))

    def test_version_parsing_and_support(self):
        self.assertEqual(python_bootstrap.parse_version("Python 3.15.2"), (3, 15, 2))
        self.assertEqual(python_bootstrap.parse_version("3.10"), (3, 10))
        self.assertEqual(python_bootstrap.parse_version(""), ())
        self.assertTrue(python_bootstrap.version_supported("Python 3.15.2"))
        self.assertTrue(python_bootstrap.version_supported("3.10.0"))
        self.assertFalse(python_bootstrap.version_supported("Python 3.9.13"))
        self.assertFalse(python_bootstrap.version_supported("Python 2.7.18"))

    def test_runtime_executable_layout(self):
        windows = python_bootstrap.runtime_executable("/opt/runtime", system="Windows")
        unix = python_bootstrap.runtime_executable("/opt/runtime", system="Linux")
        self.assertTrue(windows.replace("\\", "/").endswith("python.exe"))
        self.assertTrue(unix.replace("\\", "/").endswith("bin/python3"))

    def test_manifest_and_env_roundtrip(self):
        with tempfile.TemporaryDirectory(prefix="websql_manifest_") as root:
            exe = sys.executable
            manifest = python_bootstrap.write_manifest(root, exe, "3.15.2", "system")
            with open(manifest, "r", encoding="utf-8") as handle:
                data = json.load(handle)
            # Go 端只读取这三个字段，字段名变更会导致启动时找不到解释器
            self.assertIn("pythonPath", data)
            self.assertIn("version", data)
            self.assertIn("source", data)
            self.assertEqual(os.path.abspath(data["pythonPath"]), os.path.abspath(exe))
            self.assertEqual(python_bootstrap.read_manifest_python(root), os.path.abspath(exe))

            bat = python_bootstrap.env_bat_content(exe, "3.15.2")
            self.assertIn("WEBSQL_PYTHON", bat)
            self.assertIn("PYTHONUTF8=1", bat)
            self.assertIn("\r\n", bat, "python-env.bat 必须使用 CRLF 换行")
            sh = python_bootstrap.env_sh_content(exe, "3.15.2")
            self.assertIn("export WEBSQL_PYTHON=", sh)
            self.assertIn("export PATH=", sh)

            written = python_bootstrap.write_env_files(root, exe, "3.15.2")
            for path in written:
                self.assertTrue(os.path.isfile(path))

    def test_check_mode_runs_on_current_interpreter(self):
        with tempfile.TemporaryDirectory(prefix="websql_check_") as root:
            result = subprocess.run(
                [sys.executable, BOOTSTRAP_PATH, "--check", "--root", root],
                capture_output=True, text=True, timeout=180,
                env={**os.environ, "PYTHONIOENCODING": "utf-8"})
            self.assertIn(result.returncode, (0, 1),
                          f"--check 异常退出: {result.returncode}\n{result.stderr}")
            self.assertIn("检测结果", result.stdout)


class InstallerScriptConsistencyTest(unittest.TestCase):
    def test_scripts_exist(self):
        for path in (BOOTSTRAP_PATH, SETUP_BAT_PATH, SETUP_PS1_PATH, SETUP_SH_PATH):
            self.assertTrue(os.path.isfile(path), f"缺少外置模式脚本: {path}")

    def test_windows_script_encodings(self):
        """Windows 脚本需 CRLF；.ps1 需 BOM（PowerShell 5.1 否则按 ANSI 解析中文失败）。"""
        with open(SETUP_BAT_PATH, "rb") as handle:
            bat = handle.read()
        self.assertIn(b"\r\n", bat, "setup-python.bat 必须使用 CRLF")
        with open(SETUP_PS1_PATH, "rb") as handle:
            ps1 = handle.read()
        self.assertTrue(ps1.startswith(b"\xef\xbb\xbf"), "setup-python.ps1 缺少 UTF-8 BOM")
        with open(SETUP_SH_PATH, "rb") as handle:
            shell = handle.read()
        self.assertNotIn(b"\r\n", shell, "setup-python.sh 必须使用 LF")

    def test_shell_and_powershell_share_locked_version(self):
        """Windows/macOS/Linux 的兜底下载脚本必须使用与引导脚本一致的版本。"""
        with open(SETUP_SH_PATH, "r", encoding="utf-8") as handle:
            shell = handle.read()
        with open(SETUP_PS1_PATH, "r", encoding="utf-8") as handle:
            powershell = handle.read()
        for content, name in ((shell, "setup-python.sh"), (powershell, "setup-python.ps1")):
            self.assertIn(python_bootstrap.PYTHON_FULL_VERSION, content,
                          f"{name} 未使用锁定的 Python 版本")
            self.assertIn(python_bootstrap.STANDALONE_RELEASE, content,
                          f"{name} 未使用锁定的运行时发布标签")

    def test_windows_launcher_delegates_to_bootstrap(self):
        with open(SETUP_BAT_PATH, "r", encoding="utf-8") as handle:
            launcher = handle.read()
        self.assertIn("python_bootstrap.py", launcher)
        self.assertIn("setup-python.ps1", launcher)
        self.assertIn("--check", launcher)


if __name__ == "__main__":
    unittest.main(verbosity=2)
