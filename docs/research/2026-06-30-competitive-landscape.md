# Competitive Landscape — 2026-06-30

> Research record. Frozen at the date in the filename. Sources cited.
> For the strategic conclusions drawn from this, see
> `docs/adr/0004-supply-chain-enforcement-layer.md`.
> For the resulting roadmap changes, see `docs/DIRECTION.md`.

## Why this exists

Three batches of competitive research on 2026-06-30 (the day after T1
Task 1 shipped) materially changed depsilo's strategic picture. This
file captures what we found, with sources, so future maintainers can
see the data the pivot was based on and re-evaluate when those facts
change.

The research was triggered by the user question "再调研一下市场的竞品" and
expanded as new entrants surfaced.

## TL;DR

- **Artifact Keeper** ([artifactkeeper.com](https://artifactkeeper.com),
  [GitHub](https://github.com/artifact-keeper/artifact-keeper)) is a
  794-star, MIT, Rust-based "Artifactory alternative" with 45+ ecosystem
  formats, SSO/LDAP/SAML/RBAC in OSS, GPG + cosign signing, Helm
  charts, native iOS / Android / Windows clients, and **no Pro tier**.
  It has executed most of depsilo's T2 roadmap before depsilo started
  it. Latest release v1.2.3 (2026-06-29).
- **pnpm 11** turned `min-release-age` to default ON (24h) in 2026.
  **npm 11.10.0** added `--min-release-age` natively. **uv** has
  `exclude-newer`. The supply-chain-cooldown idea is now mainstream
  at the client level.
- **[cooldowns.dev](https://cooldowns.dev/)** ships a client-side
  helper to configure cooldowns across pip / uv / npm / pnpm / yarn /
  bun / cargo / bundler. Free, OSS, run by mprpic.
- **RepoFlow** ([repoflow.io](https://www.repoflow.io)) is
  **closed-source commercial** (Personal tier prohibits commercial use),
  $1,999–2,999/year self-hosted, 13 ecosystems, has CVE scanning +
  audit trails but **no minimum-release-age / quarantine / SBOM /
  malicious blocklist**.
- **Two real attacks during spring 2026** (LiteLLM on PyPI ~2h
  exposure, axios on npm ~2-3h exposure) validated the depsilo
  T1 Task 1 wedge thesis: 3+ day cooldown would have stopped both.

## Direct competitors

### Artifact Keeper (the new threat)

Verified facts as of 2026-06-30:

| Field | Value |
|---|---|
| Source code | [github.com/artifact-keeper/artifact-keeper](https://github.com/artifact-keeper/artifact-keeper) |
| Stars | 794 |
| Language | Rust |
| License | MIT — explicitly "no open-core, no source-available" |
| Latest release | v1.2.3 (2026-06-29) |
| Sub-projects | web (Next.js 15 + shadcn), iOS (SwiftUI), Android (Compose), Windows MSI, Helm/Terraform IaC, Swift SDK, WASM plugin template |
| Ecosystems | "45+ formats" — explicit list: Maven, npm, PyPI, NuGet, Docker/OCI, Helm, Terraform, Debian/APT, RPM, Alpine, HuggingFace, Conda, "many others" |
| Plugin model | WASM plugins for custom formats |
| Container base | DISA STIG-approved Red Hat UBI 9, non-root |
| Vuln scanning | Trivy + Grype + OpenSCAP + OWASP Dependency-Track |
| Signing | GPG + cosign integrated |
| SSO | LDAP + SAML + OIDC **in OSS** |
| RBAC | Fine-grained, in OSS |
| Audit logging | In OSS |
| Pricing | **$0 forever**, no Pro tier |
| Quarantine | "Policy engine, quarantine workflow" — likely manual upload-policy rules, **NOT** time-based minimum release age |
| Min release age | **Not mentioned in README** |
| Malicious package detection | **Not mentioned in README** |
| SBOM export | Not in README (may not have) |
| Activity signals | 85 open issues, 24 PRs, 79 forks, multiple sponsors (narek01, injectedfusion, dragonpaw); commits within last 24h |

**Bottom line:** AK has feature-parity OR exceeds depsilo on every dimension
except the supply-chain enforcement primitives (min release age, malicious
blocklist, CRA SBOM workflow) that depsilo is actively building.

### RepoFlow (closed-source commercial)

Verified facts as of 2026-06-30:

| Field | Value |
|---|---|
| Source code | **Not open-source.** [Org](https://github.com/RepoFlow-Package-Management) only has auxiliary tools (benchmarks, sync service, mock server) |
| Ecosystems | 13 — Docker, npm, PyPI, Maven, NuGet, Go, Helm, RPM, Debian, Cargo, Composer, RubyGems, universal files |
| Self-hosted pricing | Personal: free **but no commercial / client use** · Standard: $1,999/yr · Pro: $2,999/yr · Enterprise: contact |
| Cloud pricing | Free (10 GB) / $79/mo / $499/mo / Enterprise |
| Security features | CVE scanning, audit trails, upload restrictions ("upload policy") |
| Quarantine / min release age / SBOM | **Not mentioned** in marketing |
| AI features | Yes — "AI requests" is a quota dimension |

**Bottom line:** Not a OSS competitor. Same price band as Artifactory.
depsilo's $99 lifetime is 20-30× cheaper. depsilo's MIT is a real
differentiator (RepoFlow's "Personal free, no commercial use" is the
classic source-available trap).

### Other direct competitors (existing assessment unchanged)

- **Nexus Repository** (Sonatype) — OSS edition + Pro ~$5K/yr, JVM,
  heavy. Strong enterprise install base. Has quarantine (commercial).
- **JFrog Artifactory** — $25K+/yr, very expensive. Industry standard
  for highly regulated industries. Has quarantine via X-Ray.
- **Verdaccio** — npm-only OSS, no plans to expand.
- **ProGet** (Inedo) — $3K/yr Pro, Windows-first heritage, license
  enforcement is the selling point.
- **Cloudsmith** — SaaS only ($129+/mo). Not actually competitive with
  self-hosted depsilo.
- **Reposilite** — Maven-only OSS.
- **Forgejo / Gitea Package Registry** — bolt-on to Git platform, not
  a serious standalone proxy.

## Adjacent (supply-chain security tools, not direct competitors)

Per earlier research: Snyk / Socket / Phylum (acquired by Veracode
late 2024) / Endor Labs / Chainguard / JFrog X-Ray / Aikido / Mend.

These are **scan-and-report** tools. None occupy depsilo's
**proxy-as-enforcement-point** position. They tell you a package is
risky after-the-fact; depsilo refuses to serve it in the first place.

**Aikido** is consistently ranked #1 in "supply chain security tools"
listicles for 2026, but it's an all-in-one app-sec SaaS, not a proxy.

## The cooldown / minimum-release-age movement (2026)

Material market shift between depsilo's strategic bet (ADR-0003,
2026-06-28) and today (2026-06-30):

| Tool | Feature | When | Default? |
|---|---|---|---|
| pnpm 11 | `minimum-release-age` | 2026 | **ON, 24h default** |
| npm 11.10.0 | `--min-release-age` | 2026 | OFF |
| uv | `exclude-newer = "3 days"` | shipped | OFF |
| Renovate | min-release-age key concept | shipped | OFF |
| [cooldowns.dev](https://cooldowns.dev/) | Cross-PM helper | 2026 | Tool-driven |
| [dehrenschwender/set-minimum-package-release-age](https://github.com/dehrenschwender/set-minimum-package-release-age) | Same | 2026 | 7-day default |

**Validation:** the security community converged on "wait 3+ days
before installing fresh packages" as the right answer. depsilo bet on
this and won the directional call.

**Threat:** if pnpm has it on by default, why would a pnpm user buy
depsilo for this?

**Answer:** depsilo is an **organization-level enforcement layer**.
Client cooldowns are a personal choice; a proxy-level policy is what
makes "the org doesn't ship unreviewed dependencies, ever" true:

- Works for **any** client (legacy npm, pip, cargo, etc., regardless
  of whether the client supports cooldowns natively)
- One config covers all 14 ecosystems with consistent semantics
- Dev can't disable it by tweaking local config
- Audit trail of every decision (client-side gives no telemetry)
- Org-level allow-list approve workflow

The right framing: *"You should turn on pnpm 11's cooldown AND deploy
depsilo. The first protects each dev; the second protects the
organization."*

## Real attacks during the window (2026 spring)

Sources: [dev.to "Lessons from the Spring 2026 OSS Incidents"](https://dev.to/trknhr/lessons-from-the-spring-2026-oss-incidents-hardening-npm-pnpm-and-github-actions-against-1jnp),
search results above.

| Incident | Package | Damage window |
|---|---|---|
| 2026-03 | LiteLLM (PyPI) | ~2 hours (credential harvesting) |
| 2026-03 | axios (npm, 100M+/wk downloads) | ~2-3 hours live |
| 2025-09 | Shai-Hulud (~25K repos) | days-to-weeks before yank |
| 2026-Q1 | Mini Shai-Hulud (GitHub Actions secrets) | hours-to-days |

Analyst summary: "10 prominent supply-chain attacks → 8 had
exploitation windows under 1 week → a 3-day cooldown would have
blocked most." This is exactly depsilo T1 Task 1's mechanism.

## Sources

Direct:
- [artifactkeeper.com](https://artifactkeeper.com/)
- [github.com/artifact-keeper](https://github.com/artifact-keeper)
- [github.com/artifact-keeper/artifact-keeper README](https://github.com/artifact-keeper/artifact-keeper)
- [repoflow.io](https://www.repoflow.io/) + [pricing](https://www.repoflow.io/pricing) + [features](https://www.repoflow.io/features)
- [github.com/RepoFlow-Package-Management](https://github.com/RepoFlow-Package-Management)
- [cooldowns.dev](https://cooldowns.dev/)
- [github.com/dehrenschwender/set-minimum-package-release-age](https://github.com/dehrenschwender/set-minimum-package-release-age)

Movement:
- [pnpm 11 default ON (Cryptika)](https://www.cryptika.com/pnpm-11-turns-on-minimum-release-age-by-default-to-reduce-npm-supply-chain-risk/)
- [npm 11.10.0 min-release-age (Brandon Pugh)](https://www.brandonpugh.com/til/node/package-version-cooldown/)
- [Renovate docs](https://docs.renovatebot.com/key-concepts/minimum-release-age/)
- [pnpm 11 (cybersecuritynews)](https://cybersecuritynews.com/pnpm-11-turns-on-minimum-release-age/)

Incidents:
- [Lessons from Spring 2026 OSS Incidents (dev.to)](https://dev.to/trknhr/lessons-from-the-spring-2026-oss-incidents-hardening-npm-pnpm-and-github-actions-against-1jnp)
- [Anchore: RepoFlow + Grype case study](https://anchore.com/blog/security-without-friction-how-repoflow-created-a-devsecops-package-manager-with-grype/)

Market context:
- [Aikido top 12 supply chain security tools 2026](https://www.aikido.dev/blog/top-software-supply-chain-security-tools)
- [Cloudsmith 2026 supply chain security guide](https://cloudsmith.com/blog/the-2026-guide-to-software-supply-chain-security-from-static-sboms-to-agentic-governance)
- [awesome-software-supply-chain-security](https://github.com/bureado/awesome-software-supply-chain-security)

## What needs re-checking later

- AK's actual deployment count in production (stars ≠ users)
- AK's roadmap — they may add min release age + malicious blocklist
  next quarter, closing depsilo's remaining moat
- Whether pnpm 11's default-ON cooldown changes operator buying
  behavior for proxy-level enforcement (does the existence of
  client-side cooldown reduce demand for proxy-side?)
- China-specific: AK and RepoFlow are both English-first; whether
  depsilo's Chinese docs / 信创 angle is a real moat needs a
  Chinese-buyer interview, not desk research
- Verification of exact pricing: RepoFlow pricing scraped from
  marketing page only; no buyer confirmation of actual deal prices
