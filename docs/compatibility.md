# Compatibility Policy

> Published 2026-10-06 for [ADR-0005](adr/0005-enforcement-first-until-1-0.md)
> exit criterion 6. This file is the authority for what Depsilo promises to
> keep working, for how long, and how a removal is announced.

**Status: not in force yet.** Depsilo has no production users, and the
surfaces below are not stable: any release may change them without the
deprecation window. Each change is still listed in the changelog with what to
do. The release that puts this policy in force says so in its release notes.

## What this covers

| Surface | Versioning unit | Authority |
| --- | --- | --- |
| Admin and public JSON HTTP API | `/api/v1` path prefix | this file |
| MCP endpoint | `/mcp` | this file |
| Health, readiness, and metrics | `/health`, `/live`, `/ready`, `/metrics` | this file |
| Compiler-cache HTTP APIs | `/ccache/v1`, `/sccache/v1` | this file |
| Configuration file | `config_version` + the schema in `config.example.toml` | this file |
| Database schema | numbered migration ledger | this file |
| Exported documents | format identifiers ending in `/vN` (`depsilo/snapshot/v1`, `depsilo/cra-technical-file/v1`) | this file |
| Webhook payloads, Prometheus metrics, CLI output | additive fields | this file |
| Package-proxy routes (`/pypi`, `/npm`, `/maven`, `/v2`, …) | the upstream client protocol | the package manager, not Depsilo |

Package-proxy routes exist so that `pip`, `npm`, `docker` and the other
clients keep working. Their compatibility is defined by those clients'
protocols; Depsilo does not add its own version suffix there.

## Release lines

