#!/bin/bash
# envGo — Double-click installer for macOS (Intel & Apple Silicon)
# Just double-click this file in Finder. It installs envGo and shows help.

set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION="v1.0.4"
CYAN="\033[36m"
GREEN="\033[32m"
YELLOW="\033[33m"
RESET="\033[0m"

print_banner() {
  echo -e "${CYAN}"
  echo " ____        "
  echo "   ___ _ ____   __/ ___| ___  "
  echo "  / _ \\ '_ \\ \\ / / |  _ / _ \\ "
  echo " |  __/ | | \\ V /| |_| | (_) |"
  echo "  \\___|_| |_|\\_/  \\____|\\___/ "
  echo -e "${RESET}  envGo v${VERSION} — Zero-dependency micro-runtime"
  echo "  Secure .env injection — secrets never reach the browser"
  echo ""
}

print_banner
echo "Welcome! This will install envGo."
echo ""

ARCH="$(uname -m)"
OS="$(uname -s)"
BIN_SRC=""
if [ "$OS" = "Darwin" ]; then
  if [ "$ARCH" = "arm64" ] || [ "$ARCH" = "aarch64" ]; then
    BIN_SRC="$DIR/dist/envgo-darwin-arm64"
  else
    BIN_SRC="$DIR/dist/envgo-darwin-amd64"
  fi
else
  echo "This double-click installer is for macOS. On Linux/Windows, copy dist/envgo-* to your PATH."
  read -p "Press Enter to close..."
  exit 0
fi

if [ ! -f "$BIN_SRC" ]; then
  # Fallback: if you shared only this .command + binary, look next to it
  BIN_SRC="$DIR/envgo-darwin-amd64"
  [ -f "$BIN_SRC" ] || BIN_SRC="$DIR/envgo-darwin-arm64"
fi

if [ ! -f "$BIN_SRC" ]; then
  echo -e "${YELLOW}envGo binary not found next to this installer.${RESET}"
  echo "Expected: $DIR/dist/envgo-darwin-*"
  echo "Please keep Install_envGo.command together with the dist/ folder when sharing."
  read -p "Press Enter to close..."
  exit 1
fi

# Install to ~/.local/bin (no sudo needed) and /usr/local/bin if writable
DEST1="$HOME/.local/bin/envgo"
DEST2="/usr/local/bin/envgo"
mkdir -p "$HOME/.local/bin"
echo "Installing..."
echo "  From: $BIN_SRC"
echo "  To  : $DEST1"
cp "$BIN_SRC" "$DEST1"
chmod +x "$DEST1"
echo -e "${GREEN}✓ Installed to $DEST1${RESET}"

if [ -w "/usr/local/bin" ]; then
  cp "$BIN_SRC" "$DEST2" 2>/dev/null && chmod +x "$DEST2" && echo -e "${GREEN}✓ Also installed to $DEST2${RESET}"
fi

# Ensure PATH
if ! echo ":$PATH:" | grep -q ":$HOME/.local/bin:"; then
  echo ""
  echo -e "${YELLOW}Add to PATH (once):${RESET}"
  echo '  echo '\''export PATH="$PATH:$HOME/.local/bin"'\'' >> ~/.zshrc && source ~/.zshrc'
  # Also add now for this session
  export PATH="$PATH:$HOME/.local/bin"
fi

echo ""
echo "────────────────────────────────────────"
print_banner
echo -e "${GREEN}Installation complete!${RESET}"
echo ""
echo "Try:"
echo "  envgo -h          # show help (English)"
echo "  envgo -v          # version"
echo "  envgo -b -e .env -a httpbin.org  # start + open browser"
echo ""
"$DEST1" -h || true
echo ""
echo "────────────────────────────────────────"
echo "Press Enter to open an interactive shell where you can type:"
echo "  envgo -h"
echo "  envgo -v"
echo "  envgo -b -e .env -a httpbin.org"
read -p "Press Enter to continue..."
exec "$SHELL"
