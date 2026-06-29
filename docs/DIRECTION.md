# Depsilo — Product & Engineering Direction

> A north-star brief for **Claude Code**. Read this before picking up roadmap work.
> It explains where Depsilo stands, the strategic bet, and a prioritized build
> order with concrete first tasks. When in doubt, optimize for the bet below.

---

## TL;DR — the bet

Depsilo today reads as *"a fast, lightweight, self-hosted multi-ecosystem package cache."*
That is a **commodity** in a crowded field (Artifactory, Nexus, ProGet, Cloudsmith,
Reposilite, RepoFlow, and new all-open-source clones appearing monthly).

**The bet:** reposition Depsilo as **the lightweight, self-hosted supply-chain _control
point_** for small/mid teams who can't or won't run Artifactory. The cache is the wedge;
the value is **governance + security over everything that enters a build.**

Two tailwinds make the timing good, and both are *proxy-shaped* — a proxy you control is
the natural place to enforce them:

1. **EU CRA** makes a machine-readable SBOM legally mandatory (reporting from **11 Sep
   2026**, full compliance **11 Dec 2027**, fines up to **€15M / 2.5% of global
   turnover**; formats in practice are CycloneDX + SPDX — both already supported). US EO
   14028 points the same way.
2. **Shai-Hulud / Mini Shai-Hulud** (Sep 2025 → May 2026): self-propagating worms across
   npm + PyPI, 25k+ repos, major projects hit (TanStack, Zapier, PostHog, Postman…),
   even forging SLSA L3 provenance and adding persistence hooks that target AI coding
   agents. The recommended mitigations — **minimum release age**, invalidate caches,
   rebuild from a **trusted snapshot**, **block known-malicious versions** — are exactly
   what a controlled proxy can enforce.

---

## Where we are (honest)

- **Pre-traction:** ~0 stars/forks, no published releases, `v0.7.1+dev`.
- **Product surface is broad and well-built:** 13 ecosystems, SBOM, CVE/OSV scanning,
  multi-upstream with health checks, local/S3 storage, Prometheus, a polished site.
- **The missing 90% is users, trust, distribution, monetization — not features.**

**Implication for engineering:** do **not** add breadth for its own sake. Add the few
things that (a) make the *control-point* story real and (b) make the tool trustworthy
enough for a security-conscious buyer.

---

## Positioning & non-goals

**We ARE:** lightweight (single binary, ~50 MB RAM, ~10-min deploy), self-hosted,
multi-ecosystem, with a built-in **supply-chain control + compliance** layer.

**We are NOT trying to:**
- Out-format Artifactory — do **not** chase 40+ ecosystems.
- Beat dedicated SBOM/SCA vendors (Snyk, FOSSA, Anchore, Socket) on scanning depth.
- Be a general artifact *host* first. "Good enough + convenient + self-hosted + governed"
  beats "most features."

**Security-tool discipline (non-negotiable — we sell trust):**
- **Transparent by default.** Never ship a feature that hides what the tool did to a
  codebase. (This directly affects T0 below.)
