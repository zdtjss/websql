﻿# ═══════════════════════════════════════════════════════════════════
#  WebSQL Python 运行时下载辅助脚本（Windows / PowerShell）
#  仅在 setup-python.bat 未找到任何可用 Python 时被调用：
#  先下载本机架构对应的独立运行时并解压到 python/，再交给
#  python_bootstrap.py 安装第三方依赖并写出运行时清单。
# ═══════════════════════════════════════════════════════════════════
param(
    [string]$BootstrapArgs = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

# 与 python_bootstrap.py 的 PYTHON_FULL_VERSION / STANDALONE_RELEASE 保持一致
$PyVersion = "3.15.2"
$StandaloneRelease = "20261026"
$DefaultBase = "https://github.com/astral-sh/python-build-standalone/releases/download"

function Write-Step([string]$Message) {
    Write-Host "[Python] $Message"
}

$BootstrapArgs = $BootstrapArgs.Trim()
if ($BootstrapArgs -match "--check") {
    Write-Step "未检测到可用 Python（>=3.8），--check 模式退出"
    exit 1
}

$ArgList = @()
if ($BootstrapArgs.Length -gt 0) {
    $ArgList = $BootstrapArgs -split '\s+' | Where-Object { $_ }
}

# 已存在可用的托管运行时则直接复用
$ManagedExe = Join-Path $Root "python\python.exe"
if (Test-Path $ManagedExe) {
    Write-Step "复用已有托管运行时: $ManagedExe"
    & $ManagedExe (Join-Path $Root "python_bootstrap.py") @ArgList
    exit $LASTEXITCODE
}

$Machine = [System.Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
if ($Machine -eq "ARM64") {
    Write-Step "Windows ARM64 将使用 x64 运行时（系统自动仿真）"
}
$Triple = "x86_64-pc-windows-msvc"

$Base = $env:WEBSQL_PYTHON_MIRROR
if ([string]::IsNullOrWhiteSpace($Base)) { $Base = $DefaultBase }
$Base = $Base.TrimEnd("/")

$Asset = "cpython-$PyVersion+$StandaloneRelease-$Triple-install_only.tar.gz"
$Url = "$Base/$StandaloneRelease/$Asset"
$Archive = Join-Path $env:TEMP $Asset
$Staging = Join-Path $Root ".python-staging"

Write-Step "下载运行时: $Url"
try {
    Invoke-WebRequest -Uri $Url -OutFile $Archive -UseBasicParsing
} catch {
    Write-Step "[ERROR] 下载失败: $($_.Exception.Message)"
    Write-Step "可设置镜像后重试: `$env:WEBSQL_PYTHON_MIRROR = '<镜像基地址>'"
    exit 1
}

$Tar = (Get-Command tar.exe -ErrorAction SilentlyContinue)
if (-not $Tar) {
    Write-Step "[ERROR] 当前系统缺少 tar.exe（Windows 10 1803+ 自带），无法解压运行时"
    Write-Step "可手动安装 Python 3.10+ 后重新运行 setup-python.bat"
    exit 1
}

Write-Step "解压运行时 ..."
if (Test-Path $Staging) { Remove-Item -Recurse -Force $Staging }
New-Item -ItemType Directory -Path $Staging | Out-Null
& tar.exe -xzf $Archive -C $Staging
if ($LASTEXITCODE -ne 0 -or -not (Test-Path (Join-Path $Staging "python\python.exe"))) {
    Write-Step "[ERROR] 运行时解压失败，压缩包可能不完整"
    Remove-Item -Recurse -Force $Staging -ErrorAction SilentlyContinue
    exit 1
}

$Target = Join-Path $Root "python"
if (Test-Path $Target) { Remove-Item -Recurse -Force $Target }
Move-Item (Join-Path $Staging "python") $Target
Remove-Item -Recurse -Force $Staging -ErrorAction SilentlyContinue

Write-Step "运行时已解压: $Target"
& (Join-Path $Target "python.exe") (Join-Path $Root "python_bootstrap.py") @ArgList
exit $LASTEXITCODE
