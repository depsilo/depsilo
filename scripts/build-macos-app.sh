#!/usr/bin/env bash
# Build the Depsilo.app bundle on macOS.
#
# Requirements (macOS only):
#   - iconutil   (built-in)
#   - sips       (built-in)
#   - rsvg-convert OR qlmanage  (for SVG -> PNG; rsvg gives crisper output)
#
# Usage:
#   scripts/build-macos-app.sh <version>
#
# Output:
#   bin/Depsilo.app
#
set -euo pipefail

VERSION="${1:-dev}"

if [[ "$(uname)" != "Darwin" ]]; then
  echo "ERROR: this script must run on macOS (uses iconutil + sips)." >&2
  exit 1
fi

if [[ ! -f bin/depsilo-tray ]]; then
  echo "ERROR: bin/depsilo-tray not found. Run 'make tray' first." >&2
  exit 1
fi

APP_DIR="bin/Depsilo.app"
CONTENTS="${APP_DIR}/Contents"
MACOS="${CONTENTS}/MacOS"
RESOURCES="${CONTENTS}/Resources"

rm -rf "${APP_DIR}"
mkdir -p "${MACOS}" "${RESOURCES}"

# ── Copy executable ─────────────────────────────────────────────────
cp bin/depsilo-tray "${MACOS}/depsilo-tray"
chmod +x "${MACOS}/depsilo-tray"

# ── Info.plist ──────────────────────────────────────────────────────
sed "s/__VERSION__/${VERSION}/g" assets/macos/Info.plist.template \
  > "${CONTENTS}/Info.plist"

# ── Icon: SVG → multi-size iconset → .icns ──────────────────────────
ICON_SRC="assets/macos/icon.svg"
ICONSET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "${ICONSET}"

render_svg() {
  local size="$1"
  local out="$2"
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "${size}" -h "${size}" "${ICON_SRC}" -o "${out}"
  elif command -v qlmanage >/dev/null 2>&1; then
    local tmpdir
    tmpdir="$(mktemp -d)"
    qlmanage -t -s "${size}" -o "${tmpdir}" "${ICON_SRC}" >/dev/null 2>&1
    mv "${tmpdir}/$(basename "${ICON_SRC}").png" "${out}"
    rm -rf "${tmpdir}"
  else
    echo "ERROR: need rsvg-convert (brew install librsvg) or qlmanage" >&2
    exit 1
  fi
}

# Apple's required sizes for a complete .icns (point + @2x).
for size in 16 32 128 256 512; do
  render_svg "${size}"        "${ICONSET}/icon_${size}x${size}.png"
  render_svg "$((size * 2))"  "${ICONSET}/icon_${size}x${size}@2x.png"
done

iconutil -c icns "${ICONSET}" -o "${RESOURCES}/AppIcon.icns"

# ── Optional: clear quarantine flag for local builds ────────────────
xattr -cr "${APP_DIR}" 2>/dev/null || true

SIZE=$(du -sh "${APP_DIR}" | cut -f1)
echo "✓ Built ${APP_DIR}  (${SIZE})"
echo
echo "  Drag it to /Applications, or run directly:"
echo "    open ${APP_DIR}"
