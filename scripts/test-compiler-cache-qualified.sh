#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
binary=${DEPSILO_COMPILER_CACHE_BINARY:-$root/bin/depsilo}
[[ -x "$binary" ]] || { echo "compiler-cache qualification binary is missing: $binary" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo 'jq is required' >&2; exit 1; }

port=${COMPILER_CACHE_TEST_PORT:-$((32000 + ($$ % 20000)))}
for _ in $(seq 1 100); do
  if ! (exec 9<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    break
  fi
  exec 9>&-
  exec 9<&-
  port=$((port + 1))
done

state_dir=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-compiler-cache.XXXXXX")
config="$state_dir/config.toml"
log="$state_dir/server.log"
origin="http://127.0.0.1:$port"
server_pid=''

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" >/dev/null 2>&1; then
    kill -TERM "$server_pid" >/dev/null 2>&1 || true
    for _ in $(seq 1 120); do
      kill -0 "$server_pid" >/dev/null 2>&1 || break
      sleep 0.1
    done
    if kill -0 "$server_pid" >/dev/null 2>&1; then
      kill -KILL "$server_pid" >/dev/null 2>&1 || true
    fi
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  find "$state_dir" -depth -delete >/dev/null 2>&1 || true
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

cat >"$config" <<EOF
config_version = 1

[server]
host = "127.0.0.1"
port = $port
log_level = "warn"

[database]
driver = "sqlite"
dsn = "$state_dir/depsilo.db"

[storage]
type = "local"
path = "$state_dir/cache"

[auth]
enabled = true
token_ttl = "1h"

[compile_cache]
enabled = true
public_url = "$origin"

[compile_cache.storage]
type = "local"
path = "$state_dir/compiler-cache"

[security]
enabled = false

[supply_chain.blocklist]
enabled = false
EOF

DEPSILO_AUTH_JWT_SECRET='compiler-cache-qualification-jwt-secret-0123456789abcdef' \
DEPSILO_ADMIN_USERNAME='release-operator' \
DEPSILO_ADMIN_PASSWORD='Release&Qualification-Password-47' \
  "$binary" serve --config "$config" >"$log" 2>&1 &
server_pid=$!

ready=false
for _ in $(seq 1 200); do
  if curl --fail --silent "$origin/ready" >/dev/null; then
    ready=true
    break
  fi
  if ! kill -0 "$server_pid" >/dev/null 2>&1; then
    cat "$log" >&2
    echo 'Depsilo exited before compiler-cache qualification' >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then
  cat "$log" >&2
  echo 'Depsilo did not become ready for compiler-cache qualification' >&2
  exit 1
fi

login_payload=$(jq -nc --arg username 'release-operator' --arg password 'Release&Qualification-Password-47' \
  '{username: $username, password: $password}')
admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')

credential_payload=$(jq -nc \
  '{name: "release qualification", namespace: "release-ci", permissions: "readwrite", ttl_days: 1}')
compiler_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $admin_token" \
  --data "$credential_payload" \
  "$origin/api/v1/admin/compile-cache/credentials" | jq -er '.token')

COMPILER_CACHE_ENDPOINT="$origin/ccache/v1/release-ci" \
COMPILER_CACHE_TOKEN="$compiler_token" \
  bash "$root/scripts/test-compiler-cache.sh"
