#!/usr/bin/env bash
set -euo pipefail

# Contract test for scripts/install-linux.sh, the Linux tray installer. It
# installs from two layouts: the release archive (its files beside the script)
# and a repository checkout. Both are built here from stand-in files and
# installed into a temporary HOME, so the test needs neither Linux nor a
# desktop. The archive is produced by the release workflow's own packaging
# step, so that step and the installer cannot drift apart.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORKFLOW="$ROOT/.github/workflows/release.yml"
# The installer reports the paths it resolved; compare against the physical
# path because the temporary directory is behind a symlink on macOS.
TMP=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$TMP"' EXIT

fail() {
    echo "linux tray installer test failed: $*" >&2
    exit 1
}

assert_contains() {
    local text=$1
    local expected=$2
    [[ "$text" == *"$expected"* ]] || fail "missing: $expected"$'\n'"in output:"$'\n'"$text"
}

assert_not_contains() {
    local text=$1
    local unexpected=$2
    [[ "$text" != *"$unexpected"* ]] || fail "unexpected: $unexpected"$'\n'"in output:"$'\n'"$text"
}

# The installer runs with nothing on PATH except the tools it needs, so an SVG
# renderer or desktop-cache tool installed on this machine cannot change the
# result. uname is deliberately absent: DEPSILO_INSTALL_LINUX_TEST_OS, which
# only this test sets, stands in for it. The last section runs the installer
# without that variable, the way users do, against a stand-in uname.
TOOLS="$TMP/tools"
mkdir "$TOOLS"
ln -s "$BASH" "$TOOLS/bash"
for tool in dirname basename mkdir install sed chmod cat rm ln; do
    ln -s "$(command -v "$tool")" "$TOOLS/$tool"
done

# Stand-in for: rsvg-convert -w 256 -h 256 <svg> -o <png>. The "PNG" records
# which SVG the installer rendered.
RENDERER="$TMP/renderer"
mkdir "$RENDERER"
cat > "$RENDERER/rsvg-convert" <<'SH'
#!/bin/sh
printf '%s\n' "$5" > "$7"
SH
chmod +x "$RENDERER/rsvg-convert"

INSTALLER_CWD="$TMP/elsewhere"
INSTALLER_PATH="$TOOLS"
INSTALLER_OS=Linux
FAKE_HOME=
mkdir "$INSTALLER_CWD"

# installer <program> [argument...] runs it the way a user's shell would, but
# in the controlled environment above. An empty INSTALLER_OS leaves the
# override out of the environment altogether.
installer() {
    (
        cd "$INSTALLER_CWD"
        if [ -n "$INSTALLER_OS" ]; then
            env -i HOME="$FAKE_HOME" PATH="$INSTALLER_PATH" \
                DEPSILO_INSTALL_LINUX_TEST_OS="$INSTALLER_OS" "$@"
        else
            env -i HOME="$FAKE_HOME" PATH="$INSTALLER_PATH" "$@"
        fi
    )
}

new_home() {
    FAKE_HOME=$(mktemp -d "$TMP/home.XXXXXX")
}

installed_bin() { printf '%s\n' "$FAKE_HOME/.local/bin/depsilo-tray"; }
installed_desktop() { printf '%s\n' "$FAKE_HOME/.local/share/applications/depsilo-tray.desktop"; }
installed_icon() { printf '%s\n' "$FAKE_HOME/.local/share/icons/hicolor/256x256/apps/depsilo-tray.png"; }
installed_autostart() { printf '%s\n' "$FAKE_HOME/.config/autostart/depsilo-tray.desktop"; }

assert_installed() {
    [ -x "$(installed_bin)" ] || fail "tray binary was not installed as an executable"
    cmp -s "$STAND_IN_BINARY" "$(installed_bin)" || fail "installed tray binary differs from the source"
    grep -Fxq "Exec=$(installed_bin)" "$(installed_desktop)" ||
        fail "launcher must start the installed binary by absolute path"
    if grep -Fq '__EXEC__' "$(installed_desktop)"; then
        fail "launcher still contains the Exec placeholder"
    fi
    grep -Fxq 'Icon=depsilo-tray' "$(installed_desktop)" || fail "launcher lost its icon name"
}

assert_nothing_installed() {
    [ ! -e "$FAKE_HOME/.local" ] && [ ! -e "$FAKE_HOME/.config" ] ||
        fail "a refused run must not write into HOME"
}

