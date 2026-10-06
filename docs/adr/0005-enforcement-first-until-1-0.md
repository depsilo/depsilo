# ADR-0005: Enforcement First Until 1.0, and a Decided Entitlement Boundary

**Status:** accepted
**Date:** 2026-10-06
**Amends:** [ADR-0004](./0004-supply-chain-enforcement-layer.md) — keeps its
general-purpose-repository non-goal in force for the pre-1.0 horizon and
replaces its open question about the Pro tier with a decided boundary.
**Reconciles:** `PRODUCT.md` positioning, future-direction, and commercial
statements.
**Companion docs:**
- Product intent: [`PRODUCT.md`](../../PRODUCT.md)
- Historical strategy: [`docs/DIRECTION.md`](../DIRECTION.md)
- Competitive record:
  [`docs/research/2026-06-30-competitive-landscape.md`](../research/2026-06-30-competitive-landscape.md)

## Context

Three unresolved questions were taxing every roadmap decision:

1. **Scope conflict.** ADR-0004 (2026-06-30) repositioned Depsilo as a
   supply-chain enforcement layer and treated a general-purpose artifact
   repository as a non-goal. `PRODUCT.md` later called that repository a
   *confirmed future direction*. Both documents were current authority for
   different readers, so architecture work could not start without
   re-litigating the conflict.

2. **Capability gap behind the positioning.** The enforcement story is the
   stated differentiator, but its flagship primitive — minimum release age —
   is safety-disabled at startup until artifact source and timestamp
   provenance are bound end to end. Known-malicious blocking is guaranteed for
   six ecosystems; PyPI and RubyGems are deliberately excluded. Client-side
   cooldowns (pnpm 11, npm 11.10, uv) are making the idea mainstream, so the
   window for a proxy-side implementation is narrowing.

3. **Undecided entitlement boundary.** `PRODUCT.md` said the commercial model
   was undecided, while `internal/entitlement` already grants a 14-day trial
   and a paid key access to multi-project workspaces and the runtime
   per-project SBOM export. Operators and contributors could not tell which
   capabilities are open source by design.

The project also has a healthy release chain again (v0.10.1 published with
signed artifacts and green qualification), so scope decisions — not release
mechanics — are the current bottleneck.

## Decision

### 1. Until 1.0, Depsilo is an enforcement layer plus cache

Every roadmap item must answer one question: *does this only matter because
Depsilo sits on the dependency request path?* The 14 standard ecosystems plus
the Docker OCI route stay fixed. Cache, storage, upstream health, and the
Admin/Portal surfaces are supporting infrastructure for enforcement, not a
separate product direction.

### 2. A general-purpose artifact repository is post-1.0 and not yet committed

`PRODUCT.md` now records the repository direction as a **candidate post-1.0
direction**, not a commitment. Before 1.0 there is no hosted publishing,
repository management, format expansion, or migration promise. Reopening the
direction after 1.0 requires a new ADR that supersedes ADR-0004 explicitly.
ADR-0004's non-goal remains in force until then.

### 3. The entitlement boundary is decided; pricing is not

**Open source (no license):** proxying and caching for the supported
ecosystems, upstream management, minimum release age, known-malicious
blocklist, OSV scanning, package rules, quarantine approvals, audit and access
logs, webhooks, Prometheus metrics, read-only MCP, compiler cache, and the
`backup` / `doctor` / `diagnose` CLI surface.

**Pro (14-day trial or paid key):** multi-project workspaces and the runtime
per-project SBOM export. Future compliance-report generation that depends on
the multi-project surface belongs here too.

Pricing, packaging, support terms, and any `$99 lifetime` framing remain
undecided and must not be presented as durable product truth. Governance
primitives stay open source because a self-hosted enforcement product cannot
ask buyers to audit a closed decision path.

### 4. Non-goals for the next six months

- In-product SSO / LDAP / SAML / RBAC. Recommend oauth2-proxy, Authelia, or
  Pomerium in front of Depsilo instead.
- Multi-node HA, PostgreSQL, and shared-storage clustering. SQLite plus local
  or S3 object storage is the supported single-instance deployment.
- New ecosystems beyond the 14 standard routes plus Docker OCI.
- Scanning-depth competition with Snyk / Fossa / Socket.
- Native mobile clients and a hosted/SaaS control plane.
- Telemetry, analytics, or any phone-home behavior.

### 5. 1.0 exit criteria

All of the following must be true before `v1.0.0`:

1. Minimum release age is restored with artifact-source and timestamp
   provenance bound end to end for every ecosystem where the gate is enabled.
2. The guaranteed malicious-package dataset covers npm, PyPI, Cargo,
   RubyGems, Composer, NuGet, Go, and Maven, or permanent exclusions are
   documented with a compensating control.
3. A freeze / golden-snapshot MVP exists: promote the current cache to a
   snapshot, export/import it, and serve a snapshot-only mode.
4. CRA-mode SBOM export carries purl, SHA-256, supplier, license, and
   dependency relationships, is signable, and has a technical-file preset.
5. Release evidence is current on the 1.0 candidate: signed artifacts, S3
   contract, upgrade/rollback rehearsals, and the 14-ecosystem real-client
   suite.
6. A configuration and HTTP API compatibility policy is published with an
   explicit deprecation window.

### 6. Build order until 1.0

1. Minimum release age provenance binding (v0.11 flagship).
2. CRA-mode SBOM export.
3. Malicious-dataset coverage closure for PyPI and RubyGems.
4. Freeze / golden-snapshot MVP.

Helm, SIEM-grade audit routing, threat-model documentation, and Dashboard
polish follow after these four. They are adoption work, not differentiation.

## Consequences

**Positive:**
- Scope and open-source boundaries stop being re-litigated per feature.
- The roadmap test is mechanical: only a request-path proxy can do this.
- The Pro boundary is defensible next to Artifact Keeper's OSS surface:
  enforcement primitives stay open; the paid trigger is team scale
  (multi-project) and the compliance artifact it produces.
- Release work can focus on the four enforcement deliverables that define 1.0.

**Negative / accepted:**
- Some prospective users want a general-purpose registry; they are told to use
  Artifact Keeper or Nexus and optionally put Depsilo in front of it.
- The Pro tier remains thin compared with commercial artifact platforms until
  the compliance-report surface lands.
- Leaving pricing undecided still blocks a durable sales motion; that is a
  deliberate separation between entitlement (decided here) and packaging
  (not decided here).

**Engineering implications:**
- The safety-disabled startup rejection in `internal/quarantine` is now a
  release blocker rather than an accepted limitation.
- Provenance work must prefer the artifact's actual configured Upstream over a
  public registry resolver, and must fail closed when the source cannot prove
  the timestamp.
- Per-project SBOM export stays behind entitlement; the open runtime surface
  must not silently grow a second SBOM path.

## Alternatives considered

**Commit now to the general-purpose artifact repository.** Rejected: it
re-enters a space Artifact Keeper already occupies with 45+ formats, SSO/RBAC,
and IaC, while Depsilo's own enforcement flagship is still disabled.

**Remove the Pro tier entirely.** Rejected: multi-project workspaces and
runtime per-project SBOM export are a real boundary, and removing them would
leave no funded path while weakening nothing about the open-source governance
surface.

**Keep both authorities and decide case by case.** Rejected: the visible cost
is zero and the invisible cost is a re-decision on every roadmap item, which
is what prompted this ADR.
