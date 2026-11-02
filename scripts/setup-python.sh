#!/bin/sh
# ═══════════════════════════════════════════════════════════════════
#  WebSQL Python 运行时自动安装脚本（Linux / macOS）
#  由 scripts/build_release.py 的外置模式生成到发行包根目录。
#
#  用法:
#    ./setup-python.sh            安装/修复（幂等）
#    ./setup-python.sh --ensure   供 startup.sh 调用，检查并按需安装
#    ./setup-python.sh --check    仅检测版本与依赖
#    ./setup-python.sh --force    删除托管运行时后重新下载
# ═══════════════════════════════════════════════════════════════════

cd "$(dirname "$0")" || exit 1

BOOTSTRAP="./python_bootstrap.py"
if [ ! -f "$BOOTSTRAP" ]; then
    echo "[Python][ERROR] 发行包缺少 python_bootstrap.py，请重新获取完整安装包"
    exit 1
fi

usable_python() {
    "$1" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 8) else 1)' >/dev/null 2>&1
}

# 1) 托管运行时（随包或此前自动下载）
if [ -x "./python/bin/python3" ] && usable_python "./python/bin/python3"; then
    exec ./python/bin/python3 "$BOOTSTRAP" "$@"
fi

# 2) 系统 Python（版本由 python_bootstrap.py 进一步校验，过旧会自动下载托管运行时）
if command -v python3 >/dev/null 2>&1 && usable_python python3; then
    exec python3 "$BOOTSTRAP" "$@"
fi
if command -v python >/dev/null 2>&1 && usable_python python; then
    exec python "$BOOTSTRAP" "$@"
fi

# --check 模式不触发下载
for arg in "$@"; do
    if [ "$arg" = "--check" ]; then
        echo "[Python] 未检测到可用 Python（>=3.8），--check 模式退出"
        exit 1
    fi
done

echo "[Python] 未检测到可用 Python，开始下载独立运行时..."

# 与 python_bootstrap.py 的 PYTHON_FULL_VERSION / STANDALONE_RELEASE 保持一致
PY_VERSION="3.15.2"
STANDALONE_RELEASE="20261026"
DEFAULT_BASE="https://github.com/astral-sh/python-build-standalone/releases/download"
BASE="${WEBSQL_PYTHON_MIRROR:-$DEFAULT_BASE}"

case "$(uname -s)" in
    Linux)  OS_TAG="unknown-linux-gnu" ;;
    Darwin) OS_TAG="apple-darwin" ;;
    *)
        echo "[Python][ERROR] 不支持的操作系统: $(uname -s)"
        exit 1
        ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH_TAG="x86_64" ;;
    arm64|aarch64) ARCH_TAG="aarch64" ;;
    *)
        echo "[Python][ERROR] 不支持的处理器架构: $(uname -m)"
        exit 1
        ;;
esac

ASSET="cpython-${PY_VERSION}+${STANDALONE_RELEASE}-${ARCH_TAG}-${OS_TAG}-install_only.tar.gz"
URL="${BASE%/}/${STANDALONE_RELEASE}/${ASSET}"

WORK_DIR="${TMPDIR:-/tmp}/websql-python-$$"
mkdir -p "$WORK_DIR" || exit 1
ARCHIVE="$WORK_DIR/$ASSET"

echo "[Python] 下载运行时: $URL"
if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 30 -o "$ARCHIVE" "$URL" || {
        echo "[Python][ERROR] 下载失败，可设置镜像后重试: export WEBSQL_PYTHON_MIRROR=<镜像基地址>"
        rm -rf "$WORK_DIR"
        exit 1
    }
elif command -v wget >/dev/null 2>&1; then
    wget -O "$ARCHIVE" "$URL" || {
        echo "[Python][ERROR] 下载失败，可设置镜像后重试: export WEBSQL_PYTHON_MIRROR=<镜像基地址>"
        rm -rf "$WORK_DIR"
        exit 1
    }
else
    echo "[Python][ERROR] 缺少 curl 或 wget，无法下载运行时"
    rm -rf "$WORK_DIR"
    exit 1
fi

echo "[Python] 解压运行时 ..."
tar -xzf "$ARCHIVE" -C "$WORK_DIR" || {
    echo "[Python][ERROR] 解压失败，压缩包可能不完整"
    rm -rf "$WORK_DIR"
    exit 1
}

if [ ! -x "$WORK_DIR/python/bin/python3" ]; then
    chmod +x "$WORK_DIR/python/bin/python3" 2>/dev/null || true
fi
if [ ! -f "$WORK_DIR/python/bin/python3" ]; then
    echo "[Python][ERROR] 运行时目录结构异常（缺少 bin/python3）"
    rm -rf "$WORK_DIR"
    exit 1
fi

rm -rf "./python"
mv "$WORK_DIR/python" "./python" || {
    echo "[Python][ERROR] 无法写入运行时目录 ./python"
    rm -rf "$WORK_DIR"
    exit 1
}
rm -rf "$WORK_DIR"

echo "[Python] 运行时已解压: ./python"
exec ./python/bin/python3 "$BOOTSTRAP" "$@"