# ── Stand-in checkout ───────────────────────────────────────────────
REPO="$TMP/checkout"
mkdir -p "$REPO/scripts" "$REPO/assets/linux" "$REPO/assets/macos" "$REPO/bin"
cp "$ROOT/scripts/install-linux.sh" "$REPO/scripts/"
cp "$ROOT/assets/linux/depsilo-tray.desktop" "$REPO/assets/linux/"
cp "$ROOT/assets/macos/icon.svg" "$REPO/assets/macos/"
STAND_IN_BINARY="$TMP/stand-in-depsilo-tray"
printf '#!/bin/sh\necho stand-in depsilo-tray\n' > "$STAND_IN_BINARY"
chmod +x "$STAND_IN_BINARY"
cp "$STAND_IN_BINARY" "$REPO/bin/depsilo-tray"

# ── Release archive, built by the workflow's packaging step ─────────
tray_linux_job=$(sed -n '/^  tray-linux:/,/^  [a-zA-Z0-9_-]*:/p' "$WORKFLOW")
package_script=$(sed -n '/^      - name: Package tar\.gz$/,/^      - name: /p' <<<"$tray_linux_job" |
    sed -n 's/^          //p')
[ -n "$package_script" ] || fail "release workflow has no tray-linux packaging script"
if grep -Fq '${{' <<<"$package_script"; then
    fail "the tray-linux packaging step must stay plain shell so this test can run it"
fi
(cd "$REPO" && COPYFILE_DISABLE=1 bash -e -c "$package_script") ||
    fail "the tray-linux packaging step failed against a checkout that holds every file it copies"
TARBALL="$REPO/depsilo-tray-linux-amd64.tar.gz"
[ -f "$TARBALL" ] || fail "the tray-linux packaging step no longer writes depsilo-tray-linux-amd64.tar.gz"

# The members are exactly the installer's archive layout, stored by name. A
# "./" member would make GNU tar apply the staging directory's mode and
# timestamp to the directory the user extracts into, which changes a private
# home directory and fails outright in one they do not own, such as /tmp.
archive_members=$(tar tzf "$TARBALL" | LC_ALL=C sort | tr '\n' ' ')
expected_members='depsilo-tray depsilo-tray.desktop depsilo-tray.svg install-linux.sh '
[ "$archive_members" = "$expected_members" ] ||
    fail "release archive holds [$archive_members], the installer's archive layout is [$expected_members]"

# A space in the path exercises the quoting of the commands the installer prints.
ARCHIVE="$TMP/extracted archive"
mkdir "$ARCHIVE"
tar xzf "$TARBALL" -C "$ARCHIVE"
[ -x "$ARCHIVE/install-linux.sh" ] || fail "archived installer must be executable"

# ── Archive layout: install as documented, from the extracted directory ──
new_home
INSTALLER_CWD="$ARCHIVE"
INSTALLER_PATH="$RENDERER:$TOOLS"
output=$(installer bash install-linux.sh install 2>&1) || fail "archive install failed:"$'\n'"$output"
assert_installed
[ "$(cat "$(installed_icon)")" = "$ARCHIVE/depsilo-tray.svg" ] ||
    fail "archive install must render the icon shipped beside the script"
autostart_command="bash $(printf '%q' "$ARCHIVE/install-linux.sh") autostart-enable"
assert_contains "$output" "Autostart on login:          $autostart_command"
assert_not_contains "$output" 'make '

# The printed autostart command must work as printed, from any directory.
INSTALLER_CWD="$TMP/elsewhere"
INSTALLER_PATH="$TOOLS"
installer bash -c "$autostart_command" >/dev/null
[ -L "$(installed_autostart)" ] || fail "autostart-enable must link the launcher into autostart"
[ "$(readlink "$(installed_autostart)")" = "$(installed_desktop)" ] ||
    fail "autostart entry must point at the installed launcher"
installer bash "$ARCHIVE/install-linux.sh" autostart-disable >/dev/null
[ ! -e "$(installed_autostart)" ] && [ ! -L "$(installed_autostart)" ] ||
    fail "autostart-disable must remove the autostart entry"

installer bash "$ARCHIVE/install-linux.sh" autostart-enable >/dev/null
output=$(installer bash "$ARCHIVE/install-linux.sh" uninstall 2>&1)
for path in "$(installed_bin)" "$(installed_desktop)" "$(installed_icon)" "$(installed_autostart)"; do
    [ ! -e "$path" ] && [ ! -L "$path" ] || fail "uninstall left $path behind"
    assert_contains "$output" "removed $path"
done
output=$(installer bash "$ARCHIVE/install-linux.sh" uninstall 2>&1)
assert_contains "$output" 'nothing to remove'

