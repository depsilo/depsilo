#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
baseline_tag=${DEPSILO_UPGRADE_BASELINE_TAG:-v0.9.0}
baseline_commit=${DEPSILO_V090_TAG_COMMIT:-71e9f029877e66ae9fdb353e134358bfa55c280c}
baseline_compose_sha256=${DEPSILO_V090_COMPOSE_SHA256:-8aba53b51aa8c8c953fe28f5edc2981b4350172c1a3fc4ead0303cf868a3b34a}
baseline_image=${DEPSILO_V090_IMAGE:-ghcr.io/depsilo/depsilo@sha256:fbc16cae946eccfdbb115ec6523047702bef5d1e3e15359f9c16dcd6a8e6e56e}
mock_image=${DEPSILO_V090_MOCK_IMAGE:-python@sha256:500143b21c4a79c30f55075414929ad7ad8c55e251665e6d93efe7822947079d}
candidate_input=${DEPSILO_UPGRADE_IMAGE:-}
project="depsilo-v090-compose-$$"
network="${project}-network"
mock_container="${project}-upstream"
candidate_project="${project}-candidate"
candidate_network="${candidate_project}_default"
candidate_tag="${project}:candidate"
work=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-v090-compose-upgrade.XXXXXX")
old_source="$work/v090"
old_compose="$old_source/docker-compose.yml"
old_override="$work/v090-image.override.yaml"
candidate_state="$work/candidate-state"
backup_dir="$work/backup"
mock_root="$work/mock"
old_container=''
candidate_container=''
nested_old_container=''
nested_mount_target=''
old_image_id=''
candidate_image_id=''
candidate_built=false

for command in docker git curl jq python3 sha256sum stat find findmnt; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }
done
docker compose version >/dev/null 2>&1 || { echo 'Docker Compose is required' >&2; exit 1; }
resolved_baseline_commit=$(git -C "$root" rev-parse --verify --quiet "refs/tags/$baseline_tag^{commit}") || {
  echo "upgrade baseline tag is unavailable: $baseline_tag" >&2
  exit 1
}
[[ "$resolved_baseline_commit" == "$baseline_commit" ]] || {
  echo "upgrade baseline tag does not resolve to the qualified commit: $baseline_tag" >&2
  exit 1
}

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  for container in "$candidate_container" "$old_container" "$mock_container" "$nested_old_container"; do
    [[ -n "$container" ]] || continue
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
  if [[ -n "$nested_mount_target" ]] && findmnt --mountpoint "$nested_mount_target" >/dev/null 2>&1; then
    docker run --rm --network none --privileged --pid=host --user 0:0 \
      --entrypoint /usr/bin/nsenter "$candidate_image_id" \
      -t 1 -m -- umount "$nested_mount_target" >/dev/null 2>&1 || true
    if findmnt --mountpoint "$nested_mount_target" >/dev/null 2>&1; then
      printf 'failed to unmount nested-mount fixture %s\n' "$nested_mount_target" >&2
      exit_code=1
    fi
  fi
  for cleanup_network in "$candidate_network" "$network"; do
    docker network rm "$cleanup_network" >/dev/null 2>&1 || true
    if docker network inspect "$cleanup_network" >/dev/null 2>&1; then
      printf 'failed to clean qualification network %s\n' "$cleanup_network" >&2
      exit_code=1
    fi
  done
  cleanup_image=$candidate_image_id
  [[ -n "$cleanup_image" ]] || cleanup_image=$old_image_id
  if [[ -n "$cleanup_image" && -d "$work" && ! -L "$work" ]]; then
    docker run --rm --network none --user 0:0 --entrypoint /bin/chown \
      --mount "type=bind,src=$work,dst=/cleanup" \
      "$cleanup_image" -R "$(id -u):$(id -g)" /cleanup >/dev/null 2>&1 || true
  fi
  if [[ -d "$work" && ! -L "$work" ]]; then
    find "$work" -xdev -depth -delete >/dev/null 2>&1 || true
  fi
  if [[ -e "$work" || -L "$work" ]]; then
    printf 'failed to clean qualification work tree %s\n' "$work" >&2
    exit_code=1
  fi
  if [[ "$candidate_built" == true ]] &&
      ! docker image rm "$candidate_tag" >/dev/null 2>&1; then
    printf 'failed to clean qualification candidate image %s\n' "$candidate_tag" >&2
    exit_code=1
  fi
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

