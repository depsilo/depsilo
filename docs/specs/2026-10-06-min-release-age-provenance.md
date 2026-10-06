# Minimum Release Age Provenance Binding

> Design snapshot, 2026-10-06. Implements [ADR-0005](../adr/0005-enforcement-first-until-1-0.md)
> build order item 1. `PRODUCT.md` and the ADRs remain the authority for
> product intent and decisions; this file records the intended implementation.

## Implementation status

- 2026-10-06: the npm slice is implemented — the packument `time[version]`
  travels in the authenticated tarball token, an enabled npm threshold is
  accepted at startup, missing or legacy provenance fails closed, and the
  capability summary reports npm as source-bound.
- 2026-10-06: the PyPI slice is implemented — the adapter negotiates the
  PEP 691 JSON simple index, signs `upload-time` and the declaring upstream
  into the artifact reference, renders the legacy HTML representation for
  clients that do not accept JSON, and fails closed on HTML-only upstreams.
- 2026-10-06: the Composer slice is implemented — the dist handler uses the
  p2 metadata entry's own `time` and a source identity derived from the
  declared artifact, and the policy accepts positive Composer thresholds.
  Composer remains best-effort because clients can fall back to the original
  dist URL after a 451; hard enforcement needs egress control or metadata
  filtering.
- 2026-10-06: the NuGet slice is implemented — the flat-container gate resolves
  the v3 registration index's `published` time through the configured upstream
  (including paginated registration pages), treats the 1900 unlisted sentinel
  as missing provenance, and the policy accepts positive NuGet thresholds.
- The remaining ecosystems follow the rollout order below.

## Goal

Restore the minimum-release-age gate by binding three facts to one source
identity per request:

1. the artifact coordinate (ecosystem, package, version),
2. the publish timestamp,
3. the upstream that will serve the artifact bytes.

The gate may only operate for an ecosystem once that binding is end to end.
Until then positive thresholds stay rejected at startup.

## Current gap

- `internal/quarantine/resolvers` queries fixed public registries
  (`registry.npmjs.org`, `pypi.org`, ...). The comment on `NewRegistry` states
  these resolvers cannot govern an arbitrary configured Upstream.
- `internal/quarantine/lookup.go` caches by `(ecosystem, package, version)`
  with no source identity, so two mirrors with different publish times for the
  same version collide.
- `quarantine.Policy.sourceProvenanceBound` is the seam for a future
  composition root; it is nil, and `NewPolicy` rejects every positive
  threshold.
- npm already binds metadata to artifact bytes: `PreparePackument` records
  `upstream.ProvenanceSourceID()` and the declared tarball digest, and
  `handleSignedTarball` resolves that exact source instead of running failover
  again. The signed token carries package, version, source, and digest — but
  not the publish time.

## Binding model

A `Provenance` record travels with the authenticated artifact reference:

```go
type Provenance struct {
    SourceID  string    // upstream.ProvenanceSourceID()
    PublishAt time.Time // from the same metadata document that declared the artifact
}
```

Rules:

- **Same document.** `PublishAt` must be extracted from the metadata document
  whose artifact URL produced the signed reference. A separately resolved
  public-registry timestamp is not sufficient.
- **Same source at fetch.** The artifact request must resolve the same
  `SourceID` (`ResolveProvenanceSource`); failover must not silently substitute
  another source for a quarantined version.
- **Fail closed on missing provenance.** If `SourceID` is empty or `PublishAt`
  is zero while a threshold is enabled for that ecosystem, the request is
  refused with `QUARANTINED` and a reason naming the missing binding.
- **Threshold still applies only when the operator enables it.** Defaults stay
  at zero; the gate is opt-in per ecosystem.

## npm first slice

npm is first because the source-bound metadata → artifact path already exists.
No resolver call is needed on the hot path: the timestamp is carried in the
authenticated token.

### Changes

1. `internal/adapter/npm/provenance.go`
   - parse the packument `time` map while preparing each version;
   - add `PublishAt` to `preparedTarballReference`;
   - include the timestamp (Unix seconds) in the authenticated token payload;
   - keep the token length bounded and reject unknown/legacy payload versions;
   - a version whose `time[version]` is missing or malformed gets no publish
     time and cannot satisfy an enabled gate.
