# Product

## Platform

Self-hosted service with a web Portal, authenticated Admin, and CLI.

## Users

Depsilo serves companies with constrained or costly internet bandwidth,
Homelab operators, and teams with repeated dependency downloads across
development machines and CI workers. Operators of isolated networks are a
target audience for the planned offline dependency-transfer workflow; that
workflow is not yet available.

The hands-on Operator may be an individual developer, a technical founder, or
a DevOps/platform engineer in a small team. They deploy Depsilo, configure
storage and Upstreams, connect package managers and build machines, review
service health, and manage supply-chain policy. Planned offline delivery also
requires preparing dependency sets and transferring them through the
organization's approved process.

End Users are developers and CI workers whose package installs and builds pass
through Depsilo. They may never open the UI; they need dependency resolution to
remain fast, predictable, and available.

In a small company, the Buyer may be the technical founder, engineering lead,
or infrastructure/security owner. They need evidence of reduced upstream
traffic, less build waiting, or less manual dependency-transfer work. Adoption
must be understandable and proportionate to the team's operational capacity.

## Product Purpose

Depsilo helps machines and networks reuse public dependencies instead of
repeatedly fetching them over constrained links. Its three product goals are:

1. **Reduce company internet traffic.** Share downloaded dependencies across
   developers and CI workers, reducing repeated transfers and contention at
   the internet connection.
2. **Deliver dependencies into isolated networks.** Prepare complete dependency
   sets on a connected node, transfer them through an approved offline process,
   and deliver later changes incrementally. This is planned work.
3. **Reuse dependencies through cascade caches.** Let clients and build machines
   use a nearby node whose misses can be filled by a parent node, reducing
   repeated upstream downloads across Homelabs and company networks.

Today shared caching, cascade, upstream management, and request-path policies
support the connected workflows. Cached content can tolerate some upstream
failures under its existing freshness and policy rules; this is not a promise
of complete installation in an isolated network. The separate ccache and
sccache service reuses compilation results.

Success means Operators can keep existing package-manager workflows, show
measurable reuse across machines, and maintain the service without specialist
artifact-infrastructure staff. Offline success will require a native client to
install a prepared dependency set with external networking unavailable.

## Positioning

Depsilo is a **self-hosted dependency cache and distribution service for company
networks, isolated networks, and Homelabs**. Shared caching and cascade are
current capabilities; offline artifact transfer and incremental delivery are
the next product direction, not shipped features.

Security policies, authentication, audit, and project attribution support
dependable dependency delivery. Their existing protections remain in force;
they no longer define an enforcement-only roadmap. [ADR-0008](docs/adr/0008-dependency-cache-and-distribution.md)
records this change to the positioning and priorities in ADR-0003/0004/0005.

Importing a prepared dependency set is distinct from publishing arbitrary
private artifacts. General-purpose hosted repositories remain outside the
pre-1.0 scope; reopening that candidate direction requires a separate ADR.

## Product Direction and Evidence

| Goal | Current status | Evidence needed for the next step |
| --- | --- | --- |
| Shared dependency cache | Implemented | Actual upstream bytes and installation time across multiple clean clients, compared with direct access and existing client caches |
| Cascade reuse | Implemented; Docker OCI excluded | A second child reuses the parent's cached content; measure external traffic at the internet-facing node without adding duplicate savings across levels |
| Offline dependency delivery | Planned; current snapshots and backups exclude artifact bytes | A complete artifact-and-metadata bundle installs successfully on the declared target environment with no external networking and an empty client cache |
| Incremental offline delivery | Planned | A target baseline identifies missing objects; subsequent bundles omit confirmed existing content and update the dependency set safely and repeatably |

An opt-in [dependency-set preparation catalog](docs/distribution.md) now provides
authenticated CLI/API staging, independent content retention, and streaming
length/SHA-256 verification. Its drafts remain operator-declared and are not
served to package managers. Signed bundles, offline installation, dependency
closure verification, and incremental transfer are still planned.

The next sequence is to validate shared/cascade reuse, complete one scoped
offline workflow, then add baseline-based incremental transfer before expanding
coverage. Target ecosystems, bundle format, and release scope need an
implementation specification; existing proxy support does not imply equivalent
offline support.

Traffic reduction is not automatically monetary savings. Metered transfers
and fixed-capacity connections have different costs; the latter may benefit
through reduced peak contention or deferred capacity upgrades. Request latency
and cache-hit bytes must not be presented as measured end-to-end build speedup.
Performance evidence should include client-cache baselines and the cost of
running Depsilo. Operators may share results voluntarily; Depsilo collects no
telemetry.

## Operating Context

- Depsilo runs inside a developer's or small company's network as a single
  binary or Docker container and exposes a web Portal, first-run Setup, and an
  authenticated Admin UI.
- Operators point existing package managers, CI jobs, AI coding agents, and
  optionally ccache or sccache clients at the service.
- The Portal supports initial connection and live status. Admin supports
  repeated work across cache, Upstreams, logs, policy, security, users, and
  settings.
