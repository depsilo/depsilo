# Product

## Platform

web

## Users

Depsilo primarily serves individual developers and small companies that want
to run their own dependency infrastructure without operating a large artifact
platform.

The hands-on Operator may be an individual developer, a technical founder, or
a DevOps/platform engineer in a small team. They deploy Depsilo, configure
storage and Upstreams, connect package managers and build machines, review
service health, and manage supply-chain policy.

End Users are developers and CI workers whose package installs and builds pass
through Depsilo. They may never open the UI; they need dependency resolution to
remain fast, predictable, and available.

In a small company, the Buyer may be the technical founder, engineering lead,
or infrastructure/security owner. Adoption must be understandable and
proportionate to a small team's operational capacity.

## Product Purpose

Depsilo gives individuals and small teams one self-hosted service for caching
dependency traffic, managing Upstreams, and enforcing supply-chain decisions
on the package-install request path.

Today it accelerates repeated installs, provides an offline-tolerant cache,
observes dependency traffic, and can refuse packages that violate enabled
policy. It also provides an isolated compiler-cache service for ccache and
sccache.

Success means a small team can deploy and understand the service without
specialist artifact-infrastructure staff, while End User installs and builds
remain fast and dependable and Operator decisions remain visible and
auditable.

## Positioning

Depsilo is a supply-chain enforcement layer plus cache until 1.0. It is
differentiated by sitting directly on the dependency request path: it can
cache, verify, audit, and refuse content at the moment a package is requested
rather than only scanning and reporting later.

A general-purpose artifact repository is a candidate post-1.0 direction
recorded in ADR-0005, not a commitment and not part of the current release.
Reopening that direction requires a new ADR that explicitly supersedes
ADR-0004.

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
  clients can fall back to the original dist URL after a 451. Every other
  ecosystem still rejects positive thresholds at startup until its artifact
  source and timestamp are bound. Conda is available with approximate
  Last-Modified provenance once the operator acknowledges it through
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
- The current operational model is lightweight and single-instance. SQLite is
  the current database authority; multi-node HA is not a shipped capability.
- The web product is bilingual in Chinese and English. Native mobile clients
  are not part of the current product.
- The entitlement boundary is decided: governance primitives (minimum release
  age, known-malicious blocklist, OSV scanning, package rules, quarantine,
  audit logs, and webhooks) are open source; multi-project workspaces and the
  runtime per-project SBOM export — including its CRA mode and technical-file
  preset — are Pro features with a 14-day trial.
  Pricing, packaging, and support terms remain undecided. Do not present
  `$99 lifetime Pro` or Enterprise contract licensing as durable product
  truth.
- General-purpose artifact repository support is a candidate post-1.0
  direction recorded in ADR-0005, not a shipped capability or a pre-1.0
  commitment. Its formats, hosted-repository workflows, permissions,
  retention model, and compatibility promises remain undefined.
- ADR-0005 reconciles the earlier conflict between PRODUCT.md and ADR-0004:
  ADR-0004's general-purpose-repository non-goal stays in force until 1.0,
  and any post-1.0 direction requires a new superseding ADR.

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

1. **Small-team operability first.** Features must justify their setup,
   maintenance, and cognitive cost for individuals and small companies.
2. **Keep the request path dependable.** Speed, clear failures, offline
   tolerance, and safe recovery protect every End User build.
3. **Enforce transparently.** Security decisions must be explainable,
   auditable, and reversible only through explicit Operator action.
4. **Grow without pretending the future has shipped.** Keep pre-1.0 scope on
   request-path enforcement; record any post-1.0 repository direction
   separately and never present it as current capability.
5. **Preserve self-hosted trust.** Keep the open-source core inspectable, avoid
   telemetry, and never hide integrations or configuration changes.

## Accessibility & Inclusion

Chinese and English are supported product languages. UI work should continue
to target WCAG 2.1 A/AA behavior, including keyboard access, visible focus,
semantic status communication, sufficient contrast, responsive layouts, and
respect for `prefers-reduced-motion`. Existing automated coverage is strongest
for Admin; Portal and Setup should converge on the same product-wide standard.