# Archive layout from another directory, without an SVG renderer: the icon is
# best effort and its absence must not fail the install.
new_home
output=$(installer bash "$ARCHIVE/install-linux.sh" install 2>&1) ||
    fail "archive install from another directory failed:"$'\n'"$output"
assert_installed
assert_contains "$output" 'skipping icon'
[ ! -e "$(installed_icon)" ] || fail "no icon can exist without a renderer"

# ── Archive layout: incomplete extractions name what is missing ─────
new_home
if output=$(installer bash "$ARCHIVE/install-linux.sh" autostart-enable 2>&1); then
    fail "autostart-enable must fail before the application is installed"
fi
assert_contains "$output" "Run 'bash $(printf '%q' "$ARCHIVE/install-linux.sh") install' first."

PARTIAL="$TMP/partial"
mkdir "$PARTIAL"
cp "$ARCHIVE/install-linux.sh" "$ARCHIVE/depsilo-tray.desktop" "$ARCHIVE/depsilo-tray.svg" "$PARTIAL/"
if output=$(installer bash "$PARTIAL/install-linux.sh" install 2>&1); then
    fail "an archive without the tray binary was installed"
fi
assert_contains "$output" "not found in $PARTIAL: depsilo-tray"$'\n'
assert_not_contains "$output" 'make tray'
assert_nothing_installed

cp "$ARCHIVE/depsilo-tray" "$PARTIAL/"
rm "$PARTIAL/depsilo-tray.desktop"
if output=$(installer bash "$PARTIAL/install-linux.sh" install 2>&1); then
    fail "an archive without the launcher was installed"
fi
assert_contains "$output" "not found in $PARTIAL: depsilo-tray.desktop"$'\n'
assert_nothing_installed

cp "$ARCHIVE/depsilo-tray.desktop" "$PARTIAL/"
rm "$PARTIAL/depsilo-tray.svg"
INSTALLER_PATH="$RENDERER:$TOOLS"
output=$(installer bash "$PARTIAL/install-linux.sh" install 2>&1) ||
    fail "a missing icon must not fail the install:"$'\n'"$output"
INSTALLER_PATH="$TOOLS"
assert_installed
assert_contains "$output" "$PARTIAL/depsilo-tray.svg not found"
[ ! -e "$(installed_icon)" ] || fail "no icon can exist without its source"

# The script alone, in neither layout, says what each layout needs.
new_home
ALONE="$TMP/alone/dir"
mkdir -p "$ALONE"
cp "$ARCHIVE/install-linux.sh" "$ALONE/"
if output=$(installer bash "$ALONE/install-linux.sh" install 2>&1); then
    fail "the installer ran without anything to install"
fi
assert_contains "$output" "release archive: depsilo-tray and depsilo-tray.desktop in $ALONE"
assert_contains "$output" "repository:      bin/depsilo-tray and assets/linux/depsilo-tray.desktop in $TMP/alone"
assert_nothing_installed

# ── Read from standard input: no location, so no layout ─────────────
# "bash -s <command> < install-linux.sh" (for example over ssh) gives the
# script no path. The commands that need no sources work all the same.
new_home
installer bash "$ARCHIVE/install-linux.sh" install >/dev/null 2>&1
installer bash -s autostart-enable < "$ARCHIVE/install-linux.sh" >/dev/null ||
    fail "autostart-enable failed when the installer was read from standard input"
[ "$(readlink "$(installed_autostart)")" = "$(installed_desktop)" ] ||
    fail "autostart-enable from standard input must link the installed launcher"
installer bash -s autostart-disable < "$ARCHIVE/install-linux.sh" >/dev/null ||
    fail "autostart-disable failed when the installer was read from standard input"
[ ! -e "$(installed_autostart)" ] && [ ! -L "$(installed_autostart)" ] ||
    fail "autostart-disable from standard input must remove the autostart entry"
installer bash -s uninstall < "$ARCHIVE/install-linux.sh" >/dev/null ||
    fail "uninstall failed when the installer was read from standard input"
[ ! -e "$(installed_bin)" ] && [ ! -e "$(installed_desktop)" ] ||
    fail "uninstall from standard input left files behind"

# install has nothing to resolve its sources from, even with the archive's
# files in the working directory, and says how to run it instead.
new_home
INSTALLER_CWD="$ARCHIVE"
if output=$(installer bash -s install < "$ARCHIVE/install-linux.sh" 2>&1); then
    fail "the installer installed without a location to find its files"
fi
assert_contains "$output" 'standard input'
assert_contains "$output" 'bash install-linux.sh install'
assert_nothing_installed
if output=$(installer bash -s autostart-enable < "$ARCHIVE/install-linux.sh" 2>&1); then
    fail "autostart-enable must fail before the application is installed"