- Package-manager behavior should stay familiar to End Users. Policy failures
  must explain what was refused and what an Operator can do next.
- Clients on one network can share one instance across many code projects.
  Additional cascade nodes are useful for network locality and cache placement;
  a separate node per code project is not required.
- The planned isolated-network workflow uses approved offline transfer media
  or processes. An isolated target must not depend on a reachable parent or
  silently fall back to the public internet.
- Depsilo may coexist with an existing registry or Upstream. Post-1.0
  repository work, if it is ever committed, must define how proxying, hosted
  artifacts, and enforcement compose.

## Capabilities and Constraints

- The current product supports 14 standard path-prefixed Ecosystems plus
  Docker's separate OCI route: 15 install surfaces, not 15 Ecosystems.
- It provides package caching, request coalescing, multi-Upstream selection,
  local or S3-backed artifact storage, health monitoring, access and audit
  logs, Prometheus metrics, package rules, supply-chain intelligence, and
  webhook alerts.
- Minimum-release-age enforcement is source-bound for npm, PyPI, Composer,
  NuGet, Cargo, and RubyGems. The npm packument publish time travels in the
  authenticated tarball token; PyPI uses the PEP 691 JSON simple index
  `upload-time` from the same upstream that declares the artifact, and
  HTML-only upstreams fail closed while the gate is enabled; NuGet uses the v3
  registration index's `published` timestamp, treating the 1900 unlisted
  sentinel as missing provenance; Cargo uses the sparse index entry's `pubtime`
  together with its `cksum` identity; RubyGems resolves the compact index's
  `created_at` and checksum for the exact `.gem` filename. Composer uses the p2
  metadata entry's own `time` field but remains best-effort because Composer
  clients can fall back to the original dist URL after a 451. With the age gate
  enabled, positive thresholds without supported provenance fail at startup.
  Conda is available with approximate Last-Modified provenance once the
  operator acknowledges it through
  `supply_chain.approximate_sources`, and the capability summary labels it
  `approximate` rather than `source_bound`. CRAN is available under the same
  acknowledgement: the current source tarball uses the DESCRIPTION
  `Date/Publication`, while archives and binaries use the artifact's
  Last-Modified. Maven and Alpine are available under the same acknowledgement
  and use the artifact's Last-Modified as the approximate publish time. Helm
  resolves the chart identity from the same upstream's `index.yaml` (the
  filename is ambiguous) and then applies the same Last-Modified rule. Docker
  registries expose no portable publish time, so Docker is available only with
  first-observation age, acknowledged through
  `supply_chain.observation_sources`: an armed threshold resolves a tag to its
  digest, records the first time this instance saw that digest, and fetches
  the manifest pinned to the digest. The capability summary labels it
  `observed`; the age is explicitly not presented as a publish time.
  Delivered, unreleased, and planned security capabilities must be described
  according to the actual release state rather than presented as uniformly
  available.
- The compiler cache is an isolated ccache HTTP and narrow sccache WebDAV
  compatibility service. It is not an sccache-dist scheduler or a public S3
  API.
- Cascade cache peering lets a node egress selected Upstreams through another
  Depsilo's authenticated relay, so a parent fills its cache once and nearby
  devices read from it at LAN speed (ADR-0006). Peers share cache results
  only: every node keeps its own database, policy, and cache. Docker is out
  of scope, and cascade does not aggregate bandwidth across peers.
- The current operational model is lightweight: SQLite is the database
  authority per instance, and cascade connects independent nodes without a
  shared control plane. Multi-node HA and shared-database deployments are not
  shipped capabilities.
