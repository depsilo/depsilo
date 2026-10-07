# Cascade deployment (multi-level cache)

Cascade lets one Depsilo node fetch through another Depsilo node instead of
going to the internet directly. The child keeps its own cache, the parent
keeps a second-level cache, and clients still talk only to their nearest
node. This turns "every device downloads the same artifact over the WAN" into
"the first device fills the parent cache; later devices read from it at LAN
speed."

The design and its boundaries are recorded in
[ADR-0006](adr/0006-cascade-cache-peering.md). This page is the operator
guide.

## Topology

```text
laptop (child) ─┐
                ├─► home server (parent, has the fast WAN path) ─► registry
build box (child)┘
```

Every node still runs its own database, policy, and cache. Cascade only shares
cache results. A chain of three or more levels works when each intermediate
node has the same upstream source URL configured through its own peer; a node
without a matching binding fetches directly.

## Parent configuration

The parent must know which secret its children present. Everything else stays
as it is; the parent keeps proxying for its own clients normally.

```toml
[cascade]
enabled = true
# Shared across the mesh. Use a random value of at least 16 bytes; 32 is better.
token = "replace-with-a-random-shared-secret"
# Optional. Empty generates and persists an identity on first start.
# instance_id = ""
max_hops = 4
max_ttl = "168h"
# Needed only when a peer is reached over plain HTTP on a non-loopback host.
allow_insecure_http = false
```

The relay endpoint is `/_depsilo/relay/v1`. It requires the shared token,
rejects requests that re-enter this node, and refuses to fetch private
addresses unless the target is one of this node's configured upstreams.

## Child configuration

Define at least one peer, then bind one or more upstreams to it with `via`:

```toml
[cascade]
enabled = true
token = "replace-with-a-random-shared-secret"

[[cascade.peers]]
name = "home"
url = "http://192.168.1.10:23333"   # requires allow_insecure_http = true

# The URL is still the real upstream origin: cache keys, provenance, and
# health accounting keep their normal meaning. Only the egress path changes.
[[npm.upstreams]]
name = "via-home"
url = "https://registry.npmjs.org"
priority = 1
via = "home"

# Keep a direct path so an offline parent degrades to the internet instead of
# failing. The health-tracked selector only uses it while the peer is down.
[[npm.upstreams]]
name = "direct"
url = "https://registry.npmjs.org"
priority = 2
```

Existing installations can bind an upstream to a peer in
**Admin → Upstreams** instead of editing `config.toml`; the peer list itself
stays in the configuration file because it carries addresses and secrets.
The **Admin → Upstreams → Cascade** tab is a read-only view of the cascade
state: this node's role and instance ID, configured parents, which upstreams
egress through them, and any binding whose parent has been removed from the
configuration.

The same tab edits the cascade section directly on the page — no dialog layer:

- The page reads top to bottom as a live topology (clients → this node →
  parents → upstream), the inline configuration, a tabbed quick-connect card,
  and the upstream bindings table.
- The patch is written atomically into `config.toml`, preserving comments and
  every key outside `[cascade]` / `[[cascade.peers]]`.
- Fields and peer rows are edited inline; a sticky save bar appears only while
  there are unsaved changes and offers **Discard** or **Save**.
- Shared and per-peer secrets are write-only. The form never echoes a stored
  value; it shows whether one exists and offers an explicit clear action.
- Saving validates the whole result before writing. Removing a peer that is
  still referenced by an upstream (in the file or in the database) is refused
  so no upstream silently falls back to direct egress.
- Cascade values are read at startup, so a successful save marks the page
  **pending restart** until Depsilo restarts.
- The API enforces the operator's write role. The page also checks the file
  itself: when the account is read-only or `config.toml` is not writable, the
  fields are disabled and the reason is shown inline. A write that races a
  permission change still fails closed with `409 CONFIG_READ_ONLY`.
- The **Upstreams using a cascade egress** table can rebind an upstream with a
  single select (direct or one of the running peers). That path changes only
  the database binding and applies immediately, without a restart.

The **Quick connect** section generates copy-ready commands from an editable
address field (defaulting to the browser origin):

- Package-manager commands for npm, pip, and Go that point clients at this
  node, plus a link to the full Connect page for the remaining ecosystems.
- A child-Depsilo `config.toml` snippet and a relay probe command, so another
  node can use this one as its parent without leaving the page.
- A parent-Depsilo snippet with the matching `via` binding, a relay probe, and
  a health check per configured parent.

The address field is only a generator; it is kept in browser local storage and
is never written to the server. The snippets use a placeholder for the shared
secret and warn against pointing a node at itself, which the relay rejects
with `508`.

## Operational notes

- **Cache semantics.** The child sends the TTL and metadata/artifact class it
  already uses for the entry. The parent clamps that to `cascade.max_ttl`, so
  a parent cache hit never serves metadata longer than the child's own
  policy allows.
- **Observability.** Relayed entries appear in the cache tables with adapter
  type `relay` (`relay-v1/index/...`, `relay-v1/artifact/...`). The
  `X-Depsilo-Relay-Cache` response header reports `HIT`, `MISS`, or
  `BYPASS`; `X-Depsilo-Instance` identifies the relaying node.
- **Credentials.** Upstream credentials are stripped before crossing a peer.
  If a peer must forward them, set `forward_credentials = true` on that peer
  and treat the peer as fully trusted; it can read every forwarded credential.
- **TLS.** Prefer HTTPS peer URLs behind a TLS reverse proxy. Plain HTTP on a
  LAN exposes the shared token and all cached content to the path; the
  configuration requires the explicit `allow_insecure_http` opt-in for
  non-loopback hosts.
- **Unsupported.** Docker's OCI routes are not cascaded. Only GET/HEAD
  exchanges are relayed; current package protocols use nothing else.
- **Enforcement.** The child applies package rules, quarantine, and
  provenance to what it serves. The parent's relay path is a cache and egress
  accelerator and does not re-evaluate the child's policy.
- **Scale.** Cascade does not parallelize one artifact across multiple WAN
  paths. It removes duplicate WAN downloads and serves cached bytes at LAN
  speed.

## Verification

1. On the parent, confirm `cascade relay enabled` in the startup log and open
   Admin → Upstreams to see the peer list via the API.
2. On a child, request a package twice. The first request logs a relay fetch;
   the second is served from the child cache.
3. Query the parent cache for `adapter_type = 'relay'` rows, or request the
   same target through the relay a second time and check for
   `X-Depsilo-Relay-Cache: HIT`.
4. Misconfigure a loop (bind the parent's upstream back to the child) and
   confirm the request fails with `508` instead of recursing.
