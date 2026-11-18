# Changelog

All notable changes to Depsilo will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added
- `depsilo reset-password [--config PATH] [<username>]` sets a local
  account's password in the SQLite database. Stop Depsilo first: it refuses
  while a server uses the database. It reads a no-echo prompt (twice) or one
  line of non-terminal stdin, never a flag, argument, or variable; without a
  known username it lists the accounts and exits 1. It refuses disabled or
  externally managed accounts and another schema version, never migrates,
  and ends the account's sessions; API tokens keep working, so revoke them
  if needed. The audit log records `user.cli_reset` on `user:<id>` with the
  operating-system user (Admin: "Password reset from the local CLI").
- Go: checksum database requests (`/go/sumdb/<name>/...`) are relayed to the
  Go Upstreams, skipping one that answers 404 or 410. When one mirrors the
  database, as goproxy.cn does, the default `GOSUMDB` verifies through
  Depsilo; otherwise `supported` answers 404 and the go command asks the
  database itself. Tiles and lookups are cached for `cache.ttl_blob`,
  `supported` and `latest` for `cache.ttl_index`. They count as metadata:
  audited as `metadata` with no package name, never raise `tamper_detected`,
  and are left out of download feeds, usage, and "Cache indexes" (a refresh
  answers `422 NOT_REFRESHABLE`), but stay among cache objects. Quick Start,
  "Connect a client", and the integration guide keep `GOSUMDB` at its
  default, or set `GOSUMDB=sum.golang.google.cn` when no Go Upstream mirrors
  the database and sum.golang.org is unreachable.
- Admin lists (access logs, audit log, refresh records, cache objects, index
  cache, quarantine events) filter by several ecosystem chips with counts
  and show when rows were fetched; the first three share a time control (1
  hour, 24 hours, 7 days, 30 days, all time, custom). The API takes repeated
  or comma-separated `?adapter_type=` or `?ecosystem=` values and returns
  per-ecosystem counts in `ecosystems`; `GET /api/v1/admin/logs` and
  `GET /api/v1/admin/upstream-updates` accept RFC 3339 `start` and `end`.
- Setup status: `GET /api/v1/discover` reports `setup_required` and lists
  `setup_status`; while setup is pending it and `/api/v1/agent-prompt` say a
  human must finish setup in the Portal. `GET /api/v1/setup/status` returns
  `listen_port`, `config_exists` and `token_command` while setup is pending,
  then `"restart_pending": true` until the process restarts (by the operator
  when `restart_strategy` is `supervisor_required`). Package routes are
  served once a `200` answer has `needs_setup` false and no
  `restart_pending`.
- `depsilo setup token --port`/`-p` and `--host` reach another listener; it
  names the URL asked when setup is complete. The startup summary, the setup
  page and `BOOTSTRAP_TOKEN_REQUIRED` print `--port N` on a non-default port
  and `--host <host>` when 127.0.0.1 does not reach the listener.
- The Portal page links `/api/v1/discover` in `noscript` and
  `link rel="alternate"` for clients that do not run JavaScript.
- `make test-docker-npm` tests fresh and lockfile installs with npm, pnpm,
  Yarn 1, Yarn 4, and Bun; the Go fixture verifies checksums.
- `[egress] allow_fake_ip` (environment variable
  `DEPSILO_EGRESS_ALLOW_FAKE_IP`, `true` or `false`; read at startup), on
  unless set to `false`: where no proxy carries a request, Depsilo connects
  to a host name answered only with fake-IP addresses for the proxy
  software's TUN to carry (see Changed). `false` refuses such answers before
  anything is dialed unless a proxy carries the request, as earlier releases
  did; `config.example.toml` shows the key commented out.

### Changed
- Development: `make security` and the CI frontend job audit frontend
  dependencies with `scripts/npm-audit-gate.py`, which fails on advisories of
  moderate or higher severity, as `npm audit --audit-level=moderate` did,
  except those that `web/npm-audit-allowlist.json` lists with a reason and an
  expiry date at most 180 days ahead. The first entry,
  GHSA-fc5q-39xc-vjpf until 2027-01-31, covers picomatch, which has no patched
  release and reaches the tree only through development tooling. CI now runs
  the audit after the frontend tests, so an advisory no longer skips them.
- `POST /mcp` answers JSON-RPC notifications (no `id`), and batches of only
  notifications, with `202 Accepted` and no body instead of `200`; a
  `notifications/*` method with an `id` gets error `-32601`. `GET /mcp` and
  `DELETE /mcp` answer `405` with `Allow: POST`, or `401` without
  credentials, instead of `404`.
- Unknown `/api` routes answer `404` with a JSON body, code `NOT_FOUND`, and
  a pointer to `GET /api/v1/discover` instead of plain `404 not found`.
- `GET /api/v1/admin/audit-logs` and `/audit-logs/export` answer
  `400 BAD_REQUEST` for a `start` or `end` that is not RFC 3339, or an `end`
  before `start`, instead of returning all time.
- `POST /api/v1/admin/snapshots/import` answers an invalid manifest (for
  example over 200000 items) with `400 SNAPSHOT_FAILED` instead of `500`; a
  failure storing a valid one still answers `500`.
- `depsilo doctor` fails an `Initial setup` check while first-run setup is
  pending, points to the Portal, and exits non-zero; probe `/health` for
  liveness only. `depsilo status` prints a `Setup:` line; `--json` of both
  has `setup_required`. With a token, doctor lists Upstream failure codes
  with counts and, for `egress_fake_ip`, its causes and remedies (the proxy
  software's TUN mode, `upstream_proxy` under `[egress]` or
  `DEPSILO_EGRESS_UPSTREAM_PROXY`, `allow_fake_ip`, or the IPv6 fake-IP
  range `2001:2::/48`); without one, its hint mentions fake-IP DNS whose TUN
  mode is off. It no longer asks for a valid token that
  was accepted, and calls an Upstream slow at 1000 ms (was 150), as the
  Portal does.
- `depsilo init-agent` exits 1 with `"ok": false` and no success sentence
  when a target file cannot be updated (a `depsilo:start` marker without its
  end marker is now left untouched), and exits 1 writing nothing while setup
  is pending. `--json` reports `source` (`server` or `embedded`); when the
  server does not answer within one request timeout, it warns on stderr and
  writes the built-in text.
- `depsilo warmup` treats HTTP 202 as success instead of exiting 1, so
  scripts no longer retry into duplicate jobs, and prints the job id and
  status; `--json` has `job_id` and `status`.
- `depsilo serve` reports a failed start as one line on stderr and exit 1
  (`Port N is already in use. ...` or `Error: <message>`) instead of JSON
  with a stack trace, and explains a refused state directory (UID and GID
  10001 in the official image), config file, or relative `database.dsn` or
  storage path. `depsilo start --daemon` explains a refused path the same
  way, prints the daemon's last message before
  `Error: daemon exited before readiness: ...`, and prints `Error: <message>`
  instead of `Error loading daemon configuration: ...`.
- `--help` and `-h` print usage on stdout and exit 0 for every command
  without running it (`depsilo flush --help` sent the cleanup,
  `depsilo stop --help` stopped the daemon); `depsilo setup` alone still
  exits 1 on stderr. `depsilo help` gains a tagline and the `--json`
  commands, `depsilo setup --help` its flags and the `docker exec` form.
- Egress: `DEPSILO_EGRESS_UPSTREAM_PROXY`, `DEPSILO_EGRESS_NO_PROXY`,
  `DEPSILO_EGRESS_PRIVATE_RANGES`, and the new `DEPSILO_EGRESS_ALLOW_FAKE_IP`
  override `[egress]` even when the key is not in `config.toml`; lists are
  comma-separated and replace the file list, and an invalid value fails
  startup. When all Upstreams of an ecosystem failed for one egress reason,
  the `502` message `all upstreams are unhealthy` appends the code and
  points at `[egress]` and `docs/deployment.md#upstream-network-egress`
  (plus the TUN mode, the proxy variable and `allow_fake_ip` for
  `egress_fake_ip`) until the Upstream succeeds again.
- Docker: a registry without its own `proxy` follows
  `[egress] upstream_proxy` (or `DEPSILO_EGRESS_UPSTREAM_PROXY`) and
  `no_proxy` like a package Upstream; a registry whose URL names `localhost`
  or a loopback address stays direct, and a registry's own `proxy` still
  wins. Under `upstream_proxy`, each request goes where Depsilo's own DNS
  answer for its host (the registry, its Bearer realm, every redirect
  target) sends it: a private or loopback answer, an address written in the
  URL included, is dialed directly, and a public or fake-IP answer goes
  through the proxy, so a private registry's redirects to public storage
  keep the proxy. The direct path resolves the host again when it connects
  and refuses an answer that is no longer private or loopback with
  `Docker direct target left the private network`. Through a proxy, a
  registry, Bearer realm or redirect host whose every DNS answer is a
  fake-IP address (`198.18.0.0/15` or `2001:2::/48`,
  from either family or both, in any order) is left to the proxy to resolve,
  so pulls work behind proxy software in TUN/fake-IP mode, also through a
  registry's own `proxy`, where they failed with
  `unsafe Docker proxy target`. Answers that mix fake-IP and other
  addresses, address literals in either range, and a realm or redirect
  target that resolves to a private or loopback address for a public
  registry are still refused. Without a proxy, Depsilo dials such a host
  name's fake-IP addresses itself, as the next entry describes, instead of
  refusing it with `unsafe Docker dial target`. The startup log names each
  registry's route in a `docker registry egress` line (`own proxy`,
  `default proxy (private answers direct)` or `direct`), and a
  `docker registry host dialed directly` line names the registry and the
  host, without its address, the first time Depsilo dials that host
  directly.
- Egress: behind proxy software in TUN/fake-IP mode, Depsilo now connects to
  fake-IP answers as the other programs on the machine do, for the proxy
  software's TUN to carry, wherever no proxy carries the request: package
  Upstreams, extra indexes and cascade parents without a proxy, their checks,
  and Docker registries without a proxy, also those that `no_proxy` or a
  loopback URL keeps off `upstream_proxy`. A host name whose DNS answer
  consists only of addresses in `198.18.0.0/15` or `2001:2::/48` counts as
  public and is dialed at those addresses, IPv4 ones first and a quarter
  second apart, so `upstream_proxy` is needed only where nothing carries
  them. Each address gets at most 5 seconds. When none connects, an Upstream
  check records `egress_fake_ip` with the address dialed first and its
  range, its error reading
  `nothing carried the connection to the fake-IP dial target`, and a Docker
  pull fails with a `502` whose reason reads `<host> resolved to
  <address>, a fake-IP address from proxy software, but nothing carried the
  connection to it; turn on the proxy software's TUN mode, or set [egress]
  upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to its HTTP port and
  restart, see docs/deployment.md#upstream-network-egress`; for a registry
  kept off a configured `upstream_proxy`, the remedy after `TUN mode, or`
  asks for real DNS answers or its removal from `no_proxy` instead. The
  server log adds the dial errors. On these direct routes, a fake-IP answer
  pins the network scope of an Upstream or registry only once a connection
  to it succeeded, so a real DNS answer given after nothing carried it, a
  private one for a host on the local network included, takes effect at the
  next request without a restart. Address literals in either range, answers
  that mix fake-IP and other addresses, and unique-local IPv6 fake-IP stay
  refused. With `[egress] allow_fake_ip` off (see Added), such an answer is
  refused before anything is dialed, as before, and the Docker reason adds
  `, and [egress] allow_fake_ip is off` after `in TUN/fake-IP mode`.
  `egress_fake_ip` therefore means that nothing carried the connection, that
  `allow_fake_ip` is off, or an answer that is always refused: one that
  mixes fake-IP and other addresses, holds a unique-local IPv6 fake-IP
  address, or is an address in either range written in the Upstream URL.
  Admin, "Connect a client", `depsilo doctor` and the package-route `502`
  (`all upstreams are unhealthy: DNS returned fake-IP addresses for every upstream (egress_fake_ip), and Depsilo could not connect to them: nothing carried the connections, [egress] allow_fake_ip is off, or the answers mixed fake-IP and other addresses or held unique-local IPv6 fake-IP addresses, which Depsilo always refuses; ...`)
  name these causes, and as remedies the TUN mode, `upstream_proxy` with its
  container form, `allow_fake_ip` (unless it was turned off on purpose) and
  the IPv6 fake-IP range `2001:2::/48`; the one-line reason on each Admin
  card reads `not connected`. Declaring `198.18.0.0/15` in `private_ranges`
  stays rejected, now with a pointer to `allow_fake_ip`.
