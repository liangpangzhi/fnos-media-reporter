#!/bin/bash
# 构建飞牛 fnOS fpk 安装包
# 用法: ./build.sh [amd64|arm64]
# 依赖: Go 1.21+、python3（打包用，无需安装 fnpack）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCH="${1:-amd64}"

case "$ARCH" in
    amd64) GOARCH_VAL=amd64; PLATFORM=x86 ;;
    arm64) GOARCH_VAL=arm64; PLATFORM=arm ;;
    *) echo "不支持的架构: $ARCH（可选 amd64 / arm64）" >&2; exit 1 ;;
esac

BIN="$ROOT/fpk/app/bin/media-reporter"

echo "==> 编译 linux/${GOARCH_VAL} 静态二进制"
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH_VAL" \
    go build -trimpath -ldflags "-s -w" -o "$BIN" "$ROOT"

echo "==> 打包 fpk（platform=${PLATFORM}）"
python3 "$ROOT/tools/package.py" "$ARCH"
