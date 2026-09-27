#!/bin/bash
# envGo — Double-click installer for macOS (Intel & Apple Silicon)
# Just double-click this file in Finder. It installs envGo and shows help.

set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
CYAN="\033[36m"
GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

TMP_BIN=""
cleanup() {
  [ -n "$TMP_BIN" ] && rm -f "$TMP_BIN"
  return 0
}
trap cleanup EXIT

die() {
  echo -e "${RED}Error: $*${RESET}" >&2
  read -p "Press Enter to close..." || true
  exit 1
}

# The version is never hardcoded here. It comes from the VERSION file the
# release process stamps next to the binary, or straight from the binary
# itself, so this installer can never advertise a version it did not install.
detect_version() {
  local binary="$1" stamp
  for candidate in "$DIR/dist/VERSION" "$DIR/VERSION"; do
    if [ -f "$candidate" ]; then
      stamp="$(tr -d ' \t\r\n' < "$candidate")"
      if [ -n "$stamp" ]; then
        printf '%s\n' "$stamp"
        return 0
      fi
    fi
  done
  # Fall back to the compiled-in value: `envgo -v` prints "envGo <version>".
  "$binary" -v 2>/dev/null | awk '/^envGo /{ print $2; exit }'
}

# expected_checksum reads the published SHA256SUMS entry for a binary. Both
# manifest layouts are accepted: "dist/<name>" as shipped in the release
# directory, and a bare "<name>" as used by the release checksum self-check.
expected_checksum() {
  local binary="$1" name manifest expected
  name="$(basename "$binary")"
  for manifest in "$(dirname "$binary")/SHA256SUMS" "$DIR/dist/SHA256SUMS" "$DIR/SHA256SUMS"; do
    [ -f "$manifest" ] || continue
    expected="$(awk -v n="$name" '
      length($1) == 64 {
        path = $2
        sub(/^\*/, "", path)
        sub(/^.*[\/\\]/, "", path)
        if (path == n) print $1
      }' "$manifest")"
    case "$expected" in
      "") continue ;;
      *"\n"*) echo "Ambiguous checksum entries for $name in $manifest" >&2; return 1 ;;
      *) printf '%s\n' "$expected"; return 0 ;;
    esac
  done
  return 1
}

hash_file() {
  local output
  if command -v sha256sum >/dev/null 2>&1; then
    output="$(sha256sum "$1")"
  elif command -v shasum >/dev/null 2>&1; then
    output="$(shasum -a 256 "$1")"
  else
    echo "sha256sum or shasum is required to verify a prebuilt binary" >&2
    return 1
  fi
  printf '%s\n' "${output%% *}"
}

verify_file() {
  local file="$1" name="$2" expected="$3" actual
  actual="$(hash_file "$file")" || return 1
  if [ "$actual" != "$expected" ]; then
    echo -e "${RED}Checksum mismatch for $name${RESET}" >&2
    echo "  expected: $expected" >&2
    echo "  actual:   $actual" >&2
    return 1
  fi
  echo -e "${GREEN}✓ Checksum verified ($name)${RESET}"
}

print_banner() {
  echo -e "${CYAN}"
  cat <<'LOGO'
▄▄
          █▀▀▌
 ▟█▙ ▐▙██▖▐▙ ▟▌▐▌    ▟█▙
▐▙▄▟▌▐▛ ▐▌ █ █ ▐▌▗▄▖▐▛ ▜▌
▐▛▀▀▘▐▌ ▐▌ ▜▄▛ ▐▌▝▜▌▐▌ ▐▌
▝█▄▄▌▐▌ ▐▌ ▐█▌  █▄▟▌▝█▄█▘
 ▝▀▀ ▝▘ ▝▘  ▀    ▀▀  ▝▀▘
LOGO
  echo -e "${RESET}  envGo v${VERSION} — Zero-dependency micro-runtime"
  echo "  Secure .env injection — secrets never reach the browser"
  echo ""
}

ARCH="$(uname -m)"
OS="$(uname -s)"
if [ "$OS" != "Darwin" ]; then
  echo "This double-click installer is for macOS. On Linux/Windows, copy dist/envgo-* to your PATH."
  read -p "Press Enter to close..." || true
  exit 0
