---
name: add-adapter
description: Scaffold a new ecosystem adapter for Depsilo
---

The user wants to add a new package ecosystem adapter. Ask for:
1. Ecosystem name (e.g. "docker", "hex")
2. Proxy type: passthrough (no response modification) or rewrite (URL rewriting needed)
3. URL path prefix (e.g. "/docker/")

Then create the following files following existing adapters as reference:
- `internal/adapter/{name}/handler.go` — Register routes, implement proxy logic
- `internal/adapter/{name}/keyer.go` — Cache key extraction from request path

For passthrough adapters, reference `internal/adapter/maven/` as template.
For rewrite adapters, reference `internal/adapter/pypi/` as template.

Also update:
- `internal/adapter/interface.go` — ensure the new adapter implements the Adapter interface
- `internal/api/router.go` — register the new adapter's routes
- `config.example.toml` — add upstream config section
- `web/src/i18n/zh.ts` and `en.ts` — add translation keys for the new ecosystem
- `web/src/portal/pages/QuickStartV2.tsx` — add tab entry and configuration methods
- `CLAUDE.md` — add to the supported ecosystems table
