# Deployment defaults

Depsilo keeps a zero-config installation under one state root. Persist and back
up that root in every deployment; S3 can move object data elsewhere, but the
generated configuration and SQLite database remain local.

## Default paths

| State | Binary | Official container |
| --- | --- | --- |
| State root | `~/.depsilo` | `/root/.depsilo` |
| Configuration | `~/.depsilo/config.toml` | `/root/.depsilo/config.toml` |
| SQLite database | `~/.depsilo/data/depsilo.db` | `/root/.depsilo/data/depsilo.db` |
| Package cache | `~/.depsilo/data/cache` | `/root/.depsilo/data/cache` |
| Compiler cache | `~/.depsilo/data/compile-cache` | `/root/.depsilo/data/compile-cache` |

The setup flow creates the state directory and writes its generated
`config.toml` with absolute database and local-cache paths. The compiler cache
path is reserved under the same root but remains disabled until configured.

For Docker and Compose, persist `/root/.depsilo` with one named volume. This
keeps the generated configuration, administrator and user records, policy
state, SQLite database, and local caches across restart and container
recreation.

The official image runs Depsilo as the fixed non-root UID/GID `10001:10001`.
New named volumes inherit that ownership automatically.

### Upgrade the Compose layout shipped in v0.9.0

The v0.9.0 `docker-compose.yml` did not use the current state volume. It
bind-mounted the project directory's `data/` at `/app/data` and its
`config.toml` at `/app/config.toml`. Use the fail-closed preparer and
[`compose.v090-compat.yaml`](../compose.v090-compat.yaml) for this exact layout.
Do not adapt the named-volume procedure below or guess a volume name.

Run preparation on Linux with GNU coreutils. Keep the stopped old container
until preparation succeeds: the script verifies its exact two bind mounts,
configuration path, old JWT secret, and candidate JWT policy. Supply the
candidate by immutable digest (or exact local image ID), and choose absent,
non-symlink state and backup targets.

If the v0.9.0 secret is already at least 32 bytes with no surrounding
whitespace, reuse it as the candidate secret:

```bash
IFS= read -rsp 'Existing v0.9.0 JWT secret: ' DEPSILO_AUTH_JWT_SECRET
printf '\n'
export DEPSILO_AUTH_JWT_SECRET
```

If the old secret is shorter, rotate it explicitly during preparation. Retrieve
the old value from the same secret source used by the stopped container; avoid
putting either value in shell history:

```bash
IFS= read -rsp 'Existing v0.9.0 JWT secret: ' DEPSILO_V090_AUTH_JWT_SECRET
printf '\n'
export DEPSILO_V090_AUTH_JWT_SECRET
export DEPSILO_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
export DEPSILO_ACCEPT_JWT_ROTATION=1
```

A rotation invalidates existing browser JWTs, so Operators must sign in again.
Passwords and API tokens remain valid. Metadata from an extra PyPI-compatible
index must be fetched online once because its signed cache identity changes;
already-cached artifact objects remain reusable.

Then prepare the state:

```bash
release_checkout=/path/to/depsilo-release-source
v090_source=/path/to/v090-compose-project
old_container=<stopped-v090-container-name-or-id>
docker stop "$old_container"

export DEPSILO_IMAGE='ghcr.io/depsilo/depsilo@sha256:<release-digest>'
v090_parent="$(dirname "$(realpath "$v090_source")")"
export DEPSILO_V090_DATA_DIR="$(realpath "$v090_source/data")"
export DEPSILO_UPGRADE_STATE_DIR="$v090_parent/depsilo-upgrade-state"
backup_dir="$v090_parent/depsilo-v090-backup"
docker pull "$DEPSILO_IMAGE"

bash "$release_checkout/scripts/prepare-v090-compose-upgrade.sh" \
  --source-dir "$v090_source" \
  --state-dir "$DEPSILO_UPGRADE_STATE_DIR" \
  --backup-dir "$backup_dir" \
  --old-container "$old_container" \
  --image "$DEPSILO_IMAGE"

docker compose -f "$release_checkout/compose.v090-compat.yaml" up -d
(cd "$backup_dir" && sha256sum --check SHA256SUMS)
```

