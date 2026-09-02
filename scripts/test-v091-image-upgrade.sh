#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
baseline_tag=v0.9.1
baseline_commit=773b9ad673615d5df6a8281f7cb658e3df84527d
baseline_compose_sha256=5abaad918604e045a32eacb81a4db14019e6a3c3d33d2f82be61e3bbe1a2a3ae
baseline_image_index=ghcr.io/depsilo/depsilo@sha256:bd3a2aeb8f7f461ed91cd583edc16e8f8958f103ebeb4946c626fd2a0d60b8f6
baseline_image_amd64=ghcr.io/depsilo/depsilo@sha256:c242b5dba39aa891cb49ba815d3232f06a59344682a97c0a6f2841bdeb0dd571
baseline_image_arm64=ghcr.io/depsilo/depsilo@sha256:2bef80088d03255a02b512763196006132e7827688b6400c9cd98286e2ee889a
candidate_input=${DEPSILO_UPGRADE_IMAGE:-}
project="depsilo-v091-upgrade-$$"
container_name="${project}-server"
candidate_tag="${project}:candidate"
work=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-v091-image-upgrade.XXXXXX")
baseline_compose="$work/compose.yaml"
override_compose="$work/image.override.yaml"
database_snapshot="$work/database"
active_image=''
container_id=''
candidate_image_id=''
candidate_built=false
compose_initialized=false
server_port=''
bootstrap_token=''
jwt_secret=''

for command in docker git curl jq python3 sha256sum; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }
done
docker compose version >/dev/null 2>&1 || { echo 'Docker Compose is required' >&2; exit 1; }
docker buildx version >/dev/null 2>&1 || { echo 'Docker Buildx is required' >&2; exit 1; }

engine_arch=$(docker version --format '{{.Server.Arch}}')
case "$engine_arch" in
  amd64)
    baseline_platform=linux/amd64
    baseline_image=$baseline_image_amd64
    ;;
  arm64)
    baseline_platform=linux/arm64
    baseline_image=$baseline_image_arm64
    ;;
  *)
    echo "unsupported Docker Engine architecture for v0.9.1 qualification: $engine_arch" >&2
    exit 1
    ;;
esac

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
  if [[ "$compose_initialized" == true && -f "$baseline_compose" && -f "$override_compose" ]]; then
    if ! run_compose down --volumes --remove-orphans >/dev/null 2>&1; then
      echo "failed to clean qualification Compose project $project" >&2
      exit_code=1
    fi
  fi
  if docker container inspect "$container_name" >/dev/null 2>&1 &&
      ! docker rm -f "$container_name" >/dev/null 2>&1; then
    echo "failed to clean qualification container $container_name" >&2
    exit_code=1
  fi
  if [[ "$candidate_built" == true ]]; then
    docker image rm "$candidate_tag" >/dev/null 2>&1 || {
      echo "failed to clean qualification candidate image $candidate_tag" >&2
      exit_code=1
    }
  fi
  if [[ -d "$work" && ! -L "$work" ]]; then
    find "$work" -xdev -depth -delete >/dev/null 2>&1 || true
  fi
  if [[ -e "$work" || -L "$work" ]]; then
    echo "failed to clean qualification work tree $work" >&2
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

run_compose() {
  DEPSILO_V091_EXACT_IMAGE="$active_image" \
  DEPSILO_V091_PLATFORM="$baseline_platform" \
  DEPSILO_V091_CONTAINER_NAME="$container_name" \
  DEPSILO_V091_BOOTSTRAP_TOKEN="$bootstrap_token" \
  DEPSILO_V091_JWT_SECRET="$jwt_secret" \
  PORT="$server_port" \
    docker compose --project-name "$project" \
      --file "$baseline_compose" --file "$override_compose" "$@"
}

container_logs() {
  [[ -n "$container_id" ]] || return 0
  docker logs "$container_id" >&2 || true
}

