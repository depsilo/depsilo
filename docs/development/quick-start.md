# Development quick start

Use this path for a fresh checkout or a new Codex session. Commands are defined
by the root `Makefile`; run `make help` if this guide and the Makefile diverge.

## Prerequisites

- Go 1.26.7 or newer, as pinned by `go.mod`.
- Node.js 22.23.2 or newer and npm 10 or newer.
- GNU Make.
- Docker only for container and real-client tests.

Install locked dependencies:

```bash
make setup
```

Install the Playwright Chromium binary as well when working on the UI:

```bash
make setup-ui
```

## Start the normal development service

```bash
make dev
make logs
```

`make dev` builds the embedded frontend and backend, starts Depsilo in the
background, and waits for the configured readiness URL. The default URL is
`http://localhost:23333`; override both listener and readiness probe with, for
example, `PORT=18080 make dev`.

A project `config.toml` is optional. Without one, the loader uses its normal
search path and built-in first-run defaults, and the web setup flow owns initial
configuration. Configuration precedence is:

```text
CLI flag -> DEPSILO_* environment -> config file -> built-in default
```

For local development, `scripts/run-dev.sh` creates `.dev-jwt-secret` with mode
0600 when needed and reuses it. It is local state, not a production secret.

Useful lifecycle commands:

```bash
make logs          # follow the background log
make cli-status    # query a built local binary
make stop          # stop the background service
make run           # foreground build + run
make run-pro       # foreground run with local development entitlements
```

## Frontend hot reload

Use one command to build and start the backend, then keep Vite in the
foreground:

```bash
make dev-ui
```

Open `http://localhost:5173`. Pressing Ctrl-C stops both Vite and the backend
owned by that command. Port overrides stay paired, for example:

```bash
PORT=18080 UI_PORT=15173 make dev-ui
```

The Vite origin is a complete local test entry: it forwards the fixed package
protocol catalog, Docker and project-scoped routes, compiler-cache routes, and
the current runtime extra-index paths to the selected backend. This keeps
configuration copied from the Portal valid when it contains the Vite origin.
Use `make stop` if the terminal is terminated without allowing cleanup to run.

## First-run and local state

- Interactive first run prints a one-time bootstrap token to the server log.
- The setup flow creates the initial administrator; there is no default
  `admin/admin` credential.
- Setup commits the administrator and onboarding state before atomically
  publishing `config.toml`. If a process stops at any boundary, restart either
  keeps the wizard recoverable or leaves the submitted administrator able to
  log in; a configured instance with no loginable administrator automatically
  reopens setup.
- Default local data lives under the configured database and storage paths.
- `config.toml`, `.dev-jwt-secret`, `.dev.log`, `.server.pid`, `data/`, `bin/`,
  and `web/dist/` are generated or local state.

`make clean` removes build/runtime process artifacts while preserving data and
the development JWT. `make clean-all` also removes local data and the JWT and is
therefore destructive.

## Fast confidence check

After the service starts:

```bash
curl -fsS http://localhost:23333/health
curl -fsS http://localhost:23333/ready
make test
```

Use the [testing guide](testing.md) before running broader or network-dependent
checks.