The preparer rejects a running or differently mounted old container, a
non-SQLite database, symlinks, special files, multiply-linked data files, an
occupied target, an old JWT secret that does not match the stopped container, a
weak candidate secret, an unconfirmed secret change, and an image not declaring
`10001:10001`. It performs those checks before creating the backup or changing
ownership. It then backs up `config.toml`, `depsilo.db`, and present WAL/SHM
sidecars and atomically installs a mode-`0600` state config. The compatibility
Compose file disables automatic host-path creation. Never use
`docker compose down -v`, rewrite the old config with `sed`, or delete the
original project until login, API tokens, license state, and cached packages
have been verified.

The ownership helper reads Linux `/proc/self/mountinfo` inside the exact
candidate container and refuses any mount below either recursive chown target.
This prevents a nested bind, tmpfs, or other mount under `data/` from carrying
the ownership change outside the validated v0.9 tree. Use the official Linux
image, which supplies the required POSIX shell, `awk`, `find`, `chown`, and
`stat` utilities.

### Other older named-volume layouts

For an older deployment that actually mounted a named volume at
`/root/.depsilo`, resolve the volume from the old container, validate it, stop
that container, and migrate ownership once. Docker and Compose use different
naming conventions and `docker run -v` silently creates a new empty volume
when the name is wrong. The container path remains unchanged, so absolute paths
in the existing configuration remain valid:

```bash
old_container="${OLD_DEPSILO_CONTAINER:-depsilo}"
state_volume="$(docker inspect "$old_container" --format \
  '{{range .Mounts}}{{if and (eq .Type "volume") (eq .Destination "/root/.depsilo")}}{{.Name}}{{end}}{{end}}')"
test -n "$state_volume"
docker volume inspect "$state_volume" >/dev/null
docker stop "$old_container"
docker run --rm --user 0:0 --entrypoint /bin/chown \
  --mount "type=volume,src=$state_volume,dst=/root/.depsilo" \
  ghcr.io/depsilo/depsilo:0.9.1 \
  -R 10001:10001 /root/.depsilo
```

An empty `state_volume` means this is not that layout; stop and inspect the
deployment instead of continuing. Do not use this generic procedure for the
v0.9.0 Compose bind layout described above.

## Overrides

Advanced deployments can move each component independently:

- `DEPSILO_CONFIG` selects a custom configuration file.
- `DEPSILO_DATABASE_DSN` selects a custom SQLite path.
- `DEPSILO_STORAGE_PATH` selects a custom local package-cache path.
- `DEPSILO_COMPILE_CACHE_STORAGE_PATH` selects a custom compiler-cache path.
- `[storage] type = "s3"` and `[compile_cache.storage] type = "s3"` move object
  data to S3-compatible storage; the configuration and SQLite database still
  need durable local storage.

Explicit settings keep their existing precedence:

```text
CLI flag → DEPSILO_* environment → config file → built-in default
```

## Docker Registry forward-proxy boundary

Before each Docker Registry request is handed to an explicitly configured
forward proxy, Depsilo resolves the registry, Bearer realm, or redirect
hostname locally. It rejects unresolved targets, mixed network scopes, unsafe
special-purpose addresses, and a target that crosses the registry's pinned
public/private/loopback scope. The proxy socket itself may intentionally be a
local service such as `http://127.0.0.1:7890` and is not classified as an
Upstream target.

This preflight is not an end-to-end DNS pin when the forward proxy performs its
own name resolution. In split-DNS deployments, the proxy can receive a
different address from the one Depsilo checked. Treat the proxy's resolver and
egress ACL as part of the security boundary: block loopback, link-local,
metadata-service, and unintended private destinations there as well.

Docker authentication services are commonly on a different public origin from
the registry itself; Docker Hub, for example, delegates authentication to
`auth.docker.io`. Depsilo therefore allows a cross-origin Bearer realm when its
resolved network scope is safe, while removing registry credentials from later
cross-origin redirects. Configuring registry credentials means trusting that
registry to nominate its authentication service.

## Health checks

```bash
depsilo --version
depsilo status
depsilo doctor
curl -fsS http://127.0.0.1:23333/ready
```

For Docker or Compose, use the binary's stable path inside the container:

```bash
docker exec depsilo /app/depsilo status
docker exec depsilo /app/depsilo doctor
```

See [`config.example.toml`](../config.example.toml) for the complete schema and
[Admin control plane](admin-control-plane.md) for runtime ownership and restart
semantics.