available_port() {
  python3 - <<'PY'
import socket
with socket.socket() as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
PY
}

container_logs() {
  local container=$1
  [[ -n "$container" ]] || return 0
  docker logs "$container" >&2 || true
}

wait_ready() {
  local origin=$1
  local container=$2
  for _ in $(seq 1 300); do
    if curl --fail --silent "$origin/ready" >/dev/null; then
      return 0
    fi
    if ! docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null | grep -qx true; then
      container_logs "$container"
      echo "Depsilo exited before readiness at $origin" >&2
      return 1
    fi
    sleep 0.1
  done
  container_logs "$container"
  echo "Depsilo did not become ready at $origin" >&2
  return 1
}

docker pull "$baseline_image" >/dev/null
docker pull "$mock_image" >/dev/null
old_image_id=$(docker image inspect --format '{{.Id}}' "$baseline_image")
[[ "$old_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'v0.9 image did not resolve to an exact image ID' >&2; exit 1; }
old_version=$(docker run --rm --network none "$old_image_id" version)
grep -Eq '(^|[^0-9])0\.9\.0([^0-9]|$)' <<<"$old_version" || {
  echo "baseline image is not v0.9.0: $old_version" >&2
  exit 1
}

if [[ -n "$candidate_input" ]]; then
  docker image inspect "$candidate_input" >/dev/null
  candidate_image_id=$(docker image inspect --format '{{.Id}}' "$candidate_input")
else
  docker build \
    --build-arg VERSION=0.9.1-compose-qualification \
    --build-arg COMMIT=compose-qualification \
    --build-arg BUILD_DATE=compose-qualification \
    --tag "$candidate_tag" \
    "$root"
  candidate_built=true
  candidate_image_id=$(docker image inspect --format '{{.Id}}' "$candidate_tag")
fi
[[ "$candidate_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'candidate did not resolve to an exact image ID' >&2; exit 1; }
[[ "$(docker image inspect --format '{{.Config.User}}' "$candidate_image_id")" == '10001:10001' ]] || {
  echo 'candidate image does not declare User 10001:10001' >&2
  exit 1
}
docker run --rm --network none --entrypoint /bin/sh "$candidate_image_id" -euc \
  'test "$(id -u):$(id -g)" = 10001:10001'

nested_fixture="$work/nested-mount-contract"
nested_source="$nested_fixture/source"
nested_victim="$nested_fixture/external-victim"
nested_mount_target="$nested_source/data/nested"
mkdir -p "$nested_mount_target" "$nested_victim"
printf '%s\n' 'config_version = 1' >"$nested_source/config.toml"
printf 'SQLite format 3\000nested-mount-contract\n' >"$nested_source/data/depsilo.db"
docker run --rm --network none --user 0:0 --entrypoint /bin/sh \
  --mount "type=bind,src=$nested_victim,dst=/external" \
  "$candidate_image_id" -euc 'touch /external/victim; chown 0:0 /external/victim'
docker run --rm --network none --privileged --pid=host --user 0:0 \
  --entrypoint /usr/bin/nsenter "$candidate_image_id" \
  -t 1 -m -- mount --bind "$nested_victim" "$nested_mount_target"
findmnt --mountpoint "$nested_mount_target" >/dev/null || {
  echo 'privileged nested-mount fixture is not visible in the host mount namespace' >&2
  exit 1
}
nested_old_container="${project}-nested-old"
docker create --name "$nested_old_container" \
  --mount "type=bind,src=$nested_source/data,dst=/app/data" \
  --mount "type=bind,src=$nested_source/config.toml,dst=/app/config.toml" \
  --env DEPSILO_CONFIG=/app/config.toml \
  --env DEPSILO_AUTH_JWT_SECRET=nested-mount-contract-secret \
  --entrypoint /bin/true "$old_image_id" >/dev/null
set +e
DEPSILO_AUTH_JWT_SECRET=nested-mount-contract-secret \
  "$root/scripts/prepare-v090-compose-upgrade.sh" \
    --source-dir "$nested_source" \
    --state-dir "$nested_fixture/state" \
    --backup-dir "$nested_fixture/backup" \
    --old-container "$nested_old_container" \
    --image "$candidate_image_id" \
    >"$nested_fixture/preparer.log" 2>&1
nested_prepare_status=$?
set -e
nested_victim_owner=$(stat -c '%u:%g' "$nested_victim/victim")
if [[ "$nested_prepare_status" -eq 0 ]]; then
  printf 'preparer accepted nested mount; external victim owner=%s, want 0:0\n' "$nested_victim_owner" >&2
  exit 1
fi
grep -q 'nested mount' "$nested_fixture/preparer.log" || {
  cat "$nested_fixture/preparer.log" >&2
  echo 'preparer failed for a reason other than its nested-mount guard' >&2
  exit 1
}
[[ "$nested_victim_owner" == '0:0' ]] || {
  printf 'nested-mount rejection changed external victim owner to %s\n' "$nested_victim_owner" >&2
  exit 1
}
[[ ! -e "$nested_fixture/state" && ! -e "$nested_fixture/backup" ]] || {
  echo 'nested-mount guard ran after creating state or backup output' >&2
  exit 1
}
docker rm "$nested_old_container" >/dev/null
nested_old_container=''
docker run --rm --network none --privileged --pid=host --user 0:0 \
  --entrypoint /usr/bin/nsenter "$candidate_image_id" \
  -t 1 -m -- umount "$nested_mount_target"
findmnt --mountpoint "$nested_mount_target" >/dev/null 2>&1 && {
  echo 'nested-mount fixture remained mounted after its contract' >&2
  exit 1
}
nested_mount_target=''

mkdir -p "$old_source/data/cache" "$mock_root/upgrade-fixture/-"
git -C "$root" show "$baseline_tag:docker-compose.yml" >"$old_compose"
read -r actual_compose_sha256 _ < <(sha256sum -- "$old_compose")
[[ "$actual_compose_sha256" == "$baseline_compose_sha256" ]] || {
  echo 'v0.9 Compose file does not match the qualified shipped layout' >&2
  exit 1
}
cat >"$old_override" <<'YAML'
services:
  depsilo:
    build: null
    image: ${DEPSILO_V090_EXACT_IMAGE:?set exact v0.9 image ID}
networks:
  default:
    external: true
    name: ${DEPSILO_V090_NETWORK:?set qualification network}
YAML

cat >"$mock_root/upgrade-fixture/index.html" <<'EOF'
{"name":"upgrade-fixture","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"upgrade-fixture","version":"1.0.0","dist":{"tarball":"http://depsilo-v090-upstream:8080/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz"}}}}
EOF
printf '%s\n' 'depsilo-v0.9.0-compose-cache-artifact' >"$mock_root/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz"

server_port=$(available_port)
origin="http://127.0.0.1:$server_port"
legacy_jwt_secret='short-v090-secret'
candidate_jwt_secret='v091-compose-strong-jwt-secret-0123456789abcdef'
admin_username='compose-upgrade-admin'
admin_password='Compose&Upgrade-Password-47'
license_key='depsilo-compose-upgrade-contract-license-key'
npm_accept='application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*'

cat >"$old_source/config.toml" <<EOF
config_version = 1

[server]
host = "0.0.0.0"
port = 23333
log_level = "warn"

[database]
driver = "sqlite"
dsn = "./data/depsilo.db"

[storage]
type = "local"
path = "./data/cache"

[cache]
max_size_gb = 1
ttl_index = "168h"
ttl_blob = "168h"
lru_threshold = 90

[auth]
enabled = true
jwt_secret = "compose-file-secret-overridden-by-preserved-env"
token_ttl = "1h"

[upstream_updates]
enabled = false

[security]
enabled = false

[supply_chain]
min_release_age_enabled = false

[supply_chain.blocklist]
enabled = false

[supply_chain.tamper_detection]
enabled = false

[[npm.upstreams]]
name = "upgrade-fixture"
url = "http://depsilo-v090-upstream:8080"
priority = 1
probe_mode = "passive"
probe_interval = "30m"
EOF
old_config_checksum=$(sha256sum "$old_source/config.toml" | cut -d ' ' -f 1)

docker network create "$network" >/dev/null
docker run --detach --name "$mock_container" \
  --network "$network" --network-alias depsilo-v090-upstream \
  --entrypoint python \
  --mount "type=bind,src=$mock_root,dst=/www,readonly" \
  "$mock_image" -m http.server 8080 --directory /www >/dev/null
mock_ready=false
for _ in $(seq 1 100); do
  if docker exec "$mock_container" python -c \
    'import urllib.request; urllib.request.urlopen("http://127.0.0.1:8080/upgrade-fixture/", timeout=1).read()' \
    >/dev/null 2>&1; then
    mock_ready=true
    break
  fi
  docker inspect --format '{{.State.Running}}' "$mock_container" 2>/dev/null | grep -qx true || break
  sleep 0.05
done
if [[ "$mock_ready" != true ]]; then
  docker logs "$mock_container" >&2 || true
  echo 'pinned mock Upstream did not become ready' >&2
  exit 1
fi

compose_old=(docker compose --project-name "$project" --file "$old_compose" --file "$old_override")
DEPSILO_V090_EXACT_IMAGE="$old_image_id" \
DEPSILO_V090_NETWORK="$network" \
DEPSILO_AUTH_JWT_SECRET="$legacy_jwt_secret" \
DEPSILO_ADMIN_USERNAME="$admin_username" \
DEPSILO_ADMIN_PASSWORD="$admin_password" \
PORT="$server_port" \
  "${compose_old[@]}" up --detach --no-build depsilo >/dev/null
old_container=$(DEPSILO_V090_EXACT_IMAGE="$old_image_id" DEPSILO_V090_NETWORK="$network" \
  DEPSILO_AUTH_JWT_SECRET="$legacy_jwt_secret" DEPSILO_ADMIN_PASSWORD="$admin_password" \
  PORT="$server_port" "${compose_old[@]}" ps --quiet depsilo)
[[ -n "$old_container" ]] || { echo 'v0.9 Compose did not create a service container' >&2; exit 1; }
wait_ready "$origin" "$old_container"

login_payload=$(jq -nc --arg username "$admin_username" --arg password "$admin_password" \
  '{username: $username, password: $password}')
old_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
api_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data '{"name":"v090-compose-token","permissions":"readonly","ttl":"7d"}' \
  "$origin/api/v1/admin/tokens" | jq -er '.token')

trial_status=$(curl --fail --silent --show-error --request POST \
  --header "Authorization: Bearer $old_admin_token" \
  "$origin/api/v1/admin/license/trial/activate")
jq -e '.is_pro == true and .source == "trial" and .trial_used == true and .trial_available == false' \
  <<<"$trial_status" >/dev/null
license_payload=$(jq -nc --arg key "$license_key" '{key: $key}')
paid_status=$(curl --fail --silent --show-error --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$license_payload" "$origin/api/v1/admin/license/key")
jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .license_key_masked == "depsilo-***"' \
  <<<"$paid_status" >/dev/null

if ! seeded_metadata=$(curl --fail-with-body --silent --show-error --header "Accept: $npm_accept" \
  "$origin/npm/upgrade-fixture"); then
  container_logs "$old_container"
  docker logs "$mock_container" >&2 || true
  exit 1
fi
jq -e '.versions["1.0.0"].name == "upgrade-fixture"' <<<"$seeded_metadata" >/dev/null
old_tarball_url=$(jq -er '.versions["1.0.0"].dist.tarball' <<<"$seeded_metadata")
old_artifact=$(curl --fail --silent --show-error "$old_tarball_url")
[[ "$old_artifact" == 'depsilo-v0.9.0-compose-cache-artifact' ]] || {
  echo 'v0.9 Compose did not seed the expected artifact cache' >&2
  exit 1
}

docker stop --time 20 "$old_container" >/dev/null
docker stop --time 10 "$mock_container" >/dev/null
database="$old_source/data/depsilo.db"
[[ -f "$database" ]] || { echo 'v0.9 Compose did not create /app/data/depsilo.db' >&2; exit 1; }
database_identity=$(stat -c '%d:%i' "$database")

python3 - "$database" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
checks = {
    "administrator": database.execute("SELECT COUNT(*) FROM users WHERE username = 'compose-upgrade-admin' AND role = 'admin' AND enabled = 1").fetchone()[0],
    "api token": database.execute("SELECT COUNT(*) FROM api_tokens WHERE name = 'v090-compose-token' AND permissions = 'readonly'").fetchone()[0],
    "npm metadata": database.execute("SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/metadata.json'").fetchone()[0],
    "npm artifact": database.execute("SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz'").fetchone()[0],
    "trial": database.execute("SELECT COUNT(*) FROM trial_records").fetchone()[0],
    "license": database.execute("SELECT COUNT(*) FROM license_storages").fetchone()[0],
}
failed = [name for name, count in checks.items() if count != 1]
if failed:
    raise SystemExit("v0.9 Compose fixture is incomplete: " + ", ".join(failed))
PY

DEPSILO_V090_AUTH_JWT_SECRET="$legacy_jwt_secret" \
DEPSILO_AUTH_JWT_SECRET="$candidate_jwt_secret" \
DEPSILO_ACCEPT_JWT_ROTATION=1 \
  "$root/scripts/prepare-v090-compose-upgrade.sh" \
    --source-dir "$old_source" \
    --state-dir "$candidate_state" \
    --backup-dir "$backup_dir" \
    --old-container "$old_container" \
    --image "$candidate_image_id"

(cd "$backup_dir" && sha256sum --check SHA256SUMS >/dev/null)
[[ "$(stat -c '%d:%i' "$database")" == "$database_identity" ]] || {
  echo 'preparation replaced the v0.9 SQLite database instead of preserving it' >&2
  exit 1
}

compose_candidate=(docker compose --project-name "$candidate_project" --file "$root/compose.v090-compat.yaml")
DEPSILO_IMAGE="$candidate_image_id" \
DEPSILO_V090_DATA_DIR="$old_source/data" \
DEPSILO_UPGRADE_STATE_DIR="$candidate_state" \
DEPSILO_AUTH_JWT_SECRET="$candidate_jwt_secret" \
PORT="$server_port" \
  "${compose_candidate[@]}" up --detach depsilo >/dev/null
candidate_container=$(DEPSILO_IMAGE="$candidate_image_id" \
  DEPSILO_V090_DATA_DIR="$old_source/data" DEPSILO_UPGRADE_STATE_DIR="$candidate_state" \
  DEPSILO_AUTH_JWT_SECRET="$candidate_jwt_secret" PORT="$server_port" \
  "${compose_candidate[@]}" ps --quiet depsilo)
[[ -n "$candidate_container" ]] || { echo 'candidate Compose did not create a service container' >&2; exit 1; }
wait_ready "$origin" "$candidate_container"

[[ "$(docker inspect --format '{{.Config.User}}' "$candidate_container")" == '10001:10001' ]] || {
  echo 'candidate container is not running as 10001:10001' >&2
  exit 1
}
docker exec "$candidate_container" /bin/sh -euc '
  test "$(id -u):$(id -g)" = 10001:10001
  test -f /app/data/depsilo.db
  test ! -e /root/.depsilo/data/depsilo.db
  test "$(find /root/.depsilo -name depsilo.db -type f | wc -l)" -eq 0
'
[[ "$(stat -c '%d:%i' "$database")" == "$database_identity" ]] || {
  echo 'candidate did not reopen the original /app/data/depsilo.db inode' >&2
  exit 1
}

old_jwt_status=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --header "Authorization: Bearer $old_admin_token" "$origin/api/v1/auth/me")
[[ "$old_jwt_status" == 401 ]] || {
  echo "rotated candidate accepted the old v0.9 JWT: HTTP $old_jwt_status" >&2
  exit 1
}
current_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
curl --fail --silent --show-error --header "Authorization: Bearer $api_token" \
  "$origin/api/v1/auth/me" | jq -e '.username == "compose-upgrade-admin" and .auth_method == "api_token" and .token_permissions == "readonly"' >/dev/null

# Unsigned v0.9 npm cache entries have no selected-source or exact-target
# provenance. The candidate must fail closed while that source is offline.
legacy_artifact_status=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "$old_tarball_url")
[[ "$legacy_artifact_status" == 404 ]] || {
  echo "candidate accepted an unsigned v0.9 npm artifact URL: HTTP $legacy_artifact_status" >&2
  exit 1
}
offline_metadata_status=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --header "Accept: $npm_accept" "$origin/npm/upgrade-fixture")
[[ "$offline_metadata_status" == 502 ]] || {
  echo "candidate reused unsigned v0.9 npm metadata while its Upstream was offline: HTTP $offline_metadata_status" >&2
  exit 1
}

# Re-enable the same configured source only after the fail-closed assertions;
# the candidate must refetch metadata and issue a new source-bound signed URL.
docker start "$mock_container" >/dev/null
docker network connect --alias depsilo-v090-upstream "$candidate_network" "$mock_container"
mock_ready=false
for _ in $(seq 1 100); do
  if docker exec "$candidate_container" wget --quiet --output-document /dev/null \
    'http://depsilo-v090-upstream:8080/upgrade-fixture/' \
    >/dev/null 2>&1; then
    mock_ready=true
    break
  fi
  docker inspect --format '{{.State.Running}}' "$mock_container" 2>/dev/null | grep -qx true || break
  sleep 0.05
done
[[ "$mock_ready" == true ]] || { echo 'restarted pinned mock Upstream was not reachable from the candidate network' >&2; exit 1; }

current_metadata=$(curl --fail --silent --show-error --header "Accept: $npm_accept" \
  "$origin/npm/upgrade-fixture")
jq -e '.versions["1.0.0"].name == "upgrade-fixture"' <<<"$current_metadata" >/dev/null
current_tarball_url=$(jq -er '.versions["1.0.0"].dist.tarball' <<<"$current_metadata")
[[ "$current_tarball_url" == *'/__depsilo_tarball_v1/'* ]] || {
  echo 'candidate did not emit a signed npm artifact URL after provenance refetch' >&2
  exit 1
}
current_artifact=$(curl --fail --silent --show-error "$current_tarball_url")
[[ "$current_artifact" == 'depsilo-v0.9.0-compose-cache-artifact' ]] || {
  echo 'candidate did not fetch the artifact through fresh source-bound provenance' >&2
  exit 1
}

python3 - "$database" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
checks = {
    "legacy npm metadata": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/metadata.json'"
    ).fetchone()[0],
    "legacy npm artifact": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz'"
    ).fetchone()[0],
    "source-bound npm metadata": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm-exact-v1/upgrade-fixture/metadata.json'"
    ).fetchone()[0],
    "source-bound npm artifact": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key GLOB "
        "'npm-exact-v1/upgrade-fixture/-/__depsilo_tarball_v1/objects/*/upgrade-fixture-1.0.0.tgz'"
    ).fetchone()[0],
}
failed = [name for name, count in checks.items() if count != 1]
if failed:
    raise SystemExit("Compose npm provenance cache contract is incomplete: " + ", ".join(failed))
