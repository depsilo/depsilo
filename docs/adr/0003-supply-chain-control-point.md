# ADR-0003: Reposition Depsilo as a self-hosted supply-chain control point

**Status:** accepted
**Date:** 2026-06-28
**Companion document:** [docs/DIRECTION.md](../DIRECTION.md)

## Context

Depsilo today reads as *"a fast, lightweight, self-hosted multi-ecosystem package
cache."* That description, while accurate, places it in a crowded commodity field:
Artifactory, Nexus, ProGet, Cloudsmith, Reposilite, RepoFlow, and a steady stream
of new all-open-source clones. Competing on "fastest / lightest / most ecosystems"
is a race the larger incumbents and the lighter purpose-built tools each win on
different axes.

Meanwhile two external forces have reshaped the buyer's needs:

1. **EU Cyber Resilience Act.** Reporting from 11 Sep 2026, full compliance
   11 Dec 2027. Machine-readable SBOMs become a legal requirement; non-compliance
   penalties reach €15M / 2.5% of global turnover. The de-facto formats are
   CycloneDX + SPDX. The US Executive Order 14028 points the same way.

2. **Shai-Hulud / Mini Shai-Hulud worms (Sep 2025 → May 2026).** Self-propagating
   malware across npm + PyPI compromised 25k+ repos including TanStack, Zapier,
   PostHog, and Postman. The attackers forged SLSA L3 provenance and added
   persistence hooks targeting AI coding agents. The mitigations the security
   community converged on — *minimum release age*, *invalidate caches*, *rebuild
   from a trusted snapshot*, *block known-malicious versions* — are all
   proxy-shaped: they want to live at the place dependencies enter a build.

Both trends share a single architectural shape: a proxy the team controls. Depsilo
*is* that proxy, but it does not currently lean into the shape.

## Decision

Reposition Depsilo as **the lightweight, self-hosted supply-chain control point**
for small/mid teams who cannot or will not run Artifactory. The cache stays — it
is the wedge that gets the proxy installed — but the strategic value, the
positioning, and the next several releases all serve the control-point story.

Concretely:

- **What we build.** Governance and security primitives that only a controlled
  proxy can enforce well: minimum release age / quarantine, known-malicious
  blocklist, freeze / golden snapshot, tamper detection, CRA-mode SBOM export,
  policy webhooks. See `docs/DIRECTION.md` §T1 for the specced versions.
- **What we stop chasing.** Out-feature parity with Artifactory (40+ ecosystems);
  out-depth parity with Snyk / FOSSA / Anchore on scanning; being a general
  artifact *host* before being a control plane.
- **The bar everything we ship clears: security-tool discipline.** Transparent
  by default — no feature that hides what the tool did to a codebase. Signed
  releases. SBOM dogfooded. Public-registry fallback always available.

## Consequences

**Positive:**
- A defensible position. "Self-hosted, 10-minute deploy, multi-eco, CRA-ready,
  blocks Shai-Hulud" is a sentence no incumbent says naturally.
- The control-point work composes — minimum release age, blocklist, snapshot,
  and CRA SBOM are all surfaced through the same event log + webhook + UI
  shell, so each new primitive amplifies the previous ones.
- A clean monetization shape: open-source = the wedge primitives + cache;
  Pro = the governance control plane around them.

**Negative / things we accept:**
- We will not chase parity with Artifactory on ecosystem breadth. The 14 we
  support is the upper bound for now.
- The roadmap front-loads security/compliance features over UX polish for
  several months. Onboarding and HA improvements (T2) explicitly come after
  T1 wedge features.
- Pricing tension: control-plane features in a self-hosted tool with a $9/mo
  pricepoint signals "hobby." The Pro tier needs repricing alongside (founder
  decision, not engineering).

**Direct engineering implications:**
- The Quick Start "AI integration prompt" must be rewritten: the current version
  instructs the LLM to omit Depsilo's product name and drop the public-CDN
  fallback. For a security-positioned tool, those instructions pattern-match to
  the very attacks we want to defend against. (T0 #1 in DIRECTION.md.)
- We need signed, versioned releases (none today). Cosign keyless via GitHub
  Actions is the planned target. (T0 #2 — currently parked pending pre-flight
  decisions; SBOM dogfooding lands first.)
- Every governance primitive ships with: an event log entry, a webhook hook
  point, a Monitor UI surface. These are shared infrastructure — build once.

## Alternatives considered

- **Stay as "fast, lightweight cache."** Lowest engineering risk but no
  defensibility. Commoditized by the next clone.
- **Pursue Artifactory-level breadth.** Years of work; loses the "10-minute
  deploy / 50 MB RAM" pitch that justifies switching.
- **Out-scan dedicated SCA vendors.** They have years of vulnerability
  intelligence we cannot match; we end up a worse Snyk.
- **Pivot to managed SaaS.** Forecloses the self-hosted market we have
  positioned for and trades a defensible niche for a hyper-competitive one.

The "control point" framing accepts the niche, leans into self-hosted as the
moat, and converts two regulatory and security tailwinds into product-market fit.
