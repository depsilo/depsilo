#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

FAKE_BIN="$TMP/depsilo"
SECRET_FILE="$TMP/dev-jwt-secret"
CONFIG_FILE="$TMP/config.toml"
CAPTURE_SECRET="$TMP/captured-secret"
CAPTURE_CONFIG="$TMP/captured-config"
CAPTURE_ARGS="$TMP/captured-args"
export CAPTURE_SECRET CAPTURE_CONFIG CAPTURE_ARGS

cat > "$FAKE_BIN" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s' "${DEPSILO_AUTH_JWT_SECRET-}" > "$CAPTURE_SECRET"
printf '%s' "${DEPSILO_CONFIG-unset}" > "$CAPTURE_CONFIG"
printf '%s\n' "$@" > "$CAPTURE_ARGS"
SH
chmod +x "$FAKE_BIN"

run_with_generated_secret() {
    (
        unset DEPSILO_AUTH_JWT_SECRET DEPSILO_CONFIG
        DEPSILO_DEV_JWT_FILE="$SECRET_FILE" \
            bash "$ROOT/scripts/run-dev.sh" "$FAKE_BIN" "$CONFIG_FILE" --port 18080
    )
}

run_with_generated_secret

if ! grep -Eq '^[0-9a-f]{64}$' "$SECRET_FILE"; then
    echo "development JWT was not generated as 32 random bytes encoded in hex" >&2
    exit 1
fi
if [ "$(cat "$CAPTURE_SECRET")" != "$(cat "$SECRET_FILE")" ]; then
    echo "generated JWT was not passed to the server" >&2
    exit 1
fi
if [ "$(cat "$CAPTURE_CONFIG")" != "unset" ]; then
    echo "missing project config was still forced into DEPSILO_CONFIG" >&2
    exit 1
fi
if [ "$(cat "$CAPTURE_ARGS")" != $'serve\n--port\n18080' ]; then
    echo "development runner changed the server arguments" >&2
    exit 1
fi

if mode=$(stat -c '%a' "$SECRET_FILE" 2>/dev/null); then
    :
else
    mode=$(stat -f '%Lp' "$SECRET_FILE")
fi
if [ "$mode" != "600" ]; then
    echo "development JWT permissions are $mode, want 600" >&2
    exit 1
fi

first_secret=$(cat "$SECRET_FILE")
run_with_generated_secret
if [ "$(cat "$SECRET_FILE")" != "$first_secret" ]; then
    echo "development JWT changed across restarts" >&2
    exit 1
fi

: > "$CONFIG_FILE"
run_with_generated_secret
if [ "$(cat "$CAPTURE_CONFIG")" != "$CONFIG_FILE" ]; then
    echo "existing project config was not passed to the server" >&2
    exit 1
fi

rm -f "$CONFIG_FILE"
explicit_secret='explicit-development-secret-0123456789abcdef'
explicit_secret_file="$TMP/unused-dev-jwt-secret"
(
    unset DEPSILO_CONFIG
    DEPSILO_AUTH_JWT_SECRET="$explicit_secret" \
        DEPSILO_DEV_JWT_FILE="$explicit_secret_file" \
        bash "$ROOT/scripts/run-dev.sh" "$FAKE_BIN" "$CONFIG_FILE"
)
if [ "$(cat "$CAPTURE_SECRET")" != "$explicit_secret" ]; then
    echo "explicit DEPSILO_AUTH_JWT_SECRET did not take precedence" >&2
    exit 1
fi
if [ -e "$explicit_secret_file" ]; then
    echo "development JWT file was created despite an explicit environment secret" >&2
    exit 1
fi

inherited_config="$TMP/inherited.toml"
: > "$CONFIG_FILE"
(
    DEPSILO_CONFIG="$inherited_config" \
        DEPSILO_AUTH_JWT_SECRET="$explicit_secret" \
        bash "$ROOT/scripts/run-dev.sh" "$FAKE_BIN" "$CONFIG_FILE"
)
if [ "$(cat "$CAPTURE_CONFIG")" != "$inherited_config" ]; then
    echo "inherited DEPSILO_CONFIG was not preserved" >&2
    exit 1
fi

invalid_secret_file="$TMP/invalid-dev-jwt-secret"
printf '%032d\n%032d\n' 0 0 > "$invalid_secret_file"
if (
    unset DEPSILO_AUTH_JWT_SECRET DEPSILO_CONFIG
    DEPSILO_DEV_JWT_FILE="$invalid_secret_file" \
        bash "$ROOT/scripts/run-dev.sh" "$FAKE_BIN" "$CONFIG_FILE" >/dev/null 2>&1
); then
    echo "development runner accepted a JWT file containing an embedded newline" >&2
    exit 1
fi

for target in run run-pro dev; do
    dry_run=$(make -n -C "$ROOT" "$target" BIN="$FAKE_BIN" CONFIG="$CONFIG_FILE" DEV_JWT_SECRET="$SECRET_FILE")
    if ! grep -Fq 'scripts/run-dev.sh' <<<"$dry_run"; then
        echo "make $target does not use the guarded development runner" >&2
        exit 1
    fi
done

echo "make development runner tests passed"
