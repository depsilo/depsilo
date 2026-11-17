#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# The contract asserts the candidate's schema, so read the version from the
# candidate source instead of duplicating it here and letting it drift.
expected_schema_version=$(
  sed -n 's/^const CurrentSchemaVersion = \([0-9][0-9]*\)$/\1/p' "$root/internal/db/repository.go"
)
[[ -n "$expected_schema_version" ]] || {
  echo 'unable to read CurrentSchemaVersion from internal/db/repository.go' >&2
  exit 1
}

# The previous published release: what an operator runs the day before they
# upgrade to the candidate. Re-pin these six values after every release
# (docs/release-verification.md, "Re-pinning the previous-release contract");
# the freshness check below fails once a newer release exists.
baseline_tag=v0.14.0
baseline_commit=ae0eebd3bf3778cf720f9a7a8d57027888d604b8
baseline_compose_sha256=0567568083ce00f53bfaf78738c26f30d94e6ffb32268f4ab54a08b7e909d9db
baseline_image_index=ghcr.io/depsilo/depsilo@sha256:3d7880d1ba4badfbbb9b3b1166bc00736bcd1237065cc0208d3c89ddfd1ff301
baseline_image_amd64=ghcr.io/depsilo/depsilo@sha256:ba5c41591a263423499528e07aad30afc200082092f1427fcd6b18e23e1e86c0
baseline_image_arm64=ghcr.io/depsilo/depsilo@sha256:5a53f44c90bb5f6a1c465f47a6284a49f00b537ebaac179634fd778bf0cdaf20
baseline_version=${baseline_tag#v}
candidate_input=${DEPSILO_UPGRADE_IMAGE:-}
project="depsilo-previous-release-upgrade-$$"
container_name="${project}-server"
candidate_tag="${project}:candidate"
# The work directory is created where the cleanup trap is installed, after the
# checks below that can fail routinely (the freshness rule fails after every
# release), so a failing check leaves nothing behind.
work=''
baseline_compose=''
override_compose=''
active_image=''
container_id=''
candidate_image_id=''
candidate_built=false
compose_initialized=false
server_port=''
bootstrap_token=''

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
    echo "unsupported Docker Engine architecture for $baseline_tag qualification: $engine_arch" >&2
    exit 1
    ;;
esac

# A shallow clone hides the release tags beyond its boundary, which would let
# a stale pin pass the freshness check below unnoticed, so it is refused. This
# runs before the baseline tag lookup because a shallow clone usually lacks
# that tag too, and its history, not the tag, is what the operator must fix.
if [[ "$(git -C "$root" rev-parse --is-shallow-repository)" != false ]]; then
  echo 'the previous-release upgrade contract needs full history to check that its baseline is the newest release, but this checkout is shallow: run git fetch --unshallow --tags and try again' >&2
  exit 1
fi
resolved_baseline_commit=$(git -C "$root" rev-parse --verify --quiet "refs/tags/$baseline_tag^{commit}") || {
  echo "upgrade baseline tag is unavailable: $baseline_tag" >&2
  exit 1
}
[[ "$resolved_baseline_commit" == "$baseline_commit" ]] || {
  echo "upgrade baseline tag does not resolve to the qualified commit: $baseline_tag" >&2
  exit 1
}

# Freshness: this contract is about the release directly before the candidate.
# The newest final release tag in the candidate's history, other than a tag on
# the candidate commit itself (the release being qualified), must not be newer
# than the pinned baseline. A hotfix tag outside this history may be pinned.
head_commit=$(git -C "$root" rev-parse --verify --quiet 'HEAD^{commit}') || {
  echo 'unable to resolve the candidate commit' >&2
  exit 1
}
merged_tags=$(git -C "$root" tag --list 'v[0-9]*' --merged "$head_commit" --sort=-v:refname)
newest_ancestor_release=''
while IFS= read -r tag; do
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
  tag_commit=$(git -C "$root" rev-parse --verify --quiet "refs/tags/$tag^{commit}") || continue
  [[ "$tag_commit" != "$head_commit" ]] || continue
  newest_ancestor_release=$tag
  break
done <<<"$merged_tags"
if [[ -n "$newest_ancestor_release" ]]; then
  python3 - "$baseline_tag" "$newest_ancestor_release" <<'PY' || exit 1
import sys

def version(tag):
    return tuple(int(part) for part in tag[1:].split("."))

pinned, newest = sys.argv[1], sys.argv[2]
if version(pinned) < version(newest):
    raise SystemExit(
        f"the previous-release upgrade contract is pinned to {pinned}, but {newest} has been "
        "released since: re-pin scripts/test-previous-release-upgrade.sh "
        "(docs/release-verification.md, \"Re-pinning the previous-release contract\")"
    )
PY
fi

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
work=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-previous-release-upgrade.XXXXXX")
baseline_compose="$work/compose.yaml"
override_compose="$work/image.override.yaml"
database_snapshot="$work/database"

available_port() {
  python3 - <<'PY'
import socket
with socket.socket() as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
PY
}

run_compose() {
  DEPSILO_PREVIOUS_EXACT_IMAGE="$active_image" \
  DEPSILO_PREVIOUS_PLATFORM="$baseline_platform" \
  DEPSILO_PREVIOUS_CONTAINER_NAME="$container_name" \
  DEPSILO_PREVIOUS_BOOTSTRAP_TOKEN="$bootstrap_token" \
  DEPSILO_BIND_ADDRESS=127.0.0.1 \
  PORT="$server_port" \
    docker compose --project-name "$project" \
      --file "$baseline_compose" --file "$override_compose" "$@"
}

container_logs() {
  [[ -n "$container_id" ]] || return 0
  docker logs "$container_id" >&2 || true
}

# The configuration, the database and the instance signing key stay owned by
# the image user and private. The configuration, the key and the directories
# holding all three carry no group or other bits; the database file itself is
# created 0644 and is private through its 0700 directories, so it only must
# not become writable by others.
assert_private_state_files() {
  local phase=$1 listing owner mode path forbidden
  local -a forbidden_bits=(077 022 077 077 077 077)
  listing=$(docker exec "$container_id" stat -c '%u:%g %a %n' \
    /root/.depsilo/config.toml /root/.depsilo/data/depsilo.db /root/.depsilo/data/secrets/jwt.key \
    /root/.depsilo /root/.depsilo/data /root/.depsilo/data/secrets) || {
      echo "$phase lost a state file: config.toml, data/depsilo.db and data/secrets/jwt.key must exist" >&2
      exit 1
    }
  while read -r owner mode path; do
    forbidden=${forbidden_bits[0]}
    forbidden_bits=("${forbidden_bits[@]:1}")
    if [[ "$owner" != 10001:10001 || ! "$mode" =~ ^[0-7]+$ ]] || (( 8#$mode & 8#$forbidden )); then
      echo "$phase left $path as owner $owner mode $mode: state must stay owned by 10001:10001 and private" >&2
      exit 1
    fi
  done <<<"$listing"
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

index_manifest="$work/previous-release-image-index.json"
docker buildx imagetools inspect "$baseline_image_index" --raw >"$index_manifest"
read -r actual_index_sha256 _ < <(sha256sum -- "$index_manifest")
[[ "sha256:$actual_index_sha256" == "${baseline_image_index##*@}" ]] || {
  echo "published $baseline_tag image index bytes do not match the qualified digest" >&2
  exit 1
}
jq -e --arg digest "${baseline_image##*@}" --arg architecture "$engine_arch" \
  'any(.manifests[]; .digest == $digest and .platform.os == "linux" and .platform.architecture == $architecture)' \
  "$index_manifest" >/dev/null || {
    echo "qualified $baseline_platform child is not in the published $baseline_tag image index" >&2
    exit 1
  }
docker pull --platform "$baseline_platform" "$baseline_image" >/dev/null
old_image_id=$(docker image inspect --format '{{.Id}}' "$baseline_image")
[[ "$old_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "$baseline_tag image did not resolve to an exact image ID" >&2
  exit 1
}
old_version=$(docker run --rm --network none "$old_image_id" version)
# Bounded on both sides: for 0.14.0, neither 0.14.0-rc.1, 10.14.0 nor 0.14.01
# passes.
grep -Eq -- "(^|[[:space:]])v?${baseline_version//./\\.}([[:space:]]|$)" <<<"$old_version" || {
  echo "baseline image is not $baseline_tag: $old_version" >&2
  exit 1
}
[[ "$(docker image inspect --format '{{.Config.User}}' "$old_image_id")" == '10001:10001' ]] || {
  echo "published $baseline_tag image does not declare User 10001:10001" >&2
  exit 1
}

git -C "$root" show "$baseline_commit:compose.yaml" >"$baseline_compose"
read -r actual_compose_sha256 _ < <(sha256sum -- "$baseline_compose")
[[ "$actual_compose_sha256" == "$baseline_compose_sha256" ]] || {
  echo "$baseline_tag Compose file does not match the qualified shipped layout" >&2
  exit 1
}
# The release's own Compose file stays as shipped. The override pins the
# image under test, isolates the container name, and presets the bootstrap
# token; the signing key is left to the instance, as the release documents.
cat >"$override_compose" <<'YAML'
services:
  depsilo:
    image: ${DEPSILO_PREVIOUS_EXACT_IMAGE:?set exact qualification image ID}
    platform: ${DEPSILO_PREVIOUS_PLATFORM:?set exact qualification platform}
    container_name: ${DEPSILO_PREVIOUS_CONTAINER_NAME:?set isolated qualification container name}
    restart: "no"
    environment:
      DEPSILO_BOOTSTRAP_TOKEN: ${DEPSILO_PREVIOUS_BOOTSTRAP_TOKEN:?set qualification bootstrap token}
YAML

if [[ -n "$candidate_input" ]]; then
  docker image inspect "$candidate_input" >/dev/null
  candidate_image_id=$(docker image inspect --format '{{.Id}}' "$candidate_input")
else
  docker build \
    --build-arg VERSION=previous-release-upgrade-candidate \
    --build-arg COMMIT=previous-release-upgrade-candidate \
    --build-arg BUILD_DATE=previous-release-upgrade-candidate \
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
bootstrap_token='previous-release-upgrade-bootstrap-0123456789'
admin_username='upgrade-admin'
admin_password='Prev&Release-Contract-Password-47'
reader_username='upgrade-reader'
reader_password='Reader&Contract-Sentinel-Pass-58'

active_image=$old_image_id
compose_initialized=true
run_compose up --detach --no-build depsilo >/dev/null
container_id=$(run_compose ps --quiet depsilo)
[[ -n "$container_id" ]] || { echo "$baseline_tag Compose did not create a service container" >&2; exit 1; }
wait_ready

mount_contract=$(docker inspect --format \
  '{{range .Mounts}}{{if eq .Destination "/root/.depsilo"}}{{.Type}} {{.Name}}{{end}}{{end}}' \
  "$container_id")
read -r mount_type state_volume <<<"$mount_contract"
[[ "$mount_type" == volume && -n "$state_volume" ]] || {
  echo "$baseline_tag Compose did not mount a named state volume at /root/.depsilo: $mount_contract" >&2
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
      curl --fail --silent "$origin/api/v1/setup/status" |
        jq -e '.needs_setup == false and .restart_pending != true' >/dev/null; then
    configured=true
    break
  fi
  if ! docker inspect --format '{{.State.Running}}' "$container_id" 2>/dev/null | grep -qx true; then
    container_logs
    echo "$baseline_tag image exited while applying first-run configuration" >&2
    exit 1
  fi
  sleep 0.1
done
[[ "$configured" == true ]] || { container_logs; echo "$baseline_tag image did not apply first-run configuration" >&2; exit 1; }
# The setup's upstreams take effect on the next start; restart once so there
# is an upstream to edit, as an operator's instance has.
docker restart --time 20 "$container_id" >/dev/null
wait_ready

login_payload=$(jq -nc --arg username "$admin_username" --arg password "$admin_password" \
  '{username: $username, password: $password}')
old_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
api_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data '{"name":"previous-release-upgrade-token","permissions":"readonly","ttl":"7d"}' \
  "$origin/api/v1/admin/tokens" | jq -er '.token')

reader_payload=$(jq -nc --arg username "$reader_username" --arg password "$reader_password" \
  '{username: $username, password: $password, role: "readonly"}')
curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$reader_payload" "$origin/api/v1/admin/users" |
  jq -e --arg username "$reader_username" '.username == $username and .role == "readonly" and .enabled == true' >/dev/null

rule_payload='{"ecosystem":"pypi","package_name":"*","version":"*","action":"deny","reason":"previous-release upgrade sentinel"}'
curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$rule_payload" "$origin/api/v1/admin/rules" |
  jq -e '.ecosystem == "pypi" and .package_name == "*" and .version == "*" and .action == "deny"' >/dev/null

# Trial entitlement: activation also writes a management row to the audit
# chain, so the chain below is never empty.
trial_status=$(curl --fail --silent --show-error --request POST \
  --header "Authorization: Bearer $old_admin_token" \
  "$origin/api/v1/admin/license/trial/activate")
jq -e '.is_pro == true and .source == "trial" and .trial_used == true and (.expires_at | type) == "string"' \
  <<<"$trial_status" >/dev/null || {
    echo "$baseline_tag did not activate the trial: $trial_status" >&2
    exit 1
  }
old_license_status=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $old_admin_token" "$origin/api/v1/admin/license/status" |
  jq -c '{is_pro, source, trial_used, expires_at}')

# One upstream edited through the Admin API. Passive probing keeps the
# candidate from contacting the edited (unresolvable) address.
npm_upstream_id=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $old_admin_token" "$origin/api/v1/admin/upstreams" |
  jq -er '[.items[] | select(.adapter_type == "npm")] | if length == 1 then .[0].id else error("expected one npm upstream") end')
upstream_payload='{"adapter_type":"npm","name":"npm-upgrade-edited","url":"https://npm.previous-release-upgrade.invalid","proxy":"","priority":3,"probe_mode":"passive","probe_interval":"45s","via":""}'
upstream_fields='{id, adapter_type, name, url, proxy, priority, probe_mode, probe_interval, via}'
old_upstream=$(curl --fail --silent --show-error --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$upstream_payload" "$origin/api/v1/admin/upstreams/$npm_upstream_id" |
  jq -c "$upstream_fields")
jq -e --argjson id "$npm_upstream_id" --argjson edit "$upstream_payload" \
  '. == ({id: $id} + $edit)' <<<"$old_upstream" >/dev/null || {
    echo "$baseline_tag did not apply the upstream edit: $old_upstream" >&2
    exit 1
  }

# The audit chain verifies before the upgrade; its row count must not shrink.
old_audit=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $old_admin_token" "$origin/api/v1/admin/audit/integrity")
jq -e '.integrity.ok == true and .integrity.rows_scanned >= 1 and .integrity.chained_rows >= 1 and .anchors.ok == true' <<<"$old_audit" >/dev/null || {
  echo "$baseline_tag audit chain does not verify or is empty before the upgrade: $old_audit" >&2
  exit 1
}
old_audit_rows=$(jq -r '.integrity.rows_scanned' <<<"$old_audit")
old_audit_chained_rows=$(jq -r '.integrity.chained_rows' <<<"$old_audit")
assert_private_state_files "$baseline_tag"

docker exec "$container_id" sh -euc \
  'printf "%s\n" previous-release-state-retained > /root/.depsilo/.previous-release-upgrade-marker'
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
  echo "candidate mounted $candidate_state_volume instead of $baseline_tag state volume $state_volume" >&2
  exit 1
}
docker exec "$container_id" grep -qx 'previous-release-state-retained' /root/.depsilo/.previous-release-upgrade-marker
candidate_config_sha256=$(docker exec "$container_id" sha256sum /root/.depsilo/config.toml | awk '{print $1}')
[[ "$candidate_config_sha256" == "$old_config_sha256" ]] || {
  echo "candidate changed the $baseline_tag configuration during the upgrade" >&2
  exit 1
}

# An upgrade must never reopen first-run setup: the preset bootstrap token is
# still in the environment, and a reopened setup would accept it.
curl --fail --silent --show-error "$origin/api/v1/setup/status" |
  jq -e '.needs_setup == false' >/dev/null || {
    container_logs
    echo "candidate reopened first-run setup on $baseline_tag state" >&2
    exit 1
  }

# A session issued before the upgrade stays valid: the instance-managed
# signing key survives in the state volume.
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
reader_login_payload=$(jq -nc --arg username "$reader_username" --arg password "$reader_password" \
  '{username: $username, password: $password}')
reader_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' --data "$reader_login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
curl --fail --silent --show-error \
  --header "Authorization: Bearer $reader_token" "$origin/api/v1/auth/me" |
  jq -e --arg username "$reader_username" '.username == $username and .role == "readonly"' >/dev/null
curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/rules" |
  jq -e '.total == 1 and .items[0].ecosystem == "pypi" and .items[0].package_name == "*" and .items[0].version == "*" and .items[0].action == "deny" and .items[0].reason == "previous-release upgrade sentinel"' >/dev/null

current_license_status=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/license/status" |
  jq -c '{is_pro, source, trial_used, expires_at}') || {
    container_logs
    echo "candidate could not report the $baseline_tag trial entitlement" >&2
    exit 1
  }
[[ "$current_license_status" == "$old_license_status" ]] || {
  echo "candidate lost the $baseline_tag trial entitlement: expected $old_license_status, got $current_license_status" >&2
  exit 1
}
current_upstreams=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/upstreams") || {
    container_logs
    echo "candidate could not report the upstream edited through the Admin API on $baseline_tag" >&2
    exit 1
  }
