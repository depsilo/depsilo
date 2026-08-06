# Product

<!-- impeccable:product-schema 1 -->

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

The current implementation is differentiated by sitting directly on the
dependency request path: it can cache, verify, audit, and refuse content at the
moment a package is requested rather than only scanning and reporting later.

The confirmed future direction is to expand Depsilo into a general-purpose
artifact repository. That direction is not a claim about the current release:
the product today is primarily a multi-ecosystem proxy, cache, and
supply-chain enforcement service. The exact hosting, publishing, repository
management, and migration scope of the future artifact repository remains to
be defined.

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
- Depsilo may coexist with an existing registry or Upstream. Future
  general-purpose repository work must define how proxying, hosted artifacts,
  and enforcement compose.

## Capabilities and Constraints

- The current product supports 14 standard path-prefixed Ecosystems plus
  Docker's separate OCI route: 15 install surfaces, not 15 Ecosystems.
- It provides package caching, request coalescing, multi-Upstream selection,
  local or S3-backed artifact storage, health monitoring, access and audit
  logs, Prometheus metrics, package rules, supply-chain intelligence, and
  webhook alerts.
- The minimum-release-age gate is optional and off by default. Delivered,
  unreleased, and planned security capabilities must be described according to
  the actual release state rather than presented as uniformly available.
- The compiler cache is an isolated ccache HTTP and narrow sccache WebDAV
  compatibility service. It is not an sccache-dist scheduler or a public S3
  API.
- The current operational model is lightweight and single-instance. SQLite is
  the current database authority; multi-node HA is not a shipped capability.
- The web product is bilingual in Chinese and English. Native mobile clients
  are not part of the current product.
- The commercial model is undecided. Do not present `$99 lifetime Pro`,
  Enterprise contract licensing, trial terms, or future premium boundaries as
  durable product truth until this decision is resolved.
- General-purpose artifact repository support is a confirmed future direction,
  not a shipped capability. Its formats, hosted-repository workflows,
  permissions, retention model, and compatibility promises are open decisions.
- This future direction conflicts with the accepted non-goal in
  `docs/adr/0004-supply-chain-enforcement-layer.md`. PRODUCT.md records the
  newer product intent; the ADR must be reconsidered separately before it is
  used to guide repository architecture.

## Brand Commitments

- The formal product name is **Depsilo**. Chinese material may use **依仓** when
  a localized name is useful.
- Depsilo is MIT-licensed, open-source, and self-hosted.
- Product behavior and integration guidance must be transparent, reviewable,
  and honest about what is changed, blocked, cached, or not yet implemented.
- Depsilo does not phone home or collect anonymous telemetry.
- The canonical brand mark is **Pocket Switch**: three staggered request tracks
  merge into one dominant route, while a short blind siding represents the
  local cache and retained-artifact pocket. The masters under `docs/brand/` are
  authoritative for every product surface.
- Use the formal wordmark **Depsilo**. The mark is flat `#0A8654` on light
  backgrounds and `#3DDC91` on dark backgrounds; the wordmark uses the matching
  neutral foreground. The mark has no lightning, gradient, or attached tagline.
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
4. **Grow without pretending the future has shipped.** Evolve toward a
   general-purpose artifact repository while clearly separating current
   capability from planned scope.
5. **Preserve self-hosted trust.** Keep the open-source core inspectable, avoid
   telemetry, and never hide integrations or configuration changes.

## Accessibility & Inclusion

Chinese and English are supported product languages. UI work should continue
to target WCAG 2.1 A/AA behavior, including keyboard access, visible focus,
semantic status communication, sufficient contrast, responsive layouts, and
respect for `prefers-reduced-motion`. Existing automated coverage is strongest
for Admin; Portal and Setup should converge on the same product-wide standard.
