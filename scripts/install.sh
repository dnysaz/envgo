#!/usr/bin/env bash
set -e

# envGo interactive installer
# Builds from source (CGO_ENABLED=0) and installs to ~/.local/bin/envgo by default.

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || date +%Y%m%d)}"
DEFAULT_PREFIX="$HOME/.local/bin"
CYAN="\033[36m"
GREEN="\033[32m"
YELLOW="\033[33m"
RESET="\033[0m"

print_banner() {
  echo -e "${CYAN}"
  cat <<'BANNER'
   ____              ____
  | __|_ _   __ __  / __| ___
  | _| | ' \/ _` | | |  / _ \
  |___|_|_|_\__,_|  \_| \___/
BANNER
  echo -e "${RESET}  envGo v${VERSION} — Secure .env runtime for HTML/Vanilla JS"
  echo "  Zero-dependency micro-runtime • https://github.com/anomalyco/opencode"
  echo ""
}

print_banner
echo "Welcome to the envGo interactive installer."
echo ""

# Ask for install directory
read -r -p "Install directory [${DEFAULT_PREFIX}]: " PREFIX
PREFIX="${PREFIX:-$DEFAULT_PREFIX}"

if [ ! -d "$PREFIX" ]; then
  echo -e "${YELLOW}Creating ${PREFIX}...${RESET}"
  mkdir -p "$PREFIX"
fi

# Check if we have a prebuilt binary in dist for this platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
esac
case "$OS" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  msys*|mingw*|cygwin*) OS="windows" ;;
esac

DIST_BIN=""
if [ "$OS" = "windows" ]; then
  DIST_BIN="dist/envgo-${OS}-${ARCH}.exe"
else
  DIST_BIN="dist/envgo-${OS}-${ARCH}"
fi

echo ""
echo "Building envGo v${VERSION}..."
if [ -f "$DIST_BIN" ]; then
  read -r -p "Found prebuilt ${DIST_BIN}. Use it? [Y/n]: " USE_DIST
  USE_DIST="${USE_DIST:-Y}"
  if [[ "$USE_DIST" =~ ^[Yy] ]]; then
    cp "$DIST_BIN" "$PREFIX/envgo"
    chmod +x "$PREFIX/envgo"
    echo -e "${GREEN}Installed from ${DIST_BIN} → ${PREFIX}/envgo${RESET}"
  else
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "$PREFIX/envgo" .
    echo -e "${GREEN}Built and installed → ${PREFIX}/envgo${RESET}"
  fi
else
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "$PREFIX/envgo" .
  echo -e "${GREEN}Built and installed → ${PREFIX}/envgo${RESET}"
fi

# Handle .exe on Windows
if [ "$OS" = "windows" ] && [ ! -f "$PREFIX/envgo.exe" ] && [ -f "$PREFIX/envgo" ]; then
  mv "$PREFIX/envgo" "$PREFIX/envgo.exe"
  echo "Installed as $PREFIX/envgo.exe"
fi

BIN_NAME="envgo"
if [ "$OS" = "windows" ]; then BIN_NAME="envgo.exe"; fi

# PATH check
if ! echo ":$PATH:" | grep -q ":$PREFIX:"; then
  echo ""
  echo -e "${YELLOW}NOTE: ${PREFIX} is not in your PATH.${RESET}"
  echo "Add this to your shell profile (~/.zshrc, ~/.bashrc):"
  echo "  export PATH=\"\$PATH:${PREFIX}\""
fi

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

# Show help automatically
if [ -x "$PREFIX/$BIN_NAME" ]; then
  echo "Help output:"
  echo "────────────────────────────────────────"
  "$PREFIX/$BIN_NAME" -h || true
else
  echo "Run: envgo -h"
fi
