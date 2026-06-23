# Changelog

All notable changes to Depsilo will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/).

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
- New `[[huggingface.upstreams]]` config block with `hf-mirror.com` + `huggingface.co` defaults (LatencySelector picks the faster).
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
