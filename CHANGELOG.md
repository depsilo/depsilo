# Changelog

All notable changes to Depsilo will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/).

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
