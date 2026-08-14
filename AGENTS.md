# Depsilo repository guide

This is the stable entry point for coding agents. Keep it short. Read linked
guides only when the current task needs them; do not preload historical plans.

## First minute

1. Run `git status --short`. The worktree may contain user changes; preserve
   them and do not reset unrelated files.
2. Read [docs/development/quick-start.md](docs/development/quick-start.md) when
   starting or running the project.
3. Use [docs/development/change-map.md](docs/development/change-map.md) to find
   the owning module before searching broadly.
4. Select the smallest relevant check from
   [docs/development/testing.md](docs/development/testing.md).

## Current project shape

- Depsilo is a self-hosted, single-instance dependency proxy, cache, and
  supply-chain enforcement service for individuals and small teams.
- The supported deployment authority is SQLite plus local or S3 object
  storage. Shared-database multi-instance/HA is not shipped.
- The anonymous Portal handles connection guidance and status. Authenticated
  Admin handles operations, policy, security, users, and settings.
- Minimum release age is opt-in and off by default. Do not turn planned or
  unreleased behavior into a product claim.
- General-purpose artifact repository support is a future direction, not a
  current capability. Architectural work in that direction requires an ADR
  update because ADR-0004 records an older non-goal.

## Sources of truth

Use the narrowest current authority for the fact in question:

- Executable behavior: code, tests, `Makefile`, CI, and config loader/schema.
- Product intent and claims: [PRODUCT.md](PRODUCT.md).
- Domain language: [CONTEXT.md](CONTEXT.md).
- Accepted architectural decisions: relevant files under [docs/adr](docs/adr).
- UI contracts: [DESIGN.md](DESIGN.md) and the current frontend implementation.
- Admin runtime authority: [docs/admin-control-plane.md](docs/admin-control-plane.md).
- Historical specs and research: `docs/specs/` and `docs/research/`; these are
  evidence, not current implementation instructions.

When sources conflict, report the conflict. Do not silently choose the more
convenient document.

## Repository map

- `cmd/depsilo/`: thin CLI entry.
- `internal/cli/`: command parsing and process lifecycle.
- `internal/server/`: composition root and runtime wiring.
- `internal/adapter/`: package-protocol adapters and request translation.
- `internal/cache/`: cache coordination, streaming, retention, local/S3 seam.
- `internal/upstream/`: runtime pools, selection, health, and registry state.
- `internal/api/`: public and authenticated HTTP interfaces.
- `internal/config/`: schema, precedence, validation, and durable settings.
- `internal/db/`: SQLite models, repositories, and migrations.
- `web/src/portal/`: anonymous onboarding and status UI.
- `web/src/admin/`: authenticated operator UI.
- `tests/integration/`: tagged HTTP tests with a local mock upstream.
- `web/unit/`, `web/e2e/`: Vitest logic tests and Playwright user-flow tests.
- `testground/`: network-dependent real package-manager clients.

See [docs/development/architecture.md](docs/development/architecture.md) for
the runtime flow and ownership seams.

## Commands

```bash
make setup       # locked Go and frontend dependencies
make dev         # build and start the background development service
make logs        # follow .dev.log
make stop        # stop the background service
make test        # fast cached Go tests
make test-ui     # mocked Chromium smoke suite
make check       # normal local change gate
make verify      # complete offline gate used before push
```

`make security` and `make test-e2e` use the network and are deliberately not
part of `make verify`.

## Change rules

- Prefer tests through the owning module's interface. When a deeper test
  replaces a shallow implementation test, delete the duplicate instead of
  layering both.
- Stream package and model artifacts; never buffer an entire large response.
- Preserve signed metadata bytes such as APT `InRelease`.
- Do not let package or machine HTTP routes fall through to the SPA.
- Keep ordinary Upstream, Docker registry, and extra-index authority distinct;
  consult the Admin control-plane guide before changing ownership.
- Derive browser-visible service origins from `window.location.origin`.
- Update both `web/src/i18n/zh.ts` and `web/src/i18n/en.ts`, then run
  `make lint-i18n`.
- `config.toml`, `.dev-jwt-secret`, runtime data, logs, and generated build
  output are local state. Never commit secrets or generated credentials.
- Use focused tests while iterating and expand verification in proportion to
  the changed seam. Protocol changes require the relevant real-client E2E.

## Agent skills

### Issue tracker

Issues are tracked as GitHub issues via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Triage roles use the default label strings (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: root `CONTEXT.md` plus `docs/adr/`. See `docs/agents/domain.md`.
