# Development quick start

Use this path for a fresh checkout or a new coding-agent session. Commands are
defined by the root `Makefile`; run `make help` if this guide and the Makefile
diverge.

## Prerequisites

- Go 1.27.4 or newer, as pinned by `go.mod`.
- Node.js 22.23.2 or newer and npm 10 or newer.
- GNU Make.
- Docker for container, S3 storage-contract, upgrade, and real-client tests;
  ordinary local development does not need it.

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

A project `config.toml` is optional, and Git ignores it, so a fresh clone or
worktree has none. `scripts/run-dev.sh` then leaves `DEPSILO_CONFIG` unset, and
the loader searches `./config.toml`, `/app/config.toml`, and
`~/.depsilo/config.toml`:

- If `~/.depsilo/config.toml` exists, `make dev` and `make run` start with it
  and use the database and cache it names. On a machine that already runs
  Depsilo from the home directory, that is the state of that instance.
- If no file is found, the instance waits for first-run setup. Its database,
  cache, and secrets default to `~/.depsilo/data/`, and setup writes
  `~/.depsilo/config.toml`.

To keep a checkout's state inside the checkout, run
`cp config.example.toml config.toml` before the first `make dev`. State then
lives in `./data/`. The example listens on all interfaces; set
`host = "127.0.0.1"` under `[server]` in the copy to keep the instance local.
Setup starts as reopened setup, because a configuration file exists and no
administrator does; finishing it replaces `config.toml` and keeps the copy
beside it as `config.toml.pre-setup-<UTC time>.bak`.

Configuration precedence is:

```text
CLI flag -> DEPSILO_* environment -> config file -> built-in default
```

For local development, `scripts/run-dev.sh` creates `.dev-jwt-secret` with mode
0600 when needed and reuses it through `DEPSILO_AUTH_JWT_SECRET`. This remains a
supported explicit override and is local state, not a production secret.

Ordinary `depsilo serve` does not require a JWT environment variable. For a new
instance with no explicit `DEPSILO_AUTH_JWT_SECRET` or `auth.jwt_secret`, it
generates a 256-bit instance key at `data/secrets/jwt.key`, relative to the
configuration file's directory, and reuses it on restart. The secrets directory
has mode 0700 and the key file has mode 0600. Existing explicit secrets keep
working without migration. This key also signs npm and extra PyPI artifact
URLs, so preserve it together with the database when moving or backing up an
instance. An existing database without its key, or an invalid key file, stops
startup rather than silently replacing the key.

CLI backups include the effective signing key in the archived `config.toml`,
including keys supplied through the environment. Protect these archives as
secrets. Restore keeps the same value in configuration, so a separate key file
is not needed for that restored instance.

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

- Interactive first run requires a bootstrap token. Read the current token
  from the deployment directory with `depsilo setup token`, or, for Docker
  Compose, `docker compose exec depsilo depsilo setup token`. A custom native
  configuration can use `depsilo setup token --config /path/to/config.toml`.
  In a checkout, run the built binary: after `PORT=18080 make dev`, that is
  `./bin/depsilo setup token --port 18080`. An instance started with
  `depsilo serve --port 18080` needs the same flag: without `--port` the
  command asks the configured port, 23333 by default. The command only
  returns a token while the running service needs setup. Startup logs remain
  a fallback.
- During setup, `data/secrets/bootstrap.token` holds the current token with
  mode 0600. It is refreshed on server restart and removed after setup
  completes. It is separate from the persistent `jwt.key` signing key; do not
  enter `jwt.key` in the setup form. No public API returns the token.
- The setup flow creates the initial administrator; there is no default
  `admin/admin` credential.
- Setup commits the administrator and onboarding state before atomically
  publishing `config.toml`. If a process stops at any boundary, restart either
  keeps the wizard recoverable or leaves the submitted administrator able to
  log in; a configured instance with no loginable administrator automatically
  reopens setup.
- Default local data lives under the configured database and storage paths.
- The managed instance key lives under the configuration directory's
  `data/secrets/`, independently of custom cache-storage paths. Containers must
  persist this directory as well as their database and cache.
- `config.toml`, `config.toml.pre-setup-*.bak`, `.dev-jwt-secret`, `.dev.log`,
  `.server.pid`, `data/`, `bin/`, and `web/dist/` are generated or local
  state.

`make clean` removes build/runtime process artifacts while preserving data and
the development JWT. `make clean-all` also removes the checkout's `data/`
directory and `.dev-jwt-secret` and is therefore destructive. It leaves
`config.toml` in place and removes nothing under `~/.depsilo/`. In a checkout
without `config.toml`, the database, cache, and written configuration therefore
survive `make clean-all`. Delete them from `~/.depsilo/` by hand, and only when
no instance you use keeps its state there.

## CLI conventions

Follow these when adding or changing a command under `internal/cli/`. Existing
commands differ in places; the form named here is the one to follow.

- Registration: add the command to the `commands` table in
  `internal/cli/root.go` and to the `Commands:` block of `helpText` in the
  same file. Nothing else dispatches commands.
  `TestHelpCommandsMatchTheCommandList` fails when the two differ.
- Help: `dispatch` in `internal/cli/root.go` answers it for every command
  and subcommand. An argument that is exactly `--help` or `-h`, anywhere
  before a `--`, prints the command's usage to stdout and returns 0 before
  the handler runs. Give the command a `usage` function in the `commands`
  table; without one, its entry in the `Commands:` block of `helpText` is
  printed. Do not handle `--help` in the handler, and do not give `-h`
  another meaning. `TestBinaryDispatchesEveryCommandNamedInHelp` in
  `internal/cli/cli_process_test.go` runs the built binary with `--help` and
  `-h` for every command in the help, for the documented subcommands, and
  with arguments that would make a handler act. It fails when one exits
  non-zero, writes to stderr, prints no usage, sends a request to its stub
  server, or creates a file in the home or working directory.
- `--json`: call `stripJSONFlag(args)` first; it accepts `--json` or `-j` at
  any position. Print one value with `printJSON`, which writes indented JSON
  to stdout. List the command under `Accepted by:` in `helpText` and in
  `TestHelpListsTheCommandsThatAcceptJSON`.
- JSON errors: print `{"ok": false, "error": "<message>"}` to stdout and
  return 1, for argument errors too, as `backup` and `restore` do; a success
  object carries `"ok": true`. Existing commands differ: `status` and
  `warmup` also put short codes such as `unreachable` and `missing_args` in
  `error`; `warmup` and `flush` report an unexpected HTTP status as
  `{"ok": false, "http_status": <code>}` without `error`; `distribution` and
  `version` omit `ok`; `audit` reports argument errors as plain text.
- Streams and exit codes: results go to stdout; without `--json`, errors go to
  stderr as `Error: <message>`. Return 0 for success and 1 for any failure,
  including a check that ran and failed. `serve` and `start` return 2 for an
  invalid flag; new commands return 1.

## Fast confidence check

After the service starts:

```bash
curl -fsS http://localhost:23333/health
curl -fsS http://localhost:23333/ready
make test
```

Use the [testing guide](testing.md) before running broader or network-dependent
checks.
