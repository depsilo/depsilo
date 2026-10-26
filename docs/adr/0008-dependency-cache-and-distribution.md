# ADR-0008: Prioritize Dependency Caching and Distribution Across Networks

**Status:** accepted
**Date:** 2026-10-26
**Partially supersedes:** the product positioning and roadmap priorities in
[ADR-0003](0003-supply-chain-control-point.md) and
[ADR-0004](0004-supply-chain-enforcement-layer.md), and Decision 1's
enforcement-first criterion and Decision 6's build order in
[ADR-0005](0005-enforcement-first-until-1-0.md).
**Companion documents:** [PRODUCT.md](../../PRODUCT.md),
[CONTEXT.md](../../CONTEXT.md), [cascade design](0006-cascade-cache-peering.md).

## Context

The founder clarified the original problems Depsilo should solve:

1. Companies repeatedly download public dependencies across expensive or
   congested internet links.
2. Isolated networks need dependencies and software packages prepared elsewhere,
   carried through an approved transfer process, and updated incrementally.
3. Homelabs and CI machines running different projects should reuse nearby
   caches and parent caches instead of repeatedly fetching from public Upstreams.

The enforcement-first positioning in ADR-0003/0004/0005 made shared caching
supporting infrastructure and admitted roadmap work mainly when it required
request-path enforcement. That criterion underweights the first and third
problems and excludes essential offline preparation and delivery work.
Supply-chain controls remain necessary for trustworthy dependency delivery.

## Decision

Depsilo is a **self-hosted dependency cache and distribution service for company
networks, isolated networks, and homelabs**. Its primary outcomes are reuse across
machines, less repeated internet traffic, dependable dependency availability,
and manageable delivery across network boundaries. Security, identity, audit,
and project attribution support those outcomes.

### Current capability and planned work

Shared caching and online cascade are implemented. Each node owns its database,
cache, and policy; peers share cache results under ADR-0006. Docker OCI remains
outside cascade support. A first uncached download remains limited by its
Upstream path, and the parent's relay does not apply the child's package policy.

Artifact-inclusive offline transfer and incremental import are **planned, not
shipped**. Current golden snapshots contain identities and hashes, not artifact
bytes; CLI backups preserve instance configuration and database state while
excluding cached artifacts. Neither proves a complete offline installation.
See [snapshot limitations](../supply-chain.md#snapshots-and-tamper-detection) and
[backup scope](../compatibility.md#database-and-backups).

### Offline delivery contract to establish

The first implementation must define a bounded dependency set for an explicit
Ecosystem and target environment. It should use native package-manager resolution
where possible and include the required artifact bytes and matching metadata,
including transitive dependencies within its declared scope. Target OS,
architecture, runtime, and source-built dependencies affect completeness; a
successful download on the preparation machine alone is insufficient evidence.

The intended flow is preparation, completeness checking, export, approved
physical or administrative transfer, verified import, and native client use on
the isolated network. Delivery must preserve package signatures and provenance,
define trusted bundle verification, and keep instance credentials out of package
transfer bundles. Import needs explicit failure recovery and repeatable behavior
without exposing a partially imported dependency set as ready.

Start with a complete transfer bundle. Incremental delivery then compares its
required content with a declared target baseline inventory and transfers missing
whole objects with the corresponding metadata. A missing or incompatible
baseline must be reported. Byte-level patches are not required for the first
incremental implementation. An export based only on file modification times or
an unverified copy of the cache directory does not satisfy this contract.

Dependency sets promised for offline use need explicit retention and presence
checks beyond ordinary evictable caching. Acceptance requires an actual package
manager installing in the declared target environment with a clean client cache
and internet access blocked. Report missing content and unsupported installation
steps; do not silently restore connectivity or weaken verification to pass.

The initial Ecosystem, target matrix, bundle format, trust model, storage/import
design, and version compatibility will be specified separately before
implementation. This ADR sets the product direction, not a new wire format or
an implementation-complete claim.

### Development and evidence order

1. Prove shared-cache and cascade reuse with multiple clean clients, including
   concurrent CI requests and a second child reading the parent's filled cache.
2. Complete one bounded offline installation workflow, including artifact
   retention, export, verification, import, and disconnected client acceptance.
3. Add incremental delivery against a known target inventory, with missing-base,
   interrupted-transfer, and repeated-import recovery.
4. Expand to further supported Ecosystems and target networks according to
   observed use, preserving native-client verification for each declared scope.

Measure actual internet traffic at the egress point; summing hits across cascade
levels double-counts reuse. Compare installation time against both cold and warm
client caches, and distinguish request latency from full build duration. Reduced
bytes do not by themselves prove a lower fixed-bandwidth bill. Operators should
be able to assess traffic, congestion, failures, and maintenance effort without
mandatory telemetry.

### Decisions that remain in force

- SQLite authority per instance, local or S3 storage, ADR-0006's raw-response
  relay, and ADR-0007's account/identity ownership remain unchanged. Cascade is
  online cache reuse; fully isolated delivery requires a separate transfer path.
- Existing security controls, defaults, permission checks, signed metadata
  preservation, and compatibility promises remain. This positioning change
  does not authorize bypassing policy or merging trust between nodes.
- ADR-0005 Decision 5's security and release-evidence requirements are retained.
  Its golden-snapshot criterion cannot substitute for offline installation
  acceptance. This ADR does not declare those requirements complete or set a
  new 1.0 date or offline release commitment.
- General-purpose hosted publishing remains a candidate post-1.0 direction,
  requiring a separate superseding ADR. Moving already acquired dependencies
  between networks does not establish a private publishing registry. New
  Ecosystems, HA, shared databases, and a hosted control plane are outside this
  decision.
- Existing entitlement remains: one project is available without a license;
  creating additional projects and runtime per-project SBOM export require Pro
  or trial. Existing projects and dependency traffic remain usable after expiry.
  This limit concerns attribution records, not how many code repositories may
  use the shared cache. Offline/cascade commercial packaging, pricing, and
  support terms are not decided here; existing free capabilities remain free.

## Consequences and alternatives

The roadmap is now evaluated by whether work improves dependency reuse,
availability, or delivery for the three original settings. Cache operations,
onboarding, diagnosis, and recovery can be primary work without proving a novel
security primitive. Current security and authentication integrations continue to
be maintained; their presence is not a reason to expand them ahead of this flow.

Keeping enforcement as the exclusive differentiator was rejected because it
does not cover offline delivery and understates measurable network reuse.
Expanding immediately into a universal artifact registry was rejected because
publishing, tenancy, and format breadth do not complete these workflows.
Treating cache copies or golden snapshots as offline transfer was rejected
because neither establishes complete, retained, usable dependency content.

Offline delivery adds durable content and transfer lifecycle responsibilities
that an evictable cache did not previously promise. That cost is accepted as a
direction to design and validate in a narrow scope. Revenue from bandwidth
reuse, offline delivery, or support remains a hypothesis until real deployments
demonstrate the outcomes and willingness to pay.