fi

BIN_SRC=""
if [ "$ARCH" = "arm64" ] || [ "$ARCH" = "aarch64" ]; then
  BIN_SRC="$DIR/dist/envgo-darwin-arm64"
else
  BIN_SRC="$DIR/dist/envgo-darwin-amd64"
fi

if [ ! -f "$BIN_SRC" ]; then
  # Fallback: if you shared only this .command + binary, look next to it.
  BIN_SRC=""
  for candidate in "$DIR/envgo-darwin-arm64" "$DIR/envgo-darwin-amd64"; do
    if [ -f "$candidate" ]; then
      BIN_SRC="$candidate"
      break
    fi
  done
fi

if [ -z "$BIN_SRC" ]; then
  echo -e "${YELLOW}envGo binary not found next to this installer.${RESET}"
  echo "Expected: $DIR/dist/envgo-darwin-*"
  echo "Please keep Install_envGo.command together with the dist/ folder when sharing."
  read -p "Press Enter to close..." || true
  exit 1
fi

# Verify before anything is executed. A downloaded binary is untrusted input.
EXPECTED=""
if EXPECTED="$(expected_checksum "$BIN_SRC")"; then
  :
else
  EXPECTED=""
  echo -e "${YELLOW}No SHA256SUMS manifest found next to the binary.${RESET}"
  echo "  The download cannot be verified. This is expected when you received a"
  echo "  .zip that contains only the binary — the checksum manifest lives in the"
  echo "  full release. Fetch dist/SHA256SUMS from the release page to verify:"
  echo "    shasum -a 256 \"$(basename "$BIN_SRC")\""
  echo ""
fi

if [ -n "$EXPECTED" ]; then
  verify_file "$BIN_SRC" "$(basename "$BIN_SRC")" "$EXPECTED" \
    || die "the binary does not match the published checksum. Not installing."
fi

# Staged copy: install from a temp file on the same filesystem so the final move
# is atomic, and re-verify afterwards in case the staged file was swapped while
# its startup check ran.
DEST1="$HOME/.local/bin/envgo"
mkdir -p "$HOME/.local/bin"
TMP_BIN="$DEST1.envgo-install.$$"
trap 'rm -f "$TMP_BIN"' EXIT

VERSION="$(detect_version "$BIN_SRC")"
[ -n "$VERSION" ] || VERSION="unknown"

print_banner
echo "Welcome! This will install envGo v${VERSION}."
echo ""

echo "Installing..."
echo "  From: $BIN_SRC"
echo "  To  : $DEST1"
cp "$BIN_SRC" "$TMP_BIN"
chmod +x "$TMP_BIN"
if [ -n "$EXPECTED" ]; then
  verify_file "$TMP_BIN" "$(basename "$BIN_SRC")" "$EXPECTED" \
    || die "the staged copy does not match the published checksum. Not installing."
fi

# Startup check: prove the binary actually runs before it replaces anything.
if ! "$TMP_BIN" -h >/dev/null 2>&1; then
  die "the envGo binary failed its startup check. Not installing."
fi
if [ -n "$EXPECTED" ]; then
  verify_file "$TMP_BIN" "$(basename "$BIN_SRC")" "$EXPECTED" \
    || die "the binary changed while it was being checked. Not installing."
fi

mv -f "$TMP_BIN" "$DEST1"
TMP_BIN=""
chmod +x "$DEST1"
echo -e "${GREEN}✓ Installed to $DEST1${RESET}"

if [ -w "/usr/local/bin" ] && [ ! -d "/usr/local/bin/envgo" ]; then
  if cp "$DEST1" "/usr/local/bin/envgo" 2>/dev/null; then
    chmod +x "/usr/local/bin/envgo"
    echo -e "${GREEN}✓ Also installed to /usr/local/bin/envgo${RESET}"
  fi
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
echo "  envgo run dev --qr  # share on your phone's network via QR code"
echo ""
"$DEST1" -h || true
echo ""
echo "────────────────────────────────────────"
echo "Press Enter to open an interactive shell where you can type:"
echo "  envgo -h"
echo "  envgo -v"
echo "  envgo run dev --qr"
read -p "Press Enter to continue..." || true
exec "$SHELL"
