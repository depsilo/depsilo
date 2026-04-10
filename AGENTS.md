# AGENTS.md — Depsilo

> Universal AI agent instructions. Works with Hermes, OpenClaw, Claude Code, Cursor, Copilot, Windsurf, Aider, Continue, and any tool that reads AGENTS.md.

## Project

**Depsilo** — lightweight dependency package proxy cache gateway, written in Go, single binary.

- 12 ecosystems: pip, apt, npm, Go, Cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm
- Web UI: user portal (no login) + admin dashboard (login required)
- Backend: Go + Gin + GORM + SQLite/PostgreSQL
- Frontend: React 18 + TypeScript + Tailwind CSS v4 + Recharts

## Directory Structure

```
cmd/server/main.go          # Entry point
internal/
  adapter/                  # Per-ecosystem proxy handlers (pypi/, apt/, npm/, etc.)
  cache/manager.go          # Singleflight + streaming cache engine
  cache/local.go|s3.go      # Storage backends
  upstream/                 # Upstream pool, selector, health check
  db/                       # GORM models + repository
  api/                      # REST API routes (admin/, public/, auth)
  middleware/               # JWT, logger, recovery
web/src/                    # React frontend
  portal/                   # User-facing pages (no login)
  admin/                    # Admin dashboard pages
  components/               # Shared UI components
  i18n/                     # zh.ts + en.ts translations
  lib/api.ts                # Axios API client
```

## Dev Commands

```bash
make build          # Build frontend + backend → bin/depsilo
make dev            # Build + run in background on :23333
make stop           # Stop background server
make test           # go test ./...
make lint           # golangci-lint + frontend type-check
make frontend       # Build React → web/dist (embedded in binary)
```

## Key Rules

1. **Streaming**: Never buffer entire response bodies. Large packages (torch ~2GB) must stream.
2. **URL rewriting**: PyPI HTML `href`, npm JSON `dist.tarball`, Cargo `config.json` `dl`, Composer `metadata-url`, NuGet `@id` fields must be rewritten to proxy URLs. APT/Go/Maven are passthrough.
3. **No hardcoded URLs**: Frontend uses `window.location.origin` for all service addresses.
4. **Error handling**: Never ignore Go errors. Always propagate or log with `zap.L()`.
5. **Context**: All IO operations take `context.Context`.
6. **Translations**: All user-visible text goes through i18next (`t('key')`). Both zh.ts and en.ts must be updated together.
7. **Components**: UI uses V2 component set (ButtonV2, CardV2, BadgeV2, etc.) following Stripe design system from DESIGN.md.

## Tech Stack (locked, do not replace)

Go 1.21+ (Gin, GORM, viper, zap, singleflight, gobreaker, prometheus)
React 19 + TypeScript + Vite + Tailwind v4 + Recharts + TanStack Query v5

## Detailed Reference

See `CLAUDE.md` for complete API routes, data models, adapter implementation details, and frontend component specifications.
