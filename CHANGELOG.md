# Changelog

All notable changes to Depsilo will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

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
