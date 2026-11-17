# Testing guide

Tests are organized by the seam they exercise, not by an ambition to run every
tool on every edit. Start focused, then expand to the smallest gate that covers
the changed interface.

## Test layers

| Layer | Command | Network | Purpose |
| --- | --- | --- | --- |
| Focused Go | `go test ./internal/<module> -run TestName -count=1` | No | One backend module interface |
| Fast Go | `make test` | No | Short-mode production and cross-module Go packages, with cache |
| Full Go | `make test-full` | No | Every production and cross-module Go package, uncached and without race instrumentation |
| Race-sensitive Go | `make test-race` | No | Concurrency, streaming, scheduler, and lifecycle owners under the race detector |
| Frontend unit | `npm --prefix web run test:unit` | No | Pure state/model/manifest logic |
| UI smoke | `make test-ui` | No | Critical Portal/Admin/setup routes with mocked APIs |
| Production UI smoke | `make test-ui-production` | No | One browser flow against the built Go binary and its embedded frontend |
| Tagged integration | `make test-integration` | No | Local Depsilo process plus mock Upstream over HTTP |
| Normal gate | `make check` | No | Lint, fast Go, frontend unit/build/bundle, binary, UI smoke |
| Complete gate | `make verify` | No | Full Go plus focused race coverage, integration, full Playwright, scripts, build and module checks |
| Dependency security | `make security` | Yes | Go vulnerability data and npm audit of all frontend dependencies |
| One real client | `make test-docker-<ecosystem>` | Yes | Native package manager against live Upstreams |
| All real clients | `make test-e2e` | Yes | Scheduled/opt-in package-client matrix |
| Compiler clients | `make test-compiler-cache` | Client-dependent | Installed ccache and sccache against a running service |
| Qualified compiler clients | `make test-compiler-cache-qualified` | No upstream network after client install | Bootstrap a service and credential, then prove ccache and sccache miss/write/remote-hit behavior |
| S3 storage contract | `make test-s3` | Local Docker | Signed known/unknown-length streaming, metadata, listing, deletion, and failed multipart cleanup against pinned RustFS |
| v0.9.0 state reopen | `make test-v090-upgrade` | Go modules if uncached | Build the tagged source, seed durable state, prove legacy unsigned npm cache fails closed offline, then reconnect the mock Upstream and prove fresh signed provenance |
| v0.9.0 shipped Compose upgrade | `make test-v090-compose-upgrade` | Privileged Docker, host PID namespace, util-linux `findmnt`, and registry access | Reject a real nested bind mount without changing its external victim, then run the immutable published v0.9.0 image with its exact shipped layout, rotate a weak JWT secret explicitly, and reopen its state with a UID/GID 10001 candidate |
| v0.9.1 fixed-baseline upgrade | `make test-v091-upgrade` | Go modules if uncached, Docker, and registry access | Build source from the fixed peeled tag commit, then run the immutable published image by digest with its checksummed Compose named-volume layout; preserve credentials and entitlement while migrating a safe Package Rule to schema v3 |
| Previous-release upgrade | `make test-previous-release-upgrade` | Docker and registry access | Run the previous published release's image by digest with its checksummed Compose named-volume layout, complete first-run setup, and create a session, an API token, a second user, and a Package Rule, activate the trial and edit an Upstream; then start the candidate on the same volume and require unchanged config, no reopened setup, working credentials, the rule, the trial, the edited Upstream, an intact audit chain, private state files owned by the service user, a clean startup log, and the candidate's schema version. Refuses a shallow checkout. Fails when the pinned baseline is older than the newest release in the candidate's history |
| Development scripts | `make verify-scripts` | No | Make workflows, paired Vite/backend lifecycle, and development proxy coverage |

A fresh checkout or worktree has no `web/dist/`, and five packages embed it:
`internal/cli`, `internal/server`, `cmd/depsilo`, `cmd/depsilo-tray`, and
`cmd/server`. Run `make prepare-go` once before `go test` or `go build` on
them; without it they stop with `pattern all:dist: no matching files found`.
`make clean` removes `web/dist/`; run `make prepare-go` again after it. The
Make test targets do this themselves. To run several packages, pass
`./cmd/... ./internal/...` rather than `./...`: `web/node_modules` contains a
Go package. `make test` also runs `./web`, `./tests/unit/...`, and
`./tests/mock/...`.