current_upstream=$(jq -c --argjson id "$npm_upstream_id" \
  "[.items[] | select(.id == \$id) | $upstream_fields] | .[0]" <<<"$current_upstreams")
[[ "$current_upstream" == "$old_upstream" ]] || {
  echo "candidate lost the upstream edited through the Admin API on $baseline_tag: expected $old_upstream, got $current_upstream" >&2
  exit 1
}
current_audit=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" "$origin/api/v1/admin/audit/integrity") || {
    container_logs
    echo "candidate could not report the $baseline_tag audit chain" >&2
    exit 1
  }
jq -e '.integrity.ok == true and .anchors.ok == true' <<<"$current_audit" >/dev/null || {
  echo "candidate broke the $baseline_tag audit chain: $current_audit" >&2
  exit 1
}
jq -e --argjson rows "$old_audit_rows" --argjson chained "$old_audit_chained_rows" \
  '.integrity.rows_scanned >= $rows and .integrity.chained_rows >= $chained' <<<"$current_audit" >/dev/null || {
    echo "candidate lost $baseline_tag audit rows: $old_audit_rows rows ($old_audit_chained_rows chained) before the upgrade, now $(jq -c '.integrity' <<<"$current_audit")" >&2
    exit 1
  }
assert_private_state_files candidate

