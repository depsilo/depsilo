# Deployment

Choose a container or binary installation. A first run needs no repository
checkout, configuration file, or database and cache environment variables.
This guide follows the current branch; when operating a tagged release, use
the documentation bundled with that release.

Depsilo keeps a zero-config installation under one state root. Persist and back
up that root in every deployment; S3 can move object data elsewhere, but the
generated configuration and SQLite database remain local.

## Docker

```bash
docker run -d \
  --name depsilo \
  --publish 23333:23333 \
  --volume depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
docker logs depsilo
```

Open <http://127.0.0.1:23333> and complete [first-run setup](#first-run-setup).
The published port is available on all host interfaces; use
`--publish 127.0.0.1:23333:23333` to expose it only on the Docker host.

For a long-lived deployment, replace `latest` with a full `X.Y.Z` release tag
or immutable image digest. GHCR is the canonical registry; Docker Hub is a
mirror. See [release verification](release-verification.md) for signed release
artifacts and image verification. The named volume preserves configuration,
SQLite, and local caches across container recreation. For host-mounted state
or an older deployment, use the corresponding [state procedure](#default-paths).

## Docker Compose

The repository's [`compose.yaml`](../compose.yaml) contains one service, one
published port, and one persistent named volume. Download it into a dedicated
deployment directory, or use the copy from your selected release:

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.yaml
docker compose up -d
docker compose exec depsilo depsilo setup token
```

The download above tracks `master`; replace `master` with the release tag when
pinning the deployment. Compose accepts these environment overrides:

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEPSILO_IMAGE` | `ghcr.io/depsilo/depsilo:latest` | Pinned release or locally built image |
| `DEPSILO_BIND_ADDRESS` | `0.0.0.0` | Host interface on which to publish the port |
| `PORT` | `23333` | Published host port; the container still listens on `23333` |

For example, after building the local image, recreate the container with:

```bash
DEPSILO_IMAGE=depsilo/depsilo:local \
DEPSILO_BIND_ADDRESS=127.0.0.1 \
PORT=18080 \
docker compose up -d --force-recreate
```

Keep these values in the deployment's `.env` file or export them for subsequent
Compose commands. Changing the image variable does not replace a running
container until you recreate it. Use the published host port in your browser;
the startup log reports the container listener.

For a host directory instead of a named volume, download the alternative
template and follow [Bind-mounted state](#bind-mounted-state) to prepare its
ownership before starting it:

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.bind.yaml
```

Use `-f compose.bind.yaml` consistently for that deployment, including token
retrieval: `docker compose -f compose.bind.yaml exec depsilo depsilo setup token`.

## Binary installation

On Linux or macOS:

```bash
curl -fsSL https://depsilo.com/install.sh | bash
depsilo serve
```

The installer detects the OS and CPU architecture, downloads the release
archive and `checksums.txt`, and verifies the archive's SHA-256 before installing
the binary. It needs Bash, `curl` or `wget`, an archive extractor, and
`sha256sum` or `shasum`. It does not start Depsilo or register an operating-system
service. `serve` runs in the foreground; stop it with Ctrl+C.

Alternatively, download the appropriate archive from
[GitHub Releases](https://github.com/depsilo/depsilo/releases), verify it using
[release verification](release-verification.md), and put the extracted binary
on your `PATH`. Windows users can use the native executable from its release
archive.

### Installer options

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEPSILO_VERSION` | `latest` | Exact release tag to download, including its `v` prefix |
| `DEPSILO_INSTALL_DIR` | `/usr/local/bin` | Directory in which to install the binary |

Replace `vX.Y.Z` with the desired release tag. Pass the variables to the Bash
process running the installer, rather than only to the download command:

```bash
curl -fsSL https://depsilo.com/install.sh | \
  DEPSILO_VERSION=vX.Y.Z DEPSILO_INSTALL_DIR="$HOME/.local/bin" bash
```

Ensure the chosen directory is on your `PATH`. The default `/usr/local/bin`
installation uses `sudo` when that directory is not writable; a custom
directory must be writable by the invoking user.

### Background process

```bash
depsilo start --daemon
depsilo status
depsilo stop
```

Run `stop` when you want to shut down the daemon; it can run from a later,
independent terminal under the same user. Linux and macOS detach from the
launching terminal. Windows uses a per-start shutdown event. Daemon mode does
not configure automatic startup at boot or restart after a crash.

`start --daemon` accepts the same listener and configuration flags as `serve`,
for example `depsilo start --daemon --config /etc/depsilo.toml --port 18080`.
It waits for readiness and prints the log location. The daemon record and log
normally live at `~/.local/share/depsilo/depsilo.pid` and
`~/.local/share/depsilo/depsilo.log`, separate from the application state root.
Each new daemon start replaces the previous log. For a custom port, select the
matching URL when running [health checks](#health-checks).

### Upgrade a v0.9.0 daemon

Before replacing a v0.9.0 binary running in daemon mode, stop it with the
v0.9.0 binary. Starting with v0.9.1, daemon control requires a process-identity
record and deliberately refuses the older PID-only file. If the binary has
already been replaced, first verify that the recorded process has ended, then
remove `~/.local/share/depsilo/depsilo.pid` manually before starting the new
binary. This is distinct from the
[v0.9.0 Compose state migration](#upgrade-the-compose-layout-shipped-in-v090).

## First-run setup

The startup summary reports server, database, and cache readiness and the
Portal URL. For an unconfigured instance it also shows the generated bootstrap
token, or notes that a token was supplied through `DEPSILO_BOOTSTRAP_TOKEN`.
Retrieve the current token while the service is running:

```bash
depsilo setup token
```

Run this as the service user with the same configuration path and listener
environment. For a custom file, use `depsilo setup token --config PATH` (or
`DEPSILO_CONFIG`). This command uses the local configuration and intentionally
does not follow `DEPSILO_URL` to a remote instance. In containers, run it inside
the running service:

```bash
docker exec depsilo /app/depsilo setup token
```

For Compose, use `docker compose exec depsilo depsilo setup token`. Paste the
command's output into the setup form. `jwt.key` is the persistent signing key,
not the bootstrap token; token retrieval stops after setup is complete.

1. Open the Portal at the address exposed by your deployment and enter the
   bootstrap token.
2. Create the first administrator. There is no default administrator password.
3. Continue to the client connection guide, choose a package manager, and run
   the configuration and test commands on the client machine.

## Client setup

The Portal provides package-manager instructions; authenticated operators can
reopen **Connect a client** from the Dashboard. Configuration is generated from
the browser's current origin, including the hostname, HTTPS, and custom port.
Open Depsilo through the address that your client can reach. The Portal does
not change configuration on your machine.

The authenticated guide observes related requests and distinguishes artifact
delivery from cache hits and policy blocks. Repeat the test following its
instructions to check a cache hit: client caching, server cache rules, and
expiry can affect the result. You can return to the Dashboard at any time;
existing configured deployments do not repeat first-run setup on login.

### Optional ecosystems

Docker Registry and Hugging Face have client instructions but are not activated
by the initial setup wizard:

- **Docker:** an administrator can select Docker in **Connect a client** and
  initialize Docker Hub when no registries are configured, the configuration
  file is writable, and no Docker environment override is active. Restart the
  service after saving. Existing custom registries remain file-managed under
  `[docker]` and `[[docker.registries]]`; ordinary Upstream management does not
  change them. The OCI route caches client `GET` and `HEAD` pulls, not image
  pushes or hosted repositories.
- **Hugging Face:** add `[[huggingface.upstreams]]` to the active configuration
  and restart to activate the ecosystem for the first time. After activation,
  its ordinary Upstreams can be added, edited, and removed through Admin's
  Upstreams page without restarting.

Use [`config.example.toml`](../config.example.toml) for the relevant sections and
[Admin control plane](admin-control-plane.md#authority) for configuration versus
runtime ownership. Restart through the process or container that owns your
deployment; saving these initial settings does not restart it automatically.

### AI coding agents

Run this in the project whose dependency commands should use Depsilo:

```bash
depsilo init-agent
```

The command updates a marker-owned section in the detected `AGENTS.md`,
`CLAUDE.md`, or `.cursorrules`, preserving surrounding content. If none is
detected, it creates `AGENTS.md`. Use `--dry-run` to preview changes and
`--endpoint https://depsilo.example.com` to select another instance.

MCP-aware clients can connect to `POST /mcp` on the instance with a regular API
token created in Admin, supplied as `Authorization: Bearer <token>`. The
bootstrap token is only for first-run setup and is not a package or MCP
credential.

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

After setup, the browser reconnects through its current scheme, hostname, and
port. For example, an HTTPS reverse proxy can keep serving port 443 while
Depsilo listens on HTTP port 23333 behind it. If you change Depsilo's listener
port, update the proxy target or container port mapping as needed. With direct
access to a changed listener, reopen the new address manually.

For Docker and Compose, persist `/root/.depsilo` with one named volume. This
keeps the generated configuration, administrator and user records, policy
state, SQLite database, and local caches across restart and container
recreation.

The official image runs Depsilo as the fixed non-root UID/GID `10001:10001`.
New named volumes inherit that ownership automatically.

### Bind-mounted state

Use a bind mount when host-side tools must read or back up the state root
directly. Name the host directory `state/` (rather than `data/`) because it
contains `config.toml` as well as the nested `data/` directory. The bind source
must be owned by the image's fixed service identity, `10001:10001`:

```bash
state_dir="${DEPSILO_STATE_DIR:-$HOME/.local/share/depsilo/state}"
mkdir -p "$state_dir"
docker run --rm --user 0:0 \
  --entrypoint /bin/chown \
  --mount "type=bind,src=$state_dir,dst=/root/.depsilo" \
  ghcr.io/depsilo/depsilo:latest \
  -R 10001:10001 /root/.depsilo
```

Start the bind deployment with the checked-in template:

```bash
export DEPSILO_STATE_DIR="$HOME/.local/share/depsilo/state"
export DEPSILO_IMAGE=ghcr.io/depsilo/depsilo:X.Y.Z
docker compose -f compose.bind.yaml up -d
```

The same bind layout can be started directly with Docker Engine:

```bash
docker run -d --name depsilo \
  --restart unless-stopped \
  --publish "${PORT:-23333}:23333" \
  --mount "type=bind,src=$state_dir,dst=/root/.depsilo" \
  "${DEPSILO_IMAGE:-ghcr.io/depsilo/depsilo:X.Y.Z}"
```

Use `--mount type=volume,src=depsilo-data,dst=/root/.depsilo` in the same
command for the named-volume variant. Keep one container and one state source
at a time; changing the source creates a separate state unless you migrate it
explicitly.

The template uses `create_host_path: false`, so a typo or missing directory
fails before the container starts instead of silently creating a root-owned
directory. Do not use `user: root` as a permissions workaround; it can create
state that the fixed service identity cannot reopen after an upgrade.

On SELinux hosts, apply the host's normal container labeling policy to the bind
directory. On rootless Docker, prefer the named-volume template because the
daemon's UID mapping can differ from the numeric UID shown inside the image.

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
old_container='<stopped-v090-container-name-or-id>'
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

## Upstream network egress

Depsilo connects to package Upstreams and cascade parents only after checking
the addresses their host names resolve to. It never dials link-local,
multicast, documentation, benchmarking or other special-purpose addresses, and
a public Upstream cannot redirect into a private or loopback address. Two
common networks resolve to such addresses; Admin → Upstreams names the case on
the failing Upstream and in its check result.

**Proxy software in fake-IP mode.** Clash, mihomo, Surge and sing-box in
TUN/fake-IP mode answer DNS queries for public hosts with addresses from
`198.18.0.0/15`. Depsilo refuses those addresses, so every Upstream fails its
check. Route Upstreams through the proxy software's HTTP port instead, or give
Depsilo a DNS server that returns real addresses:

```toml
[egress]
upstream_proxy = "http://127.0.0.1:7890"   # the proxy software's HTTP or mixed port
no_proxy = ["mirror.lan", "192.168.0.0/16"] # optional: Upstream hosts that stay direct
```

`upstream_proxy` is the default HTTP(S) proxy for every package Upstream and
extra index that has neither its own proxy nor a cascade parent; it is chosen
once per Upstream from its configured host. An Upstream's own proxy always
wins, loopback hosts are never proxied, and `no_proxy` accepts host names,
domains (a leading `.` matches subdomains only), IP addresses and CIDR ranges.
Like an Upstream's own proxy, the proxy then resolves and connects to every
host that Upstream uses, so its egress rules become part of the boundary.
Docker registries, the cascade relay and vulnerability or blocklist sync keep
their own settings; Docker registry requests still need real DNS answers (see
below).

**Overlay networks.** Tailscale, Headscale and NetBird assign node addresses
from the shared address space `100.64.0.0/10`, which Depsilo also refuses by
default. Declare the part your own nodes use:

```toml
[egress]
private_ranges = ["100.64.0.0/10"]   # or narrower ranges, or single addresses
```

Declared addresses are treated as private networks: an Upstream or cascade
parent configured with such an address (or a name resolving to it) is
reachable, and it may reach other private addresses, but a public Upstream
still cannot redirect into them. Only ranges inside `100.64.0.0/10` are
accepted, `100.100.100.200` (the Alibaba Cloud instance-metadata endpoint)
stays blocked, and `198.18.0.0/15` is rejected with a pointer to
`upstream_proxy`, because treating fake-IP answers as private would let every
public Upstream redirect into the local network. `[egress]` settings are read
at startup; restart after changing them.

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

## When Upstreams are unreachable

Expiry never deletes cached content. When a package index expires, Depsilo
refreshes it before answering; if the refresh fails, or is still waiting after
10 seconds, the cached index is served while the refresh continues. Cached
artifacts are served directly, so only content that was never cached needs a
reachable Upstream.

An actively probed Upstream whose requests fail slowly twice in a row, each
waiting at least 10 seconds as on a link that drops packets, leaves selection
until a probe succeeds. Depsilo re-probes it after 30 seconds and then at
doubling delays of up to 5 minutes rather than its regular probe interval.
Failures that return at once, such as a refused connection, do not count:
the cached copy already answers them immediately.

While the known-malicious dataset covers PyPI, a legacy archive such as a
`.zip` sdist proves its identity from the cached project index, so it stays
available after a restart without network.

## Health checks

`status` reports service health and available statistics; `doctor` performs
read-only checks for reachability, readiness, version mismatch, storage,
Upstream health, and recent cache activity, with actionable hints:

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

For a different port or remote instance, select its origin explicitly:

```bash
DEPSILO_URL=http://127.0.0.1:18080 depsilo doctor
```

The CLI otherwise uses `http://localhost:23333`; `status` and `doctor` do not
derive the target from a custom server configuration file. Both support
`--json`. Some checks need an Admin credential; set `DEPSILO_TOKEN` or use the
CLI's `~/.config/depsilo/token` file to include authorized details. Missing
credentials are reported separately from service failure. The `/ready` probe
checks database and storage availability; it does not prove that every
Upstream is reachable or that a dependency download will succeed.

### Diagnostic report

To collect a local report for troubleshooting:

```bash
depsilo diagnose --out depsilo-diagnostic.json
```

The report uses the same `DEPSILO_URL` selection, includes enumerated health
and capability facts, and omits configuration, credentials, request samples,
package identities, and client addresses. It is created with owner-only
permissions and refuses to overwrite an existing file; choose a new output
name for each report.

See [`config.example.toml`](../config.example.toml) for the complete schema and
[Admin control plane](admin-control-plane.md) for runtime ownership and restart
semantics.
