# ADR-0004: Reposition Depsilo as a Supply-Chain Enforcement Layer

**Status:** accepted
**Date:** 2026-06-30
**Supersedes parts of:** [ADR-0003 (Supply-Chain Control Point)](./0003-supply-chain-control-point.md)
**Companion docs:**
- Research: [`docs/research/2026-06-30-competitive-landscape.md`](../research/2026-06-30-competitive-landscape.md)
- North star: [`docs/DIRECTION.md`](../DIRECTION.md)

## Context

ADR-0003 (2 days ago) repositioned depsilo from "fast multi-eco cache"
to "self-hosted supply-chain control point." The bet was that no
incumbent occupied the proxy-as-control-point position; depsilo's
14-ecosystem coverage + governance primitives + lightness would let
it own that space.

Research on 2026-06-30 invalidated parts of that bet:

1. **[Artifact Keeper](https://artifactkeeper.com/)** is a 794-star,
   MIT-licensed, Rust-based "Artifactory alternative" that has
   already shipped what ADR-0003 put on depsilo's T2 roadmap:
   - 45+ ecosystem formats (depsilo: 14)
   - SSO / LDAP / SAML / RBAC in OSS
   - Helm chart + Terraform modules + Windows MSI + iOS + Android
     native clients
   - GPG + cosign signing integrated
   - WASM plugin system for custom formats
   - **$0 forever, no Pro tier** ([source](https://github.com/artifact-keeper/artifact-keeper))

2. **pnpm 11** turned `min-release-age` to default ON; **npm 11.10.0**
   added it natively; **uv** has `exclude-newer`; **cooldowns.dev**
   ships a cross-PM helper. The "cooldown / minimum release age"
   feature is becoming default behavior at the client level. ([source](https://www.cryptika.com/pnpm-11-turns-on-minimum-release-age-by-default-to-reduce-npm-supply-chain-risk/))

3. Two real spring 2026 attacks (LiteLLM ~2h exposure, axios ~2-3h
   exposure) validated the cooldown thesis — and increased competitive
   pressure to ship the feature. ([source](https://dev.to/trknhr/lessons-from-the-spring-2026-oss-incidents-hardening-npm-pnpm-and-github-actions-against-1jnp))

ADR-0003's "lightweight multi-ecosystem self-hosted general-purpose
proxy" position is no longer defensible against Artifact Keeper. The
control-point narrative still holds, but the positioning that depsilo
IS the control point — singular — is wrong. Artifact Keeper is in
that space already and ahead on most surfaces.

The honest question becomes: **what does depsilo do that AK does not,
and can it be enough to justify continued investment?**

What AK explicitly does NOT have (verified from their README, 2026-06-30):
- **Minimum release age / time-based quarantine** — only manual policy-
  engine upload rules
- **OSV / GHSA malicious-package automated blocklist**
- **CRA-mode SBOM workflow** (per-project SBOM with regulatory
  signing preset)
- **Freeze / golden snapshot** (reproducible-build mode for
  air-gapped / compliance use)
- **Tamper detection** (hash-of-first-fetch + alert on republish)

These are exactly the T1 Task 1 wedge (just shipped) and T1 Task 2 /
follow-ups (in flight). They share an architectural shape:
**the proxy refuses to serve, in real time, based on a supply-chain
policy.** No scanner-and-report tool can do this — they're off the
request path. Neither can AK without a redesign that loops upstream
metadata into the serve decision (which depsilo has done with the
resolver registry + 3-tier lookup in `internal/quarantine/`).

## Decision

Reposition depsilo from "the lightweight multi-eco self-hosted
control point" to "**the supply-chain enforcement layer**." Concretely:

1. **Narrow the surface.** Stop competing on ecosystem breadth (cap
   stays 14), client-side polish (no iOS / Android / Windows MSI),
   storage / IaC depth (no Terraform modules), and the "Artifactory
   replacement" general-purpose narrative. **Artifact Keeper owns
   that surface and shipped it first.**

2. **Double down on enforcement primitives.** The roadmap focuses
   exclusively on features only a proxy on the request path can
   provide:
   - Minimum release age quarantine ✅ (T1 Task 1, shipped 2026-06-29)
   - OSV / GHSA malicious-package automatic blocklist (T1 Task 2,
     in progress)
   - CRA-mode SBOM workflow with signing presets
   - Freeze / golden snapshot mode
   - Tamper detection (republish hash check)
   - SIEM-grade audit + multi-channel webhook routing
   - Future: behavioral analysis hooks (process spawn / network
     egress fingerprints from cached tarballs)

3. **Reframe the Pro tier.** AK's free SSO / RBAC / multi-tenant
   workspace makes depsilo's current Pro ("multi-project workspace
   + email support") look thin. Pro should become "**Supply-Chain
   Premium**" — features that only matter to organizations putting
   depsilo in the build-policy enforcement path:
   - Long-term audit retention (>90 days)
   - SIEM integration (Splunk / Datadog / Elastic feed format)
   - Custom policy DSL / scriptable allow-list rules
   - Compliance report generation (CRA, NIST SSDF, FedRAMP
     evidence packs)
   - Priority email support

   "Multi-project workspace" stays Pro but is no longer the
   headline; supply-chain depth is.

4. **Coexistence over competition.** Documentation should
   acknowledge AK by name and recommend the right tool for the
   right job:
   - "Need a general-purpose multi-format artifact registry? Use
     Artifact Keeper or Nexus."
   - "Need supply-chain enforcement at the proxy layer? Use
     depsilo, optionally in front of your existing registry."
   - Explore the "chainable proxy" deployment model: depsilo can
     sit in front of AK / Nexus / Artifactory, doing quarantine
     + blocklist + SBOM-of-real-traffic, while the downstream
     registry handles storage / hosting.

5. **Honesty as positioning.** In OSS culture, "we acknowledge
   $competitor's strengths and explain our specific value" beats
   "pretend they don't exist." The README should link to AK and
   explain the complementary positioning in the first 200 words.

6. **Drop redundant T2 items.** The ADR-0003 T2 list (Helm chart,
   easy HA, SSO/RBAC, alerting) has already been delivered by AK in
   OSS. depsilo should:
   - **Keep**: alerting (webhook routing is enforcement-adjacent)
   - **De-prioritize but ship**: Helm chart, basic HA (table stakes
     for any production deployment of the enforcement layer)
   - **Drop**: building SSO/RBAC ourselves. Recommend operators
     deploy depsilo behind an OIDC proxy (oauth2-proxy / Authelia /
     Pomerium) — these are mature and OSS.

## Consequences

**Positive:**
- Clear scope: every roadmap item is "does only a serve-path proxy
  do this?" If no, defer or recommend a different tool.
- Honest positioning is rewarded in OSS communities (HN /
  r/selfhosted / r/devops).
- Coexistence framing creates a partnership story instead of a
  zero-sum competition with AK.
- Pro tier reframing solves the "AK gives more in OSS than we
  charge $99 for" awkwardness.
- The 14-ecosystem cap is now a feature, not a limitation: "we
  support 14 because deeper enforcement on 14 beats shallow
  enforcement on 45."

**Negative / accepted:**
- Drop the implicit claim of being "the alternative to Artifactory."
  Forfeit the broad-narrative search traffic.
- Drop the multi-platform client roadmap (iOS / Android / Windows
  MSI). Forfeit a slice of solo-developer / hobbyist appeal.
- The OIDC-via-external-proxy recommendation puts a small ops
  burden on operators wanting SSO. AK's in-product SSO is more
  convenient.
- Some buyers will see "supply-chain enforcement only" as
  too-narrow; they'll choose AK for the all-in-one story even
  though they need depsilo's primitives. We accept this filter —
  the buyers who reach us are the ones who specifically need the
  enforcement layer, which is the conversion we want.

**Engineering implications:**
- Pause T2 roadmap items 2 (HA) + 3 (SSO/RBAC) until enforcement
  features are ahead of AK's plausible roadmap delta.
- Reorder T1 follow-ups to extend the enforcement moat:
  next-up Task 2 (malicious blocklist) stays first; freeze /
  tamper detection move up before HA + Helm.
- README rewrite is the single highest-leverage non-engineering
  action — should land before any new feature commit. Same for
  landing page.

## Alternatives considered

**Match Artifact Keeper feature-for-feature.** Would require months
of catch-up work (45 ecosystems, iOS/Android, SSO/RBAC, WASM plugin
host). Even if achieved, AK's 794-star + active development means
depsilo lands as "AK clone" — worst marketing position. Rejected.

**Pivot away from proxy entirely.** Become a scanner-and-report
tool like Snyk / Socket. Crowded SaaS market, OSS self-hosted
posture would be hard to differentiate, abandons the
proxy-as-enforcement insight that's the actual moat. Rejected.

**Become an Artifact Keeper plugin.** Distribute depsilo as a WASM
plugin in AK's plugin marketplace. Effectively kills depsilo as
an independent project. Considered as a possible long-term move
if enforcement features become commoditized, but premature now.
Rejected for v0.x.

**Focus on Chinese market only.** AK and RepoFlow are
English-first; the 信创 (autonomous controllable) / 等保 2.0
narrative would let depsilo dominate a sub-market. Considered as
a complementary angle alongside the enforcement-layer pivot.
**Not adopted as primary positioning** because (a) it would forfeit
global supply-chain regulatory tailwinds (EU CRA), and (b) the
Chinese self-hosted devtool market is small at current pricing.
Worth a Chinese docs page + 信创 narrative but not the whole
strategy.

**Acknowledge AK's existence is what makes the supply-chain
enforcement narrative defensible.** The acquired position is
clarified, not weakened, by naming a credible adjacent product
and explaining the specific value-add. Adopted.
