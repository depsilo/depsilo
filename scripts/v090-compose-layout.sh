#!/bin/sh
set -eu

die() {
  printf 'v0.9 compose layout refused: %s\n' "$1" >&2
  exit 1
}

require_no_nested_mounts() {
  root=$1
  if awk -v root="$root" '
    $5 != root && index($5, root "/") == 1 { found = 1 }
    END { exit(found ? 0 : 1) }
  ' /proc/self/mountinfo; then
    die "nested mount exists below $root"
  fi
}

validate_tree() {
  root=$1
  [ -d "$root" ] && [ ! -L "$root" ] || die "$root must be a real directory"
  require_no_nested_mounts "$root"
  unsafe=$(find "$root" \( -type l -o \( ! -type d ! -type f \) \) -print -quit)
  [ -z "$unsafe" ] || die "$root contains a symlink or non-regular entry"
  multiply_linked=$(find "$root" -type f -links +1 -print -quit)
  [ -z "$multiply_linked" ] || die "$root contains a multiply-linked file"
}

mode=${1:-}
case "$mode" in
  validate-data)
    [ "$#" -eq 2 ] || die 'validate-data requires DATA_ROOT'
    validate_tree "$2"
    ;;
  prepare)
    [ "$#" -eq 3 ] || die 'prepare requires DATA_ROOT STATE_ROOT'
    data_root=$2
    state_root=$3
    validate_tree "$data_root"
    validate_tree "$state_root"
    chown -R 10001:10001 "$data_root" "$state_root"
    unexpected=$(find "$data_root" "$state_root" \( ! -user 10001 -o ! -group 10001 \) -print -quit)
    [ -z "$unexpected" ] || die 'candidate ownership preparation left an unexpected owner'
    [ -f "$data_root/depsilo.db" ] && [ ! -L "$data_root/depsilo.db" ] \
      || die 'database changed file type during ownership preparation'
    [ -f "$state_root/config.toml" ] && [ ! -L "$state_root/config.toml" ] \
      || die 'config changed file type during ownership preparation'
    [ "$(stat -c %a "$state_root/config.toml")" = 600 ] \
      || die 'prepared config mode is not 0600'
    ;;
  *)
    die 'expected validate-data or prepare mode'
    ;;
esac