fi
assert_contains "$output" "Run 'bash install-linux.sh install' first."
INSTALLER_CWD="$TMP/elsewhere"

# ── Repository layout: as the make targets call it, from the root ───
new_home
INSTALLER_CWD="$REPO"
output=$(installer bash scripts/install-linux.sh install 2>&1) || fail "repository install failed:"$'\n'"$output"
assert_installed
assert_contains "$output" 'Autostart on login:          make autostart-linux'
assert_contains "$output" 'skipping icon'

# The sources follow the script, not the working directory.
new_home
INSTALLER_CWD="$TMP/elsewhere"
INSTALLER_PATH="$RENDERER:$TOOLS"
output=$(installer bash "$REPO/scripts/install-linux.sh" install 2>&1) ||
    fail "repository install from another directory failed:"$'\n'"$output"
INSTALLER_PATH="$TOOLS"
assert_installed
[ "$(cat "$(installed_icon)")" = "$REPO/assets/macos/icon.svg" ] ||
    fail "repository install must render the repository icon"

# The real Makefile targets drive the real script against that installation.
# verify-scripts runs this test from make; flags of that invocation, such as
# -i or -n, must not reach the targets under test.
make_linux_target() {
    env -u MAKEFLAGS -u MFLAGS HOME="$FAKE_HOME" DEPSILO_INSTALL_LINUX_TEST_OS=Linux \
        make -C "$ROOT" --no-print-directory "$1" 2>&1
}
make_linux_target autostart-linux >/dev/null || fail "make autostart-linux failed"
[ "$(readlink "$(installed_autostart)")" = "$(installed_desktop)" ] ||
    fail "make autostart-linux must link the installed launcher into autostart"
make_linux_target unautostart-linux >/dev/null || fail "make unautostart-linux failed"
[ ! -e "$(installed_autostart)" ] && [ ! -L "$(installed_autostart)" ] ||
    fail "make unautostart-linux must remove the autostart entry"
make_linux_target uninstall-linux >/dev/null || fail "make uninstall-linux failed"
[ ! -e "$(installed_bin)" ] && [ ! -e "$(installed_desktop)" ] && [ ! -e "$(installed_icon)" ] ||
    fail "make uninstall-linux left files behind"
if output=$(make_linux_target autostart-linux); then
    fail "make autostart-linux must fail before the application is installed"
fi
assert_contains "$output" "Run 'make install-linux' first."
install_plan=$(env -u MAKEFLAGS -u MFLAGS make -C "$ROOT" -n install-linux)
assert_contains "$install_plan" './cmd/depsilo-tray'
assert_contains "$install_plan" 'bash scripts/install-linux.sh install'

# A checkout without the built binary points at the target that builds it.
new_home
rm "$REPO/bin/depsilo-tray"
if output=$(installer bash "$REPO/scripts/install-linux.sh" install 2>&1); then
    fail "a checkout without the tray binary was installed"
fi
assert_contains "$output" "$REPO/bin/depsilo-tray not found"
assert_contains "$output" "run 'make tray' first"
assert_nothing_installed

# ── Any other system is still refused ───────────────────────────────
new_home
INSTALLER_OS=Darwin
if output=$(installer bash "$ARCHIVE/install-linux.sh" install 2>&1); then
    fail "the installer ran on a system that is not Linux"
fi
assert_contains "$output" 'this script targets Linux'
assert_nothing_installed

# Users run the installer without the override, and then the answer is uname's.
# uname_stand_in <system> prints a directory whose uname reports that system.
uname_stand_in() {
    local dir="$TMP/uname-$1"
    mkdir "$dir"
    printf '#!/bin/sh\necho %s\n' "$1" > "$dir/uname"
    chmod +x "$dir/uname"
    printf '%s\n' "$dir"
}
INSTALLER_OS=

new_home
INSTALLER_PATH="$(uname_stand_in Darwin):$TOOLS"
if output=$(installer bash "$ARCHIVE/install-linux.sh" install 2>&1); then
    fail "the installer ran although uname does not report Linux"
fi
assert_contains "$output" 'this script targets Linux'
assert_nothing_installed

new_home
INSTALLER_PATH="$(uname_stand_in Linux):$TOOLS"
output=$(installer bash "$ARCHIVE/install-linux.sh" install 2>&1) ||
    fail "the installer refused a system whose uname reports Linux:"$'\n'"$output"
assert_installed

echo "linux tray installer tests passed"
