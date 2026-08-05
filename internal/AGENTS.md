# Backend guide

These instructions apply under `internal/`. Read the root `AGENTS.md` first.

## Find the owner

- `server`: constructs and closes the runtime; keep lifecycle ordering here.
- `config`: config schema, precedence, validation, and durable file updates.
- `db`: SQLite schema, migrations, and repositories.
- `adapter`: protocol-specific request parsing, keys, TTLs, and rewrites.
- `cache`: shared streaming hit/miss behavior and storage interface.
- `upstream`: live pools, selection, health, and registry mutation.
- `api`: public/Admin HTTP interfaces and permission groups.
- `quarantine`, `blocklist`, `tamper`, `rules`, `security`: supply-chain
  decisions with distinct semantics; do not collapse their audit behavior.

The runtime flow is documented in `docs/development/architecture.md`.

## Backend invariants

- Stream large responses and close every body on all paths.
- Preserve native-client signature and checksum semantics.
- Cache keys must isolate ecosystems, projects, extra indexes, and variants.
- A cache hit must not contact the Upstream; an inflight follower must observe
  the same committed representation as the leader.
- Fail closed for authentication, authorization, config validation, and
  revocation. Preserve documented availability behavior for transient Upstream
  and cache-storage failures.
- Ordinary Upstreams are database-authoritative after bootstrap. Docker
  registries and extra indexes remain config-authoritative.
- SQLite and process-local coordination imply one active instance per state
  directory/database.
- New background work must be owned by the server runtime and stop on context
  cancellation.

## Tests

Keep behavior tests beside the owning package. Use the interface as the test
surface and avoid exporting internals only for tests.

```bash
go test ./internal/<module> -run TestName -count=1
go test -race ./internal/<module> -count=1
make test-integration
make verify
```

Protocol changes also need the matching `make test-docker-<ecosystem>` when a
networked real-client run is in scope. See `docs/development/testing.md`.