- Cache snapshots contain identities and hashes, not artifact bytes; instance
  backups preserve configuration and database state while excluding cached
  artifacts. Neither is an offline dependency-transfer bundle. See
  [snapshot boundaries](docs/supply-chain.md#snapshots-and-tamper-detection) and
  [backup boundaries](docs/compatibility.md#database-and-backups).
- Admin supports local password login and multiple named authentication sources
  configured by protocol: OIDC and LDAP over LDAPS or mandatory StartTLS. Casdoor
  is one tested OIDC provider, not a default source. OIDC sources require distinct
  issuers; directory identities use a stable UUID or GUID. A locally
  managed account can bind one identity per source and unlink each separately
  after proving its password; a new external account requires an explicit group
  mapping to the existing `admin` or `readonly` role and an explicit first-login
  creation choice. Unknown identities may instead prove and link an existing local
  account. System management offers isolated connection and sign-in tests. External account
  permissions remain with their original provider. Those accounts cannot bind
  another source, add a local password, or mint durable machine credentials.
  Provider sessions have a bounded absolute lifetime, and a local password
  administrator remains the recovery authority. This is an open-source Admin
  integration, without SCIM, SAML, directory synchronization, immediate remote revocation, or new
  project-level roles. [ADR-0007](docs/adr/0007-operator-oidc-identities.md)
  partially supersedes the earlier SSO non-goal. ADR-0008 changes product
  positioning while retaining this identity model and the repository limits.
- The web product is bilingual in Chinese and English. Native mobile clients
  are not part of the current product.
- The entitlement boundary is decided: governance primitives (minimum release
  age, known-malicious blocklist, OSV scanning, package rules, quarantine,
  audit logs, and webhooks) are open source. Without a license, an operator can
  create and use one project, including its connection URL, package inventory,
  and project token. Creating additional projects and the runtime per-project
  SBOM export — including its CRA mode and technical-file preset — require Pro
  or the 14-day trial. When entitlement expires, existing projects and their
  dependency traffic remain available; further creation is limited until the
  instance has no projects or Pro is restored.
  This project limit concerns attribution records, not the number of code
  projects or clients allowed to use the shared cache. Projects share instance
  cache and policy; they do not provide tenant or package-access isolation.
  Pricing, packaging, and support terms remain undecided. Do not present
  `$99 lifetime Pro` or Enterprise contract licensing as durable product
  truth. Packaging for planned offline delivery is also undecided; ADR-0008
  does not add paywalls or change current entitlements.
- General-purpose artifact repository support is a candidate post-1.0
  direction recorded in ADR-0005, not a shipped capability or a pre-1.0
  commitment. Its formats, hosted-repository workflows, permissions,
  retention model, and compatibility promises remain undefined.
- ADR-0008 replaces the enforcement-first positioning and build order while
  retaining the existing safety, compatibility, and release-verification
  requirements. It does not declare the 1.0 exit criteria complete or set a
  release date for offline delivery.

## Brand Commitments

- The formal product name is **Depsilo**. Chinese material may use **依仓** when
  a localized name is useful.
- Depsilo is MIT-licensed, open-source, and self-hosted.
- Product behavior and integration guidance must be transparent, reviewable,
  and honest about what is changed, blocked, cached, or not yet implemented.
- Depsilo does not phone home or collect anonymous telemetry.
- The canonical brand mark is the **open repository boundary**: two blue shell
  halves around a green cached dependency module. It expresses a cached
  dependency inside an open repository boundary rather than a closed container.
  The masters under `docs/brand/` are authoritative for every product surface.
- Use the formal wordmark **Depsilo** with the descriptor **Repository Cache
  Control**. Brand colours: Electric Blue `#3B82F6`, Deep Blue `#2563EB`, Cache
  Green `#22C55E`, Deep Green `#16A34A`, Ink `#0F172A`, Slate `#64748B`. The
  chromatic mark is used on both light and dark backgrounds; monochrome
  variants remain available for single-colour contexts. Brand copy may use the
  tagline "Dependencies closer. Builds faster."; the mark itself carries no
  attached tagline.
- Avoid claims that imply enterprise scale, certification, customer adoption,
  or capabilities that the project cannot currently demonstrate.

## Evidence on Hand

- The repository contains working backend, CLI, Portal, Setup, and Admin
  implementations with unit, integration, and browser tests.
- `README.md`, `CONTEXT.md`, `docs/DIRECTION.md`, and `docs/adr/` document
  current features, terminology, operating constraints, and prior strategic
  decisions.
- Release automation produces CycloneDX and SPDX source and container-image
  SBOM artifacts.
- Brand assets are available under `docs/brand/`.
- The Admin interface has automated axe coverage across responsive,
  light/dark, and Chinese/English variants. Accessibility behaviors also
  include visible focus, keyboard operation, reduced-motion handling, and
  bounded horizontal scrolling.
- There are no confirmed customer case studies, testimonials, press mentions,
  certifications, or independent performance benchmarks on hand. Existing
  claims such as memory use, deployment time, and LAN-speed delivery must not
  be reframed as independently validated evidence.

## Product Principles

1. **Reuse across machines and networks.** Prioritize reduced upstream traffic,
   useful cache locality, and complete dependency delivery over feature count.
2. **Keep operation proportionate.** Features must justify their setup,
   maintenance, and cognitive cost for individuals and small teams.
3. **Keep downloads dependable.** Clear failures, explicit egress behavior,
   safe updates, and recovery protect End User installs and builds.
4. **Preserve explainable protection.** Policy decisions and transfer integrity
   must be visible and auditable; positioning changes do not weaken checks.
5. **Prove each delivery mode.** Distinguish cache tolerance, online cascade,
   and complete offline installation. Describe planned work as planned and
   qualify it against real package managers before claiming support.
6. **Preserve self-hosted trust.** Keep the open-source core inspectable, avoid
   telemetry, and never hide integrations or configuration changes.

## Accessibility & Inclusion

Chinese and English are supported product languages. UI work should continue
to target WCAG 2.1 A/AA behavior, including keyboard access, visible focus,
semantic status communication, sufficient contrast, responsive layouts, and
respect for `prefers-reduced-motion`. Existing automated coverage is strongest
for Admin; Portal and Setup should converge on the same product-wide standard.
