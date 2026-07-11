# Depsilo — Product & Engineering Direction

> A north-star brief for **Claude Code**. Read this before picking up roadmap work.
> It explains where Depsilo stands, the strategic bet, and a prioritized build
> order with concrete first tasks. When in doubt, optimize for the bet below.
>
> **2026-06-30 pivot:** The original "control point" framing (ADR-0003) is
> narrowed to "**enforcement layer**" after Artifact Keeper was found in the
> same space with significantly more shipped features. See
> [`docs/adr/0004-supply-chain-enforcement-layer.md`](./adr/0004-supply-chain-enforcement-layer.md)
> and [`docs/research/2026-06-30-competitive-landscape.md`](./research/2026-06-30-competitive-landscape.md)
> for the full reasoning. The TL;DR below reflects the post-pivot position.

---

## TL;DR — the bet

Depsilo is **a supply-chain enforcement layer that sits on the package-install
request path.** Not a general-purpose Artifactory replacement (that space is
won by [Artifact Keeper](https://artifactkeeper.com/) on the OSS side and by
Nexus/Artifactory on commercial). What depsilo does that none of those do
today: refuse to serve packages in real time based on supply-chain policy —
minimum release age, malicious blocklist, golden snapshot, tamper detection,
CRA-mode SBOM.

The 14-ecosystem cap is deliberate. Depth of enforcement on a focused
ecosystem set beats shallow coverage of 45. The cache function is the wedge
that gets the proxy installed; the value is the enforcement layer
on top of it.

**Coexistence over competition.** Operators who need a general artifact
registry should run Artifact Keeper or Nexus. Operators who need
supply-chain enforcement should run depsilo — optionally **in front of**
their existing registry, doing quarantine + blocklist + SBOM-of-actual-
traffic before requests reach the storage tier.

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

- **Early distribution:** `v0.8.0` is released; the malicious blocklist and tamper
  detection have landed on `master` and remain unreleased.
- **Product surface is broad and well-built:** 14 ecosystems, minimum-age quarantine,
  malicious-package blocking, tamper alerts, SBOM, CVE/OSV scanning, multi-upstream
  health checks, local/S3 artifact storage, Prometheus, and a polished site.
- **The missing 90% is users, trust, and distribution — not features.**

**Implication for engineering:** do **not** add breadth for its own sake. Add the few
things that (a) make the *enforcement-layer* story real and (b) make the tool trustworthy
enough for a security-conscious buyer.

---

## Positioning & non-goals

**We ARE:** lightweight (single binary, ~50 MB RAM, ~10-min deploy), self-hosted,
multi-ecosystem, with a built-in **supply-chain enforcement** layer that operates
on the request path (refuses to serve based on policy — not just scans and reports).

**We are NOT trying to:**
- Out-format Artifactory **or [Artifact Keeper](https://artifactkeeper.com/)** — do **not**
  chase 45+ ecosystems. The 14 we have stays. Deep enforcement on 14 beats shallow on 45.
- Beat dedicated SBOM/SCA vendors (Snyk, FOSSA, Anchore, Socket) on scanning depth.
- Build the surface AK has already shipped: iOS / Android / Windows MSI clients,
  WASM plugin host for custom formats, in-product SSO / SAML / LDAP, Helm chart parity.
  Recommend operators run AK or Nexus alongside depsilo for those.
- Be a general artifact *host* first. "Enforcement layer + cache wedge" beats
  "another Artifactory clone."

**Security-tool discipline (non-negotiable — we sell trust):**
- **Transparent by default.** Never ship a feature that hides what the tool did to a
  codebase. (This directly affects T0 below.)
- **Sign releases. Dogfood** (publish Depsilo's own SBOM). **Always keep an explicit
  public-registry recovery path:** preserve the original setting as a documented
  rollback, without configuring a parallel source that can bypass a 451 decision.

---

## Build order

### T0 — Credibility fixes (do first; small, high trust-value)

- [x] **Remove the "brand discipline" stealth integration prompt.** The Quick Start
      "one-prompt AI integration" currently tells the agent **not to write the product
      name/hostname** and to **drop the public-CDN fallback**. For a security-positioned
      tool this pattern-matches to the very attacks we defend against and will repel the
      buyers we want. Replace with a **transparent** prompt: name Depsilo + the URL,
      preserve the public index as a documented rollback (not a parallel policy bypass),
      and make the change reviewable. *(Frontend Quick Start page + the prompt generator;
      completed 2026-07-02.)*
- [ ] **Sign releases.** CI already publishes versioned binaries, checksums, container
      images, and SBOMs; add cosign/sigstore signatures and attestations.
- [x] **Dogfood SBOM.** Source and container-image CycloneDX + SPDX SBOMs are generated
      and attached to tagged releases. *(Completed 2026-07-09.)*

### T1 — The enforcement-layer wedge (the differentiator)

- [x] **Minimum release age / quarantine** — flagship. See **Task 1**.
      *(Released in v0.8.0, 2026-07-09.)*
- [x] **Known-malicious blocklist** — hard-block malicious (not merely vulnerable)
      versions. See **Task 2**. *(Landed on master 2026-07-09; unreleased.)*
- [ ] **Freeze / golden snapshot.** A mode that serves only versions in an approved
      snapshot; "promote current cache to snapshot," export/import. Enables reproducible /
      offline builds immune to upstream poisoning.
- [x] **Tamper detection.** Persist a content hash per immutable artifact on first
      fetch; alert if its upstream content later changes. *(Landed on master
      2026-07-10; unreleased.)*
- [ ] **CRA-mode SBOM export.** Ensure output carries NTIA/CRA minimum elements
      (name+version, supplier, **purl**, **SHA-256** hash, license, dependency
      relationships), is signable, and has a "technical-file" export preset.

### T2 — Adoptability & trust (table stakes — **reduced scope post-2026-06-30**)

ADR-0004 found Artifact Keeper already ships SSO / RBAC / Helm chart in OSS.
Rebuilding those is now a low-ROI use of time. Updated T2:

- [x] **Alerting:** webhook/Slack on policy hits (blocked malware, quarantined
      version, CVE over threshold). **Shipped 2026-06-29 (T1/7).**
- [ ] **One-command deploy + Helm chart** — basic only, not feature-parity with
      AK's IaC bundle. Just enough so a k8s operator can `helm install` and have
      a working enforcement layer.
- [ ] **Strong docs** (deployment guide, threat model, policy-tuning guide,
      AK + Nexus + Verdaccio coexistence guide).
- [ ] ~~**RBAC + SSO (OIDC/LDAP) in open-source.**~~ **Dropped.** Recommend
      operators deploy depsilo behind an OIDC reverse proxy
      ([oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/) /
      [Authelia](https://www.authelia.com/) / [Pomerium](https://www.pomerium.com/)).
      These are mature, OSS, and one-line YAML to wire. The "no SSO tax"
      narrative is preserved (operators still get SSO for free) without us
      building + maintaining yet another OIDC implementation.
- [ ] ~~**Multi-node / HA made genuinely easy.**~~ **De-prioritized.** Single-
      node deployment is the realistic target for the enforcement-layer use
      case (most teams gate behind one proxy). SQLite + local/S3 artifact
      storage is the supported deployment today.
      PostgreSQL support would need to land before documenting a multi-node
      database + S3 + load-balancer pattern.

## First tasks, specced

### Task 1 — Minimum release age (quarantine)

**Goal:** refuse to serve any package version whose upstream publish time is younger than
a configurable threshold, so a freshly-poisoned version cannot enter a build before the
community catches it.

**Config — built-in per-ecosystem defaults + explicit overrides:**

```yaml
supply_chain:
  min_release_age:
    default: 0             # fallback for unknown/future ecosystem keys only
    pypi: 3d
    npm: 7d
    cargo: 3d
  mode: block            # the only implemented mode
  allow:                 # bypass quarantine for trusted pins
    - "npm:internal-*"
    - "pypi:requests==2.32.3"
```

**Behavior:**
- On request for `(ecosystem, pkg, version)`, look up the upstream publish timestamp
  (npm registry `time[version]`; PyPI JSON `upload_time_iso_8601`; equivalent per
  ecosystem). **Cache the timestamps** — never hammer upstream per request.
- If `now - publish_time < threshold` and not matched by `allow`:
  - `block` mode → return a clear error with package/version, age, and threshold.
  - `serve_last_eligible` remains a deferred design option. This build rejects it at
    startup instead of silently behaving like `block`.
- Record blocks in the Admin quarantine event stream and fire their webhook. Bypass,
  approve, revoke, and override actions are audited and visible but do not fire webhooks.
- **Manual approve:** UI button + API to release a quarantined `(pkg, version)` early
  (adds to the approved set). Audit who/when.

**Edge cases:** not-found/unsupported publish timestamps follow `fail_closed`; a true
upstream outage always allows with a warning so a registry incident does not halt every
build. An exact-pinned lockfile version under quarantine fails closed with the same error.
Pre-release / yanked versions use the same decision path.

**Acceptance:**
- [x] Configurable for the 13 resolver-backed ecosystems; `allow` overrides work.
      APT has no gate and Go has no publish-time resolver, so both remain at zero.
- [x] A version published `< threshold` is blocked with a clear
      message; one `≥ threshold` serves normally.
- [x] Approve action releases it immediately and is audited.
- [x] Quarantine decisions appear in the Admin event stream; block events fire a webhook.
- [x] Timestamps are cached; no per-request upstream calls.

### Task 2 — Known-malicious blocklist

**Goal:** hard-block versions known to be **malicious** (distinct from merely vulnerable).

- Sync the **OSV malicious-packages** dataset (MAL-* advisories, including any GHSA
  aliases carried by those records) for npm, PyPI, Cargo, RubyGems, Composer, NuGet,
  Go, and Maven. Store `(source_id, ecosystem, package)` rows with an explicit-version
  list or all-version marker. Bounded ranges without explicit versions are skipped;
  range evaluation is not implemented.
- On request, if matched: refuse with a clear *"known-malicious, blocked"* error and an
  alert — **never serve.** Provide an explicit, **audited** admin override for false
  positives.
- Surface decisions in the Admin quarantine/blocklist views; block events fire a
  webhook. Keep entirely separate from CVE handling (CVE = warn/serve; malware =
  hard block).

**Acceptance:**
- [x] Sync runs + refreshes on schedule.
- [x] A known-malicious version is blocked end-to-end.
- [x] Override is audited and visible; block events are visible and alerted.

---

## Decisions locked-in alongside this brief (2026-06-28)

This file is the living north star — decisions that landed when this file was first
written are captured here so future-you remembers the rationale. ADR-0003 captures
the strategic shift itself; the items below are settings within it.

**Integration prompt:** transparent by default (T0 #1). Every config edit names
Depsilo + the mirror URL in a comment. Preserve a documented one-line rollback to the
original registry, but do not configure parallel indexes or all-error fallthrough that
can bypass enforcement. The agent must print a diff summary at the end.

**Released artifacts (signing currently parked):** v0.8.0 shipped binaries, checksums,
container images, and SBOM attachments without signatures. The target signing stack is
**cosign keyless (OIDC via GitHub Actions)** for binaries, checksums, container images,
and SBOMs, distributed via GitHub Releases + GHCR + Docker Hub. Use an RC soak before the
first signed stable release.
Reproducibility goal is **deterministic** (`-trimpath`, `-buildvcs=false`, locked
versions); bit-for-bit reproducible is out of scope for v0.x.

**SBOM:** Syft generates **CycloneDX + SPDX** covering Go module/source deps, `web/` npm
deps, **and** the container image. Tagged-release workflows upload them as GitHub
Release attachments. Publishing at `.well-known/sbom/{version}.cdx.json` and
cosign attestations remain planned.

**T1 Task 1 defaults:** `pypi: 3d` · `npm: 7d` · `cargo: 3d` · `go: 0` (Go modules
are immutable + checksum DB already mitigates) · `maven/nuget/rubygems: 3d`.
Not-found/unsupported timestamp → **fail-closed**; upstream outage → allow + warning.
Allow-list supports glob
(`npm:@scope/internal-*`), exact pin (`pypi:requests==2.32.3`), and range
(`npm:react>=18.0.0`). Default mode = `block`. Approve action is admin-only with
mandatory reason text + audit log entry.

**T1 Task 2 defaults:** data source = **OSV malicious-packages** (MAL-* advisories;
no paid third-party feeds or direct GitHub Advisory API sync). Sync every 6 hours.
Override mechanism is admin-only,
mandatory reason, **auto-expires in 24h by default** — no permanent whitelist.
Block response = HTTP 451 with `code: MALICIOUS_BLOCKED`. Matched versions are
gated before cache lookup, so they are neither served nor newly cached. Existing cache
bytes remain on disk until LRU or operator cleanup but are unreachable through the gate.

**Governance features go in open-source.** Minimum release age, malicious blocklist,
OSV scanning, the audit log, the package allow/deny rules engine, and the security
intelligence dashboard are open-source product wedges. Depsilo's own release SBOMs
are public; the current runtime per-project SBOM endpoint remains part of the Pro
multi-project surface. CRA-mode export must make that boundary explicit.

**SSO / RBAC stay external.** This supersedes the original plan to build them in-product.
The "SSO tax" pattern damages trust, but mature reverse proxies already solve the problem.
Document oauth2-proxy / Authelia / Pomerium instead of building a shallow OIDC layer;
commodity self-hosted infrastructure should not distract from enforcement primitives.

**Versioning policy until v1.0:** breaking changes allowed across minor versions;
config-file backwards compatibility guaranteed within each `v0.x` line.

**No telemetry / anonymous reporting.** Self-hosted trust commitment is incompatible
with phoning home.

**Docs language:** main README + `docs/` in English; `CONTEXT.md` + this file may
keep bilingual notes.

---

## Decisions locked-in 2026-06-30 — enforcement-layer pivot

**Pivot to "Supply-Chain Enforcement Layer" (2026-06-30 —
[ADR-0004](./adr/0004-supply-chain-enforcement-layer.md)).** Competitive research
on 2026-06-30 found [Artifact Keeper](https://artifactkeeper.com/) — a 794-star,
MIT-licensed, Rust-based "Artifactory alternative" that already ships 45+
ecosystem formats, SSO/LDAP/SAML/RBAC in OSS, native iOS/Android/Windows
clients, Helm + Terraform IaC, and a WASM plugin system. That executed most of
ADR-0003's T2 roadmap before depsilo started it. Concurrently,
**pnpm 11 / npm 11.10.0 / uv** all shipped client-side
minimum-release-age — validating the T1 wedge thesis but making "we offer this
feature" insufficient. Pivot:

- Reposition from "lightweight multi-eco control point" → "**supply-chain
  enforcement layer**." Cap ecosystems at 14, focus on enforcement primitives
  only a proxy on the request path can do: minimum release age, malicious blocklist,
  and tamper detection (all landed), followed by freeze/snapshot and CRA SBOM,
  SIEM-grade audit + webhook routing.
- **Drop building SSO/RBAC ourselves.** Recommend operators front depsilo with
  oauth2-proxy / Authelia / Pomerium. "No SSO tax" preserved without
  re-implementing yet another OIDC stack.
- **De-prioritize HA.** Single-node is the realistic enforcement-layer deploy.
  SQLite is the only database backend today. Add PostgreSQL support before documenting
  a PostgreSQL + S3 + LB pattern; don't ship managed HA until enforcement features are
  demonstrably ahead of AK.
- **Coexistence over competition.** README + landing should acknowledge AK by
  name and recommend it for general artifact storage. Position depsilo as
  optionally deployable **in front of** AK / Nexus / Artifactory as a
  supply-chain wall. Honesty is positioning in OSS culture.
- **Research record:** [`docs/research/2026-06-30-competitive-landscape.md`](./research/2026-06-30-competitive-landscape.md).

---

## How to use this doc

1. Read this file **and** the repo (Go backend, TS/React frontend) before starting.
2. Order: **T0 first** (small, high trust-value) → **Task 1** → **Task 2**. Keep PRs small
   and reversible; show a short plan before any large change.
3. Hold to the **security-tool discipline**: transparent by default, signed, dogfooded,
   and preserve an explicit public-registry recovery path.
4. **Out of scope for you (founder TODOs):** getting the first 10 users, launch
   content, directory listings. Don't spend cycles here.
