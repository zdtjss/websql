@ECHO OFF
REM ═══════════════════════════════════════════════════════════════════
REM  WebSQL Python 运行时自动安装脚本（Windows）
REM  由 scripts/build_release.py 的外置模式生成到发行包根目录。
REM
REM  用法:
REM    setup-python.bat            安装/修复（幂等）
REM    setup-python.bat --ensure   供 startup.bat 调用，检查并按需安装
REM    setup-python.bat --check    仅检测版本与依赖
REM    setup-python.bat --force    删除托管运行时后重新下载
REM ═══════════════════════════════════════════════════════════════════
setlocal
chcp 65001 >nul 2>&1
cd /d "%~dp0"

set "BOOTSTRAP=%~dp0python_bootstrap.py"
if not exist "%BOOTSTRAP%" (
    echo [Python][ERROR] 发行包缺少 python_bootstrap.py，请重新获取完整安装包
    exit /b 1
)

REM ── 1) 托管运行时（随包或此前自动下载） ──
if exist "%~dp0python\python.exe" goto :try_managed

REM ── 2) PyLauncher ──
where py >nul 2>&1
if not errorlevel 1 goto :try_py

REM ── 3) PATH 中的 python ──
where python >nul 2>&1
if not errorlevel 1 goto :try_python

goto :download

:try_managed
"%~dp0python\python.exe" -c "import sys;raise SystemExit(0 if sys.version_info >= (3, 8) else 1)" >nul 2>&1
if errorlevel 1 goto :download
"%~dp0python\python.exe" "%BOOTSTRAP%" %*
exit /b %errorlevel%

:try_py
py -3 -c "import sys;raise SystemExit(0 if sys.version_info >= (3, 8) else 1)" >nul 2>&1
if errorlevel 1 goto :try_python
py -3 "%BOOTSTRAP%" %*
if not errorlevel 1 exit /b 0
goto :try_python

:try_python
python -c "import sys;raise SystemExit(0 if sys.version_info >= (3, 8) else 1)" >nul 2>&1
if errorlevel 1 goto :download
python "%BOOTSTRAP%" %*
if not errorlevel 1 exit /b 0

:download
REM ── 4) 没有任何可用 Python：先下载独立运行时，再交给引导脚本装依赖 ──
echo [Python] 未检测到可用 Python（要求 3.8 及以上），正在准备独立运行时...
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0setup-python.ps1" -BootstrapArgs "%*"
exit /b %errorlevel%
