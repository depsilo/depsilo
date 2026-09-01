#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
current_binary=${DEPSILO_UPGRADE_BINARY:-$root/bin/depsilo}
baseline_tag=${DEPSILO_UPGRADE_BASELINE_TAG:-v0.9.0}

for command in git go tar curl jq python3; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }
done
[[ -x "$current_binary" ]] || { echo "upgrade qualification binary is missing: $current_binary" >&2; exit 1; }
git -C "$root" rev-parse --verify --quiet "refs/tags/$baseline_tag^{commit}" >/dev/null || {
  echo "upgrade baseline tag is unavailable: $baseline_tag" >&2
  exit 1
}

state_dir=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-v090-upgrade.XXXXXX")
old_source="$state_dir/source"
old_binary="$state_dir/depsilo-v0.9.0"
config="$state_dir/config.toml"
database="$state_dir/depsilo.db"
cache_dir="$state_dir/cache"
mock_script="$state_dir/mock_registry.py"
old_log="$state_dir/v0.9.0.log"
current_log="$state_dir/current.log"
entitlement_snapshot="$state_dir/entitlement-snapshot.json"
server_pid=''
mock_pid=''

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  for pid in "$server_pid" "$mock_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
      kill -TERM "$pid" >/dev/null 2>&1 || true
      for _ in $(seq 1 120); do
        kill -0 "$pid" >/dev/null 2>&1 || break
        sleep 0.1
      done
      if kill -0 "$pid" >/dev/null 2>&1; then
        kill -KILL "$pid" >/dev/null 2>&1 || true
      fi
      wait "$pid" >/dev/null 2>&1 || true
    fi
  done
  find "$state_dir" -depth -delete >/dev/null 2>&1 || true
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

wait_ready() {
  local origin=$1
  local log_file=$2
  for _ in $(seq 1 300); do
    if curl --fail --silent "$origin/ready" >/dev/null; then
      return 0
    fi
    if [[ -n "$server_pid" ]] && ! kill -0 "$server_pid" >/dev/null 2>&1; then
      cat "$log_file" >&2
      echo "Depsilo exited before readiness at $origin" >&2
      return 1
    fi
    sleep 0.1
  done
  cat "$log_file" >&2
  echo "Depsilo did not become ready at $origin" >&2
  return 1
}

stop_server() {
  local log_file=$1
  [[ -n "$server_pid" ]] || return 0
  kill -TERM "$server_pid"
  for _ in $(seq 1 150); do
    if ! kill -0 "$server_pid" >/dev/null 2>&1; then
      wait "$server_pid"
      server_pid=''
      return 0
    fi
    sleep 0.1
  done
  cat "$log_file" >&2
  echo "Depsilo did not stop gracefully" >&2
  return 1
}

mkdir -p "$old_source/web/dist"
git -C "$root" archive --format=tar "$baseline_tag" | tar -xf - -C "$old_source"
printf '%s\n' '<!doctype html><title>Depsilo upgrade fixture</title>' >"$old_source/web/dist/index.html"
(cd "$old_source" && go build -trimpath -buildvcs=false -o "$old_binary" ./cmd/depsilo)

mock_port=$(available_port)
server_port=$(available_port)
mock_origin="http://127.0.0.1:$mock_port"
origin="http://127.0.0.1:$server_port"

cat >"$mock_script" <<'PY'
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

port = int(os.environ["DEPSILO_UPGRADE_MOCK_PORT"])
origin = f"http://127.0.0.1:{port}"
artifact = b"depsilo-v0.9.0-cache-artifact\n"

class Handler(BaseHTTPRequestHandler):
    def do_HEAD(self):
        self.send_response(200)
        self.end_headers()

    def do_GET(self):
        if self.path == "/upgrade-fixture":
            body = json.dumps({
                "name": "upgrade-fixture",
                "dist-tags": {"latest": "1.0.0"},
                "versions": {
                    "1.0.0": {
                        "name": "upgrade-fixture",
                        "version": "1.0.0",
                        "dist": {
                            "tarball": origin + "/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz"
                        },
                    }
                },
            }).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if self.path == "/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz":
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(len(artifact)))
            self.end_headers()
            self.wfile.write(artifact)
            return
        self.send_response(404)
        self.end_headers()

    def log_message(self, _format, *_args):
        pass

ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

DEPSILO_UPGRADE_MOCK_PORT="$mock_port" python3 "$mock_script" &
mock_pid=$!
mock_ready=false
for _ in $(seq 1 100); do

  if curl --head --fail --silent "$mock_origin/" >/dev/null; then
    mock_ready=true
    break
  fi
  kill -0 "$mock_pid" >/dev/null 2>&1 || { echo 'mock registry exited early' >&2; exit 1; }
  sleep 0.05
done
[[ "$mock_ready" == true ]] || { echo 'mock registry did not become ready' >&2; exit 1; }

cat >"$config" <<EOF
config_version = 1

[server]
host = "127.0.0.1"
port = $server_port
log_level = "warn"

