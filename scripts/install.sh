#!/usr/bin/env bash
set -euo pipefail

# envGo interactive installer
# Builds from source (CGO_ENABLED=0) and installs to ~/.local/bin/envgo by default.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always 2>/dev/null || date +%Y%m%d)}"
DEFAULT_PREFIX="$HOME/.local/bin"
CYAN="\033[36m"
GREEN="\033[32m"
YELLOW="\033[33m"
RESET="\033[0m"
GO="${GO:-go}"

print_banner() {
  echo -e "${CYAN}"
  cat <<'BANNER'
   ____              ____
  | __|_ _   __ __  / __| ___
  | _| | ' \/ _` | | |  / _ \
  |___|_|_|_\__,_|  \_| \___/
BANNER
  echo -e "${RESET}  envGo v${VERSION} — Secure .env runtime for HTML/Vanilla JS"
  echo "  Zero-dependency micro-runtime • https://github.com/dnysaz/envgo"
  echo ""
}

expected_checksum() {
  local binary="$1"
  local relative="${binary#"$ROOT"/}"
  local manifest="$ROOT/dist/SHA256SUMS"
  local expected
  if [ ! -f "$manifest" ]; then
    echo "Missing release checksum manifest: $manifest" >&2
    return 1
  fi
  expected="$(awk -v name="$relative" 'length($1) == 64 && $2 == name { print $1 }' "$manifest")"
  if [ "${#expected}" -ne 64 ]; then
    echo "No unique checksum for $relative" >&2
    return 1
  fi
  printf '%s\n' "$expected"
}

verify_file() {
  local file="$1"
  local relative="$2"
  local expected="$3"
  local output actual
  if command -v sha256sum >/dev/null 2>&1; then
    output="$(sha256sum "$file")"
  elif command -v shasum >/dev/null 2>&1; then
    output="$(shasum -a 256 "$file")"
  else
    echo "sha256sum or shasum is required to verify a prebuilt binary" >&2
    return 1
  fi
  actual="${output%% *}"
  if [ "$actual" != "$expected" ]; then
    echo "Checksum mismatch for $relative" >&2
    return 1
  fi
}

install_prebuilt() {
  local expected="$1"
  cp "$DIST_BIN" "$INSTALL_TMP"
  chmod +x "$INSTALL_TMP"
  verify_file "$INSTALL_TMP" "$DIST_REL" "$expected"
  "$INSTALL_TMP" -h >/dev/null 2>&1 || {
    echo "Prebuilt binary failed its startup check: $DIST_REL" >&2
    return 1
  }
  verify_file "$INSTALL_TMP" "$DIST_REL" "$expected"
}

build_binary() {
  local host_os host_arch
  if ! command -v "$GO" >/dev/null 2>&1; then
    echo "Go is required to build envGo" >&2
    return 1
  fi
  host_os="$("$GO" env GOHOSTOS)"
  host_arch="$("$GO" env GOHOSTARCH)"
  CGO_ENABLED=0 GOOS="$host_os" GOARCH="$host_arch" "$GO" -C "$ROOT" build \
    -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" \
    -o "$INSTALL_TMP" .
  if ! "$INSTALL_TMP" -h >/dev/null 2>&1; then
    echo "Built binary failed its startup check" >&2
    return 1
  fi
}

print_banner
echo "Welcome to the envGo interactive installer."
echo ""

read -r -p "Install directory [${DEFAULT_PREFIX}]: " PREFIX
PREFIX="${PREFIX:-$DEFAULT_PREFIX}"

if [ ! -d "$PREFIX" ]; then
  echo -e "${YELLOW}Creating ${PREFIX}...${RESET}"
  mkdir -p "$PREFIX"
fi
PREFIX="$(cd "$PREFIX" && pwd -P)"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac
case "$OS" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  msys*|mingw*|cygwin*) OS="windows" ;;
  *)
    echo "Unsupported operating system: $OS" >&2
    exit 1
    ;;
esac

BIN_NAME="envgo"
INSTALL_EXT=""
DIST_REL="dist/envgo-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  BIN_NAME="envgo.exe"
  INSTALL_EXT=".exe"
  DIST_REL="${DIST_REL}.exe"
fi
DIST_BIN="$ROOT/$DIST_REL"
DESTINATION="$PREFIX/$BIN_NAME"
INSTALL_TMP="$PREFIX/.envgo.install.$$$INSTALL_EXT"
trap 'rm -f "$INSTALL_TMP"' EXIT

if [ -d "$DESTINATION" ]; then
  echo "Install destination is a directory: $DESTINATION" >&2
  exit 1
fi

echo ""
echo "Building envGo v${VERSION}..."
USE_DIST=false
EXPECTED=""
if [ -f "$DIST_BIN" ]; then
  if EXPECTED="$(expected_checksum "$DIST_BIN")" && verify_file "$DIST_BIN" "$DIST_REL" "$EXPECTED"; then
    USE_DIST=true
  else
    echo "Ignoring unverified prebuilt binary: $DIST_BIN" >&2
  fi
fi

if "$USE_DIST"; then
  read -r -p "Found verified prebuilt ${DIST_REL}. Use it? [Y/n]: " USE_PREBUILT
  USE_PREBUILT="${USE_PREBUILT:-Y}"
  if [[ "$USE_PREBUILT" =~ ^[Yy] ]]; then
    if install_prebuilt "$EXPECTED"; then
      echo -e "${GREEN}Installed verified ${DIST_REL} → ${DESTINATION}${RESET}"
    else
      rm -f "$INSTALL_TMP"
      build_binary
      echo -e "${GREEN}Built and installed → ${DESTINATION}${RESET}"
    fi
  else
    build_binary
    echo -e "${GREEN}Built and installed → ${DESTINATION}${RESET}"
  fi
else
  build_binary
  echo -e "${GREEN}Built and installed → ${DESTINATION}${RESET}"
fi

if [ -d "$DESTINATION" ]; then
  echo "Install destination became a directory: $DESTINATION" >&2
  exit 1
fi
mv -f "$INSTALL_TMP" "$DESTINATION"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    echo ""
    echo -e "${YELLOW}NOTE: ${PREFIX} is not in your PATH.${RESET}"
    echo "Add this to your shell profile (~/.zshrc, ~/.bashrc):"
    echo "  export PATH=\"\$PATH:${PREFIX}\""
    ;;
esac

echo ""
echo "────────────────────────────────────────"
print_banner
echo "Installation complete!"
echo ""
echo "Try these commands:"
echo "  ${BIN_NAME} -h          # show help"
echo "  ${BIN_NAME} -v          # print version"
echo "  ${BIN_NAME} -b          # start server and open browser"
echo "  ${BIN_NAME} -p 3000 -d ./public -b"
echo ""

if [ -f "$DESTINATION" ]; then
  echo "Help output:"
  echo "────────────────────────────────────────"
  "$DESTINATION" -h
else
  echo "Run: $BIN_NAME -h"
fi