- **Patch releases (`v0.x.y` → `v0.x.z`)** are compatible: no removal or
  behavior change for any surface in the table above. The one exception is a
  security change that ships immediately under the rule in the
  [deprecation window](#deprecation-window).
- **Minor releases (`v0.x` → `v0.y`)** may contain breaking changes until
  `v1.0.0`. Every breaking change is listed under `Changed` or `Removed` in
  the changelog with a migration note, and — for the HTTP API and the
  configuration file — is deprecated for a full minor release first (see the
  window below).
- **From `v1.0.0` on**, the same rules apply with the longer window below and
  semantic versioning: breaking changes require a major release.
- **Security fixes** are released for the latest minor release line only, as
  [SECURITY.md](../SECURITY.md#supported-versions) states. Older lines receive
  no backports.

## Deprecation window

A removal or a breaking change to the HTTP API, the configuration file, a
`/vN` document format, a webhook payload, a metric name/label set, or a CLI
flag is announced and kept working for:

| Phase | Minimum window |
| --- | --- |
| Before `v1.0.0` (minor releases) | one full minor release **or 90 days, whichever is longer** |
| From `v1.0.0` on | two minor releases **or 180 days, whichever is longer** |

The window starts at the release that first announces the deprecation. During
the window the old behavior keeps working; where the surface can carry
signals, the deprecation is also visible there (HTTP responses carry
`Deprecation: true` and `Sunset: <date>` headers, the configuration loader
logs a startup warning naming the replacement, and CLI output prints the
same). Removal happens no earlier than the advertised `Sunset` date.

Security posture changes are the documented exception. When keeping the old
behavior would leave a known hole (for example failing open on a proven
malware match), the change ships immediately and is called out in the
changelog as a security change with the reason and any operator-visible
consequence.

## HTTP API

- Admin and public JSON API routes live under `/api/v1`; new JSON API routes
  are added there, and a future incompatible JSON API becomes `/api/v2`
  rather than changing `/api/v1`. The authenticated MCP endpoint (`POST /mcp`),
  health/readiness/metrics, compiler-cache, package-proxy, and cascade relay
  routes are separate HTTP surfaces, not unversioned JSON API routes.
- Within the window, changes are additive: new endpoints, new optional
  request fields, new response fields, and new enum values. Clients must
  ignore fields they do not know and treat unknown enum values as opaque.
- Existing field names, types, and status codes are stable. Error bodies keep
  the `{ "code": ..., "message": ... }` shape; `code` values are stable
  identifiers (for example `MALICIOUS_BLOCKED`, `SNAPSHOT_BLOCKED`).
- Deleting a field, renaming a field, changing a type, changing a default, or
  making an optional request field required is a breaking change and follows
  the deprecation window.
- Response ordering is not part of the contract unless an endpoint documents
  an order. Lists may return new items between calls; pagination is
  endpoint-specific.
- An unknown route under `/api` answers `404` with the JSON error body and
  code `NOT_FOUND`; its message names `GET /api/v1/discover`. Unknown routes
  under `/mcp`, the health and metrics paths, the compiler-cache paths, and
  the package routes answer plain text `404 not found`.
- `POST /mcp` answers a JSON-RPC message that carries an `id` with `200` and a
  JSON-RPC response. An unknown method is JSON-RPC error `-32601`; that
  includes a `notifications/*` method sent with an `id`. A notification (a
  message without an `id`) and a batch made only of notifications are
  answered with `202 Accepted` and no body. `GET /mcp` and `DELETE /mcp`
  answer `405` with `Allow: POST`. All three methods answer `401` without
  credentials. A client must treat `202` as success.
- The MCP tool `depsilo_configure` returns an ecosystem's snippets in the
  `setup` object, whose string fields `shell`, `env`, `config`, `verify`,
  and `note` appear only where the ecosystem has one. The `apt`, `npm`, and
  `rubygems` results carry `setup.note`, as does `nuget` for a plain-HTTP
  instance, and the `rubygems` `setup.shell` is the project-scope command
  `bundle config set --local ...`.
- `GET /api/v1/integration-prompt` accepts in `url` only a bare `http` or
  `https` origin: a host, an optional port, and at most a trailing slash. A
  value with credentials, a path, a query, a fragment, whitespace, or control
  characters is refused with `400` and code `INVALID_URL`.
- `GET /api/v1/discover` carries `setup_required` and lists
  `endpoints.setup_status`. `GET /api/v1/setup/status` always returns
  `needs_setup` and `token_required`. While setup is pending it also returns
  `listen_port`, `config_exists`, and `token_command`, the command that
  prints the bootstrap token on the server itself (with `--port` and
  `--host` where 127.0.0.1 on port 23333 does not reach the listener); all
  three are absent once setup is complete.
  From a completed setup until the process restarts it also returns
  `"restart_pending": true`; the field is never `false` and is absent in
  every other state. Package routes are served once a `200` answer has
  `needs_setup` false and no `restart_pending`.
- `reconnect_url` in the `POST /api/v1/setup/complete` response is the
  request's own scheme and `Host` with path `/` when the request carried
  `X-Forwarded-Proto` or its `Host` did not name the port Depsilo listens on,
  including a `Host` without a port. Otherwise it is the requested host name
  with the port chosen in the setup form.
- Absolute URLs that Depsilo builds from a request (rewritten npm, PyPI,
  Cargo, Composer, and NuGet metadata, a project's `proxy_url`,
  `GET /api/v1/discover`, and the agent and integration prompts) take their
  scheme from one rule: `https` on a direct TLS connection; otherwise the
  first comma-separated element of `X-Forwarded-Proto`, trimmed and in any
  case, when it is `http` or `https`; otherwise `http`. The header is read
  from any peer: `[server] trusted_proxies` governs only the client address.
  The host is the request's `Host` header; `X-Forwarded-Host` is not read.
- `GET /api/v1/events/stream` sends its status line and headers when the
  stream opens, including `Content-Type: text/event-stream` and
  `X-Accel-Buffering: no`. Events are `data: <json>` lines. While the stream
  is open, a `: keep-alive` comment line follows every 25 seconds; clients
  must ignore comment lines.
- `POST /api/v1/admin/snapshots/import` answers `400` with code
  `SNAPSHOT_FAILED` when the upload is not a manifest the store accepts: a
  body that is not JSON or exceeds the upload limit, an unsupported format,
  no name, more items than the import limit, or an item with a missing
  identity field, an invalid SHA-256, or a negative size. A manifest without
  items answers `400 EMPTY_SNAPSHOT`, a taken name `409 NAME_TAKEN`, and a
  failure while storing a valid manifest `500 SNAPSHOT_FAILED`.
- `GET /api/v1/admin/cache/indexes` lists no Go checksum database entry
  (keys under `go/sumdb/`), and
  `POST /api/v1/admin/cache/indexes/:id/refresh` answers such an entry, like
  any entry that is not index metadata, with `422` and code
  `NOT_REFRESHABLE`. `GET /api/v1/admin/cache` lists these entries with
  `cache_kind` `metadata`.
- `GET /api/v1/admin/audit-logs` and `GET /api/v1/admin/audit-logs/export`
  answer `400` with code `BAD_REQUEST` when `start` or `end` is not an RFC
  3339 time or `end` is before `start`.
- Package-proxy routes keep the status codes their client protocol expects.
  The npm route refuses a tarball path it did not issue with `404`, as
  before, and that response carries the JSON error body with code
  `NPM_UNSIGNED_TARBALL`. The npm route's `404` and `502` error bodies also
  repeat the message in an `error` field, which is the field the npm client
  prints.
- A Docker Registry `502` keeps its codes `UPSTREAM_UNAVAILABLE` and, for a
  manifest `HEAD` whose authentication failed, `AUTH_FAILED`. Its message
  reads
  `<stage> failed at <origin>: <reason> (request <id>; details in the Depsilo server log)`,
  with the stage, the failed host's origin and a reason from a fixed
  vocabulary (listed under
  [Upstream network egress](deployment.md#upstream-network-egress)); it never
  repeats an answer body, error message, status phrase, or URL path or query.
  The request ID is the one in the `X-Request-ID` header and the access log.
- A package Upstream check that fails at the proxy that carries it,
  `upstream_proxy` or the Upstream's own, records the failure code
  `egress_proxy` in the Upstream's `last_failure` and in the check's
  `failure`. When the proxy answers `CONNECT` for an `https` Upstream with a
  status other than `200`, `status` holds that status (the check recorded
  `network`); when Depsilo cannot connect to the proxy, `status` is absent (it
  recorded `connection_refused`, `dns`, `tls` or `network`, after the cause).
  Other failures through a proxy keep their codes: `timeout` for a proxy that
  does not answer in time, `connection_refused` or `network` for a connection
  that the proxy closes, also after it answered `CONNECT` with `200`, and
  `http_status` for a `5xx` that the proxy answers for a plain-`http`
  Upstream. The package-route `502` message `all upstreams are unhealthy`
  names `egress_proxy` when every Upstream of an ecosystem failed at the
  proxy.
- With `[egress] allow_fake_ip` on, the default, a package Upstream without
  a proxy whose host name resolves only to fake-IP addresses
  (`198.18.0.0/15`, `2001:2::/48`) is checked and fetched through those
  addresses and no longer records `egress_fake_ip` when they connect. The
  code now records that none of them connected within 5 seconds (`address`
  is the one dialed first, an IPv4 one when the answer had one, and `range`
  its range), that `allow_fake_ip` is off, or, as before, an answer that the
  guard always refuses: one that mixes fake-IP and other addresses, holds a
  unique-local address of proxy software, or is an address from these ranges
  written in the URL. When the answer holds such a unique-local address,
  `address` and `range` name it, whichever address DNS listed first. A
  fake-IP answer that holds an address of the machine's network interfaces
  records `egress_reserved` with that `address` and no `range`. The check's
  `error` reads `nothing carried the connection to the fake-IP dial target`
  for addresses that did not connect. The package-route `502` message for
  `egress_fake_ip` reads
  `all upstreams are unhealthy: DNS returned fake-IP addresses for every upstream (egress_fake_ip), and Depsilo could not connect to them: nothing carried the connections, [egress] allow_fake_ip is off, or the answers mixed fake-IP and other addresses or held unique-local IPv6 fake-IP addresses, which Depsilo always refuses; turn on the proxy software's TUN mode, or set [egress] upstream_proxy in config.toml (in a container, the environment variable DEPSILO_EGRESS_UPSTREAM_PROXY) and restart, or check allow_fake_ip; for unique-local IPv6 fake-IP addresses, set the proxy software's IPv6 fake-IP range to 2001:2::/48, see docs/deployment.md#upstream-network-egress`.

## Configuration

- `config_version` records the schema generation of the document and is
  pinned in memory at load. A document with a version **newer** than the
  binary supports is rejected at startup instead of being partially
  interpreted; an older or unversioned document is migrated in memory to the
  current generation.
- Unknown keys are rejected at startup with the offending key named, so a
  typo or a key from a newer binary fails loudly instead of silently doing
  nothing.
- A deprecated key keeps working for the full window and logs a startup
  warning naming its replacement. It is removed only in a release whose
  changelog entry says so, and removal requires a `config_version` bump.
- The `custom` table is operator-owned: Depsilo preserves it when patching
  managed settings and never interprets its contents.
- Safety rejections are not deprecations. Values that cannot be enforced
  safely (for example a positive minimum-release-age threshold without bound
  provenance) keep failing at startup, with the fix named in the error.
- While first-run setup is pending, a preset `DEPSILO_BOOTSTRAP_TOKEN` must
  have at least 24 characters, no surrounding whitespace, and at least 8
  distinct characters; startup refuses any other value. The variable is not
  read once setup is complete.
- `[egress]` follows the same file/environment precedence as other settings:
  `DEPSILO_EGRESS_UPSTREAM_PROXY`, `DEPSILO_EGRESS_NO_PROXY`,
  `DEPSILO_EGRESS_PRIVATE_RANGES`, and `DEPSILO_EGRESS_ALLOW_FAKE_IP` override
  `upstream_proxy`, `no_proxy`, `private_ranges`, and `allow_fake_ip`. The two
  lists take comma-separated values, and an environment list replaces the
  file list as a whole. Environment values pass the same validation as file
  values, so an invalid one fails startup.
- `[egress] allow_fake_ip` is new and `true` unless the file or
  `DEPSILO_EGRESS_ALLOW_FAKE_IP` sets it to `false`; like the rest of
  `[egress]`, it is read at startup. Where no proxy carries a request
  (package Upstreams, extra indexes and cascade parents without one, their
  checks, and Docker registries that no proxy carries), Depsilo then dials a
  host name whose DNS answer consists only of fake-IP addresses in
  `198.18.0.0/15` or `2001:2::/48`, IPv4 ones first and a quarter second
  apart, each for at most 5 seconds, instead of refusing it. An address from
  these ranges written in a URL, an answer that mixes them with other
  addresses, and an answer that holds an address of the machine's network
  interfaces stay refused. Set it to `false` to keep the earlier refusal; a
  proxy that carries a request resolves such names whatever the setting.
- `[egress] upstream_proxy` is also the proxy of every `[[docker.registries]]`
  entry without its own `proxy`, chosen from the registry URL as for a package
  Upstream: an entry whose URL names `localhost` or a loopback address, or
  whose host `no_proxy` matches, stays direct (IP and CIDR entries match only
  a URL written with an address), and a registry's own `proxy` always wins.
  Such an entry sends a request through `upstream_proxy` only when Depsilo's
  own answer for the request's host (the registry, its Bearer realm, or a
  redirect target) is public or fake-IP; a private or loopback answer, or
  such an address written in the URL, is dialed directly. When
  `upstream_proxy` answers a plain-`http` request with `502`, `503` or `504`,
  the request fails with a `502` whose reason reads
  `returned <status> through the egress proxy`, and a manifest `HEAD` then
  answers `502` instead of passing the status on. A failure of
  `upstream_proxy` can add a hint to the reason, which asks for a real DNS
  answer or a `no_proxy` entry.
  A host name whose every DNS answer lies in `198.18.0.0/15` or `2001:2::/48`
  counts as public: a proxy that carries the request resolves it, and
  without a proxy, Depsilo dials those addresses itself while
  `allow_fake_ip` is on. When none of them connects, the reason in the `502`
  message names the host and the address dialed first and reads
  `<host> resolved to <address>, a fake-IP address from proxy software, but nothing carried the connection to it;`,
  followed by the proxy software's TUN mode and `[egress] upstream_proxy`,
  or, when `no_proxy` or a loopback URL keeps the registry off a configured
  `upstream_proxy`, by the TUN mode and real DNS answers for its host names.
  On this direct route, the registry's own fake-IP answer pins its network
  scope only once a connection to it succeeded, so a real answer that
  replaces placeholders nothing carried, a private one included, is used at
  the next pull without a restart.
  With `allow_fake_ip` off, such an answer is refused as before, and the
  reason reads
  `<host> resolved to <address>, a fake-IP address from proxy software in TUN/fake-IP mode, and [egress] allow_fake_ip is off;`
  before the earlier remedies. A fake-IP
  answer that holds an address of the machine's network interfaces reads
  `unsafe Docker dial target`, or `unsafe Docker proxy target` through a
  proxy.
- A Docker Bearer realm or redirect target written as a private or loopback
  IP address is no longer refused for a registry configured by host name:
  the network scope that the registry's own answer pinned decides, as it
  does for a host name. `localhost`, `.localhost` names, an address written
  with a trailing dot and an unsafe address stay refused, and a registry
  configured by address keeps the earlier rule.
- Addresses in the unique-local ranges that proxy software documents for IPv6
  fake-IP and for its own TUN, VIF and DNS addresses, `fdfe:dcba:9876::/48`,
  `fd00:6152::/32` and `fc00::/18`, are refused for package Upstreams,
  cascade parents and Docker registries and no longer count as a private
  network; an Upstream check refused for one records `egress_fake_ip`, as one
  refused for an address in `2001:2::/48` now does (it recorded
  `egress_reserved`). A Docker registry, Bearer realm or redirect host name
  whose answer consists of fake-IP and unique-local addresses only, with at
  least one unique-local address and either a fake-IP address or an address
  in these ranges, gets a `502` reason, with or without a proxy, that asks to
  set the proxy software's IPv6 fake-IP range to `2001:2::/48` or turn IPv6
  fake-IP off. A unique-local answer outside these ranges and without fake-IP
  addresses remains a private network.
- `[server] host` and `DEPSILO_SERVER_HOST` accept an IPv6 address with or
  without brackets (`::1` or `[::1]`).
- Optional `[auth.sources.<id>]` configuration declares stable source IDs and
  a protocol (`oidc` or `ldap`). No external source is created by defaults.
  Sources use the existing file/environment precedence, are restart-managed,
  and are preserved by ordinary settings writes. Enabling a source requires a
  complete valid effective configuration. File-only validation permits omitted
  fields supplied by the service environment; runtime loading validates the
  combined result. Per-source environment keys use
  `DEPSILO_AUTH_SOURCES_<UPPERCASE_ID_WITH_HYPHENS_AS_UNDERSCORES>_<FIELD>`;
  only source IDs declared in TOML are enumerated. Nonempty issuer values must
  be distinct across configured sources, including disabled ones; disabled
  drafts may leave the issuer unset. Enabled callbacks
  must share one instance origin. See [OIDC configuration](oidc.md#configure-authentication-sources).
- Existing `[auth.oidc]` remains source ID `oidc`, with its old callback path and
  `DEPSILO_AUTH_OIDC_*` overrides. Explicit names are preserved; the fallback
  name is OIDC. The reserved ID cannot be used by a new named source. Moving
  configuration to another ID does not migrate account bindings.

## Operator authentication

- OIDC and LDAP add public provider/login endpoints and authenticated self-service
  binding endpoints under `/api/v1/auth`; existing local password login remains
  available. `auth/me` adds `permission_source`, `login_method`, and
  `can_manage_credentials`. Clients should use those capabilities rather than
  assuming every administrator session can manage long-lived credentials.
- System management is visible only to administrators; personal Account
  security remains available to every authenticated browser account. The
  existing Users, Settings, and License URLs are preserved. The new
  `/api/v1/admin/authentication/sources` collection supports listing, creation,
  per-source updates, and removal; reads require an administrator role and
  writes additionally require local credential-management authority. The old
  GET/PUT `/api/v1/admin/authentication` remains legacy-only. Client
  secrets are write-only, environment overrides cannot be edited, and saved
  configuration applies after restart. Removed running sources remain listed
  with null configured state until restart; removal preserves account bindings.
- Public providers include `id`, `name`, and `protocol`; login and linking accept
  `provider_id`. Existing legacy callback and completion paths remain. Requests
  without a source select the legacy source, or a sole enabled named source if
  legacy is absent; ambiguous multi-source requests require explicit selection.
  Self-service identities add `providers` while retaining the old `provider`
  field when a legacy or sole source supplies an unambiguous value.
- Existing local password JWTs without an external session claim retain their
  local authentication behavior when the account is enabled, has a password,
  and its credential version and role still match. External accounts and
  external sessions cannot use that legacy path.
- External sessions have a separate absolute expiry and revocation record; refresh
  never converts them into local sessions or extends the provider admission
  window. Existing locally owned API tokens keep their role/expiry behavior;
  provider-managed accounts cannot use API tokens to bypass that window.
  Startup invalidation is source-scoped: changing, disabling, or removing one
  source does not revoke valid sessions for unchanged sources. An unchanged
  legacy OIDC configuration upgrades its exact old fingerprint transactionally
  while preserving valid sessions. Subsequent display-name-only changes also
  preserve sessions. A simultaneous rename during that first legacy upgrade
  cannot prove the old configuration and revokes those sessions conservatively.
- Linking and unlinking are explicit account operations and do not merge
  usernames or emails. Local accounts may bind one identity per source and
  unlink each separately. Provider-managed accounts cannot add another source;
  their deletion, password/role edits, and removal of the required identity are
  rejected. These identity capabilities do not change the authority of existing
  local accounts.

## Database and backups

- Schema changes are numbered, one-way migrations applied exactly once and
  recorded in `schema_migrations`. A database written by a newer binary is
  rejected at startup instead of being mutated by an older binary.
- Schema version 12 adds external login identities, sessions, and a local
  default for account Permission Source. Existing account IDs, password hashes,
  credential versions, and API-token rows are preserved. Multiple OIDC and LDAP sources
  reuse version 12 tables and the global `(issuer,subject)` unique key, without
  an additional database migration. Downgrading across
  this boundary requires the pre-upgrade SQLite backup as well as the older
  binary/image.
- Schema version 17 adds the access log archive (`access_log_archive`,
  `access_log_package_archive`). Rollup retention folds an expiring day into
  it before deleting the day, so all-time Overview totals survive the rollup
  horizon. Days deleted before this version are not recovered. A version 16
  binary refuses the upgraded database; downgrading requires the pre-upgrade
  backup.
- The supported downgrade path is restoring a backup taken before the
  upgrade with `depsilo restore`; migrations are not reversed in place.
- `depsilo backup` writes a checksummed `depsilo-backup` archive containing
  a portable configuration snapshot and a transactionally consistent SQLite
  snapshot. The archived configuration includes the effective JWT signing
  secret, including a key managed in `data/secrets/jwt.key` or supplied through
  `DEPSILO_AUTH_JWT_SECRET`; the source configuration is not modified. Treat
  archives as credentials and keep them private. Restore preserves the signing
  key as `auth.jwt_secret` in the restored configuration, so restoring to a new
  directory does not require a separate secret-file copy. An unreadable,
  missing, or corrupt managed key fails backup rather than generating a
  replacement key or publishing an incomplete archive. Explicit legacy inline
  keys remain supported when archiving older state.
  Cached artifact bytes are deliberately excluded; copy the storage directory
  or bucket out-of-band if you need them.
  OIDC client secrets supplied only through the service environment are not
  copied into the archive; restore the corresponding environment separately.
  Inline OIDC configuration and persisted identity/session records remain part
  of the saved configuration/database and must be protected as private state.
- `depsilo restore` accepts the current archive version (`v2` today) and
  rejects other versions with an explicit error rather than guessing. When a
  new archive version is introduced, the previous reader stays available for
  the deprecation window above.
- Local and S3-compatible cache storage keep their on-disk layout across
  patch releases; cache eviction may remove entries at any time, so the cache
  is never part of the compatibility contract.

## Documents, webhooks, and metrics

- Documents that leave the process carry a format identifier with a version
  suffix (`depsilo/snapshot/v1`, `depsilo/cra-technical-file/v1`). A breaking
  shape change bumps the suffix; the previous reader stays available for the
  window and the changelog states the migration.
- SBOM output follows the published SPDX 2.3 and CycloneDX 1.5 schemas;
  Depsilo's additions are confined to documented fields/properties.
- Webhook payloads gain fields additively. Consumers must tolerate unknown
  fields and new event names.
- Prometheus metric names and existing label sets are stable. New metrics and
  new label *values* are additive; adding or renaming a label follows the
  deprecation window because it creates new series.
- CLI `--json` output gains fields additively; consumers must tolerate unknown
  fields. Scripts can rely on these exit statuses and fields:
  - `depsilo doctor` exits non-zero when a check fails. Pending first-run
    setup is a failed check named `Initial setup`. `depsilo doctor --json`
    and `depsilo status --json` report that state in `setup_required`.
  - `depsilo init-agent` exits `1` and reports `"ok": false` when a target
    file cannot be updated. It also exits `1`, and writes nothing, while the
    instance has not finished first-run setup. Its `--json` output names
    where the written text came from in `source` (`server` or `embedded`).
  - `depsilo prompt --url` applies the origin rule of the integration prompt
    and exits `1` for any other value.
  - `depsilo warmup` exits `0` when the server accepts the job, which it
    answers with `202`; it exited `1` before. `depsilo warmup --json`
    reports `job_id` and `status`.
  - An argument that is exactly `--help` or `-h`, anywhere before a `--`
    argument, makes every command and subcommand print its usage on
    standard output and exit `0` without doing its work. `depsilo setup`
    without arguments prints its usage on standard error and exits `1`;
    `depsilo distribution` without arguments prints its usage on standard
    output and exits `1`.
  - `depsilo serve` reports a failure to start in one line on standard error
    and exits `1`, without a JSON error entry. An occupied port and a
    refused path have their own sentences (`Port N is already in use. ...`,
    `Permission denied: ...`); every other failure, including a
    configuration the loader or the server rejects, is `Error: <message>`.
  - When the daemon of `depsilo start --daemon` exits before it is ready, the
    command prints the daemon's last message, then
    `Error: daemon exited before readiness: <status> (see <log>)`, and exits
    `1`. A configuration error found before the daemon starts is
    `Error: <message>`.
  - `depsilo backup` that cannot create or write the output directory exits
    `1` with `Error: cannot write the backup into <directory>: ...`, or with
    that text in `error` of `{"ok": false, "error": ...}` under `--json`.
  - The startup summary prints `Bootstrap token: <token>` while setup is
    pending (`Bootstrap token: configured via DEPSILO_BOOTSTRAP_TOKEN` for a
    preset token), followed by
    `Retrieve the current bootstrap token: depsilo setup token`, with
    `--port N` for a port other than 23333 and `--host <host>` for a listen
    host other than a wildcard address, `127.0.0.1`, or `localhost`. In a
    container (where `/.dockerenv` or `/run/.containerenv` exists) the
    command reads `docker exec <container> depsilo setup token` with the
    same flags; the `BOOTSTRAP_TOKEN_REQUIRED` message gives the same
    form.

## How to deprecate something

1. Keep the old behavior working and add the replacement.
2. Announce in the changelog (under `Deprecated`) with the `Sunset` date, and
   document the migration in the owning guide (`config.example.toml`,
   `docs/`, or the API reference).
3. Make it visible at runtime: `Deprecation`/`Sunset` headers, a startup
   warning, or CLI output — whichever the surface supports.
4. Remove only after the window; list the removal under `Removed` with the
   replacement and, for configuration, bump `config_version`.

## Current deprecations

No removal has an announced `Sunset` date. The legacy Admin query aliases
`q` (vulnerabilities) and `search` (audit logs) still work when `package` is
absent. The next formally announced deprecation is added here with its
announcement release and `Sunset` date.

## Enforcement

The mechanically checkable parts of this policy are covered by tests:

- `internal/api/api_version_contract_test.go` keeps `/api/` JSON routes under
  the versioned `/api/v1` group; separate HTTP surfaces retain their own paths.
- `internal/config/loader_test.go` covers unknown-key rejection,
  newer-`config_version` rejection, and that `config.example.toml` loads at
  the current version.
- `internal/db/schema_v1_test.go` covers the contiguous migration ledger and
  the fresh/upgraded schema convergence.
- `internal/snapshot` and `internal/sbom` tests assert their exported format
  identifiers keep the `/vN` suffix.


LDAP source settings are nested under `[auth.sources.<id>.ldap]`; see
[LDAP configuration](ldap.md). Existing OIDC paths, source IDs and source
configuration remain compatible. The shared browser account-choice endpoints
also have `/api/v1/auth/identity/{complete,resolve,cancel}` aliases. LDAP adds
`ldap` to `permission_source`, `login_method` and provider protocol values.
Presentation-only source renames retain external sessions after restart. On the
first upgrade from the old full-config OIDC fingerprint, only an exact current
configuration match preserves sessions; changing configuration during that
upgrade remains fail-closed. This refinement and LDAP reuse schema version 12.
