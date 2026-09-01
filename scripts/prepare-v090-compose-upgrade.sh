#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

usage() {
  cat <<'EOF'
Usage: prepare-v090-compose-upgrade.sh \
  --source-dir DIR --state-dir DIR --backup-dir DIR \
  --old-container NAME --image EXACT_IMAGE

Prepare the bind-mounted layout shipped by Depsilo v0.9.0 for the fixed
UID/GID 10001:10001 candidate container. EXACT_IMAGE must be an image ID or digest.

DEPSILO_AUTH_JWT_SECRET is the candidate secret and must be at least 32 bytes.
To rotate, set DEPSILO_V090_AUTH_JWT_SECRET to the exact old value and
DEPSILO_ACCEPT_JWT_ROTATION=1. Otherwise the candidate value must match v0.9.0.
EOF
}

die() {
  printf 'v0.9 compose upgrade preparation refused: %s\n' "$1" >&2
  exit 1
}

source_arg=''
state_arg=''
backup_arg=''
old_container=''
candidate_image=''
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
layout_helper="$script_dir/v090-compose-layout.sh"

while (($# > 0)); do
  case "$1" in
    --source-dir|--state-dir|--backup-dir|--old-container|--image)
      (($# >= 2)) || die "missing value for $1"
      option=$1
      value=$2
      case "$option" in
        --source-dir) [[ -z "$source_arg" ]] || die 'duplicate --source-dir'; source_arg=$value ;;
        --state-dir) [[ -z "$state_arg" ]] || die 'duplicate --state-dir'; state_arg=$value ;;
        --backup-dir) [[ -z "$backup_arg" ]] || die 'duplicate --backup-dir'; backup_arg=$value ;;
        --old-container) [[ -z "$old_container" ]] || die 'duplicate --old-container'; old_container=$value ;;
        --image) [[ -z "$candidate_image" ]] || die 'duplicate --image'; candidate_image=$value ;;
      esac
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$source_arg" ]] || die 'missing --source-dir'
[[ -n "$state_arg" ]] || die 'missing --state-dir'
[[ -n "$backup_arg" ]] || die 'missing --backup-dir'
[[ -n "$old_container" ]] || die 'missing --old-container'
[[ -n "$candidate_image" ]] || die 'missing --image'
[[ -f "$layout_helper" && ! -L "$layout_helper" ]] \
  || die 'v0.9 Compose layout helper is unavailable'

for command in docker jq realpath sha256sum mktemp cp mv find od tr chmod id; do
  command -v "$command" >/dev/null 2>&1 || die "$command is required"
done