[database]
driver = "sqlite"
dsn = "$database"

[storage]
type = "local"
path = "$cache_dir"

[cache]
max_size_gb = 1
ttl_index = "168h"
ttl_blob = "168h"
lru_threshold = 90

[auth]
enabled = true
jwt_secret = "upgrade-qualification-jwt-secret-0123456789abcdef"
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
url = "$mock_origin"
priority = 1
probe_mode = "passive"
probe_interval = "30m"
EOF

admin_username='upgrade-admin'
admin_password='Upgrade&Qualification-Password-47'
license_key='depsilo-upgrade-contract-qualification-key'
npm_accept='application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*'
export DEPSILO_ADMIN_USERNAME="$admin_username"
export DEPSILO_ADMIN_PASSWORD="$admin_password"
export DEPSILO_BOOTSTRAP_TOKEN='upgrade-bootstrap-token-0123456789'

"$old_binary" serve --config "$config" >"$old_log" 2>&1 &
server_pid=$!
wait_ready "$origin" "$old_log"

login_payload=$(jq -nc --arg username "$admin_username" --arg password "$admin_password" \
  '{username: $username, password: $password}')
old_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
api_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data '{"name":"v0.9-upgrade-token","permissions":"readonly","ttl":"7d"}' \
  "$origin/api/v1/admin/tokens" | jq -er '.token')

trial_status=$(curl --fail --silent --show-error \
  --request POST \
  --header "Authorization: Bearer $old_admin_token" \
  "$origin/api/v1/admin/license/trial/activate")
jq -e '.is_pro == true and .source == "trial" and .trial_used == true and .trial_available == false' \
  <<<"$trial_status" >/dev/null
license_payload=$(jq -nc --arg key "$license_key" '{key: $key}')
paid_status=$(curl --fail --silent --show-error \
  --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $old_admin_token" \
  --data "$license_payload" \
  "$origin/api/v1/admin/license/key")
jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .license_key_masked == "depsilo-***"' \
  <<<"$paid_status" >/dev/null

curl --fail --silent --show-error --header "Accept: $npm_accept" "$origin/npm/upgrade-fixture" |
  jq -e '.versions["1.0.0"].name == "upgrade-fixture"' >/dev/null
old_artifact=$(curl --fail --silent --show-error "$origin/npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz")
[[ "$old_artifact" == 'depsilo-v0.9.0-cache-artifact' ]] || { echo 'v0.9.0 artifact cache seed failed' >&2; exit 1; }

stop_server "$old_log"
kill -TERM "$mock_pid"
wait "$mock_pid" >/dev/null 2>&1 || true
mock_pid=''
unset DEPSILO_ADMIN_USERNAME DEPSILO_ADMIN_PASSWORD DEPSILO_BOOTSTRAP_TOKEN

python3 - "$database" "$entitlement_snapshot" <<'PY'
import json
import os
import sqlite3
import sys

database = sqlite3.connect(sys.argv[1])
checks = {
    "administrator": database.execute(
        "SELECT COUNT(*) FROM users WHERE username = 'upgrade-admin' AND role = 'admin' AND enabled = 1"
    ).fetchone()[0],
    "api token": database.execute(
        "SELECT COUNT(*) FROM api_tokens WHERE name = 'v0.9-upgrade-token' AND permissions = 'readonly'"
    ).fetchone()[0],
    "npm metadata": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/metadata.json'"
    ).fetchone()[0],
    "npm artifact": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz'"
    ).fetchone()[0],
    "configured upstream": database.execute(
        "SELECT COUNT(*) FROM upstream_records WHERE adapter_type = 'npm' AND name = 'upgrade-fixture'"
    ).fetchone()[0],
    "trial singleton": database.execute("SELECT COUNT(*) FROM trial_records").fetchone()[0],
    "license singleton": database.execute("SELECT COUNT(*) FROM license_storages").fetchone()[0],
}
failed = [name for name, count in checks.items() if count != 1]
if failed:
    raise SystemExit("v0.9.0 fixture is incomplete: " + ", ".join(failed))

trial = database.execute(
    "SELECT id, activated_at, expires_at, activated_by, activated_from, created_at "
    "FROM trial_records ORDER BY id"
).fetchall()
license_rows = database.execute(
    "SELECT id, key, updated_by, updated_at FROM license_storages ORDER BY id"
).fetchall()
if trial[0][0] != 1 or trial[0][3] <= 0 or license_rows[0][0] != 1 or license_rows[0][2] <= 0:
    raise SystemExit("v0.9.0 entitlement singleton fields are invalid")
snapshot = json.dumps({"trial": trial, "license": license_rows}, separators=(",", ":"))
descriptor = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(descriptor, "w", encoding="utf-8") as output:
    output.write(snapshot)
PY

"$current_binary" serve --config "$config" >"$current_log" 2>&1 &
server_pid=$!
wait_ready "$origin" "$current_log"

