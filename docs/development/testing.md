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
| Dependency security | `make security` | Yes | Go vulnerability data and npm production audit |
| One real client | `make test-docker-<ecosystem>` | Yes | Native package manager against live Upstreams |
| All real clients | `make test-e2e` | Yes | Scheduled/opt-in package-client matrix |
| Compiler clients | `make test-compiler-cache` | Client-dependent | Installed ccache and sccache against a running service |
| Qualified compiler clients | `make test-compiler-cache-qualified` | No upstream network after client install | Bootstrap a service and credential, then prove ccache and sccache miss/write/remote-hit behavior |
| S3 storage contract | `make test-s3` | Local Docker | Signed known/unknown-length streaming, metadata, listing, deletion, and failed multipart cleanup against pinned MinIO |
| v0.9.0 state reopen | `make test-v090-upgrade` | Go modules if uncached | Build the tagged source, seed durable state, and exercise it offline with the current binary |
| v0.9.0 shipped Compose upgrade | `make test-v090-compose-upgrade` | Privileged Docker, host PID namespace, util-linux `findmnt`, and registry access | Reject a real nested bind mount without changing its external victim, then run the immutable published v0.9.0 image with its exact shipped layout, rotate a weak JWT secret explicitly, and reopen its state with a UID/GID 10001 candidate |
| Development scripts | `make verify-scripts` | No | Make workflows, paired Vite/backend lifecycle, and development proxy coverage |

Docker Registry remains a separate privileged dind check:
`make test-docker-docker`.

The tag-triggered release workflow invokes the real-client workflow in release
qualification mode. GitHub release assets wait for all 14 package clients,
Docker OCI, compiler-cache, S3, tagged-source state-reopen, and immutable
v0.9.0 shipped-Compose upgrade jobs; the ordinary
weekly run keeps the smaller all-package-client matrix.

## Change-to-check matrix

| Changed seam | During iteration | Before handoff |
| --- | --- | --- |
| Backend implementation inside one module | focused `go test` | `make test`; `make check` for normal changes |
| Cache, concurrency, or lifecycle | focused test with `-race` where useful | `make verify` |
| Auth or migration | focused owning-package test | `make verify` |
| HTTP/package protocol | adjacent Go tests + tagged integration case | `make verify` and relevant `make test-docker-<ecosystem>` when network is available |
| Pure frontend model/manifest | focused Vitest file | `make check` |
| Portal/Admin interaction | focused Playwright file | `make check`; full `make verify` for shared shell/primitives |
| Embedded frontend or release delivery | `make test-ui-production` | `make test-ui-production` plus release checks |
| Storage backend or schema compatibility | focused storage/migration test | `make test-s3`, `make test-v090-upgrade`, and `make test-v090-compose-upgrade` |
| i18n | `make lint-i18n` | `make check` |
| Makefile, installer, dev or release scripts | relevant `scripts/test-*.sh` | `make verify-scripts` or `make verify` |
| Dependency versions or release inputs | focused build | `make security` plus release checks |

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

CI calls the same Make targets defined locally. `.github/workflows/verify.yml`
is orchestration; the `Makefile` remains the command interface.
