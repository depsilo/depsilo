# T01: release baseline and Docker first-connect gap

Checked on 2026-10-13. This is an implementation baseline, not release
qualification or evidence of an external trial. No Docker client was run here.

## 1. Published, on master, and missing

| Scope | Finding and evidence |
| --- | --- |
| Latest stable | GitHub Releases marks `v0.11.0` latest (published 2026-10-06 13:55 UTC). Peeled tag commit: `9d19b10d2cd9c4fe2ffef0f8893aa46803561416`. `ghcr.io/depsilo/depsilo:0.11.0` and `depsilo/depsilo:0.11.0` both resolved to the OCI index `sha256:e540704f0e2561302a1d3d381782d32ebe73853183703dfa6accce43af59d72a` during this review; this is a resolved digest, not a fresh signature check. |
| Master | Local HEAD and remote `refs/heads/master`: `6c40924dbc298835befa4591d90e02e5057bde33`. There are 18 commits after the release tag, including Cascade peering and its Admin configuration/topology, Admin layout and status work, and installer maintenance (`git log v0.11.0..HEAD`). These are **not in v0.11.0**. The existing untracked research summaries are unrelated and were left alone. |
| Docker service, shipped | A configured `[[docker.registries]]` enables the OCI `/v2/` handler at startup; an empty list does not. Docker Hub is the example in `config.example.toml:288-308`, not an automatic setup default (`internal/server/server.go:661-663,731-743`, `internal/config/writer.go:185-186`). A registry can have username/password/proxy (`internal/config/config.go:394-405`). Manifest/blob requests go through the shared cache and are logged (`internal/adapter/docker/handler.go:106-225,371`). |
| Existing connection UI, shipped | Setup accepts only standard Upstreams, intentionally rejects Docker as a no-op (`internal/api/setup_validation.go:46-60`, `internal/api/setup_test.go:708`). After Setup, Admin's `/admin/connect` shows configuration and polls new package events through `GET /api/v1/admin/onboarding/status`; it distinguishes first request and first hit (`web/src/admin/pages/ConnectProject.tsx:59-88,164-214,369-410`, `internal/api/onboarding.go:60-149`). The anonymous Portal provides Docker Engine/containerd/Podman examples using the browser-visible origin (`web/src/lib/ecosystemData.ts:447-509`, `web/src/lib/packageManagerConfig.ts:1-24`). |
| Configuration authority | Docker registries and extra indexes are `config.toml`-authoritative and restart-managed; Admin ordinary-Upstream CRUD is database-authoritative and cannot edit Docker (`docs/admin-control-plane.md:8-15`). Load precedence is CLI/env > config file > defaults (`internal/config/loader.go:22-33`). Setup publishes the config atomically with mode `0600`, then restarts or asks for supervisor restart (`internal/config/writer.go:193-199`, `internal/api/setup.go:246-305,341-345`). Subsequent Docker edits currently require an operator to edit the TOML and restart Depsilo. Never send stored registry passwords to a browser or public Portal. |
| Observability, shipped | An authenticated Admin can inspect first-request/cache-hit events and access logs. `/v2/` ping alone is not logged as a package request (`internal/adapter/docker/handler.go:76-83`); `docker info` reports local daemon configuration, not a proven Depsilo cache hit. The onboarding event cursor is instance-wide, not client-bound (`internal/api/onboarding.go:117-149`). |
| Tooling, shipped | `depsilo doctor`, `depsilo diagnose`, `scripts/benchmark-install.sh` for npm/PyPI, `make test-docker-docker` for an opt-in privileged real Docker client, and release checks already exist (`internal/cli/doctor.go`, `internal/cli/diagnose.go`, `scripts/benchmark-install.sh:4-34`, `Makefile:211-255`, `docs/release-verification.md`). The current dind fixture pulls an explicit Depsilo host (`testground/docker-docker/Dockerfile:24-25`); it does **not** prove that an unqualified `docker.io` pull uses the configured mirror. The benchmark defaults to three runs, fixes only a top-level package, and explicitly does not reset the server cache or compute savings (`scripts/benchmark-install.sh:5,25-34,206-211`). |
| Actual T03 gaps | No safe first-run/Admin Docker registry enable/edit flow or applied-vs-pending-restart state; no integrated path from that state to client setup and a Docker-specific verification command; no tested unqualified Docker Hub mirror path and second-clean-client cache-hit proof. Docker client errors (address/TLS/auth/fallback) have no targeted troubleshooting in this flow. These are product/evidence gaps, **not** a missing OCI proxy implementation. |

The unreleased commits fall into these candidate-relevant groups:

- Cascade relay, peer egress, configuration and schema v10: `80f18ac`,
  `1548583`, `eab0489`, with associated Admin topology/connect UI in
  `23f48ab`, `9c7b262`, `f440b65`, `f64cc29`. See
  `docs/adr/0006-cascade-cache-peering.md`, `internal/cascade/`,
  `internal/config/cascade_store.go`, `internal/relay/`, and
  `web/src/admin/pages/Cascade.tsx`.
