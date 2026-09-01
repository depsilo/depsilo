<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/brand/logo-stacked-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/brand/logo-stacked-light.svg">
  <img src="docs/brand/logo-stacked-light.svg" alt="Depsilo" width="200">
</picture>

**Self-hosted dependency proxy, cache, and supply-chain enforcement for
individuals and small teams.**

Route 14 package ecosystems and Docker OCI through one service. Cache repeated
downloads, manage Upstreams, inspect dependency traffic, and enforce policy on
the package-install request path.

[![Release](https://img.shields.io/github/v/release/depsilo/depsilo)](https://github.com/depsilo/depsilo/releases)
[![Verify](https://github.com/depsilo/depsilo/actions/workflows/verify.yml/badge.svg)](https://github.com/depsilo/depsilo/actions/workflows/verify.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[Website](https://depsilo.com) &bull;
[Quick start](#quick-start) &bull;
[Documentation](#documentation) &bull;
[Container image](https://github.com/depsilo/depsilo/pkgs/container/depsilo) &bull;
[中文](docs/README_zh.md)

</div>

---

## What Depsilo does

```text
Package managers / CI / coding agents
                  │
                  ▼
               Depsilo
    cache · enforce · verify · audit
                  │
                  ▼
               Upstreams
```

- **Cache** — stream artifacts into local or S3-backed storage, coalesce
  concurrent misses, and serve eligible cached artifacts when an Upstream is
  unavailable.
- **Enforce** — block operator-defined package rules, known-malicious
  versions, or releases that violate an enabled cooling period.
- **Verify** — record first-seen hashes and surface tamper alerts when immutable
  artifacts change during a natural refresh.
- **Audit** — keep requests, policy decisions, and Upstream health visible in
  the Admin UI, API, logs, webhooks, and Prometheus metrics.

Depsilo is MIT-licensed, has no telemetry, and is designed as a lightweight
single-instance service backed by SQLite. It is not a multi-node artifact
repository or an HA control plane.

> This README documents the current `master` branch. For a tagged release, use
> the README and configuration reference bundled with that release.

## Quick start

> **Release availability:** the unified state paths, startup summary, and
> first-project onboarding below are available in `v0.9.1` and later. Use the
> README bundled with `v0.9.0` when operating that older release.

Choose a binary or container install. A first run does not require a repository
checkout, `config.toml`, or database and cache environment variables.

### Binary (Linux / macOS)

```bash
curl -fsSL https://depsilo.com/install.sh | bash
depsilo serve
```

For a background process, use `depsilo start --daemon`; a later, independent
terminal can run `depsilo stop`. Linux/macOS detach from the launching terminal,
and Windows uses a per-start named shutdown event instead of relying on a shared
console.

Before replacing a v0.9.0 binary that is running in daemon mode, stop it with the
v0.9.0 binary. v0.9.1 deliberately refuses the old unauthenticated PID-only
record. If the binary was already replaced, first verify the recorded process
has ended, then remove `~/.local/share/depsilo/depsilo.pid` manually before
starting v0.9.1.

Open <http://127.0.0.1:23333>. The installer verifies the release checksum and
supports `DEPSILO_VERSION` and `DEPSILO_INSTALL_DIR` when you need to pin a
version or choose another installation directory.

You can also download an archive directly from
[GitHub Releases](https://github.com/depsilo/depsilo/releases).

### Docker

```bash
docker run -d \
  --name depsilo \
  -p 23333:23333 \
  -v depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest

docker logs depsilo
```

Open <http://127.0.0.1:23333>. The named volume keeps the generated
configuration, SQLite database, cached artifacts, and other runtime state
across container restarts and recreation.

For a long-lived deployment, replace `latest` with a full `X.Y.Z` release tag.
Release images are published to GHCR as the canonical registry; Docker Hub is
maintained as a mirror.

### Docker Compose

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.yaml
docker compose up -d
docker compose logs depsilo
```

The official Compose file contains one service, one port, and one persistent
volume. Override the host port with `PORT=18080 docker compose up -d`, then open
<http://127.0.0.1:18080>. The startup log reports the container listener; use
the published host port in your browser and the logs to find the bootstrap
token.

The image runs as fixed non-root UID/GID `10001:10001`. See the
[deployment guide](docs/deployment.md) for the one-time ownership command when
reusing a named volume created by a v0.9 root-running container or the separate
compatibility path for v0.9's shipped bind-mount Compose layout.

### Finish the first run

The startup summary tells you whether the server, database, and cache are ready.
It also shows the Portal URL and, for a new interactive installation, a
one-time bootstrap token.

1. Open the Portal and enter the bootstrap token.
2. Create the first administrator. Depsilo has no default administrator
   password.
3. Continue to **Connect your first project**.
4. Choose an ecosystem and package manager, copy the generated configuration,
   and run the suggested dependency command.
5. Depsilo detects the real request automatically. Repeat the request when you
   want to confirm a real cache hit, or continue to the Dashboard at any time.

Configuration shown in the browser is generated from the URL you used to open
Depsilo, including LAN hosts, reverse-proxy hostnames, custom ports, and HTTPS.
The Portal never modifies package-manager configuration on your machine.

Existing configured deployments are not forced into onboarding when an
operator signs in to Admin. Operators can reopen the same flow later with
**Connect a project**.

## Supported ecosystems

| Ecosystem | Common clients |
| --- | --- |
| Python | pip, uv, Poetry, Pipenv, PDM |
| Debian / Ubuntu | apt |
| Node.js | npm, pnpm, Yarn, Bun |
| Go | `go` |
| Rust | Cargo |
| Java / Kotlin / Scala | Maven, Gradle, sbt |
| Ruby | RubyGems, Bundler |
| PHP | Composer |
| .NET | `dotnet`, NuGet |
| Data science | Conda, R / CRAN |
| Infrastructure | Helm, Alpine apk |
| Models and datasets | Hugging Face `hf`, Transformers |
| Containers | Docker, containerd, Podman through the separate OCI `/v2/` route |

This is 14 standard path-prefixed ecosystems plus Docker's separate OCI route:
15 install surfaces in total.

The OCI route is a pull-through cache for client `GET` and `HEAD` requests, not
a general-purpose registry for pushing or hosting images.

Docker Registry and Hugging Face client instructions are available in the
Portal, but their Upstreams are not created by the initial setup wizard. Add
them through [`config.example.toml`](config.example.toml) and restart Depsilo
before sending those requests.

## Supply-chain controls

The controls are independent; enabling or disabling one does not silently
change the others.

| Control | Default | Behavior |
| --- | --- | --- |
| Known-malicious blocklist | On | Syncs explicit and all-version OSV MAL records for eight covered ecosystems and blocks a match before serving it. |
| Minimum release age | Off | When enabled, holds newly published versions for an operator-configured period. |
| Tamper detection | On | Compares immutable artifacts against their first-seen SHA-256 during natural refreshes and emits an alert on mismatch. It is alert-only. |
| Package allow / deny rules | Operator-defined | Applies explicit package policy on the request path and records the decision. |

A new installation begins enforcing the malicious-package dataset after its
first successful sync. Later sync failures keep using the last good dataset
instead of interrupting package traffic.

A policy block still means the dependency request reached and was handled by
Depsilo. The Portal and Admin UI distinguish cache hits, misses, policy blocks,
and Upstream errors instead of treating every non-install as the same failure.

Composer may fall back from a refused mirrored distribution to its original
distribution URL. Operators who require hard enforcement for Composer must
also restrict direct client access to that origin.

See [`config.example.toml`](config.example.toml) for the full policy schema and
current ecosystem defaults.

## State, configuration, and health

Zero-config installs keep configuration, SQLite, and local caches under one
state root: `~/.depsilo` for a binary install and `/root/.depsilo` in the
official container. Exact paths, persistence rules, and overrides are in the
[deployment defaults](docs/deployment.md).

Advanced users can override the config, database, local or S3 storage,
authentication, server, and policy settings. Precedence is:

```text
CLI flag → DEPSILO_* environment → config file → built-in default
```

Run the built-in diagnosis directly or through the container:

```bash
depsilo doctor
docker exec depsilo /app/depsilo doctor
```

## Optional integrations

### AI coding agents

```bash
cd my-project
depsilo init-agent
```

`init-agent` updates the detected `AGENTS.md`, `CLAUDE.md`, or `.cursorrules`
inside a marker-owned section. MCP-aware clients can connect to `POST /mcp`
with a regular API token created in Admin. The bootstrap token is only for
first-run setup and must not be reused as a package or MCP credential.

### Compiler caches

Depsilo includes an isolated remote cache for official ccache HTTP and sccache
WebDAV clients. It is not an `sccache-dist` scheduler or a public S3 API. See
the [compiler-cache guide (Chinese)](docs/compile-cache.md) for storage
isolation, credentials, quotas, and client setup.

## Security and release integrity

- Interactive setup is protected by a one-time bootstrap token.
- Administrator sessions use JWT authentication; API tokens are stored
  hash-only.
- Use a JWT signing secret of at least 32 random bytes even when Depsilo binds
  to loopback behind a reverse proxy.
- Put Internet-facing deployments behind a trusted reverse proxy for TLS and,
  when needed, an external identity-aware access layer. Admin authentication
  still uses Depsilo credentials.
- A signed `checksums.txt` authenticates release archives. The installer and
  SBOM attachments have their own signature bundles; container digests are
  signed directly and carry a CycloneDX attestation.
- Depsilo does not phone home or report package names, cache activity, or
  onboarding progress to depsilo.com.

Read [SECURITY.md](SECURITY.md) before an Internet-reachable deployment and use
the [release-verification guide](docs/release-verification.md) to verify release
artifacts and images.

## Develop from source

```bash
git clone https://github.com/depsilo/depsilo.git
cd depsilo
make setup build
./bin/depsilo serve
```

Current tool versions, hot reload, and testing are in the
[development quick start](docs/development/quick-start.md). Run `make check`
before a normal change; `make verify` is the complete offline gate.

## Documentation

| Goal | Read |
| --- | --- |
| Deploy and locate persistent state | [Deployment defaults](docs/deployment.md) |
| Configure every available setting | [`config.example.toml`](config.example.toml) |
| Verify a deployed instance | [Self-test checklist (Chinese)](docs/self-test-checklist.md) |
| Understand Admin and live configuration ownership | [Admin control plane](docs/admin-control-plane.md) |
| Configure ccache or sccache | [Compiler cache (Chinese)](docs/compile-cache.md) |
| Verify signed releases, images, and SBOMs | [Release verification](docs/release-verification.md) |
| See what changed in each release | [Changelog](CHANGELOG.md) · [GitHub Releases](https://github.com/depsilo/depsilo/releases) |
| Understand current product scope and constraints | [Product](PRODUCT.md) |
| Develop or contribute | [Documentation map](docs/README.md) · [Contributing](CONTRIBUTING.md) |

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the
workflow. Coding agents should begin with [AGENTS.md](AGENTS.md).

## License

[MIT License](LICENSE)

---

<div align="center">

[depsilo.com](https://depsilo.com)

</div>