Go tests, including the tagged integration suite, bind free ports. The browser
suite starts its own Vite server on port 4173 (`PLAYWRIGHT_PORT` changes it)
and mocks the API it needs. Requests it does not mock go to a closed port, not
to a local instance on port 23333; set `DEPSILO_DEV_BACKEND_URL` to forward
them to a backend you name. The real-client targets start their own server on
`PORT`, 23333 by default; pass `PORT=<free port>` when another instance
already listens there.

Two real-client fixtures cover more than one install. `make test-docker-npm`
runs npm, pnpm, Yarn 1, Yarn 4, and Bun: each does a fresh install and a
lockfile install from an empty client cache, and `npm ci` on a lockfile
recorded against the public registry must be refused, then pass once the
lockfile is rewritten. `make test-docker-go` fails unless the go command
verified the module against the checksum database. The
[testground README](../../testground/README.md) lists the clients and commands.

Docker Registry remains a separate privileged dind check:
`make test-docker-docker`. It runs two clean Docker daemons against an
unqualified Hub image and requires a higher Depsilo request count after the
first pull and a higher cache-hit count after the second, waiting up to 30
seconds for the batched access-log counters. On hosts that resolve
public registries to reserved/private synthetic IPs (for example `198.18.0.0/15`),
Depsilo's outbound safety policy rejects the mirror request; Docker may
silently fall back to Hub and this check correctly fails. Use public DNS for
this network-dependent test; do not weaken the outbound checks. Behind proxy
software in fake-IP mode (`198.18.0.0/15`, and `2001:2::/48` with IPv6
fake-IP on), you can instead set
`DEPSILO_EGRESS_UPSTREAM_PROXY` to the proxy software's HTTP port in the
environment of `make`, for example
`DEPSILO_EGRESS_UPSTREAM_PROXY=http://127.0.0.1:7890 make test-docker-docker`.
The test server reads the tracked `config.example.toml` and takes the variable
from that environment; registries without their own proxy then use it, and
the proxy resolves the fake-IP names
([deployment](../deployment.md#upstream-network-egress)). IPv6 fake-IP from a
unique-local range, such as `fdfe:dcba:9876::/48`, `fd00:6152::/32` or
`fc00::/18`, still fails through the proxy: set the proxy software's IPv6
fake-IP range to `2001:2::/48` or turn IPv6 fake-IP off. The other
real-client targets accept the same variable for package Upstreams.

The tag-triggered release workflow invokes the real-client workflow in release
qualification mode. GitHub release assets wait for all 14 package clients,
Docker OCI, compiler-cache, S3, the long-range v0.9.0 source/Compose contracts,
the fixed v0.9.1 source/image-state contract, and the upgrade from the
previous published release; the ordinary weekly run keeps the smaller
all-package-client matrix.

## Change-to-check matrix

| Changed seam | During iteration | Before handoff |
| --- | --- | --- |
| Backend implementation inside one module | focused `go test` | `make test`; `make check` for normal changes |
| Cache, concurrency, or lifecycle | focused test with `-race` where useful | `make verify` |
| Auth or migration | focused owning-package test | `make verify` |
| First-run setup durability | the setup durability command below the table | `make verify` |
| CLI command, flag, or output | `go test ./internal/cli -run TestName -count=1` | `make check`; `make verify` for daemon or process lifecycle |
| HTTP/package protocol | adjacent Go tests + tagged integration case | `make verify` and relevant `make test-docker-<ecosystem>` when network is available |
| Pure frontend model/manifest | focused Vitest file | `make check` |
| Portal/Admin interaction | focused Playwright file | `make check`; full `make verify` for shared shell/primitives |
| Embedded frontend or release delivery | `make test-ui-production` | `make test-ui-production` plus release checks |
| Storage backend or schema compatibility | focused storage/migration test | `make test-s3`, `make test-v090-upgrade`, `make test-v090-compose-upgrade`, `make test-v091-upgrade`, and `make test-previous-release-upgrade` |
| i18n or UI copy | `make lint-i18n`; the spec that asserts the old text | `make check` |
| Makefile, installer, dev or release scripts | relevant `scripts/test-*.sh` | `make verify-scripts` or `make verify` |
| Dependency versions or release inputs | focused build | `make security` plus release checks |

The setup durability command is:

```bash
go test ./internal/api ./internal/cli ./internal/config ./internal/db \
  -run 'TestSetup|TestOSAtomic|TestBeginDurable' -count=1
```

"Before handoff" names the check to pass before committing a change or
handing it to a reviewer. Whatever the row says, `make verify` is the gate
before a push or pull request.

`make check` runs only the Playwright cases tagged `@smoke`. When changing UI
copy, search `web/e2e` for the old text and run each spec that asserts it:
`npm --prefix web run test:ui -- <spec>`.

## Placement rules

- Put Go behavior tests next to the owning module. Keep `tests/unit/` only for
  cross-module behavior that cannot be expressed through one package's public
  interface.
- `make test-full` is the all-package correctness gate. Add a package to
  `GO_RACE_PKGS` when it begins owning goroutines, shared mutable state,
  streaming lifecycles, or a background scheduler; do not expand race coverage
  merely because a package calls into one of those owners.
- Use `tests/integration/` for observable HTTP behavior requiring a running
  local server and mock Upstream. Do not repeat internal state assertions there.
- Put pure TypeScript state, catalogs, and route-manifest tests in `web/unit/`.
- Use Playwright for rendered behavior: navigation, focus, responsive layout,
  accessibility, requests, loading/error states, and clipboard/download flows.
  Do not launch a browser to test a pure array or function.
- Real-client Docker fixtures prove native client compatibility. They should
  stay small and network-dependent rather than being disguised as unit tests.
  `make test-e2e` leaves its background server running on purpose so the
  matrix can reuse it; after an interrupted or failed run, execute
  `make test-clean` before retrying, otherwise the next attempt can talk to
  the stale server and report misleading client errors.
- A bash test that `make verify-scripts` runs and that sets an EXIT trap
  sources `scripts/exit-guard.sh`, installs the trap as
  `trap 'exit_guard <cleanup command>' EXIT`, and ends with
  `exit_guard_complete` as its last line. Without the guard, bash 3.2 (macOS
  `/bin/bash`) reports success for a test that stopped halfway;
  `scripts/test-exit-guard.sh` fails the gate for a test that omits it.

The [controlled npm/PyPI benchmark](../../testground/README.md#controlled-cache-and-cascade-evidence)
owns disposable nodes and verifies direct/peer traffic with native clients;
it does not reset existing services. Its script contracts run in `verify-scripts`.
The [Python offline acceptance fixture](../../testground/offline-pypi/README.md)
also has offline guard tests in that gate. Its disconnected native install
requires a separately prepared isolated target and is not part of `make verify`;
set activation and offline package serving remain planned.

For changes to local hot reload, run the focused lifecycle and route contracts
before the aggregate script gate:

```bash
bash scripts/test-dev-ui.sh
node scripts/test-vite-proxy-routes.mjs
make verify-scripts
```

## Keep the suite lean

- Test through the module interface and assert observable outcomes.
- When behavior moves behind a deeper interface, replace old shallow tests;
  do not keep both for reassurance.
- Prefer table-driven cases over one test file per input variant.
- Avoid arbitrary sleeps. Hold and release promises/channels or poll a visible
  condition with a bounded timeout.
- A regression test should fail for the original bug and survive unrelated
  implementation refactors.
- Do not put changing public-network data in the offline gates.

`.github/workflows/ci.yml` runs `verify.yml` on every push and pull request to
`master`. The backend job calls the Go targets of `make verify`. The frontend
job runs `make verify-scripts` and calls the commands behind the other targets
directly: the npm scripts for lint, type check, unit tests, bundle budget, and
the full Playwright suite, and `python3 scripts/i18n-audit.py`. CI also runs
the scans of `make security`, `make test-ui-production`, and a Windows CLI
lifecycle job. `make verify` is the local command for the same offline checks.