wait_ready() {
  for _ in $(seq 1 300); do
    if curl --fail --silent "$origin/ready" >/dev/null; then
      return 0
    fi
    if [[ -n "$container_id" ]] &&
        ! docker inspect --format '{{.State.Running}}' "$container_id" 2>/dev/null | grep -qx true; then
      container_logs
      echo "Depsilo exited before readiness at $origin" >&2
      return 1
    fi
    sleep 0.1
  done
  container_logs
  echo "Depsilo did not become ready at $origin" >&2
  return 1
}

index_manifest="$work/v0.9.1-image-index.json"
docker buildx imagetools inspect "$baseline_image_index" --raw >"$index_manifest"
read -r actual_index_sha256 _ < <(sha256sum -- "$index_manifest")
[[ "sha256:$actual_index_sha256" == "${baseline_image_index##*@}" ]] || {
  echo 'published v0.9.1 image index bytes do not match the qualified digest' >&2
  exit 1
}
jq -e --arg digest "${baseline_image##*@}" --arg architecture "$engine_arch" \
  'any(.manifests[]; .digest == $digest and .platform.os == "linux" and .platform.architecture == $architecture)' \
  "$index_manifest" >/dev/null || {
    echo "qualified $baseline_platform child is not in the published v0.9.1 image index" >&2
    exit 1
  }
