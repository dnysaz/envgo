#!/usr/bin/env sh
# Downloads (Linux) or builds (macOS) a host UPX binary into ./tools/upx.
# UPX does not publish official macOS builds, so on Darwin we compile from source.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST_DIR="$ROOT/tools"
DEST="$DEST_DIR/upx"
VERSION="${UPX_VERSION:-5.2.1}"
BASE="https://github.com/upx/upx/releases/download/v$VERSION"

mkdir -p "$DEST_DIR/src"

if [ -x "$DEST" ]; then
  echo "upx already present: $("$DEST" --version | head -1)"
  exit 0
fi

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    echo "macOS detected: building UPX $VERSION from source (no official binary)"
    SRC_DIR="upx-$VERSION-src"
    curl -fL -o "$DEST_DIR/src/upx-src.tar.xz" "$BASE/upx-$VERSION-src.tar.xz"
    rm -rf "$DEST_DIR/src/$SRC_DIR"
    tar xf "$DEST_DIR/src/upx-src.tar.xz" -C "$DEST_DIR/src"
    cmake -S "$DEST_DIR/src/$SRC_DIR" -B "$DEST_DIR/build" \
      -DCMAKE_BUILD_TYPE=Release -DUPX_CONFIG_DISABLE_GITREV=ON
    cmake --build "$DEST_DIR/build" -j
    cp "$DEST_DIR/build/upx" "$DEST"
    ;;
  Linux)
    case "$ARCH" in
      x86_64) A=amd64 ;;
      aarch64) A=arm64 ;;
      *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
    esac
    echo "Linux detected: downloading UPX $VERSION ($A)"
    curl -fL -o "$DEST_DIR/src/upx.tar.xz" "$BASE/upx-$VERSION-${A}_linux.tar.xz"
    rm -rf "$DEST_DIR/src/upx-$VERSION-${A}_linux"
    tar xf "$DEST_DIR/src/upx.tar.xz" -C "$DEST_DIR/src"
    cp "$DEST_DIR/src/upx-$VERSION-${A}_linux/upx" "$DEST"
    ;;
  *)
    echo "unsupported OS: $OS (install UPX manually and put it on PATH)" >&2
    exit 1
    ;;
esac

chmod +x "$DEST"
echo "installed: $("$DEST" --version | head -1)"