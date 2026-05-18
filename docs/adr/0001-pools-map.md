# 0001 — Pools as a map, with an explicit Ecosystem order

## Context

Before this change, the upstream `*Pool` for each of the 12 ecosystems (PyPI, APT, npm, Go, Cargo, Maven, RubyGems, Composer, NuGet, Conda, CRAN, Helm) lived as **12 separate named fields** on three structs:

- `api.Deps` (passed to `RegisterRoutes`)
- `public.StatsHandler` (and its `NewStatsHandler` constructor signature)
- `admin.DashboardHandler` (and its `NewDashboardHandler` constructor signature)

Adding a new ecosystem required touching all three places plus the `server.go` Deps-construction site — roughly **8 files of fan-out for what is logically a single fact** ("there is one more ecosystem"). The fan-out had already mutated three times in the past quarter (npm → Cargo → all of Maven/RubyGems/Composer/NuGet/Conda/CRAN/Helm), and audit `2026-05-18-code-and-tiering-review.html` flagged it as the single highest-leverage refactor in the codebase.

## Decision

Represent pools as `map[string]*upstream.Pool` keyed by ecosystem name, and carry an explicit `Ecosystems []string` alongside it for deterministic iteration order.

```go
type Deps struct {
    // ...
    Pools      map[string]*upstream.Pool
    Ecosystems []string // ordered ecosystem names; defines iteration order in UIs
    // ...
}
```

`server.go` is the single source of truth for both. `StatsHandler` and `DashboardHandler` consume them; they no longer name individual ecosystems in their type definitions.

Docker is intentionally **not** in the pools map. Its adapter (`internal/adapter/docker`) handles upstreams internally and does not participate in the pool/selector machinery, so leaving it outside this abstraction matches reality.

## Why not just a map (no order list)?

Two reasons.

1. **Map iteration in Go is randomised.** The Stats / Dashboard endpoints emit ordered JSON arrays of upstreams, and the Portal/Admin UIs render them in that order. Random order would surface as flickering UI between requests.
2. **Sorting the keys alphabetically** (apt, cargo, composer, conda, cran, go, helm, maven, npm, nuget, pypi, rubygems) would change the user-visible ordering from the historical "pypi, apt, npm, go, ..." order without buying anything.

Keeping the order list pinned in `server.go` retains the existing UI order while still collapsing the 8-file fan-out to a single source.

## Consequences

- Adding a 13th non-Docker ecosystem now touches a single file (`internal/server/server.go`) — append to `ecosystems` and to `Ecosystems`. The route registration, pools map, Deps wiring, stats output, and dashboard output all follow automatically.
- The `Pools` map is sparse-tolerant: handlers must check `if pool == nil` before use. In practice every ecosystem listed in `Ecosystems` has a pool, so the nil check is defensive.
- Docker remains an exception, hand-wired in `server.go`. If that exception ever needs to participate in stats aggregation, it joins the same map.
