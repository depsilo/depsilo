#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 2 ]; then
    echo "usage: run-dev.sh BINARY CONFIG_PATH [serve flags...]" >&2
    exit 2
fi

binary=$1
config_path=$2
shift 2

if [ ! -x "$binary" ]; then
    echo "development binary is missing or not executable: $binary" >&2
    exit 1
fi

if [ -z "${DEPSILO_AUTH_JWT_SECRET:-}" ]; then
    secret_file=${DEPSILO_DEV_JWT_FILE:-.dev-jwt-secret}
    if [ ! -s "$secret_file" ]; then
        secret_dir=$(dirname "$secret_file")
        mkdir -p "$secret_dir"
        secret_tmp="${secret_file}.tmp.$$.${RANDOM}"
        cleanup_secret_tmp() { rm -f "$secret_tmp"; }
        trap cleanup_secret_tmp EXIT INT TERM
        if command -v node >/dev/null 2>&1; then
            (umask 077 && node -e "process.stdout.write(require('node:crypto').randomBytes(32).toString('hex') + '\\n')" > "$secret_tmp")
        elif command -v openssl >/dev/null 2>&1; then
            (umask 077 && openssl rand -hex 32 > "$secret_tmp")
        else
            echo "Node.js or openssl is required to generate the development JWT secret" >&2
            exit 1
        fi

        # A hard link publishes the generated file only if another concurrent
        # `make run` has not already created it. Both paths then reuse the same
        # persisted secret instead of rotating sessions on every restart.
        if ln "$secret_tmp" "$secret_file" 2>/dev/null; then
            echo ">>> generated development JWT secret: $secret_file"
        elif [ ! -s "$secret_file" ]; then
            echo "failed to create development JWT secret: $secret_file" >&2
            exit 1
        fi
        rm -f "$secret_tmp"
        trap - EXIT INT TERM
    fi

    chmod 600 "$secret_file"
    DEPSILO_AUTH_JWT_SECRET=$(cat "$secret_file")
    if [[ ! "$DEPSILO_AUTH_JWT_SECRET" =~ ^[0-9a-f]{64}$ ]]; then
        echo "invalid development JWT secret in $secret_file; remove it and retry" >&2
        exit 1
    fi
    export DEPSILO_AUTH_JWT_SECRET
fi

# Only promote the project config to an explicit path when it exists. An
# inherited DEPSILO_CONFIG remains authoritative, while a missing local file
# lets the CLI continue through its normal home-config/default search order.
if [ -z "${DEPSILO_CONFIG:-}" ] && [ -f "$config_path" ]; then
    export DEPSILO_CONFIG="$config_path"
fi

exec "$binary" serve "$@"