# The candidate's own log, from its start on the configured state: no
# error-level line, and no bootstrap token (setup must stay closed).
candidate_log=$(docker logs "$container_id" 2>&1)
if error_lines=$(grep -E '"level":"(error|dpanic|panic|fatal)"' <<<"$candidate_log"); then
  echo "candidate logged at level error on $baseline_tag state:" >&2
  printf '%s\n' "$error_lines" >&2
  exit 1
fi
if grep -Fq -- "$bootstrap_token" <<<"$candidate_log" || grep -Fq 'Bootstrap token:' <<<"$candidate_log"; then
  echo "candidate printed a bootstrap token on configured $baseline_tag state" >&2
  exit 1
fi

docker stop --time 20 "$container_id" >/dev/null
mkdir -p "$database_snapshot"
docker cp "$container_id:/root/.depsilo/data/." "$database_snapshot/" >/dev/null
python3 - "$database_snapshot/depsilo.db" "$expected_schema_version" "$old_audit_rows" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
expected_schema_version = int(sys.argv[2])
old_audit_rows = int(sys.argv[3])
checks = {
    "schema version": database.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0] == expected_schema_version,
    "administrator": database.execute(
        "SELECT COUNT(*) FROM users WHERE username = 'upgrade-admin' AND role = 'admin' AND enabled = 1"
    ).fetchone()[0] == 1,
    "second user": database.execute(
        "SELECT COUNT(*) FROM users WHERE username = 'upgrade-reader' AND role = 'readonly' AND enabled = 1"
    ).fetchone()[0] == 1,
    "API token": database.execute(
        "SELECT COUNT(*) FROM api_tokens WHERE name = 'previous-release-upgrade-token' AND permissions = 'readonly'"
    ).fetchone()[0] == 1,
    "package rule": database.execute(
        "SELECT ecosystem, package_name, version, action, reason FROM package_rules"
    ).fetchall() == [("pypi", "*", "*", "deny", "previous-release upgrade sentinel")],
    "trial singleton": database.execute("SELECT COUNT(*) FROM trial_records").fetchone()[0] == 1,
    "edited upstream": database.execute(
        "SELECT name, url, priority, probe_mode, probe_interval FROM upstream_records WHERE adapter_type = 'npm'"
    ).fetchall() == [("npm-upgrade-edited", "https://npm.previous-release-upgrade.invalid", 3, "passive", "45s")],
    "audit rows": database.execute("SELECT COUNT(*) FROM audit_logs").fetchone()[0] >= old_audit_rows,
}
failed = [name for name, passed in checks.items() if not passed]
if failed:
    raise SystemExit("previous-release image/state upgrade contract is incomplete: " + ", ".join(failed))
PY

echo "$baseline_tag published image/state -> current upgrade contract passed (published digest, shipped named-volume layout, config, schema v$expected_schema_version, setup not reopened, session/password/API token, users, package rule, trial, edited upstream, audit chain, private state files, clean candidate log)"