docker pull --platform "$baseline_platform" "$baseline_image" >/dev/null
old_image_id=$(docker image inspect --format '{{.Id}}' "$baseline_image")
[[ "$old_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo 'v0.9.1 image did not resolve to an exact image ID' >&2
  exit 1
}
old_version=$(docker run --rm --network none "$old_image_id" version)
grep -Eq '(^|[^0-9])0\.9\.1([^0-9]|$)' <<<"$old_version" || {
  echo "baseline image is not v0.9.1: $old_version" >&2
  exit 1
}
[[ "$(docker image inspect --format '{{.Config.User}}' "$old_image_id")" == '10001:10001' ]] || {
  echo 'published v0.9.1 image does not declare User 10001:10001' >&2
  exit 1
}

git -C "$root" show "$baseline_commit:compose.yaml" >"$baseline_compose"
read -r actual_compose_sha256 _ < <(sha256sum -- "$baseline_compose")
[[ "$actual_compose_sha256" == "$baseline_compose_sha256" ]] || {
  echo 'v0.9.1 Compose file does not match the qualified shipped layout' >&2
  exit 1
}
cat >"$override_compose" <<'YAML'
services:
  depsilo:
    image: ${DEPSILO_V091_EXACT_IMAGE:?set exact qualification image ID}
    platform: ${DEPSILO_V091_PLATFORM:?set exact qualification platform}
    container_name: ${DEPSILO_V091_CONTAINER_NAME:?set isolated qualification container name}
    restart: "no"
    environment:
      DEPSILO_BOOTSTRAP_TOKEN: ${DEPSILO_V091_BOOTSTRAP_TOKEN:?set qualification bootstrap token}
      DEPSILO_AUTH_JWT_SECRET: ${DEPSILO_V091_JWT_SECRET:?set qualification JWT secret}
YAML
compose_initialized=true

if [[ -n "$candidate_input" ]]; then
  docker image inspect "$candidate_input" >/dev/null
  candidate_image_id=$(docker image inspect --format '{{.Id}}' "$candidate_input")
else
  docker build \
    --build-arg VERSION=v0.9.1-upgrade-candidate \
    --build-arg COMMIT=v0.9.1-upgrade-candidate \
    --build-arg BUILD_DATE=v0.9.1-upgrade-candidate \
    --tag "$candidate_tag" \
    "$root"
  candidate_built=true
  candidate_image_id=$(docker image inspect --format '{{.Id}}' "$candidate_tag")
fi
[[ "$candidate_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo 'candidate did not resolve to an exact image ID' >&2
  exit 1
}
[[ "$(docker image inspect --format '{{.Config.User}}' "$candidate_image_id")" == '10001:10001' ]] || {
  echo 'candidate image does not declare User 10001:10001' >&2
  exit 1
}

server_port=$(available_port)
origin="http://127.0.0.1:$server_port"
bootstrap_token='v091-image-upgrade-bootstrap-token-0123456789'
jwt_secret='v091-image-upgrade-jwt-secret-0123456789abcdef'
admin_username='v091-image-upgrade-admin'
admin_password='V091&Image-Upgrade-Password-47'
license_key='depsilo-v091-image-upgrade-license-key'

active_image=$old_image_id
run_compose up --detach --no-build depsilo >/dev/null
container_id=$(run_compose ps --quiet depsilo)
[[ -n "$container_id" ]] || { echo 'v0.9.1 Compose did not create a service container' >&2; exit 1; }
wait_ready

mount_contract=$(docker inspect --format \
  '{{range .Mounts}}{{if eq .Destination "/root/.depsilo"}}{{.Type}} {{.Name}}{{end}}{{end}}' \
  "$container_id")
read -r mount_type state_volume <<<"$mount_contract"
[[ "$mount_type" == volume && -n "$state_volume" ]] || {
  echo "v0.9.1 Compose did not mount a named state volume at /root/.depsilo: $mount_contract" >&2
  exit 1
}

setup_payload=$(jq -nc --arg username "$admin_username" --arg password "$admin_password" '{
  server: {port: 23333},
  storage: {path: "/root/.depsilo/data/cache"},
  admin: {username: $username, password: $password},
  ecosystems: {
    npm: {
      enabled: true,
      upstreams: [{name: "npm", url: "https://registry.npmjs.org", priority: 1}]
    }
  }
}')
curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "X-Depsilo-Bootstrap-Token: $bootstrap_token" \
  --data "$setup_payload" \
  "$origin/api/v1/setup/complete" | jq -e '.status == "ok"' >/dev/null

configured=false
for _ in $(seq 1 300); do
  if curl --fail --silent "$origin/ready" >/dev/null &&
      curl --fail --silent "$origin/api/v1/setup/status" | jq -e '.needs_setup == false' >/dev/null; then
    configured=true
    break
  fi
  if ! docker inspect --format '{{.State.Running}}' "$container_id" 2>/dev/null | grep -qx true; then
    container_logs
    echo 'v0.9.1 image exited while applying first-run configuration' >&2
    exit 1
  fi
  sleep 0.1
done
[[ "$configured" == true ]] || { container_logs; echo 'v0.9.1 image did not apply first-run configuration' >&2; exit 1; }

login_payload=$(jq -nc --arg username "$admin_username" --arg password "$admin_password" \
  '{username: $username, password: $password}')
old_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
api_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data '{"name":"v091-image-upgrade-token","permissions":"readonly","ttl":"7d"}' \
  "$origin/api/v1/admin/tokens" | jq -er '.token')

trial_status=$(curl --fail --silent --show-error --request POST \
  --header "Authorization: Bearer $old_admin_token" \
  "$origin/api/v1/admin/license/trial/activate")
jq -e '.is_pro == true and .source == "trial" and .trial_used == true' <<<"$trial_status" >/dev/null
license_payload=$(jq -nc --arg key "$license_key" '{key: $key}')
paid_status=$(curl --fail --silent --show-error --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$license_payload" "$origin/api/v1/admin/license/key")
jq -e '.is_pro == true and .source == "paid" and .license_key_masked == "depsilo-***"' \
  <<<"$paid_status" >/dev/null

rule_payload='{"ecosystem":"pypi","package_name":"*","version":"*","action":"deny","reason":"v0.9.1 image upgrade sentinel"}'
curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$rule_payload" "$origin/api/v1/admin/rules" |
  jq -e '.ecosystem == "pypi" and .package_name == "*" and .version == "*" and .action == "deny"' >/dev/null

docker exec "$container_id" sh -euc \
  'printf "%s\n" v0.9.1-state-retained > /root/.depsilo/.v091-upgrade-marker'
old_config_sha256=$(docker exec "$container_id" sha256sum /root/.depsilo/config.toml | awk '{print $1}')

docker stop --time 20 "$container_id" >/dev/null
active_image=$candidate_image_id
run_compose up --detach --no-build --force-recreate depsilo >/dev/null
container_id=$(run_compose ps --quiet depsilo)
[[ -n "$container_id" ]] || { echo 'candidate Compose did not create a service container' >&2; exit 1; }
wait_ready

candidate_state_volume=$(docker inspect --format \
  '{{range .Mounts}}{{if eq .Destination "/root/.depsilo"}}{{.Name}}{{end}}{{end}}' \
  "$container_id")
[[ "$candidate_state_volume" == "$state_volume" ]] || {
  echo "candidate mounted $candidate_state_volume instead of v0.9.1 state volume $state_volume" >&2
  exit 1
}
docker exec "$container_id" grep -qx 'v0.9.1-state-retained' /root/.depsilo/.v091-upgrade-marker
candidate_config_sha256=$(docker exec "$container_id" sha256sum /root/.depsilo/config.toml | awk '{print $1}')
[[ "$candidate_config_sha256" == "$old_config_sha256" ]] || {
  echo 'candidate changed the v0.9.1 generated configuration during migration' >&2
  exit 1
}

curl --fail --silent --show-error \
  --header "Authorization: Bearer $old_admin_token" "$origin/api/v1/auth/me" |
  jq -e --arg username "$admin_username" '.username == $username and .auth_method == "jwt"' >/dev/null
current_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
curl --fail --silent --show-error \
  --header "Authorization: Bearer $api_token" "$origin/api/v1/auth/me" |
  jq -e --arg username "$admin_username" \
    '.username == $username and .auth_method == "api_token" and .token_permissions == "readonly"' >/dev/null
curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/license/status" |
  jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .license_key_masked == "depsilo-***"' >/dev/null
curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/rules" |
  jq -e '.total == 1 and .items[0].ecosystem == "pypi" and .items[0].package_name == "*" and .items[0].version == "*" and .items[0].action == "deny" and .items[0].reason == "v0.9.1 image upgrade sentinel"' >/dev/null

docker stop --time 20 "$container_id" >/dev/null
mkdir -p "$database_snapshot"
docker cp "$container_id:/root/.depsilo/data/." "$database_snapshot/" >/dev/null
python3 - "$database_snapshot/depsilo.db" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
checks = {
    "schema version": database.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0] == 3,
    "administrator": database.execute(
        "SELECT COUNT(*) FROM users WHERE username = 'v091-image-upgrade-admin' AND role = 'admin' AND enabled = 1"
    ).fetchone()[0] == 1,
    "API token": database.execute(
        "SELECT COUNT(*) FROM api_tokens WHERE name = 'v091-image-upgrade-token' AND permissions = 'readonly'"
    ).fetchone()[0] == 1,
    "trial singleton": database.execute("SELECT COUNT(*) FROM trial_records").fetchone()[0] == 1,
    "license singleton": database.execute("SELECT COUNT(*) FROM license_storages").fetchone()[0] == 1,
}
rule = database.execute(
    "SELECT ecosystem, package_name, version, action, reason, "
    "normalized_package_name, normalized_version, dialect_revision "
    "FROM package_rules"
).fetchall()
checks["safe legacy package rule"] = rule == [(
    "pypi", "*", "*", "deny", "v0.9.1 image upgrade sentinel", "*", "*", 1
)]
failed = [name for name, passed in checks.items() if not passed]
if failed:
    raise SystemExit("v0.9.1 image/state upgrade contract is incomplete: " + ", ".join(failed))
PY

echo 'v0.9.1 immutable image/state -> current upgrade contract passed (published digest, shipped named-volume layout, config, schema v3, password/JWT/API tokens, entitlement, safe package-rule migration)'