PY

current_paid_status=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/admin/license/status")
jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .trial_available == false and .license_key_masked == "depsilo-***"' \
  <<<"$current_paid_status" >/dev/null

config_inode_before=$(docker exec "$candidate_container" stat -c '%d:%i' /root/.depsilo/config.toml)
settings_result=$(curl --fail --silent --show-error --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $current_admin_token" \
  --data '{"server":{"log_level":"debug"}}' \
  "$origin/api/v1/admin/settings")
jq -e '.configured.server.log_level == "debug"' <<<"$settings_result" >/dev/null
config_inode_after=$(docker exec "$candidate_container" stat -c '%d:%i' /root/.depsilo/config.toml)
[[ "$config_inode_after" != "$config_inode_before" ]] || {
  echo 'Admin Settings did not atomically replace the prepared config file' >&2
  exit 1
}
docker exec "$candidate_container" /bin/sh -euc '
  test "$(stat -c %a /root/.depsilo/config.toml)" = 600
  grep -q "log_level = \"debug\"" /root/.depsilo/config.toml
  test -z "$(find /root/.depsilo -maxdepth 1 -name ".config.toml.tmp-*" -print -quit)"
  test ! -e /root/.depsilo/data/depsilo.db
'
[[ "$(sha256sum "$old_source/config.toml" | cut -d ' ' -f 1)" == "$old_config_checksum" ]] || {
  echo 'Admin Settings overwrote the original v0.9 /app/config.toml bind' >&2
  exit 1
}
[[ "$(stat -c '%d:%i' "$database")" == "$database_identity" ]] || {
  echo 'Admin Settings caused a different SQLite database to appear' >&2
  exit 1
}
(cd "$backup_dir" && sha256sum --check SHA256SUMS >/dev/null)

echo 'v0.9 shipped-Compose bind-layout upgrade qualification passed'