- Admin Overview and management-page UI/operational states: `71c163b`,
  `65d629a`, `0c4cfbb`, `f9fe17f`, `8be5e2e`, `91c3cb5`, `6c40924`,
  plus `46b79ea` for current guides and `2184ee8` for the favicon.
- Installer tag resolution (`4b52201`) and test stabilization (`e0ab2eb`).

Candidate scope is a product decision. Recommend **excluding Cascade and unrelated
Admin redesign from the Docker change review**, but do not claim they can be
left out of a candidate built directly from current master. To release T03
without those commits requires an explicit release branch from v0.11.0 and
selected backports; otherwise they are in the candidate and require their own
qualification. Do not revert the existing master work silently.

## 2. Smallest viable Docker change

1. **Persist one source of truth.** Add a narrow Docker configuration
   read/write operation to the existing serialized `config.Store` in
   `internal/config/` (following its Cascade writer pattern), plus an
   authenticated Admin endpoint in `internal/api/admin/` registered in
   `internal/api/router.go`. Support the initial Docker Hub registry and
   default selection only; retain other operator-defined registries and
   unrelated TOML sections. Validate URL, name/default mapping, proxy, and
   credential handling before atomic publication. Preserve mode `0600` for
   secret-bearing config, including the missing-file case. Return masked
   credentials/write-only updates and `config_writable`, `effective`, and
   `pending_restart`; if an env override applies, show that it blocks the
   saved value. Never advertise a saved registry as active until restart.
2. **Connect the UI.** Extend the authenticated Admin first-connect flow in
   `web/src/admin/pages/ConnectProject.tsx` and the Docker catalog in
   `web/src/lib/ecosystemData.ts` with a conditional server-side enable step,
   explicit Depsilo restart instruction, and an active-state check. Reuse
   `renderManagerTemplate` for the browser-visible origin; explain LAN
   reachability, trusted TLS vs explicit insecure HTTP, daemon restart,
   Docker Hub-only mirror scope, and that BuildKit/containerd may need their
   own configuration. Show an explicit `docker pull <host>/library/alpine:<pin>`
   for connectivity, then an unqualified pull in the mirror-specific test.
   Do not write Docker daemon configuration from the browser. If the anonymous
   Portal renders Docker while disabled, label it as unavailable without
   exposing registry credentials or internal configuration.
3. **Make verification distinguishable.** Reuse the existing Admin onboarding
   cursor and access-log cache result; only Docker events after the current
   baseline count. Do not infer client identity from that instance-wide cursor:
   filter/check a pinned test image and correlate the log timestamp with the
   command before claiming success. Show actionable branches for no incoming
   request, a TLS/address failure, an upstream 401/403, a policy block, and
   Docker's fallback to Docker Hub. Compare two clean client daemons or isolated
   image stores; a second pull from the same daemon proves only local caching.

The Docker backend owner is `internal/adapter/docker/` only if native-client
behavior actually needs fixing; the plan above should not change cache or
OCI protocol behavior without a failing real-client reproduction. Paired
locale changes belong in `web/src/i18n/zh.ts` and `en.ts`.

## 3. Order, checks, acceptance

| Step | Depends on | Focused checks | Observable acceptance |
| --- | --- | --- | --- |
| A. Docker config contract + authenticated API | T01 | `internal/config/*_test.go`; Admin contract tests; `go test ./internal/config ./internal/api/admin ./internal/api -count=1` | Default-off installation enables Docker Hub; config survives restart and keeps permissions/secrets; read-only or malformed config is rejected without changing runtime; saved state says restart required. |
| B. Admin connect + snippets + diagnostic states | A | `make lint-i18n`; focused Vitest and `web/e2e/admin-onboarding.spec.ts` plus Docker/Portal cases | Operator can see whether service is active, copy valid HTTPS/HTTP client instructions, and diagnose a missing request. Existing npm/PyPI onboarding still works. |
| C. Docker mirror and shared-cache proof | A, B | Extend `testground/docker-docker/Dockerfile` or add an isolated dind fixture; `make test-docker-docker`; relevant integration/production UI smoke; then `make verify` for shared config/release seams | An outsider follows the UI; daemon A pulls a pinned Docker Hub image via an **unqualified** name with a fresh local store; logs prove Depsilo received a miss. Daemon B with a different empty store pulls the same digest; logs prove its manifest/blob was served as a Depsilo **hit**. Break the mirror deliberately and verify failures/fallback are recognizable. |

Run the real Docker-client test only on a privileged Docker host; it is not
part of the regular offline `make verify`. Here T01 ran focused Go tests for
Setup validation/persistence, onboarding and Docker caching (PASS, 2026-10-13),
and `bash scripts/test-benchmark-install.sh` (PASS, fake clients only). No
networked Docker pull, UI browser run, signed-image check, trial deployment,
or benchmark measurement was performed for this report.

Revised T03 estimate: **3-5 effective engineering days**, starting no earlier
than 2026-10-14, targeting the minimal flow by 2026-10-20; external-user
observation and Docker access can move acceptance further. T04 needs a separate
benchmark hardening/measurement pass: the existing script is a starting point,
not locked-dependency or server-cold evidence. Core trial recruitment (T02)
can start now on v0.11.0 with npm/Python and does not wait for T03.