curl --fail --silent --show-error \
  --header "Authorization: Bearer $old_admin_token" \
  "$origin/api/v1/auth/me" |
  jq -e '.username == "upgrade-admin" and .auth_method == "jwt"' >/dev/null
current_admin_token=$(curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --data "$login_payload" \
  "$origin/api/v1/auth/login" | jq -er '.token')
curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/auth/me" |
  jq -e '.username == "upgrade-admin" and .auth_method == "jwt"' >/dev/null
curl --fail --silent --show-error \
  --header "Authorization: Bearer $api_token" \
  "$origin/api/v1/auth/me" |
  jq -e '.username == "upgrade-admin" and .auth_method == "api_token" and .token_permissions == "readonly"' >/dev/null
curl --fail --silent --show-error --header "Accept: $npm_accept" "$origin/npm/upgrade-fixture" |
  jq -e '.versions["1.0.0"].name == "upgrade-fixture"' >/dev/null
current_artifact=$(curl --fail --silent --show-error "$origin/npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz")
[[ "$current_artifact" == 'depsilo-v0.9.0-cache-artifact' ]] || { echo 'current binary did not serve the v0.9.0 artifact offline' >&2; exit 1; }

current_paid_status=$(curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/admin/license/status")
jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .trial_available == false and .license_key_masked == "depsilo-***"' \
  <<<"$current_paid_status" >/dev/null
curl --fail --silent --show-error \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/admin/projects" >/dev/null

python3 - "$database" "$entitlement_snapshot" <<'PY'
import json
import sqlite3
import sys

database = sqlite3.connect(sys.argv[1])
expected = {
    "administrator": database.execute(
        "SELECT COUNT(*) FROM users WHERE username = 'upgrade-admin' AND role = 'admin' AND enabled = 1"
    ).fetchone()[0],
    "api token": database.execute(
        "SELECT COUNT(*) FROM api_tokens WHERE name = 'v0.9-upgrade-token' AND permissions = 'readonly'"
    ).fetchone()[0],
    "npm metadata": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/metadata.json'"
    ).fetchone()[0],
    "npm artifact": database.execute(
        "SELECT COUNT(*) FROM cache_entries WHERE key = 'npm/upgrade-fixture/-/upgrade-fixture-1.0.0.tgz'"
    ).fetchone()[0],
    "configured upstream": database.execute(
        "SELECT COUNT(*) FROM upstream_records WHERE adapter_type = 'npm' AND name = 'upgrade-fixture'"
    ).fetchone()[0],
    "schema version": database.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0] == 2,
}
failed = [name for name, value in expected.items() if value not in (1, True)]
if failed:
    raise SystemExit("upgrade did not preserve: " + ", ".join(failed))

actual = {
    "trial": database.execute(
        "SELECT id, activated_at, expires_at, activated_by, activated_from, created_at "
        "FROM trial_records ORDER BY id"
    ).fetchall(),
    "license": database.execute(
        "SELECT id, key, updated_by, updated_at FROM license_storages ORDER BY id"
    ).fetchall(),
}
with open(sys.argv[2], encoding="utf-8") as source:
    expected_entitlement = json.load(source)
if {key: [list(row) for row in rows] for key, rows in actual.items()} != expected_entitlement:
    raise SystemExit("upgrade changed durable trial/license rows")
PY

trial_after_clear=$(curl --fail --silent --show-error \
  --request DELETE \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/admin/license/key")
jq -e '.is_pro == true and .source == "trial" and .trial_used == true and .trial_available == false' \
  <<<"$trial_after_clear" >/dev/null
retry_body="$state_dir/trial-reactivation.json"
retry_status=$(curl --silent --show-error \
  --output "$retry_body" \
  --write-out '%{http_code}' \
  --request POST \
  --header "Authorization: Bearer $current_admin_token" \
  "$origin/api/v1/admin/license/trial/activate")
[[ "$retry_status" == 409 ]] || { echo "trial reactivation returned HTTP $retry_status, want 409" >&2; exit 1; }
jq -e '.code == "TRIAL_ALREADY_USED"' "$retry_body" >/dev/null
restored_paid_status=$(curl --fail --silent --show-error \
  --request PUT \
  --header 'Content-Type: application/json' \
  --header "Authorization: Bearer $current_admin_token" \
  --data "$license_payload" \
  "$origin/api/v1/admin/license/key")
jq -e '.is_pro == true and .source == "paid" and .trial_used == true and .license_key_masked == "depsilo-***"' \
  <<<"$restored_paid_status" >/dev/null
python3 - "$database" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(sys.argv[1])
if database.execute("SELECT COUNT(*) FROM trial_records").fetchone()[0] != 1:
    raise SystemExit("trial reactivation changed the singleton row count")
if database.execute("SELECT COUNT(*) FROM license_storages").fetchone()[0] != 1:
    raise SystemExit("license restoration did not restore the singleton row")
PY

stop_server "$current_log"
echo 'v0.9.0 -> current upgrade contract passed (config, SQLite identity, password/JWT/API tokens, trial/paid entitlement, real npm metadata, offline artifact)'