- Docker: when `[egress] upstream_proxy` fails a request, the `502` reason
  can end with a hint. For a host answered with fake-IP addresses, the
  registry's own or a realm or redirect host of a registry whose answer is
  private or loopback, it reads `; if <host> is on your network, give it a
  real DNS answer, for example with fake-ip-filter in the proxy software, so
  that Depsilo connects to it directly, see
  docs/deployment.md#upstream-network-egress`, with `after a restart` after
  `directly` for the registry's own host, whose first answer pins its
  network scope until a restart, when the proxy refused
  `CONNECT`, when the connection through the proxy failed, for example
  because the proxy closed the tunnel after accepting it (as mihomo does for
  a host it cannot reach) or because it timed out, or when the proxy
  answered plain `http` with `502`, `503` or `504`; a TLS failure gets no
  hint. For the registry's own host with a public answer, when the proxy
  refused `CONNECT` or answered such a status, it reads `; if <host> is
  reachable only without the proxy, list it in [egress] no_proxy and
  restart, see docs/deployment.md#upstream-network-egress`. A plain-`http`
  `502`, `503`
  or `504` through `upstream_proxy`, the registry's own included, now reads
  `returned <status> through the egress proxy`, and a manifest `HEAD` then
  answers `502` instead of passing the status on; a cached manifest still
  answers it. A proxy that cannot be reached, a registry's own `proxy` and
  the direct path give no hint.
- Docker: a Bearer realm or redirect target written as a private or loopback
  IP address, such as a storage redirect to MinIO at
  `http://10.0.0.30:9000`, is no longer refused for a registry configured by
  host name. The network scope that the registry's own DNS answer pinned
  decides, as for a host name: a registry that resolves privately follows a
  private address, one that resolves to loopback also a loopback address,
  and one that resolves publicly neither; it is refused before anything is
  sent. `localhost`, `.localhost` names and addresses written with a
  trailing dot stay refused, and so do unsafe addresses; registries
  configured by address keep the earlier rule.
- Egress: `2001:2::/48`, the IPv6 benchmarking range (RFC 5180 as corrected
  by RFC Errata 1752), counts as fake-IP like `198.18.0.0/15`; Clash Verge
  Rev in TUN fake-IP mode (since 2.5.5), sing-box's examples and OpenClash
  use it for IPv6 fake-IP. A package Upstream refused for an address in it
  records `egress_fake_ip` instead of `egress_reserved`, whichever address
  family DNS lists first, so Admin, `depsilo doctor` and the `502` give the
  fake-IP remedies; when the answer also holds an address in a unique-local
  range of proxy software, the failure names that address and range, whose
  setting must change, in either order. Admin and "Connect a client" speak
  of fake-IP ranges, naming `198.18.0.0/15` and `2001:2::/48`, instead of the
  benchmarking range.
- Egress: a package Upstream check that fails at its proxy (`upstream_proxy`
  or the Upstream's own, also toward a cascade parent) records the new
  failure code `egress_proxy` when Depsilo cannot connect to the proxy
  (instead of `connection_refused`, `dns`, `tls` or `network`) or when the
  proxy answers `CONNECT` for an `https` Upstream with a status other than
  `200` (instead of `network`). Admin says which of the two happened, with
  that status, and asks to check the proxy and to list an Upstream on your
  own network in `[egress] no_proxy`, or, for an Upstream with its own proxy,
  which `no_proxy` does not bypass, to clear that proxy and, unless it is a
  cascade, to list its host in `no_proxy` too when `upstream_proxy` is set.
  The check's error text gives that status instead of the proxy's status
  text. When every
  Upstream of an ecosystem failed there, the `502` message
  `all upstreams are unhealthy` names `egress_proxy` and that remedy. Other
  failures through a proxy keep their codes: `timeout` when it does not
  answer in time, `connection_refused` or `network` when it closes the
  connection, which is how Clash, mihomo and sing-box fail an `https`
  Upstream they cannot reach because they accept `CONNECT` first, and
  `http_status` for a `5xx` that it answers for a plain-`http` Upstream.
- npm: unsigned tarball paths (`/npm/<package>/-/<file>.tgz` and the scoped
  form) still answer `404`, now with a JSON body, code
  `NPM_UNSIGNED_TARBALL`, and a recovery message; npm `404` and `502` bodies
  repeat the message in `error`. Refused tarball requests appear in the
  access log as 404, without the token.
- `POST /api/v1/setup/complete` returns the request's own origin as
  `reconnect_url` when the request did not name the listening port or
  carried `X-Forwarded-Proto`; otherwise it still uses the chosen port.
- Startup: a reopened setup shows `Setup reopened:` (finishing it replaces
  `config.toml`); a generated administrator password is printed in the
  summary and no longer called one-time (sign in and change it; log fields
  unchanged); `DEPSILO_ADMIN_USERNAME` or `DEPSILO_ADMIN_PASSWORD` set while
  setup is pending draws a warning, as they need a configuration file.
- Bootstrap token: `BOOTSTRAP_TOKEN_REQUIRED` (still `403`) explains the
  `X-Depsilo-Bootstrap-Token` header, `DEPSILO_BOOTSTRAP_TOKEN`, and
  `depsilo setup token`. Messages read `publish bootstrap token: ...` and
  `failed to remove completed bootstrap token`.
- Setup page: names the "bootstrap token" consistently and shows it
  unmasked, gives the `docker exec` form of `depsilo setup token`, warns
  before replacing an existing configuration, starts from the listening
  port, matches the server's password rules, explains errors in the page
  language, and on Windows asks for a manual restart instead of failing.
- Agent texts (`/api/v1/agent-prompt`, `depsilo init-agent`,
  `/api/v1/integration-prompt`, `depsilo prompt`) prefer project-level
  settings and wait for approval before writing user- or system-level
  configuration. The integration guide also has the agent wait for approval
  of its plan before editing any file, so an unattended run stops after the
  plan; it reports stale pnpm, Yarn, Bun, and Python lockfiles instead of
  regenerating them and leaves a published `pyproject.toml` alone. All cover
  npm lockfiles (`omit-lockfile-registry-resolved=true` in `.npmrc`,
  `npm install --package-lock-only --omit-lockfile-registry-resolved` after
  asking, for `registry.npmjs.org` URLs and other registries alike), scoped
  Bundler commands, and pip, NuGet (`--allow-insecure-connections`, .NET
  SDK 9+) and Composer (`secure-http false`) on plain HTTP, and give the
  Maven mirror `<id>depsilo</id>`. The bootstrap prompt points pnpm, Yarn,
  Bun, uv and Poetry projects to their rows in the integration guide. MCP
  `depsilo_configure` adds `setup.note` to its `npm` and `rubygems` results
  (and `nuget` on plain HTTP), and the `rubygems` `setup.shell` is now
  `bundle config set --local ...`. Rerun `depsilo init-agent` to replace
  older written text.
- Quick Start and "Connect a client" flag loopback-only commands and explain
  lockfiles: an npm `package-lock.json` with `registry.npmjs.org` URLs fails
  with `404 NPM_UNSIGNED_TARBALL` and one from another registry silently
  bypasses Depsilo (rewrite either without resolved URLs); Bun lockfiles do
  the same, pnpm and Yarn 2+ ones from another registry fail with 404, and
  Yarn 1 ones bypass Depsilo (regenerate them). Lockfiles made through Depsilo record its
  address; pnpm 11+ needs `trustLockfile: true` in `pnpm-workspace.yaml`.
  Bundler gets `bundle config set --local`/`--global` (2.1+; 1.x silently
  leaves the mirror unset, so its form is given). Yarn 1 has its own entry
  setting `registry`: reconfigure Yarn 1 clients set up with
  `npmRegistryServer`, which bypassed Depsilo. Yarn 2+ lists only the host
  in `unsafeHttpWhitelist` (no YN0081), and Gradle and NuGet accept plain
  HTTP (`--allow-insecure-connections` needs .NET SDK 9+; SDK 8 gets a form
  without it). Quick Start gives `PNPM_CONFIG_REGISTRY` (pnpm 11+) and
  `NPM_CONFIG_REGISTRY` (up to 10), and renames "One-off setup" to "Other
  ways to connect", as some of its commands (`bundle config set`,
  `poetry source add`, `helm repo add`) persist. "Connect a client" explains
  `egress_fake_ip`; it and Admin give the container form of the proxy
  (`http://host.docker.internal:7890`, recreating the container, and
  `--add-host=host.docker.internal:host-gateway` on Linux). Admin warns that
  editing an npm Upstream's name, URL or proxy breaks lockfiles made through
  Depsilo. Generated pip and venv commands for POSIX shells run `python3`,
  and Quick Start's inline pip commands carry `--trusted-host` (or
  `PIP_TRUSTED_HOST`) on a plain-HTTP address, which pip otherwise ignores
  on any host but localhost.
- Every Admin list shows twenty rows a page (the access log showed fifty).
- On a first visit the web interface is Chinese only for a `zh` browser
  language, English otherwise (it was Chinese); the toggle's choice wins.
- Blocklist sync logs one warning per sync with skipped advisories counted
  by ecosystem; set `server.log_level` to `debug` to list them.

### Removed
- The single `[auth.oidc]` sign-in source, with its `DEPSILO_AUTH_OIDC_*`
  environment variables. A configuration file that still has `[auth.oidc]`
  no longer loads; configure a named Authentication Source under
  `[auth.sources.<id>]` with `protocol = "oidc"` (variables
  `DEPSILO_AUTH_SOURCES_<ID>_*`). `oidc` is now an ordinary source ID.
  - The unprefixed callback `/api/v1/auth/oidc/callback` is gone; register
    `/api/v1/auth/oidc/<id>/callback` with the identity provider.
  - `GET` and `PUT /api/v1/admin/authentication` are gone; manage sources
    through `/api/v1/admin/authentication/sources`.
  - Sign-in and linking requests must name `provider_id`, even when only one
    source is enabled.
- Compatibility aliases in the HTTP and MCP interfaces (the interfaces are
  not stable before 1.0). Read the current fields instead:
  - License status: `key_masked` and `activated_at` are gone; read
    `license_key_masked`.
  - Admin overview rate (`/api/v1/now`): `egress_bps` and `ingress_bps` are
    gone; read `service_bytes_per_sec` and `origin_bytes_per_sec`.
  - Policy status, readiness, and rule-test responses: `last_successful_refresh`
    is gone; read `snapshot_loaded_at`.
  - Compiler-cache status and new-credential responses: `endpoint`,
    `remote_storage`, and `warning` are gone; read `endpoints.ccache`,
    `endpoints.sccache`, and `ccache_remote_storage`.
  - Login identities (`GET /api/v1/auth/identities`): the single `provider`
    object is gone; read the `providers` list.
  - The MCP tool `depsilo_request` is gone; call `depsilo_recent` with
    `request_id`.
  - Admin audit logs no longer accept `search`, and security
    vulnerabilities no longer accept `q`; filter with `package`.
- The `cmd/server` entry point and the `build-server`, `proxy-env`, and
  `verify-installer` Make targets. `depsilo` serves when run without
  arguments; use `make build`, `make cli-activate`, and `make verify-scripts`.
- Admin webhooks no longer offer "High Disk Usage" (`disk_high`) or
  "Critical Vulnerability" (`vuln_critical`): Depsilo never sent either
  event. A webhook saved with them keeps the values, which match nothing.

### Fixed
- Admin's audit integrity notice no longer reports the remote anchor as
  synced when it has never succeeded. `GET /api/v1/admin/audit/integrity`
  now sends `null`, not `0001-01-01T00:00:00Z`, for a sink or checkpoint
  that has never succeeded (`latest_checked_at`, `file_last_success_at`,
  `remote_last_success_at`).