[[ "$old_container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] \
  || die 'old container name is invalid'
if [[ ! "$candidate_image" =~ ^sha256:[0-9a-f]{64}$ ]] &&
  [[ ! "$candidate_image" =~ ^[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]]; then
  die 'candidate image must be an exact sha256 image ID or repository digest'
fi

[[ -d "$source_arg" && ! -L "$source_arg" ]] || die 'source directory must be a real directory'
source_dir=$(realpath -e -- "$source_arg")
source_data="$source_dir/data"
source_config="$source_dir/config.toml"
source_database="$source_data/depsilo.db"
[[ -d "$source_data" && ! -L "$source_data" ]] || die 'source data must be a real directory'
[[ -f "$source_config" && ! -L "$source_config" ]] || die 'source config.toml must be a regular file'
[[ -f "$source_database" && ! -L "$source_database" ]] || die 'source data/depsilo.db must be a regular file'
[[ -s "$source_config" ]] || die 'source config.toml must not be empty'
[[ -s "$source_database" ]] || die 'source data/depsilo.db must not be empty'
database_header=$(od -An -tx1 -N16 -- "$source_database" | tr -d '[:space:]')
[[ "$database_header" == '53514c69746520666f726d6174203300' ]] \
  || die 'source data/depsilo.db does not have a SQLite 3 header'
database_backup_names=(depsilo.db)
for sidecar in depsilo.db-wal depsilo.db-shm; do
  if [[ -e "$source_data/$sidecar" || -L "$source_data/$sidecar" ]]; then
    [[ -f "$source_data/$sidecar" && ! -L "$source_data/$sidecar" ]] \
      || die "source data/$sidecar must be a regular file"
    database_backup_names+=("$sidecar")
  fi
done
resolve_absent_target() {
  local requested=$1
  local label=$2
  local parent base resolved_parent
  [[ ! -e "$requested" && ! -L "$requested" ]] || die "$label must not already exist"
  parent=$(dirname -- "$requested")
  base=$(basename -- "$requested")
  [[ "$base" != '.' && "$base" != '..' && -n "$base" ]] || die "$label has an invalid basename"
  [[ -d "$parent" && ! -L "$parent" ]] || die "$label parent must be a real existing directory"
  resolved_parent=$(realpath -e -- "$parent")
  printf '%s/%s\n' "$resolved_parent" "$base"
}

state_dir=$(resolve_absent_target "$state_arg" 'state target')
backup_dir=$(resolve_absent_target "$backup_arg" 'backup target')
[[ "$state_dir" != "$backup_dir" ]] || die 'state and backup targets must be distinct'
for path in "$source_dir" "$source_data" "$source_config" "$source_database" "$state_dir" "$backup_dir" "$layout_helper"; do
  [[ "$path" != *','* && "$path" != *$'\n'* ]] || die 'paths containing commas or newlines are unsupported'
done
case "$state_dir/" in "$source_dir/"*) die 'state target must be outside the v0.9 source directory' ;; esac
case "$backup_dir/" in "$source_dir/"*) die 'backup target must be outside the v0.9 source directory' ;; esac

configured_user=$(docker image inspect --format '{{.Config.User}}' "$candidate_image") \
  || die 'candidate image is unavailable'
[[ "$configured_user" == '10001:10001' ]] \
  || die 'candidate image must declare User 10001:10001'
docker run --rm --network none --user 0:0 --entrypoint /upgrade-helper \
  --mount "type=bind,src=$source_data,dst=/upgrade/data,readonly" \
  --mount "type=bind,src=$layout_helper,dst=/upgrade-helper,readonly" \
  "$candidate_image" validate-data /upgrade/data \
  || die 'source data contains an unsafe entry or nested mount'

container_json=$(docker inspect "$old_container") || die 'old container is unavailable'
jq -e 'length == 1 and .[0].State.Running == false' <<<"$container_json" >/dev/null \
  || die 'old container must exist and be stopped'
jq -e --arg data "$source_data" --arg config "$source_config" '
  .[0].Mounts as $mounts |
  ($mounts | length) == 2 and
  ([$mounts[] | select(.Type == "bind" and .Source == $data and .Destination == "/app/data")] | length) == 1 and
  ([$mounts[] | select(.Type == "bind" and .Source == $config and .Destination == "/app/config.toml")] | length) == 1
' <<<"$container_json" >/dev/null \
  || die 'old container does not have the exact v0.9 /app/data and /app/config.toml bind layout'
jq -e '
  [.[] | .Config.Env[]? | select(startswith("DEPSILO_CONFIG="))] ==
  ["DEPSILO_CONFIG=/app/config.toml"]
' <<<"$container_json" >/dev/null \
  || die 'old container does not use DEPSILO_CONFIG=/app/config.toml'

[[ -n "${DEPSILO_AUTH_JWT_SECRET:-}" ]] \
  || die 'DEPSILO_AUTH_JWT_SECRET must be set to the candidate signing secret'
legacy_jwt=${DEPSILO_V090_AUTH_JWT_SECRET:-$DEPSILO_AUTH_JWT_SECRET}
container_jwt=$(jq -er '
  [.[] | .Config.Env[]? | select(startswith("DEPSILO_AUTH_JWT_SECRET="))] as $values |
  if ($values | length) == 1
  then ($values[0] | sub("^DEPSILO_AUTH_JWT_SECRET="; ""))
  else error("expected exactly one JWT secret")
  end
' <<<"$container_json") || die 'old container must contain exactly one JWT secret'
[[ -n "$container_jwt" && "$container_jwt" == "$legacy_jwt" ]] \
  || die 'the v0.9 JWT secret does not exactly match the stopped v0.9 container'
if [[ "$DEPSILO_AUTH_JWT_SECRET" == 'change-me-in-production' ||
      "$DEPSILO_AUTH_JWT_SECRET" == [[:space:]]* ||
      "$DEPSILO_AUTH_JWT_SECRET" == *[[:space:]] ||
      ${#DEPSILO_AUTH_JWT_SECRET} -lt 32 ]]; then
  die 'candidate DEPSILO_AUTH_JWT_SECRET must be at least 32 bytes with no surrounding whitespace'
fi
jwt_rotated=false
if [[ "$legacy_jwt" != "$DEPSILO_AUTH_JWT_SECRET" ]]; then
  [[ "${DEPSILO_ACCEPT_JWT_ROTATION:-}" == 1 ]] \
    || die 'changing the v0.9 JWT secret requires DEPSILO_ACCEPT_JWT_ROTATION=1'
  jwt_rotated=true
fi
unset container_jwt legacy_jwt

state_parent=$(dirname -- "$state_dir")
state_base=$(basename -- "$state_dir")
backup_parent=$(dirname -- "$backup_dir")
backup_base=$(basename -- "$backup_dir")
state_tmp=''
backup_tmp=''

cleanup() {
  local temporary
  for temporary in "$state_tmp" "$backup_tmp"; do
    [[ -n "$temporary" && -d "$temporary" && ! -L "$temporary" ]] || continue
    if ! find "$temporary" -xdev -depth -delete >/dev/null 2>&1; then
      docker run --rm --network none --user 0:0 --entrypoint /bin/chown \
        --mount "type=bind,src=$temporary,dst=/cleanup" \
        "$candidate_image" -R "$(id -u):$(id -g)" /cleanup >/dev/null 2>&1 || true
      find "$temporary" -xdev -depth -delete >/dev/null 2>&1 || true
    fi
  done
}
trap cleanup EXIT INT TERM

backup_tmp=$(mktemp -d "$backup_parent/.${backup_base}.tmp.XXXXXX")
chmod 0700 "$backup_tmp"
cp -- "$source_config" "$backup_tmp/config.toml"
for database_name in "${database_backup_names[@]}"; do
  cp -- "$source_data/$database_name" "$backup_tmp/$database_name"
done
(
  cd "$backup_tmp"
  sha256sum config.toml "${database_backup_names[@]}" >SHA256SUMS
  sha256sum --check SHA256SUMS >/dev/null
)
mv --no-clobber --no-target-directory -- "$backup_tmp" "$backup_dir"
[[ ! -e "$backup_tmp" && -d "$backup_dir" && ! -L "$backup_dir" ]] \
  || die 'backup target appeared before its atomic rename'
backup_tmp=''

state_tmp=$(mktemp -d "$state_parent/.${state_base}.tmp.XXXXXX")
chmod 0700 "$state_tmp"
cp -- "$source_config" "$state_tmp/config.toml"
chmod 0600 "$state_tmp/config.toml"
cat >"$state_tmp/.depsilo-v090-bind-upgrade" <<EOF
format=depsilo-v090-bind-upgrade-v1
source_data=$source_data
backup_dir=$backup_dir
candidate_image=$candidate_image
EOF
chmod 0600 "$state_tmp/.depsilo-v090-bind-upgrade"

docker run --rm --network none --user 0:0 --entrypoint /upgrade-helper \
  --mount "type=bind,src=$source_data,dst=/upgrade/data" \
  --mount "type=bind,src=$state_tmp,dst=/upgrade/state" \
  --mount "type=bind,src=$layout_helper,dst=/upgrade-helper,readonly" \
  "$candidate_image" prepare /upgrade/data /upgrade/state \
  || die 'candidate ownership preparation rejected the validated layout'

[[ -f "$source_database" && ! -L "$source_database" ]] \
  || die 'candidate ownership preparation changed the database file type'
(
  cd "$backup_dir"
  sha256sum --check SHA256SUMS >/dev/null
)

mv --no-clobber --no-target-directory -- "$state_tmp" "$state_dir"
[[ ! -e "$state_tmp" && -d "$state_dir" && ! -L "$state_dir" ]] \
  || die 'state target appeared before its atomic rename'
state_tmp=''

trap - EXIT INT TERM
printf 'Prepared v0.9 bind-layout state at %s; verified backup at %s.\n' "$state_dir" "$backup_dir"
if [[ "$jwt_rotated" == true ]]; then
  printf 'JWT signing secret was rotated; existing browser sessions must sign in again.\n'
else
  printf 'Use compose.v090-compat.yaml with this exact candidate image and the preserved JWT secret.\n'
fi
