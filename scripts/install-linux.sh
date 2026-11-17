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
# Usage, from an extracted release archive (any working directory):
#   bash install-linux.sh install
#   bash install-linux.sh uninstall
#   bash install-linux.sh autostart-enable
#   bash install-linux.sh autostart-disable
#
# Usage, from the repository: make install-linux, uninstall-linux,
# autostart-linux and unautostart-linux run the same four commands.
set -euo pipefail

# DEPSILO_INSTALL_LINUX_TEST_OS stands in for the uname result so that
# scripts/test-install-linux.sh can exercise this script, and this refusal, on
# any developer machine. Nothing else sets it.
if [[ "${DEPSILO_INSTALL_LINUX_TEST_OS:-$(uname)}" != "Linux" ]]; then
  echo "ERROR: this script targets Linux." >&2
  exit 1
fi

PREFIX="${HOME}/.local"
BIN_DIR="${PREFIX}/bin"
APPS_DIR="${PREFIX}/share/applications"
ICONS_DIR="${PREFIX}/share/icons/hicolor/256x256/apps"
AUTOSTART_DIR="${HOME}/.config/autostart"
DESKTOP_FILE="depsilo-tray.desktop"

# Sources are resolved from this script's own location, never from the working
# directory, in one of two layouts:
#
#   archive  the release tar.gz: depsilo-tray, depsilo-tray.desktop and
#            depsilo-tray.svg beside this script. The "Package tar.gz" step of
#            .github/workflows/release.yml builds it, and
#            scripts/test-install-linux.sh keeps the two in step.
#   repo     a checkout: this script in scripts/, the binary from `make tray`
#            in bin/, the launcher and the icon under assets/.
#
# A script that bash reads from standard input (bash -s <command>) has no
# location, so it has no layout either: install is refused, and the commands
# that need no sources work as usual.
SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
if [[ -n "${SCRIPT_SOURCE}" ]]; then
  SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${SCRIPT_SOURCE}")" && pwd)"
  SCRIPT_PATH="${SCRIPT_DIR}/$(basename -- "${SCRIPT_SOURCE}")"
  REPO_ROOT="$(dirname -- "${SCRIPT_DIR}")"
else
  SCRIPT_DIR=""
  SCRIPT_PATH="install-linux.sh"
  REPO_ROOT=""
fi

if [[ -z "${SCRIPT_DIR}" ]]; then
  LAYOUT="stdin"
elif [[ -f "${SCRIPT_DIR}/depsilo-tray" || -f "${SCRIPT_DIR}/${DESKTOP_FILE}" ]]; then
  LAYOUT="archive"
  BIN_SRC="${SCRIPT_DIR}/depsilo-tray"
  DESKTOP_SRC="${SCRIPT_DIR}/${DESKTOP_FILE}"
  ICON_SRC="${SCRIPT_DIR}/depsilo-tray.svg"
elif [[ -f "${REPO_ROOT}/assets/linux/${DESKTOP_FILE}" ]]; then
  LAYOUT="repo"
  BIN_SRC="${REPO_ROOT}/bin/depsilo-tray"
  DESKTOP_SRC="${REPO_ROOT}/assets/linux/${DESKTOP_FILE}"
  ICON_SRC="${REPO_ROOT}/assets/macos/icon.svg"   # reused — same brand asset
else
  LAYOUT="none"
fi

# The command a user of this layout can run for one of this script's actions.
# Only a checkout has the make targets; everywhere else it is this script.
layout_command() {
  if [[ "${LAYOUT}" == "repo" ]]; then
    case "$1" in
      install)            echo "make install-linux" ;;
      uninstall)          echo "make uninstall-linux" ;;
      autostart-enable)   echo "make autostart-linux" ;;
      autostart-disable)  echo "make unautostart-linux" ;;
    esac
  else
    printf 'bash %q %s\n' "${SCRIPT_PATH}" "$1"
  fi
}

# Stop before anything is written when the layout cannot be installed, naming
# every missing file.
require_sources() {
  local missing=()
  case "${LAYOUT}" in
    archive)
      [[ -f "${BIN_SRC}" ]] || missing+=("depsilo-tray")
      [[ -f "${DESKTOP_SRC}" ]] || missing+=("${DESKTOP_FILE}")
      if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: not found in ${SCRIPT_DIR}: ${missing[*]}" >&2
        echo "  The release archive holds depsilo-tray, ${DESKTOP_FILE}, depsilo-tray.svg" >&2
        echo "  and install-linux.sh side by side — extract it again and keep them together." >&2
        exit 1
      fi
      ;;
    repo)
      if [[ ! -f "${BIN_SRC}" ]]; then
        echo "ERROR: ${BIN_SRC} not found — run 'make tray' first." >&2
        exit 1
      fi
      ;;
    stdin)
      echo "ERROR: install needs the files beside this script, but bash is reading it from standard input." >&2
      echo "  Run the script as a file instead: bash install-linux.sh install" >&2
      exit 1
      ;;
    *)
      echo "ERROR: nothing to install next to ${SCRIPT_PATH}. It needs one of:" >&2
      echo "  release archive: depsilo-tray and ${DESKTOP_FILE} in ${SCRIPT_DIR}" >&2
      echo "  repository:      bin/depsilo-tray and assets/linux/${DESKTOP_FILE} in ${REPO_ROOT}" >&2
      exit 1
      ;;
  esac
}

render_icon() {
  local out="${ICONS_DIR}/depsilo-tray.png"
  if [[ ! -f "${ICON_SRC}" ]]; then
    echo "  ⚠ ${ICON_SRC} not found — skipping icon" >&2
    return 1
  fi
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
  require_sources

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
  • Autostart on login:          $(layout_command autostart-enable)

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
    echo "ERROR: Depsilo is not installed. Run '$(layout_command install)' first." >&2
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