- A login identity that has never signed in now has `last_login_at: null`
  in `GET /api/v1/auth/identities`, and an unmetered export allowance has
  `resets_at: null` in `GET /api/v1/admin/distribution/usage`, instead of
  `0001-01-01T00:00:00Z`.
- The webhook sent when a Pro license or trial expires no longer says that
  audit logs, SBOM, security scanning, and rules are locked. The free
  edition keeps every feature; the message now names its limits (one
  project and the monthly transfer bundle export allowance).
- `depsilo audit verify` runs instead of `Unknown command: audit`, accepts
  a DSN with `?` options, and without a configuration file or database says
  that no instance was found and where it looked.
- The CLI trims whitespace around the token in `~/.config/depsilo/token`
  and `DEPSILO_TOKEN`.
- Run as root (`sudo`, or UID 0 in a container), `depsilo restore` gives
  what it creates or replaces (`config.toml`, the database,
  `.pre-restore-<id>.bak`, new directories) the receiving directory's owner
  and group, so the next start no longer fails with `Permission denied`.
  `depsilo reset-password` (printing a note) and a server start do the same
  for a new `.depsilo-locks`, lock file, or database directory. Existing
  entries keep their owner; if an owner cannot be changed, the command
  removes what it staged and fails. `depsilo backup --out` does the same
  inside the state directory; an archive elsewhere stays owned by root.
- `depsilo backup` creates a missing `--out` directory (mode 0700) and names
  an unwritable output directory, asking for `--out`.
- Completing a reopened setup keeps the old `config.toml` as
  `config.toml.pre-setup-<UTC timestamp>.bak` (mode 0600, secrets included,
  never deleted), or answers `WRITE_FAILED` and leaves it untouched.
- Initial setup retries for up to 45 seconds while the database is busy
  instead of failing with `ADMIN_CREATE_FAILED`, and a start with only
  `DEPSILO_ADMIN_USERNAME` set no longer fails now and then with
  `password must not contain the username`. In a container the startup
  summary and `BOOTSTRAP_TOKEN_REQUIRED` give
  `docker exec <container> depsilo setup token`.
- Creating an audit exporter with `"enabled": false` creates it disabled;
  it was stored enabled and received the audit stream at once.
- Addresses built from a request (package metadata, `proxy_url`,
  `/api/v1/discover`, the prompts) use only `http` or `https`: `https` on
  TLS, else a valid first `X-Forwarded-Proto` value in any case, else `http`.
- `GET /api/v1/events/stream` works behind nginx defaults: immediate
  headers, `X-Accel-Buffering: no`, and a keep-alive comment every 25 s.
- `--host ::1` and `DEPSILO_SERVER_HOST=::1` listen on IPv6.
- SQLite DSNs with paths in options no longer create stray directories, and
  a `file:` DSN creates its missing directory.
- Healthy starts log no "record not found" or lookup SQL traces and no ANSI
  colours; failed statements and those over 200 ms are still logged.
- MCP `tools/list` no longer returns `"properties": null`, which validating
  clients rejected.
- `/api/v1/stats` (Portal, `depsilo status`, `depsilo doctor`, MCP
  `depsilo://stats`) counts the current UTC day in "today" and "week" on
  non-UTC hosts, and MCP `depsilo_status` `last_24h` no longer adds the
  previous UTC day.
- `agent_setup.readme_anchor` in `/api/v1/discover` points at an existing
  section of `docs/deployment.md`.
- An invalid `permissions` for an API token or compiler-cache credential
  answers `permissions must be "readonly" or "readwrite"` (still `400`).
- The License page and `depsilo distribution --help` state the free export
  allowance as 500 MiB per bundle and 5 GiB per month, as enforced.
- Linux tray: `install-linux.sh` installs from the extracted release archive
  in any directory, names missing files before writing anything, prints the
  matching autostart command, and runs its other subcommands when read from
  stdin. The archive has no `./` entry and includes the icon
  `depsilo-tray.svg` (a 256x256 PNG with `rsvg-convert`, ImageMagick
  `convert`, or Inkscape).
- Docker: a manifest `HEAD` answered with `502`, which has no body, and a
  `502` from tag resolution for the observation-age gate now log their
  reason in the server log, as a `GET` does, so the fake-IP remedy reaches
  the operator when containerd resolves a tag.

### Security
- Failed and slow SQL statements are logged with placeholders instead of
  their values, so password hashes and token digests stay out of the server
  log, for example during setup's retries on a busy database.
- `GET /api/v1/integration-prompt?url=` accepts only a bare `http` or `https`
  origin (host, optional port, at most a trailing slash), so a crafted link
  cannot inject text into the prompt; credentials, a path, query, fragment,
  whitespace, or control characters answer `400 INVALID_URL`, and
  `depsilo prompt --url` exits 1.
- While setup is pending, startup rejects a preset `DEPSILO_BOOTSTRAP_TOKEN`
  with fewer than 8 distinct characters; use 32 or more random bytes. A
  set-up instance does not read the variable.
- Refused npm tarball requests are audited without a guessed version.
- The setup page warns when opened over plain HTTP from another machine.
- A Docker `502` no longer names the host and port of a registry's proxy, or
  of the `[egress]` proxy, that cannot be reached; its message says
  `connection to the egress proxy failed` instead, and the server log keeps
  the address.
- A Docker `502` message no longer repeats text from the registry, the proxy
  or the network. It used to quote the URL that failed, including the signed
  query of a redirect to a CDN or object storage, up to 64 KiB of the
  registry's answer, and Go's transport error, which can quote a server's
  malformed answer. It now reads
  `<stage> failed at <origin>: <reason> (request <id>; details in the Depsilo server log)`:
  the origin is the scheme, host and port of the registry, Bearer realm or
  redirect target that failed, and the reason is the answer's status with at
  most three OCI distribution error codes (`returned 429 (TOOMANYREQUESTS)`),
  a failure code such as `timeout` or `connection_refused`,
  `egress proxy refused CONNECT (403)`, or one of Depsilo's own refusals, the
  fake-IP remedy included; anything else reads
  `Docker registry request failed`. The server log keeps the full error, and
  its Docker error lines carry the `request_id`. In the URL that failed, and
  in every absolute `http` or `https` URL and every double-quoted URL
  reference in Go's error text or the registry's answer, userinfo is dropped
  and each query value reads `REDACTED`. A manifest `HEAD` now appears in the
  access log whatever the registry answers, except that with the
  observation-age gate armed a registry `4xx` to tag resolution is still
  passed on with only an audit entry, and one that fails with a `5xx`,
  whether Depsilo answers `502` or passes on the registry's own `5xx`, also
  gets an error line in the server log.
- A Docker manifest `HEAD` whose path cannot form the registry URL still
  answers `500` with code `REQUEST_ERROR`, but its message now reads
  `invalid Docker request path` instead of Go's parse error, which quoted
  the configured registry URL with any userinfo. The server log keeps the
  error without userinfo.
- DNS answers in the unique-local ranges that proxy software documents for
  IPv6 fake-IP and for its own TUN, VIF and DNS addresses
  (`fdfe:dcba:9876::/48`, `fd00:6152::/32`, `fc00::/18`) no longer count as
  a private network. Such an answer pinned a public Docker registry, with or
  without a proxy, or a direct package Upstream or cascade parent as a
  private origin, whose Bearer realm or redirect could then reach private
  addresses. These addresses are now refused everywhere, and an Upstream
  check records `egress_fake_ip`. A Docker registry, Bearer realm or redirect
  host name whose answer consists of fake-IP and unique-local addresses only,
  with at least one unique-local address and either a fake-IP address or an
  address in these ranges, gets a `502` reason before anything is dialed or
  sent to the proxy: it names the host and the documented range, or says
  `a unique-local address` and names no address or prefix, and asks to set
  the proxy software's IPv6 fake-IP range (`fake-ip-range6` or
  `inet6_range`) to `2001:2::/48` or turn IPv6 fake-IP off. The server log
  keeps the whole answer. A unique-local answer outside these ranges and
  without fake-IP addresses remains a private network.
- Connecting to fake-IP answers (`[egress] allow_fake_ip`, on by default)
  delegates those host names to the proxy software whose TUN carries the
  connection, which resolves them itself and connects where its own rules
  send it: the same delegation that `upstream_proxy` expresses, now without
  configuration. Depsilo checks such an answer as public, but that covers
  only the local answer: a registry or Upstream with such an answer reaches
  no target whose local answer is private or loopback, while a realm,
  redirect or Upstream host name with a fake-IP answer, a hostile one
  included, reaches whatever the proxy software resolves it to, loopback and
  private addresses included. Restrict those destinations in the proxy
  software's DNS policy and rules, as for `upstream_proxy`, or set
  `allow_fake_ip = false`. A fake-IP answer that holds an address of the
  machine's own network interfaces, such as the TUN address of the proxy
  software (`198.18.0.1` for mihomo), could reach a service on the machine
  and is refused, also before a proxy carries a Docker request: an Upstream
  check records `egress_reserved`, a Docker `502` reads
  `unsafe Docker dial target` or `unsafe Docker proxy target`, and an answer
  that Depsilo cannot check because the interfaces cannot be listed is
  refused too. The TUN address of another host whose TUN carries Depsilo's
  connections, such as the Docker host of a Depsilo container or a gateway
  that runs the proxy software, is no address of Depsilo's machine and is
  not refused, so a name that answers with it reaches that host's services.
  On a network that really uses `198.18.0.0/15` or `2001:2::/48`, Depsilo
  now dials any host name that resolves there, including a hostile realm or
  redirect name; `allow_fake_ip = false` restores the refusal.

### Upgrade notes
- No schema migration; the database stays at schema v17.
- MCP clients must accept `202 Accepted` with no body for notifications;
  `GET`/`DELETE /mcp` answer `405` (`401` without credentials), not `404`.
- `depsilo doctor` exits non-zero and `depsilo init-agent` exits 1 while
  setup is pending, init-agent also when a target file cannot be updated
  (repair the named file and rerun). `depsilo warmup` exits 0 for an
  accepted job; a script that retried on its exit 1 no longer needs to.
- With setup pending, a `DEPSILO_BOOTSTRAP_TOKEN` of fewer than 8 distinct
  characters stops startup; use `openssl rand -base64 32` or unset it.
- The `DEPSILO_EGRESS_*` variables now take effect: before upgrading, check
  the service environment for leftovers, which override `[egress]`, replace
  file lists whole, and stop startup when invalid.
- With `[egress] upstream_proxy` set, Docker registries without their own
  `proxy` now send each request whose host has a public or fake-IP answer
  through it. Like package Upstreams, those requests then rely on the
  proxy's DNS and egress rules instead of Depsilo's connect-time address
  check, and a plain-HTTP registry's credentials pass through the proxy. A
  request whose host resolves to a private or loopback address, or is
  written as one, is dialed directly with the connect-time check, so a
  private registry whose name resolves to a private address needs no
  `no_proxy` entry, and the proxy's access rules no longer see its private
  targets. Set a registry's own `proxy` where only a proxy reaches it or
  where all of its traffic must pass a proxy. Behind fake-IP DNS, give a
  private registry's names real answers, for example with a `fake-ip-filter`
  entry in the proxy software; otherwise its requests go through the proxy
  like public ones. List a registry that must stay entirely direct in
  `no_proxy` (or `DEPSILO_EGRESS_NO_PROXY`) by the host name in its URL: IP
  and CIDR entries match only a URL written with an address. A registry kept
  direct keeps the connect-time check and, behind fake-IP DNS, dials its
  fake-IP answers for the proxy software's TUN to carry, or needs a real DNS
  answer for its name where nothing carries them. Registry URLs that name
  `localhost` or a loopback address stay direct. Through `upstream_proxy`, a
  plain-`http` registry's `502`, `503` or `504` to a manifest `HEAD` is
  answered with `502`.
