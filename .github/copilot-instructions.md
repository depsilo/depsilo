# Depsilo — GitHub Copilot Instructions

You are working on **Depsilo**, a Go + React dependency proxy cache gateway supporting 12 ecosystems (pip, apt, npm, Go, Cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm).

## Architecture
- Backend: Go 1.21+ with Gin (HTTP), GORM (ORM), viper (config), zap (logging)
- Frontend: React 19 + TypeScript + Tailwind CSS v4 + Recharts, in `web/src/`
- Entry: `cmd/server/main.go` — adapters in `internal/adapter/`, cache in `internal/cache/`
- UI components use V2 set (ButtonV2, CardV2, etc.) following Stripe design (DESIGN.md)

## Key Rules
- **Stream responses** — never buffer entire package bodies (can be 2GB+)
- **URL rewriting** is required for PyPI, npm, Cargo, Composer, NuGet adapters
- **APT, Go, Maven, RubyGems, Conda, CRAN, Helm** are strict passthrough (preserves GPG signatures)
- Frontend must use `window.location.origin` for service URLs, never hardcode
- All Go functions that do IO must take `context.Context`
- Never ignore errors in Go — propagate or log with `zap.L()`
- Frontend i18n: always update both `web/src/i18n/zh.ts` and `en.ts`

## Commands
```
make build    # Build frontend + Go binary
make dev      # Run locally on :23333
make test     # go test ./...
```

## Reference
See `CLAUDE.md` for the complete project specification.