2. Bump the npm metadata cache representation so a prepared packument from the
   old format is never signed with an empty timestamp. Either a format version
   in the cache key or an explicit placeholder field is acceptable; the
   requirement is that legacy entries fail closed rather than silently
   producing unbound tokens.
3. `internal/adapter/npm/handler.go`
   - `handleSignedTarball` passes `Provenance{SourceID: claims.Source,
     PublishAt: claims.PublishAt}` into the gate;
   - metadata serving stays unchanged when the gate is disabled.
4. `internal/adapter/quarantine.go`
   - add `QuarantineGateWithProvenance(...)` alongside the existing gate;
   - the scoped checker is used through a small optional interface so adapters
     without provenance keep calling `Check` unchanged.
5. `internal/quarantine/checker.go`
   - add `CheckWithProvenance(ctx, ecosystem, pkg, version, clientIP, prov)`;
   - when a provenance record is present, skip the registry `Lookup` and use
     `prov.PublishAt`; the source identity is validated by the adapter before
     the call;
   - record the source ID and publish time in the quarantine event reason so
     the decision is auditable without a schema change.
6. `internal/quarantine/policy.go`
   - `NewPolicy` accepts a `sourceProvenanceBound func(ecosystem string) bool`;
   - positive thresholds are accepted only for bound ecosystems; every other
     positive threshold keeps the existing startup rejection with an
     ecosystem-specific message;
   - `server.go` binds `npm` only in this slice.
7. Capability reporting (`/admin/capabilities/summary`) must stop reporting the
   whole gate as `safety_disabled` once npm is bound: report per-ecosystem
   `bound` / `unsupported` states so operators can see exactly which
   ecosystems can enforce.

### Acceptance tests

- `PreparePackument` extracts `time[version]` and round-trips it through the
  signed token; missing or malformed timestamps produce no publish time.
- One mock npm upstream: a version published `now` is blocked with threshold
  `7d`; a version published `30d` ago is served; threshold `0` skips the gate.
- Two mock upstreams with the same package/version and different timestamps:
  the decision follows the source named in the signed token; the other source
  cannot make a young artifact look old.
- A legacy prepared reference without a timestamp is refused with a reason
  that names the missing binding.
- Startup accepts `npm: 7d` and still rejects `pypi: 3d` until PyPI is bound.
- Existing npm metadata, tarball, cache, and policy tests stay green.

## Rollout after npm

1. **PyPI** — `upload_time_iso_8601` from the JSON API served by the configured
   index; the same index must also declare the artifact URL.
2. **Cargo / Composer / NuGet / RubyGems** — source-bound metadata endpoints
   where the registry exposes a version-level publish time.
3. **Last-Modified ecosystems** (Maven, Conda, Helm, Alpine, Docker) — the HTTP
   `Last-Modified` of the artifact response from the same source. This is a
   weaker claim; it requires an explicit operator acknowledgement before a
   positive threshold is accepted, and the capability summary must label it
   `approximate`.
4. **Go and APT** — remain without a gate: Go has no publish-time authority
   and APT has no per-version timestamp.

## Risks and mitigations

- **Mirrors with stale or missing `time` maps.** The gate fails closed with a
  clear reason; operators can disable the gate or select a source that
  publishes timestamps. The capability summary exposes the per-upstream state.
- **Cache format drift.** Old prepared references must never be signed with an
  empty timestamp. Bump the representation and test the legacy path.
- **Multiple upstreams.** The signed token pins one source. A future
  multi-source retry may only re-evaluate the gate for the newly selected
  source; it must not reuse another source's timestamp.
- **Token growth.** The timestamp adds a bounded fixed-width field; the
  existing length limits stay in force.
- **Audit volume.** Quarantine events keep their bounded reason text; the
  source ID is truncated to a stable prefix when recorded.

## Out of scope

- Changing cache strategy, upstream selection, or the package-rules engine.
- A behavioral-analysis gate.
- Any gate for ecosystems whose source cannot prove a timestamp.

## Definition of done

- `min_release_age_enabled = true` plus `npm: <positive>` starts cleanly and
  enforces at tarball fetch.
- Every enforcement decision names the bound source and publish time.
- PyPI and the remaining ecosystems are either bound with tests or explicitly
  rejected at startup with an actionable message.
- README, `config.example.toml`, capability summary, and CHANGELOG describe the
  real state per ecosystem instead of a blanket "safety-disabled".