- Behind proxy software in TUN/fake-IP mode, Depsilo now connects to fake-IP
  answers itself wherever no proxy carries the request. With the TUN mode
  on, an `upstream_proxy` set only because of fake-IP DNS is no longer
  needed to connect, but without it the proxy software's DNS policy and
  rules, not the proxy's access rules, decide where fake-IP names connect,
  loopback and private addresses included: keep `upstream_proxy`, or reject
  loopback, link-local, metadata and unintended private destinations in the
  proxy software, where that boundary matters. With the TUN mode off, a
  request waits about 5 seconds for its fake-IP addresses before it fails
  with `egress_fake_ip` or a Docker `502`. Set
  `[egress] allow_fake_ip = false` (or `DEPSILO_EGRESS_ALLOW_FAKE_IP=false`)
  where `198.18.0.0/15` or `2001:2::/48` are real networks that host names
  should not reach without a proxy.
- A package Upstream, cascade parent or Docker registry that resolves into
  `fdfe:dcba:9876::/48`, `fd00:6152::/32` or `fc00::/18`, for example on a
  kind network whose IPv6 /64 falls in `fc00::/18`, is now refused; give it
  an address outside these ranges. Behind proxy software that hands out IPv6
  fake-IP addresses from such a range, Docker pulls that resolved only to
  them now fail with a `502`, also through a proxy, until the proxy
  software's IPv6 fake-IP range is `2001:2::/48` or IPv6 fake-IP is off.
- `GET /api/v1/integration-prompt` and `depsilo prompt --url` reject a `url`
  that is not a bare origin (`400 INVALID_URL`, exit 1); pass
  `?url=http://depsilo.example.com:23333`.
- Unknown `/api` routes return a JSON body (read the status or `code`); the
  audit log endpoints reject a malformed `start` or `end` with `400`;
  snapshot import answers an invalid manifest with `400` instead of `500`.
- Output: `serve`, `start`, and `setup` `--help` go to stdout; failed
  `depsilo serve` and `depsilo start --daemon` print `Error: <message>`, not
  JSON or `Error loading daemon configuration:`; log rules matching
  `initial setup token` or `completed setup token` must use `bootstrap token`,
  and those matching `all upstreams are unhealthy` only its beginning.
- Scripted setup: a client that reaches port 80 directly and chooses
  another port gets its old address as `reconnect_url` and must build the
  new one. Poll `GET /api/v1/setup/status` until `200` with `needs_setup`
  false and no `restart_pending`.

## [0.14.0] - 2026-11-15

### Added
- `GET /api/v1/admin/dashboard/breakdown?range=1h|24h|7d|30d|all` reports where an
  Overview period's traffic went: per ecosystem, per Upstream (the measured
  bytes and exchanges it sent to Depsilo, plus the misses served through it),
  and the ten largest packages. It uses the same boundaries and request set as
  the period block of `/admin/dashboard`, so its ecosystem rows and per-Upstream
  origin bytes add up to that block. Upstreams are keyed by ecosystem and name,
  so same-named Upstreams of different ecosystems stay apart. Ranges beyond a day rank packages by whole
  UTC days and say so in `top_packages_grain`.
- The Admin navigation rail can collapse to icons; the choice is remembered in
  the browser.
- The Overview has an All range covering everything Depsilo still knows since
  installation (schema v17). Rollup retention now folds an expiring day into
  undated archive tables (`access_log_archive`, `access_log_package_archive`)
  in the same transaction that deletes it, so all-time totals are exact past
  the rollup horizon; days deleted before this version are not recovered.
  `range=all` on the dashboard, trends, and breakdown endpoints returns those
  totals plus the retained rollups, names where coverage begins in
  `range.start`, and has no previous period. The startup rollup backfill no
  longer rebuilds days that are already archived.
- `GET /api/v1/integration-prompt?ecosystem=<id>` limits the AI integration
  prompt to one Portal ecosystem (`python`, `node`, `java`, `rust`, `go`,
  `container`, `kubernetes`, `ruby`, `dotnet`, `debian`, `alpine`, `php`, `r`,
  `huggingface`). The prompt then opens with a scope section naming the
  ecosystem, its clients, and this service's address for it, and tells the
  agent to leave every other ecosystem alone; the safety sections are
  unchanged. An unknown id is a `400` that lists the accepted ids.
  `depsilo prompt --ecosystem <id>` prints the same text.

### Upgrade notes
- Schema v17 is a forward-only migration. A 0.13.x binary refuses the
  upgraded database; to downgrade, restore the backup taken before upgrading.

### Changed
- The Admin Overview is a dense one-screen cockpit. Eight tiles cover what the
  cache served, origin traffic, client downloads, hit rate, hit and origin
  latency, the estimated wait avoided, errors, and cache usage, each with a
  change against the previous period where both periods are comparable. Cards
  show request activity as stacked columns with the recent bursts listed below,
  the live request tail, the largest packages, every Upstream's health by
  ecosystem with where misses actually went, per-ecosystem traffic, cache
  contents and free disk, what needs attention with its consequence for
  downloads, known vulnerabilities and protection coverage, and the process's
  resources. The page has a full-screen mode that hides the navigation rail.
