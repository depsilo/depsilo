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

DOWNLOAD_TOOL=""

select_downloader() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOAD_TOOL="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOAD_TOOL="wget"
    else
        err "Neither curl nor wget found. Install one of them first."
    fi
}

fetch_stdout() {
    case "$DOWNLOAD_TOOL" in
        curl) curl -fsSL "$1" ;;
        wget) wget -qO- "$1" ;;
        *) err "Download tool was not initialized" ;;
    esac
}

download_file() {
    local url="$1"
    local destination="$2"
    case "$DOWNLOAD_TOOL" in
        curl) curl -fsSL "$url" -o "$destination" ;;
        wget) wget -q "$url" -O "$destination" ;;
        *) err "Download tool was not initialized" ;;
    esac
}

archive_binary_name() {
    if [ "$1" = "windows" ]; then
        printf '%s.exe\n' "$BIN"
    else
        printf '%s\n' "$BIN"
    fi
}

print_install_success() {
    local version="$1"
    local install_path="$2"

    ok "Depsilo ${version} installed"
    printf '\n  %s\n\n' "$install_path"
    printf "${BOLD}Get started:${NC}\n\n"
    printf '  depsilo serve\n\n'
    printf "${BOLD}Then open:${NC}\n\n"
    printf '  http://127.0.0.1:23333\n\n'
    printf "${BOLD}Docs:${NC}\n\n"
    printf '  https://github.com/depsilo/depsilo#quick-start\n'
}

verify_checksum() {
    local archive="$1"
    local checksum_file="$2"
    local checksum_line

    [ -s "$checksum_file" ] || err "Checksum file is missing or empty"
    checksum_line=$(awk -v archive="$archive" '
        NF >= 2 {
            name = $2
            sub(/^\*/, "", name)
            if (name == archive) {
                print $1 "  " archive
                exit
            }
        }
    ' "$checksum_file")
    [ -n "$checksum_line" ] || err "No checksum found for ${archive}"

    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s\n' "$checksum_line" | sha256sum -c - >/dev/null 2>&1 \
            || err "Checksum verification failed for ${archive}"
    elif command -v shasum >/dev/null 2>&1; then
        printf '%s\n' "$checksum_line" | shasum -a 256 -c - >/dev/null 2>&1 \
            || err "Checksum verification failed for ${archive}"
    else
        err "Checksum verification requires sha256sum or shasum"
    fi
    info "Checksum verified"
}

# Keep the verifier sourceable for its shell regression tests without running
# downloads or installation as a side effect. Do not infer this from
# BASH_SOURCE: the documented `curl ... | bash` invocation has no script path.
if [[ "${DEPSILO_INSTALL_SOURCE_ONLY:-0}" == "1" ]]; then
    if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
        return 0
    fi
    exit 0
fi

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

select_downloader

# --- fetch latest version ---
if [ "$VERSION" = "latest" ]; then
    info "Fetching latest release..."
    if ! RELEASE_JSON=$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest"); then
        err "Could not fetch the latest release metadata"
    fi
    VERSION=$(printf '%s\n' "$RELEASE_JSON" \
        | sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' \
        | head -1)
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
ARCHIVE_BIN=$(archive_binary_name "$OS")
INSTALL_BIN="$ARCHIVE_BIN"

URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

info "Downloading Depsilo ${VERSION} for ${OS}/${ARCH}..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cd "$TMPDIR"

download_file "$URL" "$ARCHIVE" || err "Download failed. Check if release ${VERSION} exists."
download_file "$CHECKSUM_URL" checksums.txt || err "Checksum download failed for ${VERSION}"

# --- verify checksum (fail closed) ---
verify_checksum "$ARCHIVE" checksums.txt

# --- install ---
if [ "$OS" = "windows" ]; then
    unzip -q "$ARCHIVE" || err "Failed to extract archive"
else
    tar xzf "$ARCHIVE" || err "Failed to extract archive"
fi

if [ ! -f "$ARCHIVE_BIN" ]; then
    # The archive may have the binary nested
    BIN_PATH=$(find . -name "$ARCHIVE_BIN" -type f -print -quit 2>/dev/null)
    [ -n "$BIN_PATH" ] || err "Binary not found in archive"
else
    BIN_PATH="./$ARCHIVE_BIN"
fi

chmod +x "$BIN_PATH" 2>/dev/null || true

# --- place binary ---
if [ "$INSTALL_DIR" = "/usr/local/bin" ] && [ ! -w "$INSTALL_DIR" ]; then
    info "Need sudo to install to $INSTALL_DIR"
    sudo mkdir -p "$INSTALL_DIR"
    sudo cp "$BIN_PATH" "$INSTALL_DIR/$INSTALL_BIN"
    sudo chmod +x "$INSTALL_DIR/$INSTALL_BIN"
else
    mkdir -p "$INSTALL_DIR"
    cp "$BIN_PATH" "$INSTALL_DIR/$INSTALL_BIN"
    chmod +x "$INSTALL_DIR/$INSTALL_BIN" 2>/dev/null || true
fi

print_install_success "$VERSION" "${INSTALL_DIR}/${INSTALL_BIN}"
