#!/usr/bin/env bash
set -euo pipefail

# Depsilo one-liner install script.
# Usage: curl -fsSL https://depsilo.com/install.sh | bash

REPO="depsilo/depsilo"
BIN="depsilo"
INSTALL_DIR="${DEPSILO_INSTALL_DIR:-/usr/local/bin}"
VERSION="${DEPSILO_VERSION:-latest}"

# --- helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { printf "${CYAN}→${NC} %s\n" "$*"; }
ok()    { printf "${GREEN}✓${NC} %s\n" "$*"; }
err()   { printf "${RED}✗${NC} %s\n" "$*" >&2; exit 1; }

# --- detect os/arch ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             err "Unsupported architecture: $ARCH" ;;
esac

case "$OS" in
    linux|darwin) ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) err "Unsupported OS: $OS. Only Linux, macOS, and Windows (Git Bash/WSL) are supported." ;;
esac

# --- fetch latest version ---
if [ "$VERSION" = "latest" ]; then
    info "Fetching latest release..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        err "Could not determine latest version. Try setting DEPSILO_VERSION=v0.8.0"
    fi
fi

# Strip leading 'v' for archive name (goreleaser uses clean semver in filenames)
CLEAN_VERSION="${VERSION#v}"

# --- download ---
if [ "$OS" = "windows" ]; then
    ARCHIVE="depsilo_${CLEAN_VERSION}_${OS}_${ARCH}.zip"
else
    ARCHIVE="depsilo_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

info "Downloading Depsilo ${VERSION} for ${OS}/${ARCH}..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cd "$TMPDIR"

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "$ARCHIVE" || err "Download failed. Check if release ${VERSION} exists."
    curl -fsSL "$CHECKSUM_URL" -o checksums.txt 2>/dev/null || true
elif command -v wget >/dev/null 2>&1; then
    wget -q "$URL" -O "$ARCHIVE" || err "Download failed. Check if release ${VERSION} exists."
    wget -q "$CHECKSUM_URL" -O checksums.txt 2>/dev/null || true
else
    err "Neither curl nor wget found. Install one of them first."
fi

# --- verify checksum (best-effort) ---
if [ -f checksums.txt ] && command -v shasum >/dev/null 2>&1; then
    CHECKSUM_LINE=$(grep "$ARCHIVE" checksums.txt 2>/dev/null || true)
    if [ -n "$CHECKSUM_LINE" ]; then
        echo "$CHECKSUM_LINE" | shasum -a 256 -c - >/dev/null 2>&1 && \
            info "Checksum verified" || \
            info "Checksum verification skipped (shasum not found or mismatch)"
    fi
fi

# --- install ---
if [ "$OS" = "windows" ]; then
    unzip -q "$ARCHIVE" || err "Failed to extract archive"
else
    tar xzf "$ARCHIVE" || err "Failed to extract archive"
fi

if [ ! -f "$BIN" ]; then
    # The archive may have the binary nested
    BIN_PATH=$(find . -name "$BIN" -type f 2>/dev/null | head -1)
    [ -n "$BIN_PATH" ] || err "Binary not found in archive"
else
    BIN_PATH="./$BIN"
fi

chmod +x "$BIN_PATH" 2>/dev/null || true

# --- place binary ---
if [ "$INSTALL_DIR" = "/usr/local/bin" ] && [ ! -w "$INSTALL_DIR" ]; then
    info "Need sudo to install to $INSTALL_DIR"
    sudo mkdir -p "$INSTALL_DIR"
    sudo cp "$BIN_PATH" "$INSTALL_DIR/$BIN"
    sudo chmod +x "$INSTALL_DIR/$BIN"
else
    mkdir -p "$INSTALL_DIR"
    cp "$BIN_PATH" "$INSTALL_DIR/$BIN"
    chmod +x "$INSTALL_DIR/$BIN" 2>/dev/null || true
fi

ok "Depsilo ${VERSION} installed to ${INSTALL_DIR}/${BIN}"

# --- quick start ---
echo ""
printf "${BOLD}Quick start:${NC}\n"
echo "  cp config.example.toml config.toml"
echo "  depsilo serve"
echo ""
printf "${BOLD}Or run in Docker:${NC}\n"
echo "  docker run -d --name depsilo -p 23333:23333 -v depsilo-data:/app/data depsilo/depsilo:latest"
echo ""
printf "Open ${CYAN}http://localhost:23333${NC} — the portal shows copy-paste config for all 14 ecosystems.\n"