- The Portal Quick Start leads with AI setup. A one-row status band above it
  shows whether the service is serving, today's requests and hit rate, egress,
  seven-day savings, cache size, failing Upstream mirrors (each opens its
  ecosystem's setup), and the last hour. "AI setup" is the catalog's first
  entry and the default on a first visit, and every ecosystem's package
  manager picker starts with an AI setup tab whose prompt covers only that
  ecosystem. The compile cache is a catalog entry; the separate optional
  section, the recently-used list, and the prompt dialog are gone.
- Admin and Portal design pass. Status colours mean status only: a stale
  index is a neutral badge, and the cache storage card shows one usage meter
  and a single-hue share list instead of ecosystem brand colours that read as
  health. Vulnerability severity uses one red, orange, and amber ramp that
  also steps in lightness, so high and medium stay apart for colour-blind
  readers. Tables share one sentence-case header style, ecosystems appear by
  product name (PyPI, npm, Cargo) instead of uppercase adapter ids, type uses
  three weights and nothing below 12 px except units and chart ticks, Upstream
  probe bars fill their row, phone filters put search and its button on one
  row with filters two to a row, and pages without tabs use the same header
  band as tabbed workspaces.

### Fixed
- The Portal header now shows a failed first status poll and its retry
  control even when the Quick Start page loads after the failure; previously
  the late page retried silently and hid the error.

## [0.13.0] - 2026-11-11

### Added
- Free-edition export allowance for signed transfer bundles
  ([ADR-0009](docs/adr/0009-usage-limited-free-edition.md), schema v16):
  without Pro or an active trial, each bundle may be at most 500 MiB and the
  bundles exported in one UTC month at most 5 GiB. A bundle beyond either limit
  is refused with `402 EXPORT_BUNDLE_LIMIT` or `402 EXPORT_MONTHLY_LIMIT` before
  any byte streams. Only completely streamed bundles count, exports under Pro
  or a trial are not metered, and imports are never limited.
  `GET /api/v1/admin/distribution/usage` and `depsilo distribution usage`
  report the current allowance.

### Changed
- Project SBOM export, including CRA mode, the technical-file preset, and
  signing, is part of the free edition for its one project. The Admin Projects
  page no longer shows a Pro badge or paywall for it, and the License page lists
  the free edition's export allowance.

### Fixed
- A PyPI artifact missing on the selected Upstream is fetched from the other
  Upstreams of the ecosystem before a 404 reaches the client. A mirror such as
  TUNA publishes no PEP 658 core metadata and lags new releases, so after a
  failover the package index cached from pypi.org could advertise files the
  mirror then answered with 404, failing pip and uv installs. A 404 does not
  mark an Upstream unhealthy, and signed external artifact references stay
  bound to their source.
- A request the selected Upstream cannot serve, because the connection fails
  or it answers 5xx or 429, moves on to the other Upstreams of the ecosystem
  instead of failing until health checks notice the outage. This covers PyPI
  artifacts and the APT, Alpine, Conda, CRAN, Go, Helm, Maven and RubyGems
  proxies. APT, Alpine, Conda and Go packages that a mirror answers with 404
  are fetched from another Upstream too, since their clients verify packages
  against signed or checksum-bound metadata; index files never move on a 404.
  Those proxies now answer 404 rather than 502 when every Upstream reports a
  file missing.
- An Upstream whose requests fail slowly twice in a row, each waiting at least
  10 seconds as on a link that drops packets, leaves selection. An active
  Upstream is re-probed after 30 seconds and then at doubling delays of up to 5
  minutes; a passive one receives one half-open request per minute, even while
  a lower-priority Upstream serves. Previously only the next regular probe, 30
  minutes later by default, or a collapse of the lifetime success rate removed
  it, and a passive Upstream had no such probe, so every request kept waiting
  for the connection timeout.
- An expired package index whose refresh is still waiting after 10 seconds is
  served from cache while the refresh continues, instead of holding the
  request for up to the 60-second response timeout. pip gives up after 15
  seconds by default, so it no longer times out and retries while an Upstream
  is not answering. Prefetch and operator-forced refreshes still wait for the
  refresh itself.
- While the known-malicious dataset covers PyPI, a legacy archive (such as a
  `.zip` sdist) proves its identity from the cached project index. After a
  restart without network such archives were refused with `451` even when
  cached, because identity checks always asked the upstream. A candidate
  project whose index does not exist (404) now counts as not declaring the
  archive instead of failing the whole identity check.
- A Docker manifest `HEAD` is answered from the cached manifest when the
  registry or its token service cannot answer. Clients that resolve tags with
  `HEAD`, such as containerd, received `502` even though the manifest and its
  layers were cached, so cached images could not be pulled offline. With the
  observation-age gate enabled, tags still require the registry.
- Recording dependency usage retries when another write commits between its
  read and its write. SQLite rejected such a write at once with
  `database is locked`, which could also make a clean shutdown report an error.

### Security
- Release builds use Go 1.27.4, which fixes
  [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859): a header race in
  `net/http`'s `MaxBytesReader` could crash an HTTP/1 server.

### Upgrade notes
- Schema migration v16 runs automatically on first start and is forward-only:
  an older binary cannot open the upgraded database. Take a backup first
  (`depsilo backup`), and keep `data/secrets/jwt.key` with the database if the
  instance generated one.
- Without Pro or an active trial, transfer-bundle exports are now metered: at
  most 500 MiB per bundle and 5 GiB per UTC month. Exports made before the
  upgrade were not recorded and do not count.
- The APT, Alpine, Conda, CRAN, Go, Helm, Maven and RubyGems proxies answer a
  file that every Upstream reports missing with `404` instead of `502`.

## [0.12.2] - 2026-11-10

The `v0.12.0` and `v0.12.1` tags were not published. Release qualification of
`v0.12.0` failed on a Docker mirror check that read request counters before the
batched access log flushed them; the corrected check verifies that two clean
Docker clients pull through Depsilo and that the second is served from its
cache. Qualification of `v0.12.1` then caught the PyPI core-metadata defect
fixed below. 0.12.2 contains the same changes and both corrections.

### Added
- `[egress]` declares upstream network egress (restart-managed):
  `upstream_proxy` is the default HTTP(S) proxy for package Upstreams and extra
  indexes without their own proxy or cascade parent, `no_proxy` keeps listed
  Upstream hosts direct, and `private_ranges` declares parts of
  `100.64.0.0/10` (Tailscale, Headscale, NetBird) private networks so such
  Upstreams and cascade parents are reachable. Declared ranges are treated as
  private, so public Upstreams still cannot redirect into them;
  `100.100.100.200` stays blocked, and `198.18.0.0/15` is rejected in favor of
  `upstream_proxy`. Docker registries keep their own proxy and DNS checks.
- Dependency-set preparation catalog (opt-in `[distribution]`, schema v13):
  authenticated CLI/API staging of PyPI wheels and metadata for a declared
  Linux/amd64 Python target into retained storage that is separate from the
  package and compiler caches, with independent capacity accounting and
  streaming length/SHA-256 verification (`depsilo distribution stage`, `list`,
  `inspect`, `verify`, `delete`, `prune`). Sets are not served to package
  managers and have no Admin UI yet. See [docs/distribution.md](docs/distribution.md).
- Signed full transfer bundles (schema v14): `depsilo distribution export`,
  `import` and `keygen`, backed by `GET /api/v1/admin/distribution/sets/:id/bundle`
  and `POST /api/v1/admin/distribution/import`. A bundle is a streaming USTAR
  archive whose manifest carries a detached Ed25519 signature from a dedicated
  transfer key (`distribution.transfer_signing_key_file` on the exporter,
  `distribution.trusted_transfer_keys` on the importer). Import authenticates
  the signer before writing, streams and verifies every object, never activates
  or serves a set, and records `preparer_attested` provenance only after the
  whole bundle verified; repeating an interrupted import reuses verified
  content.
- Multiple named Admin authentication sources (schema v12, ADR-0007): OIDC
  sources with distinct issuers and LDAP over LDAPS or mandatory StartTLS, each
  with isolated connection and sign-in tests, explicit account linking, and
  bounded external sessions. Casdoor and OpenLDAP integration fixtures cover
  the flows.
- The free edition includes one project, with its connection URL, package
  inventory, and project token. Creating more projects and per-project SBOM
  export still require Pro or the trial; existing projects keep working after
  entitlement expires.
- Cascade traffic attribution: direct, cascade-peer and redirect origin traffic
  are metered separately (`depsilo_upstream_response_bytes_total`,
  `depsilo_upstream_exchanges_total`, and their `redirect` counterparts), and
  `scripts/benchmark-cascade.py` measures isolated native-client cache reuse
  across a parent and children.
- Interrupted PyPI and PyTorch artifact downloads resume with bounded
  same-source range retries on strong validators; Admin → Cache shows download
  progress.
- Admin can initialize the Docker Hub mirror, showing saved versus running
  state and client connection guidance.
- Lifetime dependency-usage evidence (schema v11) records completed response
  delivery independently from the lossy request log.
- Failed Upstream checks are explained. The Upstream list and check results
  carry a bounded `last_failure` / `failure` (`egress_fake_ip`,
  `egress_shared_address`, `egress_reserved`, `egress_private`, `dns`,
  `timeout`, `tls`, `connection_refused`, `http_status`, `network`) with the
  refused address and range, visible to every Admin role; Admin shows the
  observed cause with the configuration that resolves it, and whether an
  Upstream uses the default egress proxy (`proxy_source`).
- Cascade cache peering (ADR-0006): a Depsilo node can egress selected
  upstreams through another authenticated Depsilo node
  (`[cascade]` + `[[cascade.peers]]` + per-upstream `via`, schema v10).
  The parent fetches the raw upstream exchange, caches it under the `relay`
  adapter type with the child's own TTL and metadata/artifact class, and
  streams it back, so every protocol keeps its normal rewriting, provenance,
  and policy behavior. Chains carry instance IDs and a hop budget, reject
  loops with `508`, keep private targets behind the parent's configured
  upstream origins, and strip credentials unless a peer opts in. Multi-level
  cache hits arrive at LAN speed; Docker, shared control plane, HA, and
  multi-peer bandwidth aggregation remain out of scope. A read-only
  Admin → Upstreams → Cascade page shows the node role, instance ID, parent
  peers, egress bindings, and dangling peer references, and can now edit the
  `[cascade]` section and peer list in place. The editor writes `config.toml`
  atomically, keeps secrets write-only, validates the full document (including
  upstream `via` references) before saving, requires a restart to apply, and
  disables itself with an inline explanation when the account lacks the write
  role or the file is not writable. The same page generates copy-ready client
  commands (npm/pip/Go), a child-Depsilo peer snippet with a relay probe, and
  an upstream-parent snippet with per-peer health checks; the address field is
  a local-only generator and secrets stay placeholders. The page is organized
  as a live topology diagram, an inline configuration form, a tabbed
  quick-connect card, and the upstream bindings table.

### Changed
- An instance without an explicit JWT secret generates a 256-bit instance key
  at `data/secrets/jwt.key` (mode 0600) and reuses it. The key also signs npm
  and extra PyPI artifact URLs, so keep it with the database; CLI backups
  include the effective key. `depsilo setup token` prints the current first-run
  bootstrap token, which is removed after setup.
- Admin Overview leads with cache benefits, shows activity as overlapping area
  charts with explicit loading, empty and sample states, and verifies a new
  client connection end to end (request detected, artifact delivered, cache
  hit).
- Admin management pages state loading, failure, permission and stale-data
  conditions explicitly, with consistent retry and recovery flows across
  workspaces. Shared controls are consolidated on shadcn `base-nova` and Base
  UI without runtime dependency changes.
- Admin → Cache folds finished download tasks when the page opens; an active or
  failed download opens the list, and finished entries no longer repeat a full
  progress bar.
- Docker image builds accept a `GOPROXY` build argument for restricted networks.

### Fixed
- pip and uv installs failed with `451 QUARANTINED` when a PyPI Upstream
  published PEP 658 core metadata (pypi.org does) while the known-malicious
  dataset covered PyPI, which is the default since 0.11.0: the `.metadata`
  file carried no recognizable package identity. It now shares the identity
  and policy decision of the artifact it describes, so a blocked release is
  refused through its metadata as well.
- Vulnerability severity is scored from OSV's CVSS v3 vectors. The parser read
  the vector as a number, so every OSV-sourced advisory was stored as
  `unknown` with score 0: the critical count stayed 0 and severity suggestions
  stayed empty. Advisories without a v3 vector use GitHub's qualitative rating
  when present, without a numeric score.
- Periodic vulnerability scans no longer replace stored advisory details with
  bare IDs. OSV's batch endpoint lists only IDs and modification times; a
  package's complete records are now fetched whenever its listing names a new,
  changed, or previously misparsed advisory, and a failed fetch keeps the
  stored records. Schema v15 records each advisory's parser revision and makes
  every package due for a fresh scan after upgrading.
- A failed Upstream no longer shows the duration of its failed attempts as its
  average response time in Admin or the Portal monitor.
- The Admin sidebar wraps a development build label such as `dev·1eb9578`
  only after its separator, so the commit ID is never split, and the Portal
  shows the same build label as Admin instead of `v0.11.0+dev`.
- Audit chain appends retry transient SQLite busy and stale-snapshot conflicts.
- Upstream probe intervals are normalized, status refreshes stay synchronized,
  and a binding to a removed cascade peer can be cleared.
- The installer resolves the newest release through the
  `github.com/depsilo/depsilo/releases/latest` redirect instead of the
  unauthenticated GitHub REST API, which allows only 60 requests per hour per
  IP. A shared NAT or office network that exhausted that quota made
  `curl -fsSL https://depsilo.com/install.sh | bash` fail with "Could not
  fetch the latest release metadata"; the API remains the fallback for
  downloaders or networks where the redirect is unavailable.

### Security
- Frontend dependencies flagged by npm audit are updated: follow-redirects
  1.16.3 (GHSA-mrwp-f3q8-3g9p, through axios) and, in the build toolchain,
  zod 4.6.7 (GHSA-mrhx-c7xc-m3w5) and minimatch 11.0.5 for the typescript-eslint
  parser (GHSA-g3hv-75wg-m249).

### Upgrade notes
- Schema migrations v11 to v15 run automatically on first start and are
  forward-only: an older binary cannot open the upgraded database. Take a
  backup first (`depsilo backup`), and keep `data/secrets/jwt.key` with the
  database if the instance generated one.
- After upgrading, every vulnerability check is due. The next scan fetches
  complete OSV records again, at most one package request per second, so
  severity counts and suggestions fill in gradually.
- `[egress]` and `[distribution]` are optional. Without `[egress]`, Upstreams
  keep direct egress and the built-in address rules; dependency-set
  distribution stays disabled until `[distribution]` enables it.

## [0.11.0] - 2026-10-06

### Added
- Docker first-observation age gate: registries expose no portable publish
  time, so an operator can now acknowledge `supply_chain.observation_sources =
  ["docker"]` and run a positive Docker minimum-release-age threshold. An armed
  gate resolves tags to a digest with a registry-token-aware HEAD, records the
  first time this instance saw that digest (schema v9,
  `docker_image_observations`), and fetches the manifest pinned to the digest
  so a tag move cannot serve bytes that were not gated. Unseen digests stay
  quarantined until the threshold passes, a registry that omits
  `Docker-Content-Digest` fails closed, and the capability summary reports the
  age as `observed` — first sighting by this instance, explicitly not a
  publisher release time.
- Audit checkpoints can be pushed directly to a remote anchor
  (`[audit] checkpoint_url` + optional bearer token) instead of relying on an
  operator to ship the local file: the same NDJSON line is POSTed whenever the
  head changes, with per-sink deduplication, a 60-second retry after failures,
  and delivery status (`remote_head_id`, `remote_last_success_at`,
  `remote_last_error`) in the integrity API and the audit page. A stalled
  remote feed is reported as a warning; a checkpoint that contradicts the
  stored row remains a hard failure with its id.
- Audit chain anchoring: an optional `[audit] checkpoint_file` receives
  append-only chain-head checkpoints (NDJSON, fsynced, one line per changed
  head). `depsilo audit verify`, the integrity API, and the audit page
  cross-check every checkpointed `(head_id, head_hash)` pair against the
  stored row, so a rewritten or truncated chain — internally consistent and
  therefore invisible to the chain walk alone — is reported with its row id.
- The audit log is now tamper-evident: every row written since schema v8
  carries `prev_hash`/`hash` (versioned SHA-256 payload, UTC-normalized
  timestamps, unique `prev_hash` so concurrent writers cannot fork the chain).
  The audit page shows the verification result with a re-verify button,
  `GET /api/v1/admin/audit/integrity` returns the full report, and
  `depsilo audit verify [--json]` recomputes the chain offline against the
  SQLite file. Rows written before the chain are reported as a pre-chain
  prefix instead of being presented as covered; editing, deleting, or
  re-ordering a chained row names the first broken id.
- Published the threat model ([docs/threat-model.md](docs/threat-model.md)):
  deployment assumptions, the asset inventory, in-scope adversaries, trust
  boundaries (including the deliberately unauthenticated proxy surface and
  the shared-cache/no-tenant-isolation boundary), threat-by-threat controls
  with residuals, the accepted-risk list, and a verification map that ties
  each control to its tests.
- SIEM audit routing: the durable audit stream can be forwarded to external
  collectors as NDJSON or Splunk HEC envelopes. Each exporter keeps a cursor
  over `audit_logs`, so delivery is at-least-once, a collector outage stops the
  cursor instead of dropping records, restarts resume from the last successful
  batch, and failures back off from 10s to 5min. Event filters select actions
  and outcomes (`blocked`, `miss`, …); new exporters start at the current audit
  head. Admin API and an audit-page section cover CRUD, enable/disable, lag and
  last-error status, and a test delivery through the real path.
- Published the compatibility policy ([docs/compatibility.md](docs/compatibility.md)):
  patch releases stay compatible; before `v1.0.0` a breaking API/config change
  needs a full minor release or 90 days of deprecation (whichever is longer),
  and from `v1.0.0` the window is two minor releases or 180 days. It documents
  the `/api/v1` additive rule, `config_version`/unknown-key behavior, one-way
  schema migrations with backup-as-rollback, and the `/vN` document format
  identifiers, with contract tests enforcing the checkable parts.
- Freeze / golden snapshots: promote the cached artifact set into a named,
  hash-pinned snapshot, export and import the manifest
  (`depsilo/snapshot/v1`), and switch snapshot-only mode on from the Cache
  page or the Admin API. Snapshot mode refuses anything outside the active
  snapshot with 451 `SNAPSHOT_BLOCKED` after the malware gate, and
  membership lookups fail closed. Snapshots pin the SHA-256 values recorded
  by tamper detection; importing a manifest seeds missing baselines so a
  first fetch that differs from the manifest raises a tamper alert.
- The per-project SBOM export (Pro) gains a CRA mode and a technical-file
  preset. CRA mode carries the NTIA/CRA minimum elements: purl, SHA-256
  checksums recorded by tamper detection, supplier/license fields, and
  explicit product-to-component dependency relationships. The preset wraps
  the SBOM in a manifest with product identity, manufacturer, coverage
  counts, relationship depth, and explicit limitations. Supplier and license
  values come from operator-declared `[compliance.components]` annotations;
  unlisted components export as unknown. With a configured Ed25519 key,
  `sign=true` returns a detached signature over the exact response body so
  the document itself stays valid JSON for its format.
- The guaranteed malicious-package dataset now covers PyPI and RubyGems.
  PyPI verifies legacy archive filenames (`.zip`, `.egg`, `.tar.bz2`, legacy
  sdists, …) against the declaring package's simple index; RubyGems resolves
  the exact gem identity, including platform artifacts, from the compact
  index. While the dataset covers an ecosystem, an artifact whose identity
  cannot be proven is refused instead of being served unchecked, and
  RubyGems matching treats `1.17.2-x86_64-linux` as the `1.17.2` release.
- Minimum-release-age enforcement is available for npm, PyPI, Composer, NuGet,
  Cargo, and RubyGems. npm carries the packument `time[version]` inside the
  authenticated tarball token; PyPI negotiates the PEP 691 JSON simple index
  and carries `upload-time` with the exact upstream that declared the artifact;
  Composer uses the p2 metadata entry's own `time` field; NuGet uses the v3
  registration index's `published` timestamp for the same immutable version;
  Cargo uses the sparse index entry's `pubtime` and `cksum`; RubyGems resolves
  the compact index's `created_at` and checksum for the exact `.gem` filename.
  Conda is available with approximate Last-Modified provenance after the
  operator acknowledges it through `supply_chain.approximate_sources`, and the
  capability summary labels it approximate. CRAN joins under the same
  acknowledgement: the current source tarball uses the DESCRIPTION
  `Date/Publication`, and archives or binaries use the artifact's
  Last-Modified. Maven and Alpine also join with approximate Last-Modified
  provenance (the exact artifact is HEADed through the serving upstream, and
  lookups are memoized per upstream and path). Helm joins under the same
  acknowledgement: the chart identity comes from a streamed `index.yaml`
  scan (chart filenames cannot be split safely), the exact chart is HEADed for
  its Last-Modified, and artifacts the index does not declare are refused.
  Missing, legacy, HTML-only, or unlisted metadata fails closed while the gate
  is enabled. Composer remains best-effort because its clients can fall back to
  the original dist URL after a 451, and every other ecosystem still rejects
  positive thresholds at startup.

### Changed
- The Admin Overview got a correctness pass: request-rate and traffic series
  are named by link direction instead of implying a source classification,
  origin traffic that was never collected is separated from a measured zero,
  each trend states its aggregation window, the first load keeps its layout and
  makes no pre-payload claims, and the idle status strip fits its guidance
  without overflow.
- The S3 release contract now runs against pinned RustFS, replacing the
  SeaweedFS image that shipped with v0.10.1.

## [0.10.1] - 2026-10-06

### Added
- The Admin Overview now presents live service status, runtime resources,
  client-facing and origin request rates and traffic, cache benefits, activity
  trends, and recent requests, with centered detail dialogs that preserve deep
  links and page state.
- Overview reports the cached package inventory size, the number of served
  requests, and an estimated time saved derived from comparable cache-hit and
  origin latency samples.
- macOS process memory reports the current RSS through libproc instead of a
  peak-only explanation string.
- Portal Monitor pins upstream counts while scrolling, marks problem upstreams
  in the heartbeat, and reports service availability rather than mirror health.

### Changed
- The minimum toolchain is Go 1.27.1, which is also what CI and the container
  build now use.
- The web interface now uses shadcn/ui primitives over the existing Base UI
  layer. Admin workspace destinations act as page headers, and route chunks are
  warmed on tab switches instead of falling back to a loading state.
- The Depsilo brand kit mark, palette, and typography are applied across Portal
  and Admin, including the restored pale Overview palette and calmer Portal
  accent.
- Overview metric tiles are icon-first with hover measurement hints, and the
  small-screen layout sizes icons from the card instead of the viewport.
- The shadcn Tailwind layer used by the web UI is vendored under
  `web/src/styles/`, so the shadcn CLI is no longer a frontend devDependency.
  This removes its unfixable `braces` advisory chain without changing any
  component behaviour.

### Fixed
- npm metadata for packages whose names differ only in case (legacy uppercase
  names such as `JSONStream`) no longer collide in the local cache on
  case-folding filesystems such as macOS and Windows.
- Rejecting a quoted `min_cvss_score` keeps working on Go 1.27, where
  `encoding/json` runs the v2 implementation by default.
- Frontend dependencies no longer carry the known high-severity advisories
  that blocked CI: `axios` is upgraded to 1.20.0, `brace-expansion` is pinned
  to 1.1.21 / 5.0.12, and `source-map-js` is upgraded to 1.2.2.
- The v0.9.0 Compose upgrade helper now prepares its bind-layout state
  portably, and narrow Admin settings rails scroll instead of overflowing.
- Portal text no longer drops below the readable floor, sticky headers survive
  the body and root containers, and the phone ecosystem picker stays compact.
- The S3 release contract now runs against a pinned SeaweedFS image, because
  MinIO withdrew its public community images and binaries in October 2026. The
  contract still verifies signed streaming, multipart upload, listing,
  deletion, and cross-platform images through an authenticated endpoint.
- The v0.9.0 and v0.9.1 release upgrade contracts now derive the expected
  schema version from the candidate source instead of a hardcoded value, and
  wait for the legacy artifact cache row before shutting the fixture down.
- The v0.9.0 Compose contract verifies the published state directory by inode
  identity instead of reading a host-inaccessible `0700` marker, and the S3
  contract waits for SeaweedFS cold-start on CI runners.

## [0.10.0] - 2026-09-15

### Added
- Deployment now supports both named Docker volumes and host bind mounts for
  durable instance state.

### Changed
- The Admin navigation now groups work into Overview, Upstreams, Cache, Logs,
  Security, and Projects, with instance-wide administration in the sidebar
  footer.
- Bandwidth reporting is part of Overview; metadata refresh and audit records
  are grouped with access logs.
- Security and quarantine views use compact labeled selectors, while preserving
  existing deep links and browser navigation.

### Fixed
- Development server entry documents no longer remain stale after a frontend
  rebuild.

## [0.9.4] - 2026-09-10

### Changed
- Cache cleanup plans now validate request bodies strictly, report bounded
  execution outcomes honestly, and keep the preview scope visible in the Admin UI.
- Access logs preserve and filter unknown cache outcomes instead of inferring MISS
  from legacy boolean fields.
- Warmup history distinguishes all-failed jobs from partial success and evicts
  the oldest terminal record when its bounded history is full.
- The package-install benchmark uses explicit registry endpoints and exact sample
  counts, while frontend dependencies pass the moderate-level audit gate.

### Fixed
- LRU cleanup previews now page with a composite `(last_accessed, id)` cursor,
  preserving candidates when database primary-key order differs from access order.

## [0.9.3] - 2026-09-09

### Added
- Runtime capability summaries, security capability status, access-log request
  details, and local redacted diagnostics for operator troubleshooting.
- Read-only cache cleanup previews with execution bound to the reviewed plan.
- Bounded asynchronous cache warmup jobs with progress, retry, cancellation,
  restart recovery, and persisted history.
- Read-only MCP request explanations with bounded and redacted request facts.
- A reproducible package-install benchmark harness and upgrade qualification
  coverage.

### Changed
- Dashboard cache rates exclude requests whose cache outcome is unknown.
- Warmup and cache-miss UI states now distinguish accepted, completed, and
  failed work without presenting unknown outcomes as success.

### Fixed
- Diagnostic output now bounds and redacts request facts and sensitive URLs.
- Warmup history fails closed when persisted state is corrupt or unavailable;
  unreadable state is never replaced with an empty history.
- Schema-v4 upgrade and release verification coverage now exercises the current
  migration and recovery boundaries.

## [0.9.2] - 2026-09-02

### Upgrade notes
- Schema v3 does not reinterpret concrete Package Rules created by the legacy
  shared comparator. A v0.9.1 `created_by = security-scanner` value is not
  trusted as machine provenance because it can also be a human username.
  Before upgrading, review and remove every concrete rule, including generated
  auto-block rows, plus ecosystem-wide rules for unsupported proxy surfaces;
  recreate reviewed rules after upgrade. The migration then disables all
  stored automatic-block policies. See `docs/package-rules.md`.
- Schema v3 re-derives PyPI, Go, Cargo, CRAN, and Maven cache identities from
  authoritative keys and clears ambiguous metadata guesses. APT and NuGet
  cache identities are cleared because their transport keys cannot prove the
  package identity required by OSV; pre-v3 npm cache rows cannot participate in
  a new scan. Because legacy storage cannot distinguish fetched advisories from
  operator imports, all stored advisories, checks, dismissals, and project
  package rows for npm, PyPI, APT, Go, Cargo, Maven, NuGet, CRAN, RubyGems,
  and Composer are invalidated before trusted identities are scanned or
  imported again.
  Cached package objects remain available.
- Legacy Hugging Face cache rows are retired during migration and synchronously
  removed with their stored objects before request handling; ref pins are
  invalidated so a case alias cannot retain stale private bytes.
- Legacy malicious-package data is migrated only for the six end-to-end
  covered ecosystems. Unrecoverable npm and unsupported-ecosystem rows and
  overrides are removed, recoverable identities are dialect-normalized, and a
  full blocklist resync is required.

### Changed
- Package Rules now validate and persist ecosystem-specific package and
  version identities at creation time. PyPI uses PEP 440; Cargo and npm use
  strict SemVer comparisons, with npm exact/single-comparator rules enforced
  only after its signed packument provenance is authenticated. Go, Maven,
  NuGet, Conda, CRAN, and Alpine are exact-only; APT and Composer remain
  package-only where request paths do not prove a complete version. Unsupported
  ranges are rejected instead of guessed.
- Minimum-release-age enforcement is safety-disabled. Startup rejects positive
  enabled thresholds until the selected artifact source and authoritative
  timestamp provenance are bound end to end. New permanent approvals are
  disabled, while historical approvals remain readable and revocable.
- Guaranteed malicious-dataset request-path coverage is npm, Cargo, Composer,
  NuGet, Go, and Maven. PyPI and RubyGems are deliberately excluded until every
  served artifact format has complete identity provenance.
- NuGet background OSV scanning is disabled until registry canonical package
  IDs are retained instead of inferred from lowercase flat-container paths.
- APT background OSV scanning is disabled until authenticated repository
  metadata binds each binary package and served filename to Debian's source
  package identity.
- RubyGems background OSV scanning is disabled until trusted index or gemspec
  provenance is persisted; platform artifact filenames are not parsed into a
  guessed identity that could record a false-clean result.
- PyPI background OSV scanning trusts simple-index project names and strictly
  parsed PEP 427/625 artifacts only. Ambiguous legacy archives are retained in
  cache but are not assigned a guessed package identity or recorded as clean.
- Composer background OSV scanning now trusts only exact per-package `p2`
  metadata and reversible dist cache keys. `packages.json`, incomplete or
  malformed metadata paths, indexes, and arbitrary passthrough paths remain
  cacheable but cannot acquire a guessed package name or clean OSV receipt;
  legacy guessed Composer identities require migration repair before scanning.
- Cargo and CRAN background OSV scanning now trusts strict artifact keys only.
  Cargo sparse-index files, CRAN package indexes and installers, and ambiguous
  paths remain cacheable but cannot create a guessed scan or false-clean result.
- Automatic OSV-to-Package-Rule blocking is safety-disabled for every
  ecosystem until complete affected sets, including ordered ranges,
  prereleases, and explicit versions, can be represented without loss. Manual
  reviewed selectors remain available wherever the Package Rule capability
  table permits them, including single comparators for PyPI, Cargo, and npm.

### Fixed
- Frontend build tooling now pins Browserslist 4.28.8, which contains the
  upstream fixes for its unbounded query caches and unsafe custom-stats
  normalization.
- Pre-release, epoch, post-release, qualifier, revision, and build-metadata
  ordering no longer falls through a dot-splitting cross-ecosystem version
  comparator. Invalid Package Rule coordinates now fail before persistence,
  and semantic/integrity failures return `PACKAGE_POLICY_UNEVALUABLE` instead
  of silently allowing a request.
- Permanent quarantine approvals now use each ecosystem's package and version
  equality, collapse semantic aliases, and reject invalid coordinates.
- First-run setup now commits the administrator and onboarding state before
  publishing `config.toml`. SQLite setup commits use a pinned `FULL`-
  synchronous connection, config replacement is `0600` + temp-file fsync +
  atomic rename + parent-directory fsync, and startup reopens the wizard when
  a configured database has no loginable administrator. Fault-injection tests
  cover each database and filesystem publication boundary.
- Policy winner selection now uses an explicit lexicographic specificity tuple
  (reserved priority, selector levels, deny tie-break, and stable database ID)
  rather than an additive score or Store row order. The read-only Admin policy
  test shows matching candidates, winner, match levels, precedence reason, and
  policy snapshot state.

## [0.9.1] - 2026-09-01

### Upgrade notes
- Stop a running v0.9.0 background daemon with the v0.9.0 binary before
  replacing it. v0.9.1 refuses the unauthenticated PID-only record used by
  v0.9.0; if replacement already happened, verify that process has exited
  before removing the legacy PID file manually.
- The official container now runs as UID/GID `10001:10001`. Operators reusing
  a root-owned named volume must perform the documented one-time ownership
  change. Operators using the exact bind-mounted Compose layout shipped in
  v0.9.0 must use the Linux-only, fail-closed compatibility procedure in
  `docs/deployment.md`.
- Non-loopback listeners now require a JWT signing secret of at least 32 bytes
  with no surrounding whitespace. The v0.9.0 Compose preparer preserves an
  already-strong secret, or supports an explicitly confirmed rotation from a
  weak legacy secret using `DEPSILO_ACCEPT_JWT_ROTATION=1`. Rotation invalidates
  existing browser JWTs and requires a new login, but preserves passwords and
  API tokens. Signed extra-PyPI
  metadata must be fetched online once after rotation; cached artifact objects
  remain reusable.
- S3 writes larger than 8 MiB now use multipart upload. Restricted bucket
  policies need the normal object-write permissions plus
  `s3:AbortMultipartUpload` so failed writes can be cleaned up.

### Added
- First run now supports a zero-config installation with unified durable state,
  a startup summary, a single-volume `compose.yaml`, and generated bootstrap
  credentials.
- Portal and Admin now guide Operators through connecting and verifying their
  first real package-manager project.
- Hugging Face proxying now supports the Xet read-token routes used by current
  `huggingface_hub` and `hf-xet` clients without caching token responses.
- Release qualification now blocks artifact publication on all 14 official
  package-manager clients, the Docker Registry client, pinned ccache and
  sccache clients, a real MinIO S3 storage contract, and a v0.9.0 state-reopen
  contract covering config, SQLite identities, passwords and tokens, real npm
  metadata, cache metadata, and offline cached artifacts. A second contract
  runs the immutable published v0.9.0 image with its exact shipped Compose bind
  layout, then proves the fixed-UID release candidate preserves that state.
- Browser accessibility coverage now derives every Admin route from the route
  manifest, while Portal tests enforce the anonymous public-API boundary and a
  single owner for status polling.

### Changed
- Minimum-release-age quarantine and the known-malicious blocklist now offer an
  explicit `warn` mode that records the policy match while serving the request;
  `block` remains the default.
- The minimum toolchains and container bases are Go 1.26.7, Node.js 22.23.2,
  and Alpine 3.23.5; Go networking/crypto modules were refreshed with the
  corresponding security fixes.
- The official container now runs as fixed non-root UID/GID `10001:10001`
  while preserving `/root/.depsilo` as its state-volume path. The bind layout
  shipped in v0.9 has a fail-closed migration that backs up durable state and
  rejects symlinks, hard links, special files, and nested mounts before changing
  ownership; other older root-owned named volumes use a separate documented
  one-time ownership migration.
- Daemon startup now uses the effective serve configuration, waits for actual
  dependency readiness, reports a private startup log, and fails non-zero when
  the child exits or never becomes ready. Daemon records bind a PID to its OS
  process-creation identity, starts are serialized, Unix children enter a new
  session, and Windows uses a per-start named shutdown event. Graceful shutdown
  has a ten-second deadline, and foreground `start` shares the `serve` lifecycle
  and flags.
- `depsilo doctor` now checks the database and storage details reported by
  `/ready` and fails when either dependency is not ready.
- Release promotion is serialized repository-wide, binds lightweight or
  recursively peeled annotated tags to the triggering commit, and rechecks the
  authoritative ref plus the monotonic floating-tag decision immediately before
  registry mutation and GitHub Release publication.

### Fixed
- npm metadata rewriting now changes only package `dist.tarball` fields instead
  of rewriting unrelated strings that happen to contain an Upstream URL.
- Docker Registry authentication now propagates cancellation, locally
  validates registry, Bearer-realm, and redirect DNS scopes before direct dials
  and every explicitly proxied request, strips credentials on cross-origin
  redirects, bounds response bodies, preserves token parameters, and supports
  long streamed blobs without a whole-response timeout. Explicit forward-proxy
  DNS and ACL policy remains the documented final egress boundary. Docker tag
  pagination, NuGet search queries, and npm `Accept` representations now preserve
  requests upstream and use representation-safe cache keys.
- S3 storage signs non-seekable payloads, accepts known- and unknown-length
  streams with bounded buffering, aborts incomplete multipart uploads, creates
  regional AWS buckets correctly, and supports object-only credentials for an
  existing bucket.
- Package-cache and compiler-cache S3 bucket, endpoint, region, access-key, and
  secret-key settings now honor explicit `DEPSILO_*` environment-only
  configuration without requiring matching keys in `config.toml`.
- Hugging Face canonical relocation from `hf-mirror.com` to the official origin
  preserves the exact path and query, revalidates each hop, and permanently
  removes authorization after crossing origins.
- Generated and example configurations no longer enable a localhost forward
  proxy for every built-in Upstream.
- Remote listeners reject empty, short, whitespace-padded, or placeholder JWT
  signing secrets. Development and daemon logs that can contain first-run
  credentials are private.
- Portal Monitor no longer starts a second `/api/v1/stats` poll, and the shared
  Admin breadcrumb no longer exposes invalid or duplicate navigation semantics.

## [0.9.0] - 2026-08-12

### Added
- **Compiler cache service**: adds namespace-scoped remote caching for official
  ccache HTTP and sccache WebDAV clients, with read-only/read-write build
  credentials, bounded uploads and downloads, global/per-namespace quotas,
  LRU eviction, local or S3 storage, Admin management, Prometheus metrics, and
  an opt-in real-client regression target.
- **Admin control plane remediation**: Settings now atomically preserves and patches
  `config.toml` with explicit immediate/restart/environment-override results; ordinary
  upstream CRUD is database-authoritative and atomically updates live proxy Pools; fresh
  Principal authorization, exact Admin DTOs, responsive Base UI wrappers, and Playwright/
  axe regression coverage make all 13 Admin routes permission-aware and truthful about
  loading, empty, stale, error, and mutation states.
- **Tamper detection (DIRECTION T1)**: records the SHA-256 of each
  immutable artifact on first fetch; when a natural re-fetch (background
  refresh) yields different upstream bytes under the same version,
  depsilo keeps the trusted first-seen copy, refuses to cache the new
  content, and fires a critical `tamper_detected` webhook + audit event.
  Alert-only (never blocks); the hash is computed in the existing storage-pump
  reader without a second pass; immutability is inferred from TTL (no adapter
  changes). `[supply_chain.tamper_detection] enabled` (default true).
- **Known-malicious blocklist (DIRECTION Task 2)**: syncs the OSV
  malicious-packages dataset (MAL-* advisories) for eight ecosystems every 6h
  and refuses matched versions with HTTP 451 `MALICIOUS_BLOCKED` as the
  quarantine checker's step 0 — threshold-0 ecosystems (go) and the quarantine
  allow-list cannot bypass it. Audited operator overrides expire after
  24h and cannot be extended in place; continued access requires a new
  audited override. Critical-severity webhooks on every
  block; admin tab (sync status / manual sync / overrides) on the
  Quarantine page; enabled by default with degrade-on-failure (sync
  errors keep blocking on the last good dataset). `mirror_url` +
  `proxy` configurable for restricted-egress deployments.
- Go modules gained the malware gate on `@v/*.zip` downloads (with
  GOPROXY `!upper` path decoding); extra PyPI-compatible indexes
  canonicalize to the pypi blocklist rows.

### Changed
- **Minimum release age now defaults off** for new and empty configurations.
  Set `[supply_chain] min_release_age_enabled = true` to activate the
  recommended thresholds, or `false` to disable the gate while retaining a
  threshold table. Existing configs with explicit thresholds and no switch
  remain enabled for backward compatibility. The known-malicious blocklist
  and tamper detection are unaffected.

### Fixed
- Public service status now reflects actual Upstream health instead of always
  reporting healthy. Public latency history is keyed by stable Upstream ID so
  same-named sources in different ecosystems no longer share a graph;
  pre-ID rows remain isolated under `legacy:<name>` keys.
- Unified interactive password validation across setup and Admin user
  mutations, and added persistent JWT credential generations so password,
  role, and enabled-state changes immediately revoke older login sessions
  without revoking API tokens.
- Active Upstream workers now probe immediately at startup instead of waiting
  for the first interval. When every standard-pool source is unhealthy,
  passive sources receive a cooldown-limited half-open request so ordinary
  network failures can recover without admitting critical protocol failures.
- Built-in Upstreams now use canonical, live endpoints for RsProxy, Maven
  Central, TUNA RubyGems, and the Aliyun Composer mirror. Exact legacy seeded
  adapter/name/URL triples are upgraded once on restart; custom URLs and all
  other operator-managed fields are preserved.
- Blocklist import semantics hardened by adversarial review against
  the LIVE dataset: bounded-range advisories without version lists
  (fsevents, @solana/web3.js) are skipped instead of blocking every
  clean release; withdrawn advisories are never imported; NuGet names
  lowercase to match the flat-container protocol; a zero-entry archive
  can no longer wipe a populated ecosystem; all downloads and parsing finish
  before per-ecosystem transactional replacement, and sync is re-entrancy guarded.
- Quarantine/malware webhook timestamps were the zero value (rendered
  as year 0001 in chat channels).

## [0.8.0] - 2026-07-09

The "enforcement layer" release: Depsilo pivots from cache-with-extras to a
supply-chain enforcement layer (ADR-0003 / ADR-0004) and ships its first
enforcement primitive.

### Added
- **Supply-chain quarantine (minimum release age)**: package versions younger
  than a per-ecosystem threshold (npm 7d, most ecosystems 3d, go/apt exempt)
  are refused with HTTP 451 `QUARANTINED`. Not-found and unsupported publish
  timestamps fail closed by default; genuine upstream outages allow with a
  warning. The allowlist supports glob / exact pin / version ranges; blocks,
  bypasses, and admin approval/revoke actions are audited, while block decisions
  fire webhooks (Slack / DingTalk / WeCom / Feishu). Wired into 13 adapters via
  publish-time resolvers (8 real registry APIs + Last-Modified approximation for
  the rest). Ships enabled with safe defaults — an empty config is protected.
- **Composer dist mirroring**: `packages.json` now injects a preferred dist
  mirror so archives flow through the proxy — previously Composer downloads
  went straight to GitHub (no cache, no quarantine). Dist serving requires
  version *and* reference to match upstream metadata (metadata drift 404s and
  Composer falls back to the origin URL with the correct bytes); the
  quarantine resolver understands `~dev` metadata. Known limit documented:
  Composer treats a mirror 451 as a failed mirror and falls back to the
  origin, so this gate is caching + audit + best-effort blocking.
- `/api/v1/stats` gains a `week` block (7-day rolling window) so portal value
  metrics no longer reset at midnight.
- Portal redesign: dual-value hero
  ("Install fast. Install safe."), frosted film-grain background, chromatic
  shimmer on the primary CTA, prompt preview with filename/line numbers,
  hi-res (2560px) layout support, health-first Monitor with a quick filter
  ("/" to focus; matches ecosystems, aliases, upstream names/URLs) and 7-day
  value chips.
- CLI: `serve --port / --host / --config / --log-level` plus 12-factor
  `DEPSILO_*` environment variables.

### Changed
- **Pricing reset**: audit logs, package allow/deny rules and security
  scanning are now open-source; Pro narrows to multi-project workspaces +
  per-project SBOM export (contract-priced, Lemon Squeezy self-serve removed).
- "Instrument" design system (signal-green, dark default) across portal and
  admin; display typography, contrast and micro-interactions tuned in an
  aesthetic review pass (WCAG-AA small text, CJK-aware heading tracking).
- Heartbeat ticks: red now strictly means *down*; slow-but-alive upstreams
  paint amber, thresholds unified with status dots at 150ms.
- Integration prompt rewritten for transparency (no hidden product naming);
  release + image pipelines now dogfood CycloneDX + SPDX SBOMs. These artifacts
  are currently unsigned; cosign keyless signing remains planned.

### Fixed
- Integration test suite was fully red: quarantine's fail-closed default
  reached real registries for mock packages; thresholds now zeroed in the
  test config. A rules test still asserted pre-pricing-reset Pro gating.
- Admin sidebar layout jumps from Material Symbols FOUT; missing dashboard
  error-state i18n keys; stale Pro badges on unlocked pages.
- Alpine ships default upstreams (tuna / dl-cdn) and config-writer support.

## [0.7.1] - 2026-06-23

### Fixed
- Release pipeline (`.github/workflows/release.yml`) was inherited from the deprecated Wails desktop spec and started failing on every tag push: Ubuntu 24.04 (`ubuntu-latest`) dropped `libwebkit2gtk-4.0-dev`. Replaced the three Wails jobs (`desktop-macos`, `desktop-windows`, `desktop-linux`) with tray packaging that matches v0.7.0's actual deliverables.

### Added
- Release pipeline now publishes `depsilo-tray-macos.zip` (`.app` bundle, unsigned) and `depsilo-tray-linux-amd64.tar.gz` (tray binary + `.desktop` launcher + `install-linux.sh`) on every `v*.*.*` tag. Linux build pulls `libgtk-3-dev` + `libayatana-appindicator3-dev` (not webkit).

### Removed
- `cmd/server/main_desktop.go` (Wails entry, no frontend bindings), `wails.json`, the `build-desktop` Makefile target, and the `//go:build !desktop` tags on `cmd/depsilo/main.go` + `cmd/server/main_server.go`. `go mod tidy` dropped `wails/v2` and 20 transitive deps; `fyne.io/systray` is now a direct dependency.

## [0.7.0] - 2026-06-23

### Added
- **macOS menu-bar app** (`cmd/depsilo-tray/` + `internal/tray/`): live status icon shows healthy / degraded / failed with hit rate. Menu: Open Admin / Open Portal / Run Doctor / Quit. `make app-macos` packages a `Depsilo.app` bundle with `LSUIElement=true` (status-bar only, no Dock entry).
- **Linux desktop integration**: `make install-linux` writes the binary to `~/.local/bin` and a freedesktop launcher; `make autostart-linux` enables login start. GNOME on Wayland users need the AppIndicator GNOME extension.
- **`depsilo doctor`** CLI: end-to-end self-diagnosis (service / status / version / storage / upstream health / hit rate) with coloured TTY output (honours `NO_COLOR`), `--json`, and exit-code 1 on FAIL. Designed to surface "which signal is bad" with actionable hints instead of dumping stats.
- **`depsilo init-agent`** CLI: writes `CLAUDE.md` / `AGENTS.md` / `.cursorrules` with a marker-bracketed Depsilo block so AI coding agents auto-detect this project's proxy. Idempotent — preserves user content outside the markers.
- **MCP server** at `POST /mcp`: JSON-RPC 2.0 over Streamable HTTP. 6 tools (`depsilo_status`, `depsilo_doctor`, `depsilo_configure`, `depsilo_search`, `depsilo_recent`, `depsilo_warmup`) + 2 resources (`depsilo://discover`, `depsilo://stats`) + 1 prompt (`setup`). Claude Code / Cursor / any MCP-aware client can drive Depsilo via structured tool calls instead of parsing free-form prompts.

### Changed
- Brand palette switched from purple (oklch hue 295) to deep teal (oklch hue 200) across brand / aurora / page-wash / grad-rim / shadow tokens, light and dark.
- Default sans font changed from Inter Variable to Geist Variable; mono from JetBrains Mono Variable to Geist Mono Variable. Noto Sans SC retained as the CJK fallback so Chinese glyphs do not shift.
- Admin login rebuilt as a split-card: brand panel (13-ecosystem logo wall + 3 live stats from `/api/v1/stats`) on one side, form on the other. Card adds aurora-rim top accent + shadow-card + radius 14.
- Pro paywall modal redesigned: workspace_premium brand icon badge + 4-bullet Pro feature preview + footer reorganised with primary action anchored right.
- `ModalV2` lifted across all 20+ admin call sites: ESC closes, `role="dialog"` + `aria-modal` + `aria-labelledby`, viewport-centred (was `mt-[20vh]`), 160ms scale + fade entry with `prefers-reduced-motion` fallback, tokenised backdrop, larger `--shadow-pop` elevation.
- Portal hero gained a 13-ecosystem logo wall under the subtitle (clickable shortcuts). Portal `CodeBlock` now highlights URLs in brand teal and dim-renders comment lines.
- BandwidthReport's 12-colour rainbow chart palette replaced by a single shared `web/src/lib/ecosystemColors.ts` mapping ecosystems to their real-world brand colours (npm red, go cyan, cargo orange, …). CacheManage uses the same map.
- `README.md` "Use with AI agents" section restructured into three tiers (`init-agent`, MCP, copy-paste prompt) with examples.

### Fixed
- Five dead CSS tokens silently falling back to browser defaults: `var(--text-base)` (5 sites in MainLayout), `var(--border-purple)` (Button secondary + ProRequiredCallout), `var(--lemon)` (Security severity bars, Projects token warning, Dashboard cache-usage banner). All replaced with real tokens.
- Admin top-bar service status indicator was hardcoded `<StatusDot status="healthy" />` with a bare English "Healthy". Now reads `stats.service.status` and renders three-tier health (healthy / degraded / failed) via i18n.
- `Settings.tsx` webhook tab passed `icon: '🔔'` (an emoji) as a Material Symbol name, rendering as a fallback glyph. Changed to `'notifications'`.
- Six leftover `rgba(83,58,253,...)` hardcoded purple residues from before the brand switch — Button secondary hover, ProRequiredCallout backgrounds, Settings tab active state, SetupWizard ecosystem checkboxes.
- `MainLayout` `JSON.parse(localStorage.getItem('user') ...)` no longer white-screens admin on corrupt data; wrapped in try/catch with a typed fallback.
- 13 hardcoded green hex (`#10b981` / `#3bd671`) replaced with `var(--ok)` across Dashboard, BandwidthReport, AuditLogs, AccessLogs, UpstreamCard, Portal Monitor. UpstreamCard `beatColor` 4-tier palette collapsed to a 3-tier ok / warn / danger semantic.
- 12 inline `onMouseEnter` / `onMouseLeave` handlers doing hover via JS (which silently broke `:focus-visible`) converted to Tailwind `hover:` utilities across MainLayout, AccessLogs, AuditLogs, CacheManage.

### Removed
- Three orphan Portal pages (`PackagesV2.tsx`, `PackageDetailV2.tsx`, `LiveStreamV2.tsx`) and their i18n namespaces — none were wired to a route.
- `AgentPane` component (the Portal `LanguageRail` dedup made it unreachable; the per-language AI tab already covers the same workflow).
- 21 em-dashes across i18n, UI text and CLI tool output, in line with the taste-skill audit's hard ban.

## [0.6.0] - 2026-06-21

### Added
- Webhook notification system: Slack / DingTalk / WeCom / Feishu / Generic transports. Configurable per-event in Settings → Webhooks.
- Background scheduler: periodic upstream health checks and license-expiry warnings.
- `depsilo backup --out file.tar.gz` and `depsilo restore <archive>` CLI for config + database snapshots.
- One-liner installer script (`install.sh`) and Homebrew formula template.
- Goreleaser release pipeline producing per-platform binaries.

### Changed
- README documents four installation methods (binary download, install.sh, Homebrew, Docker).
- `Upstream.IsHealthy()` is now thread-safe.

### Fixed
- Goreleaser entry point was `cmd/server` instead of `cmd/depsilo`; automated release binaries now include the CLI subcommands.

## [0.5.0] - 2026-05-28

### Added
- HuggingFace Hub adapter — proxies models + datasets via `/huggingface/*`. Supports the full huggingface_hub client surface (huggingface-cli, transformers, datasets) by honoring `HF_ENDPOINT`. Server-side 302 following streams LFS blobs from `cdn-lfs.huggingface.co` inline; clients never see the signed CDN URL.
- New `[[huggingface.upstreams]]` config block with `hf-mirror.com` + `huggingface.co` defaults; the priority selector uses the first healthy source.
- Pass-through Authorization handling: gated models work when users provide their own `HF_TOKEN`, but auth'd responses are NOT cached (cross-user safety).
- New unit tests: 4 keyer + 5 resolver. New integration tests: 7 covering metadata, tree listing, dataset metadata, resolve (200 + 302), HEAD-only, unknown path. New Docker E2E (`make test-docker-huggingface`) using `prajjwal1/bert-tiny`.
- Portal QuickStart: "Hugging Face" entry with `huggingface-cli` and `transformers` setup snippets.
- `/api/v1/discover` and `/api/v1/agent-prompt` know about HuggingFace.

## [0.4.0] - 2026-05-21

### Added
- `/admin/license` page for self-serve 14-day Pro trial activation and runtime license-key management (set / change / remove)
- API endpoints: `POST /api/v1/admin/license/trial/activate`, `PUT /api/v1/admin/license/key`, `DELETE /api/v1/admin/license/key`
- New backend modules: `internal/trial` (local 14-day state machine) and `internal/entitlement` (façade combining license + trial)
- Frontend: global `ProRequiredModal` triggered by 402 responses, with inline "Start trial" CTA when available
- Landing-page `/pro-trial` page (closes the existing 404 from the Pricing CTA)

### Changed
- `GET /api/v1/admin/license/status` response body — new `source`, `days_left`, `trial_used`, `trial_available`, `license_key_masked` fields. Old `key_masked` and `activated_at` retained as deprecated aliases for one release; will be removed in 0.5.0
- `402 PRO_REQUIRED` response now includes a `trial_available` boolean
- `audit.Logger` and `rules.Engine` now depend on `entitlement.Checker` instead of `license.Manager` directly — trial users now get the same audit + rules behaviour as paid users
- `license.NewManager` signature now accepts `*gorm.DB` for runtime key persistence; DB-stored key takes precedence over `config.toml`

### Fixed
- `license.Manager.doValidate` no longer reads `m.key` outside the lock — eliminates a data race introduced when `SetKey` was added

### Deprecated
- `EntitlementStatus.key_masked` field — use `license_key_masked` (alias removed in 0.5.0)
- `EntitlementStatus.activated_at` field — derive from per-source state instead (alias removed in 0.5.0)

## [0.1.0] - 2025-03-30

### Added
- pip (PyPI) proxy and cache — production ready
- apt (Ubuntu/Debian) proxy and cache — production ready
- Web UI dashboard with cache stats and upstream health
- Prometheus metrics endpoint at /metrics
- Multi-upstream support with per-source HTTP proxy
- Automatic upstream health checks and failover
- Local filesystem storage backend
- SQLite database (default)
- Docker and docker-compose deployment
- MIT License
