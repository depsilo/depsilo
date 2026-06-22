#!/usr/bin/env bash
# Install / uninstall the Depsilo menu-bar tray app on Linux.
#
# Lays files under $HOME/.local — no sudo required.
#
#   ~/.local/bin/depsilo-tray
#   ~/.local/share/applications/depsilo-tray.desktop
#   ~/.local/share/icons/hicolor/256x256/apps/depsilo-tray.png   (best effort)
#   ~/.config/autostart/depsilo-tray.desktop                     (autostart-enable only)
#
# Usage:
#   scripts/install-linux.sh install
#   scripts/install-linux.sh uninstall
#   scripts/install-linux.sh autostart-enable
#   scripts/install-linux.sh autostart-disable
set -euo pipefail

if [[ "$(uname)" != "Linux" ]]; then
  echo "ERROR: this script targets Linux." >&2
  exit 1
fi

PREFIX="${HOME}/.local"
BIN_DIR="${PREFIX}/bin"
APPS_DIR="${PREFIX}/share/applications"
ICONS_DIR="${PREFIX}/share/icons/hicolor/256x256/apps"
AUTOSTART_DIR="${HOME}/.config/autostart"
DESKTOP_FILE="depsilo-tray.desktop"

# Repo paths (relative to repo root, which is where make calls us from)
DESKTOP_SRC="assets/linux/depsilo-tray.desktop"
ICON_SRC="assets/macos/icon.svg"        # reused — same brand asset
BIN_SRC="bin/depsilo-tray"

render_icon() {
  local out="${ICONS_DIR}/depsilo-tray.png"
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w 256 -h 256 "${ICON_SRC}" -o "${out}"
    echo "  ✓ ${out}"
  elif command -v convert >/dev/null 2>&1; then
    # ImageMagick fallback. -background none keeps transparency.
    convert -background none -resize 256x256 "${ICON_SRC}" "${out}"
    echo "  ✓ ${out} (ImageMagick)"
  elif command -v inkscape >/dev/null 2>&1; then
    inkscape "${ICON_SRC}" --export-type=png --export-filename="${out}" -w 256 -h 256 >/dev/null 2>&1
    echo "  ✓ ${out} (Inkscape)"
  else
    echo "  ⚠ no SVG renderer (apt install librsvg2-bin) — skipping icon" >&2
    return 1
  fi
}

cmd_install() {
  if [[ ! -f "${BIN_SRC}" ]]; then
    echo "ERROR: ${BIN_SRC} not found — run 'make tray' first." >&2
    exit 1
  fi

  mkdir -p "${BIN_DIR}" "${APPS_DIR}" "${ICONS_DIR}"

  # 1. Binary
  install -m 0755 "${BIN_SRC}" "${BIN_DIR}/depsilo-tray"
  echo "  ✓ ${BIN_DIR}/depsilo-tray"

  # 2. .desktop file — substitute Exec with the absolute install path so
  # the launcher works regardless of whether ~/.local/bin is on PATH.
  sed "s|__EXEC__|${BIN_DIR}/depsilo-tray|" "${DESKTOP_SRC}" \
    > "${APPS_DIR}/${DESKTOP_FILE}"
  chmod 0644 "${APPS_DIR}/${DESKTOP_FILE}"
  echo "  ✓ ${APPS_DIR}/${DESKTOP_FILE}"

  # 3. Icon (best effort)
  render_icon || true

  # 4. Refresh GUI caches (silent on failure)
  command -v update-desktop-database >/dev/null 2>&1 && \
    update-desktop-database "${APPS_DIR}" 2>/dev/null || true
  command -v gtk-update-icon-cache >/dev/null 2>&1 && \
    gtk-update-icon-cache -f -t "${PREFIX}/share/icons/hicolor" 2>/dev/null || true

  # 5. PATH check
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *)
      echo
      echo "  ⚠ ${BIN_DIR} is not on PATH. Add to your shell rc:"
      echo "      export PATH=\"\$HOME/.local/bin:\$PATH\""
      ;;
  esac

  cat <<POSTINSTALL

✓ Depsilo installed to ~/.local

Launch:
  • From your application menu: search "Depsilo"
  • From terminal:               depsilo-tray
  • Autostart on login:          make autostart-linux

GNOME on Wayland users (Ubuntu 22.04+, Fedora):
  The system tray requires the "AppIndicator and KStatusNotifierItem"
  GNOME extension. Install via:
    https://extensions.gnome.org/extension/615/appindicator-support/

POSTINSTALL
}

cmd_uninstall() {
  local removed=0
  for f in \
      "${BIN_DIR}/depsilo-tray" \
      "${APPS_DIR}/${DESKTOP_FILE}" \
      "${ICONS_DIR}/depsilo-tray.png" \
      "${AUTOSTART_DIR}/${DESKTOP_FILE}"; do
    if [[ -e "$f" || -L "$f" ]]; then
      rm -f "$f"
      echo "  ✓ removed $f"
      removed=$((removed + 1))
    fi
  done

  command -v update-desktop-database >/dev/null 2>&1 && \
    update-desktop-database "${APPS_DIR}" 2>/dev/null || true

  if [[ $removed -eq 0 ]]; then
    echo "  · nothing to remove (already clean)"
  else
    echo
    echo "✓ Depsilo uninstalled."
  fi
}

cmd_autostart_enable() {
  if [[ ! -f "${APPS_DIR}/${DESKTOP_FILE}" ]]; then
    echo "ERROR: Depsilo is not installed. Run 'make install-linux' first." >&2
    exit 1
  fi
  mkdir -p "${AUTOSTART_DIR}"
  # Symlink so future re-installs / version bumps propagate automatically.
  ln -sf "${APPS_DIR}/${DESKTOP_FILE}" "${AUTOSTART_DIR}/${DESKTOP_FILE}"
  echo "✓ Depsilo will start on next login (${AUTOSTART_DIR}/${DESKTOP_FILE})"
}

cmd_autostart_disable() {
  rm -f "${AUTOSTART_DIR}/${DESKTOP_FILE}"
  echo "✓ Depsilo autostart disabled"
}

case "${1:-}" in
  install)            cmd_install ;;
  uninstall)          cmd_uninstall ;;
  autostart-enable)   cmd_autostart_enable ;;
  autostart-disable)  cmd_autostart_disable ;;
  *)
    echo "Usage: $0 {install|uninstall|autostart-enable|autostart-disable}" >&2
    exit 1
    ;;
esac