- **Sign releases. Dogfood** (publish Depsilo's own SBOM). **Always keep a public-registry
  fallback available.**

---

## Build order

### T0 — Credibility fixes (do first; small, high trust-value)

- [ ] **Remove the "brand discipline" stealth integration prompt.** The Quick Start
      "one-prompt AI integration" currently tells the agent **not to write the product
      name/hostname** and to **drop the public-CDN fallback**. For a security-positioned
      tool this pattern-matches to the very attacks we defend against and will repel the
      buyers we want. Replace with a **transparent** prompt: name Depsilo + the URL, keep
      the public index as a fallback, make the change reviewable. *(Frontend Quick Start
      page + the prompt generator.)*
- [ ] **Ship real, signed releases.** CI to build, sign (cosign/sigstore), and publish
      versioned binaries + checksums. There are no published releases today.
- [ ] **Dogfood SBOM.** Generate + publish Depsilo's own CycloneDX + SPDX SBOM in CI.

### T1 — The control-point wedge (the differentiator)

- [ ] **Minimum release age / quarantine** — flagship. See **Task 1**.
- [ ] **Known-malicious blocklist** — hard-block malicious (not merely vulnerable)
      versions. See **Task 2**.
- [ ] **Freeze / golden snapshot.** A mode that serves only versions in an approved
      snapshot; "promote current cache to snapshot," export/import. Enables reproducible /
      offline builds immune to upstream poisoning.
- [ ] **Tamper detection.** Persist a content hash per `(package, version)` on first
      fetch; alert if an already-seen *immutable* version's upstream content later changes.
- [ ] **CRA-mode SBOM export.** Ensure output carries NTIA/CRA minimum elements
      (name+version, supplier, **purl**, **SHA-256** hash, license, dependency
      relationships), is signable, and has a "technical-file" export preset.

### T2 — Adoptability & trust (table stakes for buyers with budget)

- [ ] Rock-solid one-command deploy + **Helm chart** + strong **docs** (docs are a feature).
- [ ] **RBAC + SSO (OIDC/LDAP) in open-source.** The "SSO tax" pattern is widely hated
      and competitors give it free; locking SSO behind a paid tier kills enterprise
      adoption. See `CONTEXT.md` "Product tiers" and §16 of this file for the explicit
      open-source-only commitment.
- [ ] **Multi-node / HA made genuinely easy** (this is where Artifactory/Nexus hurt — easy
      HA is itself a wedge). Open-source.
- [ ] **Alerting:** webhook/Slack on policy hits (blocked malware, quarantined version, CVE
      over threshold). Turns a passive cache into an active guardrail. Open-source.

### T3 — Monetization shape (informs feature-gating, not pricing)

- **Two SKUs only:** Open Source (MIT, self-hosted, free) and Pro (single-tier,
  **one-time $99 lifetime**, self-serve via email order today). No subscription, no
  Community/Pro/Team ladder, no cloud/hosted SKU.
- Pro gates **exactly one** UI surface: **multi-project workspaces** (per-project
  isolation of audit/rules/SBOM/cache + per-project RBAC). Everything else — audit
  logs, the rules engine + UI, the security intelligence dashboard, SBOM export,
  OSV scanning, SSO/RBAC, HA, and the T1 supply-chain wedge — stays open-source. The
  Pro purchase bundles the multi-project UI + email priority support + automatic
  access to future Pro features.
- *Public price = $99 lifetime.* Earlier iterations tried $9/mo (read as "hobby") and
  contact-pricing (no sales bandwidth to handle inbound). $99 lifetime lands in the
  indie-tool sweet spot (Resend / Plain / Cal.com adjacent), zero-recurring is
  honest about self-hosted philosophy, and the price is high enough to filter
  non-serious buyers without enterprise friction.
- *No payment provider integrated yet.* The Buy CTA opens a mailto: order email;
  the maintainer manually processes payment (PayPal / Alipay / WeChat / bank) and
  emails a license key back. One round-trip per sale. When provider integration
  lands (Lemon Squeezy / Polar / Gumroad), only `web/src/lib/buy.ts` changes.

---

## First tasks, specced

### Task 1 — Minimum release age (quarantine)

**Goal:** refuse to serve any package version whose upstream publish time is younger than
a configurable threshold, so a freshly-poisoned version cannot enter a build before the
community catches it.

**Config — per-ecosystem, global default + overrides:**

```yaml
supply_chain:
  min_release_age:
    default: 0
    pip: 3d
    npm: 7d
    cargo: 3d
  mode: block            # block | serve_last_eligible
  allow:                 # bypass quarantine for trusted pins
    - "npm:internal-*"
    - "pip:requests==2.32.3"
```

**Behavior:**
- On request for `(ecosystem, pkg, version)`, look up the upstream publish timestamp
  (npm registry `time[version]`; PyPI JSON `upload_time_iso_8601`; equivalent per
  ecosystem). **Cache the timestamps** — never hammer upstream per request.
- If `now - publish_time < threshold` and not matched by `allow`:
  - `block` mode → return a clear, actionable error (which package/version, its age, and
    how to approve it).
  - `serve_last_eligible` mode → resolve to the newest version that *is* older than the
    threshold.
- Record the event; surface it in the Monitor UI and fire a webhook.
- **Manual approve:** UI button + API to release a quarantined `(pkg, version)` early
  (adds to the approved set). Audit who/when.

**Edge cases:** missing upstream timestamp → configurable (fail-open, or treat as age 0);
an exact-pinned lockfile version under quarantine → **fail closed** with an error that
explains how to approve; pre-release / yanked versions.

**Acceptance:**
- [ ] Configurable globally and per-ecosystem; `allow` overrides work.
- [ ] A version published `< threshold` is blocked (or down-resolved) with a clear
      message; one `≥ threshold` serves normally.
- [ ] Approve action releases it immediately and is audited.
- [ ] Quarantine events appear in Monitor **and** fire a webhook.
- [ ] Timestamps are cached; no per-request upstream calls.

### Task 2 — Known-malicious blocklist

**Goal:** hard-block versions known to be **malicious** (distinct from merely vulnerable).

- Sync **OSV malicious-packages** + **GitHub Advisory (malware)** on a schedule into a
  local store keyed by `(ecosystem, package, affected_range)`.
- On request, if matched: refuse with a clear *"known-malicious, blocked"* error and an
  alert — **never serve.** Provide an explicit, **audited** admin override for false
  positives.
- Surface in Monitor + webhook. Keep entirely separate from CVE handling (CVE =
  warn/serve; malware = hard block).

**Acceptance:**
- [ ] Sync runs + refreshes on schedule.
- [ ] A known-malicious version is blocked end-to-end.
- [ ] Override is audited; block/override events are visible + alerted.

---

## Decisions locked-in alongside this brief (2026-06-28)

This file is the living north star — decisions that landed when this file was first
written are captured here so future-you remembers the rationale. ADR-0003 captures
the strategic shift itself; the items below are settings within it.

**Integration prompt:** transparent by default (T0 #1). Every config edit names
Depsilo + the mirror URL in a comment; public-registry fallback stays as a
documented secondary; the agent must print a diff summary at the end.

**Released artifacts (signing currently parked):** when signing returns, the target
stack is **cosign keyless (OIDC via GitHub Actions)**, signing binaries + checksums
+ container images + SBOM, distributed via GitHub Releases + GHCR + Docker Hub.
First signed release will be **v0.8.0** preceded by `v0.8.0-rc.1` for a one-week soak.
Reproducibility goal is **deterministic** (`-trimpath`, `-buildvcs=false`, locked
versions); bit-for-bit reproducible is out of scope for v0.8.x.

**SBOM:** Syft generates **CycloneDX + SPDX** covering Go binary deps, `web/` npm
deps, **and** the container image. Published as Release attachment **and** at
`.well-known/sbom/{version}.cdx.json` on depsilo.com. Cosign-attest layered on
top when signing lands.

**T1 Task 1 defaults:** `pip: 3d` · `npm: 7d` · `cargo: 3d` · `go: 0` (Go modules
are immutable + checksum DB already mitigates) · `maven/nuget/rubygems: 3d`.
Missing upstream timestamp → **fail-closed**. Allow-list supports glob
(`npm:@scope/internal-*`), exact pin (`pip:requests==2.32.3`), and range
(`npm:react>=18.0.0`). Default mode = `block`. Approve action is admin-only with
mandatory reason text + audit log entry.

**T1 Task 2 defaults:** data sources = **OSV malicious-packages + GHSA malware**
(no paid third-party feeds). Sync every 6 hours. Override mechanism is admin-only,
mandatory reason, **auto-expires in 24h by default** — no permanent whitelist.
Block response = HTTP 451 with `code: MALICIOUS_BLOCKED`. Matched versions are
never cached and any existing cache entry is evicted on first match.

**Governance features go in open-source.** Minimum release age, malicious blocklist,
SBOM export, OSV scanning, the audit log, the package allow/deny rules engine, and
the security intelligence dashboard are all open-source product wedges — they must
be available to everyone running a self-hosted control point. The Pro tier is the
**multi-project workspace + support contract**: per-project isolation of all that
governance machinery, per-project RBAC, plus the support / SLA / compliance /
consulting bundle that the contract delivers.

**SSO / RBAC go in open-source.** Updated from the original direction. The "SSO tax"
pattern damages trust and competitors give it free; locking SSO behind Pro would kill
the same enterprise adoption Pro is supposed to land. SSO/RBAC, HA, multi-node, and
all other commodity self-hosted infrastructure stay open-source. See ADR-0003 §"Decision"
and CONTEXT.md "Product tiers".

**Versioning policy until v1.0:** breaking changes allowed across minor versions;
config-file backwards compatibility guaranteed within each `v0.x` line.

**No telemetry / anonymous reporting.** Self-hosted trust commitment is incompatible
with phoning home.

**Docs language:** main README + `docs/` in English; `CONTEXT.md` + this file may
keep bilingual notes.

---

## Decisions locked-in 2026-06-28 — pricing & monetization reset

A second decision pass on top of the original lock-in, captured here so the
rationale persists:

**Two SKUs only, contract-priced Pro.** The previous "Community (free) / Pro ($9/mo)
/ Team ($29/mo) / Cloud" four-tier ladder is retired. New surface:
  - **Open Source** — MIT, self-hosted, free. Everything an Operator needs.
  - **Pro** — single tier, contract via sales conversation, no public price.

**Pro gates exactly one UI surface.** Multi-project workspaces (per-project isolation
of audit/rules/SBOM/cache + per-project RBAC). The contract buys support + SLA +
compliance assistance (CRA / SBOM workflows) + production-deploy consulting (HA /
capacity / upgrade paths) + priority issue handling + that one UI. Never
feature-locked infrastructure. Multi-project is the conversational trigger for
the Pro upgrade — "we run Depsilo across many projects in production" maps directly
to "we want a support contract."

**Wedge + governance primitives stay in open-source.** Audit logs, rules engine,
security intelligence dashboard, OSV scanning, SBOM export, T1 supply-chain features
(minimum release age, malicious blocklist, freeze/snapshot, tamper detection), HA,
SSO/RBAC, webhook alerting — all open-source. The proxy itself + its governance
primitives are the compliance instrument; Pro is *workspace structure + the support
contract* around that instrument. Locking governance primitives behind Pro pattern-
matched as open-core greed and was reversed on 2026-06-28; the position is
deliberate, not provisional.

**Trial system retained.** 14-day self-evaluation of Pro features still works. The
post-trial UX no longer offers self-serve purchase; it offers a Contact sales link
to start the conversation.

**Lemon Squeezy retired.** The HTTPS license validation handshake against
api.lemonsqueezy.com was deleted along with the self-serve channel. License keys
now arrive out-of-band from a contract conversation; any non-empty key activates
Pro on the assumption "if the operator entered a key, the operator is on a
contract." Future Enterprise tooling (signed JWT keys, depsilo-owned license
server) can layer on top without breaking the public API.

**No cloud SKU.** Cloud / managed-hosted was removed from sales surfaces. For a
local-first dependency cache, cross-internet hosting cancels the value prop. We
are explicit about not selling it rather than implying it might come.

**Pro narrowed to multi-project (2026-06-28 second pass).** The original reset
left four UI features in Pro (audit logs, rules engine, security intelligence
dashboard, multi-project workspaces). A second pass on the same day moved the
first three to open-source and left only multi-project gated. Reasoning: a
self-hosted *control point* must ship governance primitives — audit, allow/deny
rules, security dashboard — free; locking them undermines the entire
positioning. Multi-project is the right single gate because it cleanly maps to
the buyer ICP (production teams running Depsilo across many projects) and
because the contract sells the support / SLA / compliance / consulting bundle,
not the feature itself. A narrow gate makes the funnel high-intent.

**Pro switched to $99 lifetime self-serve (2026-06-29 third pass).** Contact-
priced Pro from the 2026-06-28 reset turned out to assume sales bandwidth the
project does not have — every inbound contact-sales email would consume founder
attention with no infrastructure to triage. Switched to a flat one-time **$99
lifetime** price, displayed prominently in every CTA. **No payment provider
integrated yet:** the Buy CTA opens a pre-filled mailto: order email and the
maintainer processes payment (PayPal / Alipay / WeChat / bank) + emails back a
license key the operator pastes into the License page. One asynchronous
round-trip per sale, zero ongoing sales work. When provider integration lands
(Lemon Squeezy is the planned target — Merchant of Record, license-key
delivery, EU VAT handled), only `web/src/lib/buy.ts` changes — every CTA in
the admin UI and on the landing page reads from that helper. Price stays $99
lifetime through and after the provider swap; the trial system stays as the
free 14-day evaluation path before purchase.

---

## How to use this doc

1. Read this file **and** the repo (Go backend, TS/React frontend) before starting.
2. Order: **T0 first** (small, high trust-value) → **Task 1** → **Task 2**. Keep PRs small
   and reversible; show a short plan before any large change.
3. Hold to the **security-tool discipline**: transparent by default, signed, dogfooded,
   public fallback preserved.
4. **Out of scope for you (founder TODOs):** pricing, getting the first 10 users, launch
   content, directory listings. Don't spend cycles here.
