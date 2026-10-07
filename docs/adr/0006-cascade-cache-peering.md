# ADR-0006: Cascade Cache Peering via a Raw Upstream Relay

**Status:** accepted
**Date:** 2026-10-07
**Companion docs:**
- Operator guide: [`docs/cascade.md`](../cascade.md)
- Domain language: [`CONTEXT.md`](../../CONTEXT.md)
- Upstream ownership: [`docs/admin-control-plane.md`](../admin-control-plane.md)

## Context

Operators run Depsilo on more than one machine: a laptop or build box on the
same LAN as a home server, several devices behind one WAN link, or a small
office where one node has the fast upstream path. Each node already caches
independently, so the second device re-downloads bytes the first device
already has. The requested feature is a multi-level cache: a node may fetch
through another Depsilo, and that parent serves from its own cache whenever
possible.

The obvious implementation — point one Depsilo's upstream URL at another
Depsilo's client-facing protocol routes — was tested and rejected. Some
adapters rewrite artifact URLs before caching (PyPI's simple index, for
example). A child re-ingesting the parent's rewritten index resolves
`/pypi/files/...` links against the wrong path and downloads fail, and
APTsigned metadata or npm provenance would have to survive two independent
rewriting and signing passes. Protocol-level chaining also couples peer URLs
to every ecosystem's client route shape.

## Decision

### 1. Cascade is a raw upstream relay, not protocol chaining

A child node sends the exact upstream exchange it would otherwise perform
itself — method, absolute origin URL, content negotiation, and conditional
headers — to the parent's authenticated relay endpoint
`/_depsilo/relay/v1`. The parent fetches that URL through a guarded egress
client, caches the raw response, and streams it back with the origin status
and representation headers.

The child's adapters therefore see a normal upstream response and keep all
protocol rewriting, signed artifact URLs, provenance binding, and package
policy exactly as they are. No adapter knows that a peer was involved.

### 2. The parent cache is the child's freshness shadow

The child's cache manager already knows whether an entry is mutable index
metadata or immutable artifact content, and how long its own freshness window
is. Those two facts travel with the relay request, so the parent applies the
same staleness policy and conditional revalidation instead of guessing from
file extensions. Relay entries live in the shared cache under the `relay`
adapter type (`relay-v1/index/...` and `relay-v1/artifact/...`) and follow the
normal LRU, retention, and storage backends.

Redirects are not followed by the parent. The child follows each hop through
the relay, which preserves Hugging Face's explicit redirect handling and keeps
short-lived signed redirect URLs out of the cache.

### 3. Egress stays guarded on both sides

- The child's peer transport only contacts the configured peer origin with
  the shared cascade token.
- The parent's default relay client only reaches public addresses.
- A private target is allowed only when it matches one of the parent's own
  configured upstream origins, and that upstream's own client performs the
  fetch. A forged source header cannot borrow a private-origin policy for a
  different address.
- Upstream credentials (URL userinfo, `Authorization`, cookies) do not cross
  a peer unless the peer is explicitly configured with
  `forward_credentials = true`; credentials never travel to a redirect or
  artifact host outside the declared origin.
- Chains carry the visited instance IDs and a bounded hop count. Re-entering a
  node, or exceeding `cascade.max_hops`, fails with `508 Loop Detected`
  instead of recursing.

### 4. Peers are infrastructure; bindings are operational state

Peer URLs and tokens are deployment infrastructure and live in `config.toml`
under `[cascade]` / `[[cascade.peers]]`. Which upstream egresses through a
peer is a per-upstream operational decision stored in the database (`via`)
and edited in Admin → Upstreams, so the existing control-plane authority is
unchanged. A multi-level chain connects when an intermediate node has an
upstream with the same source URL configured through its own peer; otherwise
that node egresses directly.

The Admin Cascade tab may edit the peer list and cascade limits, but it does so
by patching the same `config.toml` document through the existing atomic config
writer: comments and unrelated keys survive, tokens stay write-only, the whole
result is validated before the write, and a change takes effect on restart.
The file remains the single authority; the UI is a guarded editor, not a
second source of truth.

## Consequences

- All current GET/HEAD package ecosystems can be cascaded without adapter
  changes. Docker's credential and manifest semantics are out of scope.
- A node that is both a parent and a normal proxy stores relayed bytes under
  the relay namespace in addition to its own adapter cache. Operators should
  size the cache for both roles.
- The parent's package policy is not applied to relayed fetches; the child
  applies its own policy. The parent is a trusted cache and egress
  accelerator, not a second enforcement point.
- There is no shared database, no HA, and no cross-node cache eviction
  coordination. Cascade only shares cache *results*, never control-plane
  state.
- Cascade does not aggregate bandwidth across peers or parallelize one
  artifact across multiple WAN paths. It removes repeated WAN downloads and
  lets LAN clients read at LAN speed; a first-time, uncached artifact is
  still bounded by the single egress path that fetches it.
- A parent that is unreachable is just an unhealthy upstream: put a direct
  upstream after the peer upstream in priority order to fail over.

## Alternatives considered

- **Protocol-level upstream chaining** (configure the parent's package route
  as an upstream URL): rejected after it failed end to end for PyPI and would
  have required per-ecosystem route and provenance work.
- **A transparent HTTP forward proxy**: no cache semantics for
  `Cache-Control`/validators, and HTTPS `CONNECT` tunnels bypass content
  storage entirely.
- **Reusing the parent's adapter cache by calling its own proxy routes**:
  exactly the rewriting/signing mismatch above.
