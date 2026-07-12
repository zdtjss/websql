@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion

:: ═══════════════════════════════════════════════════════
::  WebSQL Web 版多平台打包脚本
::  产物: dist-pack/websql-web-{platform}.zip
::
::  用法:
::    scripts\release.bat              — 交互式选择目标平台
::    scripts\release.bat windows      — 仅打包 Windows 版
::    scripts\release.bat linux        — 仅打包 Linux 版
::    scripts\release.bat macos        — 仅打包 macOS 版
::    scripts\release.bat all          — 打包全部平台
:: ═══════════════════════════════════════════════════════

for %%I in ("%~dp0..") do set PROJECT_ROOT=%%~fI\
set DIST_PACK=%PROJECT_ROOT%dist-pack
set WEB_SRC=%PROJECT_ROOT%web-src
set MIGRATIONS_DIR=%PROJECT_ROOT%migrations
set STAGING=%PROJECT_ROOT%.release-staging

:: 解析命令行参数
set PLATFORM=%1
if "%PLATFORM%"=="" (
    echo 请选择打包目标平台:
    echo   1. Windows
    echo   2. Linux
    echo   3. macOS
    echo   4. 全部
    set /p CHOICE="请输入选项 [1-4]: "
    if "!CHOICE!"=="1" set PLATFORM=windows
    if "!CHOICE!"=="2" set PLATFORM=linux
    if "!CHOICE!"=="3" set PLATFORM=macos
    if "!CHOICE!"=="4" set PLATFORM=all
)

echo ============================================================
echo   WebSQL Web 版打包脚本
echo   目标平台: %PLATFORM%
echo   产物目录: %DIST_PACK%
echo ============================================================
echo.

mkdir "%DIST_PACK%" 2>nul

:: ── Step 1: 构建前端 ──
echo [1/3] 构建前端...
cd /d %WEB_SRC%
call npm run build
if errorlevel 1 (
    echo [FAIL] 前端构建失败
    pause
    exit /b 1
)
echo [OK] 前端构建完成
echo.

:: ── Step 2: 编译 Go 二进制 ──
echo [2/3] 编译 Go 二进制...

if "%PLATFORM%"=="windows" (
    call :build_and_pack windows amd64 WebSql.exe windows-amd64
) else if "%PLATFORM%"=="linux" (
    call :build_and_pack linux amd64 WebSql linux-amd64
) else if "%PLATFORM%"=="macos" (
    call :build_and_pack darwin amd64 WebSql macos-amd64
    call :build_and_pack darwin arm64 WebSql macos-arm64
) else if "%PLATFORM%"=="all" (
    call :build_and_pack windows amd64 WebSql.exe windows-amd64
    call :build_and_pack linux amd64 WebSql linux-amd64
    call :build_and_pack darwin amd64 WebSql macos-amd64
    call :build_and_pack darwin arm64 WebSql macos-arm64
) else (
    echo [FAIL] 未知平台: %PLATFORM%
    pause
    exit /b 1
)

echo.
echo ============================================================
echo   打包完成! 产物目录: %DIST_PACK%
echo ============================================================
dir /b "%DIST_PACK%\*.zip" 2>nul
pause
exit /b 0

:: ═══════════════════════════════════════════════════════
::  子程序: 编译 + 打包一条龙
::  参数: %1=GOOS  %2=GOARCH  %3=二进制名  %4=包标识
:: ═══════════════════════════════════════════════════════
:build_and_pack
set B_GOOS=%~1
set B_GOARCH=%~2
set B_BIN=%~3
set B_TAG=%~4
set B_ZIP=%DIST_PACK%\websql-web-%B_TAG%.zip

echo   编译 %B_GOOS%/%B_GOARCH% ...
cd /d %PROJECT_ROOT%
set CGO_ENABLED=0
set GOOS=%B_GOOS%
set GOARCH=%B_GOARCH%
go build -o %B_BIN% main.go
if errorlevel 1 (
    echo   [FAIL] 编译失败
    exit /b 1
)
echo   [OK] %B_BIN%

:: 准备临时暂存目录
set S=%STAGING%\%B_TAG%
if exist "%S%" rd /s /q "%S%"
mkdir "%S%"

:: 复制二进制
move /Y "%PROJECT_ROOT%%B_BIN%" "%S%\" >nul 2>&1
if not exist "%S%\%B_BIN%" copy /Y "%PROJECT_ROOT%%B_BIN%" "%S%\" >nul 2>&1

:: 复制前端静态文件
mkdir "%S%\static"
xcopy /Y /E /Q "%WEB_SRC%\dist\*" "%S%\static\" >nul 2>&1

:: 复制迁移脚本目录（供 MySQL/MariaDB 手动升级和运维使用）
mkdir "%S%\migrations\sqlite"
copy /Y "%MIGRATIONS_DIR%\sqlite\*.sql" "%S%\migrations\sqlite\" >nul 2>&1
mkdir "%S%\migrations\full"
copy /Y "%MIGRATIONS_DIR%\full\*.sql" "%S%\migrations\full\" >nul 2>&1

:: 复制配置文件
copy /Y "%PROJECT_ROOT%config.json" "%S%\" >nul 2>&1

:: 复制数据库迁移工具
copy /Y "%PROJECT_ROOT%db_migrate.py" "%S%\" >nul 2>&1

:: 复制 skills 目录
if exist "%PROJECT_ROOT%skills" (
    xcopy /Y /E /Q "%PROJECT_ROOT%skills\*" "%S%\skills\" >nul 2>&1
)

:: 压缩为 zip（使用 PowerShell Compress-Archive）
if exist "%B_ZIP%" del /q "%B_ZIP%"
powershell -NoProfile -Command "Compress-Archive -Path '%S%\*' -DestinationPath '%B_ZIP%' -Force"
if errorlevel 1 (
    echo   [FAIL] 压缩失败
    exit /b 1
)

:: 清理暂存
rd /s /q "%S%"

for %%A in ("%B_ZIP%") do echo   [OK] %%~nxA (%%~zA bytes)
exit /b 0
