# T03 Docker first-connect acceptance, 2026-10-13

Status: **not accepted**. Local contracts and production UI passed, but a
real Docker Hub mirror miss and a second clean client's Depsilo cache hit have
not been demonstrated. An independent user's guided connection has not been
observed. This is a working-tree candidate, not a released version.

## Acceptance results

| Criterion | Result | Evidence |
| --- | --- | --- |
| Saved Docker Hub configuration and restart distinction | Contract passed; live operator flow not yet observed | `go test ./internal/config ./internal/api/admin ./internal/api -run 'TestDockerBootstrap|TestOnboardingRoutesRequireAdminCapabilities' -count=1` passed. Covers private atomic write, existing config preservation, idempotence, effective versus saved state, read-only file, environment override, response redaction, and read/write route permissions. |
| Admin guidance and ordinary onboarding regression | Automated UI passed | Prior `make verify` passed (305 Playwright cases, 1 skipped), including Docker selection and npm/PyPI first-request flows; `make test-ui-production` passed against the Go binary's embedded frontend on this acceptance run. These are mocked onboarding interactions, not evidence of a successful real Docker Hub pull. |
| Service reachable and endpoint protected | Passed on isolated preview | `GET /ready` returned ready at `localhost:18348`; anonymous `GET /api/v1/admin/docker/bootstrap` returned 401; OCI `/v2/` ping returned 200. The preview uses `config.example.toml`, where Docker Hub is already configured; it does not exercise the default-off enable button. |
| First fresh Docker client pulls through Depsilo | **Failed in this environment** | Built `testground/docker-docker` for `http://host.docker.internal:18348`; privileged dind ran unqualified `docker pull alpine:3.18`. Client returned success, but Depsilo recorded `GET /v2/library/alpine/manifests/3.18` as 502 (`unsafe Docker dial target`). Public stats stayed at `total_requests=0, hit_count=0` before and after. This is a Docker Hub fallback, not a proxy success. |
| Second clean client receives a Depsilo cache hit | **Not run** | The first client did not seed the Depsilo cache. Repeating a pull cannot satisfy the hit criterion until a real first miss succeeds. |
| Actionable failure signals | Partially passed | Server logs identify the blocked upstream dial and the Admin error event can point to Access Logs. A successful Docker command alone is misleading: it can mean daemon fallback, so verify the server's manifest request and subsequent cache hit. Address/TLS/401/403 branches are described in the UI, but have not been observed with a real external operator. |
| Independent user follows the instructions | **Not observed** | Requires a person who did not build the flow and an environment with a trustworthy Docker Hub network path. |

## Environment and diagnosis

- Acceptance host: macOS, OrbStack Docker Engine 29.4.0. The isolated preview
  at port 18348 uses its own `.test-e2e-state` database and cache; the existing
  service on port 23333 was not modified.
- Default DNS resolved `registry-1.docker.io` to `198.18.1.10`, which the
  Docker adapter correctly refuses as unsafe. Explicit DNS queries to
  `1.1.1.1` and `8.8.8.8` returned different addresses; a TLS probe to one
  returned address timed out. Those answers cannot justify relaxing the SSRF
  check or pretending the registry was reached.
- SQLite access-log evidence from the isolated instance: `docker`, status
  `502`, key `docker/dockerhub/manifests/library/alpine/3.18`. `GET /v2/`
  alone proves reachability, not a cacheable image fetch. The public stats
  exclude this failed request from `today.total_requests`.

## To complete acceptance

1. On a separate host with reliable public DNS or a trusted registry egress
   proxy, run `make test-docker-docker` with a free `PORT`. It runs two clean
   dind daemons. Capture the manifest miss and subsequent Depsilo hit, not
   merely two successful `docker pull` exits. Use `make test-clean` after the
   test, only for that test's owned state.
2. Have an independent operator use `/admin/connect` on a default-off instance
   to enable Docker Hub, restart Depsilo, configure Docker Engine, run the
   explicit-address and unqualified-image checks, and report confusing steps.
   Record the instance version, config state before/after restart, timestamps,
   manifest request outcome, and the second clean client's hit.
3. Exercise the address, certificate, authentication, client-mirror/fallback,
   and policy-error branches in that environment. Do not count this work item
   accepted until the real miss-to-hit and independent-operator criteria pass.
