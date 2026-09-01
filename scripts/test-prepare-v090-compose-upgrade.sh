#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
preparer="$root/scripts/prepare-v090-compose-upgrade.sh"
compat="$root/compose.v090-compat.yaml"
real_qualification="$root/scripts/test-v090-compose-upgrade.sh"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-v090-prep-test.XXXXXX")
cleanup() {
    find "$fixture" -depth -delete >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

source_dir="$fixture/source"
state_dir="$fixture/state"
backup_dir="$fixture/backup"
fake_bin="$fixture/bin"
docker_log="$fixture/docker.log"
mkdir -p "$source_dir/data/cache" "$fake_bin"
printf '%s\n' 'config_version = 1' >"$source_dir/config.toml"
printf 'SQLite format 3\000fixture-state\n' >"$source_dir/data/depsilo.db"
printf '%s\n' 'sqlite-wal-state' >"$source_dir/data/depsilo.db-wal"
printf '%s\n' 'sqlite-shm-state' >"$source_dir/data/depsilo.db-shm"
printf '%s\n' 'cached-object' >"$source_dir/data/cache/object"

cat >"$fake_bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >>"$FAKE_DOCKER_LOG"
printf '\n' >>"$FAKE_DOCKER_LOG"
if [[ ${1:-} == image && ${2:-} == inspect ]]; then
    printf '%s\n' "${FAKE_IMAGE_USER:-10001:10001}"
    exit 0
fi
if [[ ${1:-} == inspect ]]; then
    jq -nc \
      --arg source "$FAKE_SOURCE" \
      --arg jwt "$FAKE_CONTAINER_JWT" \
      --arg config_path "${FAKE_CONTAINER_CONFIG:-/app/config.toml}" \
      --arg data_mount_type "${FAKE_DATA_MOUNT_TYPE:-bind}" \
      --argjson running "$FAKE_CONTAINER_RUNNING" \
      '[{State:{Running:$running},Config:{Env:[("DEPSILO_CONFIG="+$config_path),("DEPSILO_AUTH_JWT_SECRET="+$jwt)]},Mounts:[{Type:$data_mount_type,Source:($source+"/data"),Destination:"/app/data"},{Type:"bind",Source:($source+"/config.toml"),Destination:"/app/config.toml"}]}]'
    exit 0
fi
if [[ ${1:-} == run ]]; then
    if [[ " $* " == *' validate-data /upgrade/data '* ]] &&
        find "$FAKE_SOURCE/data" -xdev -type f -links +1 -print -quit | grep -q .; then
        exit 1
    fi
    exit 0
fi
echo "unexpected fake docker command: $*" >&2
exit 1
SH
chmod +x "$fake_bin/docker"

export FAKE_DOCKER_LOG="$docker_log"
export FAKE_SOURCE="$source_dir"
export FAKE_CONTAINER_JWT='v090-preserved-jwt-secret-0123456789'
export FAKE_CONTAINER_RUNNING=false
export FAKE_IMAGE_USER='10001:10001'
export FAKE_CONTAINER_CONFIG='/app/config.toml'
export FAKE_DATA_MOUNT_TYPE='bind'
export DEPSILO_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"
image="sha256:$(printf 'a%.0s' {1..64})"

PATH="$fake_bin:$PATH" bash "$preparer" \
    --source-dir "$source_dir" \
    --state-dir "$state_dir" \
    --backup-dir "$backup_dir" \
    --old-container depsilo-v090 \
    --image "$image"

test -f "$state_dir/config.toml"
test -f "$state_dir/.depsilo-v090-bind-upgrade"
test -f "$backup_dir/config.toml"
test -f "$backup_dir/depsilo.db"
test -f "$backup_dir/depsilo.db-wal"
test -f "$backup_dir/depsilo.db-shm"
test "$(stat -c '%a' "$state_dir/config.toml")" = 600
(cd "$backup_dir" && sha256sum --check SHA256SUMS >/dev/null)
grep -q '^run ' "$docker_log"
grep -Fq -- '--network none' "$docker_log"
grep -Fq -- '--user 0:0' "$docker_log"
grep -Fq -- "$image" "$docker_log"
grep -Fq -- 'v090-compose-layout.sh' "$docker_log"
grep -Fq -- 'dst=/upgrade-helper' "$docker_log"
grep -Fq -- 'prepare /upgrade/data /upgrade/state' "$docker_log"
grep -Eq -- "src=$fixture/\.state\.tmp\.[^,[:space:]]+,dst=/upgrade/state" "$docker_log"
if grep -Fq -- "src=$state_dir,dst=/upgrade/state" "$docker_log"; then
    echo 'candidate chown must operate on same-parent staged state, not the final target' >&2
    exit 1
fi

assert_rejected() {
    local description=$1
    shift
    if PATH="$fake_bin:$PATH" bash "$preparer" "$@" >/dev/null 2>&1; then
        echo "unsafe v0.9 preparation was accepted: $description" >&2
        exit 1
    fi
}

FAKE_CONTAINER_JWT='short-v090-secret'
DEPSILO_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"
set +e
weak_secret_output=$(PATH="$fake_bin:$PATH" bash "$preparer" \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-weak-jwt" \
    --backup-dir "$fixture/unused-backup-weak-jwt" \
    --old-container depsilo-v090 --image "$image" 2>&1)
weak_secret_status=$?
set -e
if [ "$weak_secret_status" -eq 0 ]; then
    echo 'v0.9 preparation accepted a short legacy JWT secret' >&2
    exit 1
fi
grep -Fq 'candidate DEPSILO_AUTH_JWT_SECRET must be at least 32 bytes' \
    <<<"$weak_secret_output"
test ! -e "$fixture/unused-state-weak-jwt"
test ! -e "$fixture/unused-backup-weak-jwt"
FAKE_CONTAINER_JWT='v090-preserved-jwt-secret-0123456789'
DEPSILO_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"

FAKE_CONTAINER_JWT='short-v090-secret'
export DEPSILO_V090_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"
DEPSILO_AUTH_JWT_SECRET='new-v091-jwt-secret-0123456789abcdef'
set +e
unconfirmed_rotation_output=$(PATH="$fake_bin:$PATH" bash "$preparer" \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-unconfirmed-rotation" \
    --backup-dir "$fixture/unused-backup-unconfirmed-rotation" \
    --old-container depsilo-v090 --image "$image" 2>&1)
unconfirmed_rotation_status=$?
set -e
if [ "$unconfirmed_rotation_status" -eq 0 ]; then
    echo 'v0.9 preparation rotated the JWT secret without explicit confirmation' >&2
    exit 1
fi
grep -Fq 'changing the v0.9 JWT secret requires DEPSILO_ACCEPT_JWT_ROTATION=1' \
    <<<"$unconfirmed_rotation_output"
test ! -e "$fixture/unused-state-unconfirmed-rotation"
test ! -e "$fixture/unused-backup-unconfirmed-rotation"

export DEPSILO_ACCEPT_JWT_ROTATION=1
rotation_state="$fixture/rotation-state"
rotation_backup="$fixture/rotation-backup"
rotation_output=$(PATH="$fake_bin:$PATH" bash "$preparer" \
    --source-dir "$source_dir" --state-dir "$rotation_state" \
    --backup-dir "$rotation_backup" --old-container depsilo-v090 --image "$image")
grep -Fq 'JWT signing secret was rotated; existing browser sessions must sign in again.' \
    <<<"$rotation_output"
test -f "$rotation_state/.depsilo-v090-bind-upgrade"
test -f "$rotation_backup/SHA256SUMS"
if grep -Fq "$DEPSILO_V090_AUTH_JWT_SECRET" <<<"$rotation_output" ||
    grep -Fq "$DEPSILO_AUTH_JWT_SECRET" <<<"$rotation_output" ||
    grep -Fq "$DEPSILO_V090_AUTH_JWT_SECRET" "$rotation_state/.depsilo-v090-bind-upgrade" ||
    grep -Fq "$DEPSILO_AUTH_JWT_SECRET" "$rotation_state/.depsilo-v090-bind-upgrade"; then
    echo 'v0.9 preparation disclosed a JWT secret' >&2
    exit 1
fi
unset DEPSILO_ACCEPT_JWT_ROTATION
unset DEPSILO_V090_AUTH_JWT_SECRET
FAKE_CONTAINER_JWT='v090-preserved-jwt-secret-0123456789'
DEPSILO_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"

outside_hardlink="$fixture/outside-cache-object"
ln "$source_dir/data/cache/object" "$outside_hardlink"
outside_hardlink_owner=$(stat -c '%u:%g' "$outside_hardlink")
assert_rejected 'source data containing a hard link to an inode outside the source tree' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-hardlink" \
    --backup-dir "$fixture/unused-backup-hardlink" --old-container depsilo-v090 --image "$image"
test "$(stat -c '%u:%g' "$outside_hardlink")" = "$outside_hardlink_owner"
rm "$outside_hardlink"

occupied_state="$fixture/occupied-state"
mkdir "$occupied_state"
printf '%s\n' keep >"$occupied_state/sentinel"
assert_rejected 'occupied state target' \
    --source-dir "$source_dir" --state-dir "$occupied_state" \
    --backup-dir "$fixture/unused-backup-1" --old-container depsilo-v090 --image "$image"
grep -qx keep "$occupied_state/sentinel"

missing_db="$fixture/missing-db"
mkdir -p "$missing_db/data"
cp "$source_dir/config.toml" "$missing_db/config.toml"
assert_rejected 'source without SQLite database' \
    --source-dir "$missing_db" --state-dir "$fixture/unused-state-2" \
    --backup-dir "$fixture/unused-backup-2" --old-container depsilo-v090 --image "$image"

invalid_db="$fixture/invalid-db"
mkdir -p "$invalid_db/data"
cp "$source_dir/config.toml" "$invalid_db/config.toml"
printf '%s\n' 'not-sqlite' >"$invalid_db/data/depsilo.db"
assert_rejected 'source with invalid SQLite header' \
    --source-dir "$invalid_db" --state-dir "$fixture/unused-state-invalid-db" \
    --backup-dir "$fixture/unused-backup-invalid-db" --old-container depsilo-v090 --image "$image"

state_link="$fixture/state-link"
ln -s "$source_dir" "$state_link"
assert_rejected 'symlink state target' \
    --source-dir "$source_dir" --state-dir "$state_link" \
    --backup-dir "$fixture/unused-backup-3" --old-container depsilo-v090 --image "$image"

FAKE_CONTAINER_RUNNING=true
assert_rejected 'running old container' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-4" \
    --backup-dir "$fixture/unused-backup-4" --old-container depsilo-v090 --image "$image"
FAKE_CONTAINER_RUNNING=false

DEPSILO_AUTH_JWT_SECRET='different-secret'
assert_rejected 'changed JWT secret' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-5" \
    --backup-dir "$fixture/unused-backup-5" --old-container depsilo-v090 --image "$image"
DEPSILO_AUTH_JWT_SECRET="$FAKE_CONTAINER_JWT"

occupied_backup="$fixture/occupied-backup"
mkdir "$occupied_backup"
printf '%s\n' keep >"$occupied_backup/sentinel"
assert_rejected 'occupied backup target' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-occupied-backup" \
    --backup-dir "$occupied_backup" --old-container depsilo-v090 --image "$image"
grep -qx keep "$occupied_backup/sentinel"

backup_link="$fixture/backup-link"
ln -s "$source_dir" "$backup_link"
assert_rejected 'symlink backup target' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-backup-link" \
    --backup-dir "$backup_link" --old-container depsilo-v090 --image "$image"

FAKE_IMAGE_USER='0:0'
assert_rejected 'candidate without fixed image user' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-image-user" \
    --backup-dir "$fixture/unused-backup-image-user" --old-container depsilo-v090 --image "$image"
FAKE_IMAGE_USER='10001:10001'

FAKE_DATA_MOUNT_TYPE='volume'
assert_rejected 'old container using a volume instead of shipped bind layout' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-volume" \
    --backup-dir "$fixture/unused-backup-volume" --old-container depsilo-v090 --image "$image"
FAKE_DATA_MOUNT_TYPE='bind'

FAKE_CONTAINER_CONFIG='/wrong/config.toml'
assert_rejected 'old container with wrong config path' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-config-path" \
    --backup-dir "$fixture/unused-backup-config-path" --old-container depsilo-v090 --image "$image"
FAKE_CONTAINER_CONFIG='/app/config.toml'

assert_rejected 'mutable latest image' \
    --source-dir "$source_dir" --state-dir "$fixture/unused-state-6" \
    --backup-dir "$fixture/unused-backup-6" --old-container depsilo-v090 \
    --image ghcr.io/depsilo/depsilo:latest

grep -Fq '${DEPSILO_V090_DATA_DIR:?' "$compat"
grep -Fq '${DEPSILO_UPGRADE_STATE_DIR:?' "$compat"
grep -Fq 'DEPSILO_CONFIG: /root/.depsilo/config.toml' "$compat"
grep -Fq '${DEPSILO_AUTH_JWT_SECRET:?' "$compat"
test "$(grep -Fc 'create_host_path: false' "$compat")" = 2
if grep -Eq 'docker[[:space:]]+(compose[[:space:]]+)?down([^#]*[[:space:]])-v([[:space:]]|$)|docker[[:space:]]+volume|sed[[:space:]]+-i' "$preparer"; then
    echo 'preparation must not remove Compose volumes, guess volumes, or rewrite config with sed' >&2
    exit 1
fi
if grep -q ':latest' "$compat"; then
    echo 'v0.9 compatibility compose must require an exact candidate image' >&2
    exit 1
fi
grep -Fq '71e9f029877e66ae9fdb353e134358bfa55c280c' "$real_qualification"
grep -Fq '8aba53b51aa8c8c953fe28f5edc2981b4350172c1a3fc4ead0303cf868a3b34a' "$real_qualification"

echo 'v0.9 compose preparation safety tests passed'
