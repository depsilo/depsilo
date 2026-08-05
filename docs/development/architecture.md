# Architecture

Depsilo ships one Go process with an embedded React application. The backend is
organized around protocol adapters feeding a shared cache and policy path; the
frontend is split into an anonymous Portal and authenticated Admin surface.

## Startup flow

```text
cmd/depsilo/main.go
  -> internal/cli                 command parsing and lifecycle
  -> internal/server              composition root
  -> config + SQLite + storage    durable state
  -> upstream registry/pools      live routing state
  -> adapters + API router        HTTP interfaces
  -> background runtimes          health, retention, security, rollups
```

`internal/server/server.go` owns construction and shutdown ordering. Avoid
creating runtime dependencies inside handlers when they belong in this
composition root.

## Package request flow

```text
package client
  -> Gin route / project request scope
  -> ecosystem adapter
  -> package rules and enabled supply-chain checks
  -> cache.Manager
       -> hit: storage + SQLite metadata
       -> miss: selected Upstream -> streamed response -> durable cache commit
  -> access/audit observation
```

The adapter interface hides protocol-specific URL parsing, metadata rewriting,
cache-key construction, and TTL choices. `cache.Manager` owns shared hit/miss,
streaming, coalescing, stale, retention, and immutable-artifact behavior.
`internal/upstream` owns live selection and health; adapters should not grow a
second selection implementation.

Large bodies must remain streamed. Metadata formats carrying repository
signatures or checksums must preserve the bytes required by the native client.

## HTTP surfaces

- Standard ecosystem routes are path-prefixed and are also registered under
  project-scoped paths where supported.
- Docker Registry uses the OCI `/v2/` protocol and is not a normal path-prefix
  adapter.
- PyPI-compatible extra indexes use separate routes and cache identities; they
  are not ordinary Upstream rows.
- `/api/v1` contains public, setup, auth, and Admin JSON interfaces.
- `/` and `/admin/*` serve the embedded SPA. Machine and package routes must
  fail as machine routes rather than silently returning `index.html`.

The self-describing route catalog at `/api/v1/discover` is preferable to
duplicating endpoint tables in new documentation.

## State authority

| State | Current authority | Runtime behavior |
| --- | --- | --- |
| Ordinary active Upstreams | SQLite | Registry publishes updates to subsequent requests |
| Docker registries and extra indexes | `config.toml` | Restart-managed |
| Editable settings | `config.toml` | Some apply immediately; others require restart |
| Users, tokens, rules, policy, audit | SQLite | Handler-specific live behavior |
| Package objects | Local filesystem or S3 | Coordinated by cache metadata in SQLite |
| Compiler cache objects | Separate local/S3 area | Separate capacity and credentials |

See [Admin control plane](../admin-control-plane.md) before changing the
configuration/database seam.

## Frontend ownership

`web/src/App.tsx` first applies the setup gate, then dispatches `/admin/*` to
Admin and every other browser route to Portal.

- Portal is anonymous onboarding and service status. It must not expose Admin
  records or operational history.
- Admin is the authenticated control surface. Routes come from
  `web/src/admin/routes.ts`; do not duplicate their paths in navigation code.
- TanStack Query owns server state. Mutation and stale-data behavior should be
  tested through visible outcomes, not React implementation details.
- Chinese and English keys are a parity-checked interface.

## Architectural constraints

- The supported runtime is single-instance. Process-local locks and budgets are
  not distributed coordination.
- SQLite is the only current database implementation.
- General artifact hosting is future intent, not permission to overload proxy
  interfaces. Record the new seam and migration contract in an ADR first.
- `PRODUCT.md` is newer than ADR-0004 on product direction, but the accepted ADR
  remains architectural evidence until explicitly superseded.
