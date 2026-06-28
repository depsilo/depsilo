# Depsilo

A lightweight, single-binary dependency proxy/cache gateway covering 13 package ecosystems, sold as Open Core to mid-sized Chinese tech companies that want supply-chain compliance without operating Nexus.

## Language

### Customer & user roles

**Operator**:
The hands-on engineer who installs and runs Depsilo inside a customer's network. Typically a DevOps / platform engineer in a 50–300 person Chinese tech company. The product's UX is optimised for this persona.
_Avoid_: User (ambiguous with End User), Admin (technical role, not persona), 小李

**End User**:
Anyone whose `pip install` / `npm install` / `mvn` resolves through Depsilo. Never touches the UI. Cares only that the proxy is fast and doesn't break their build.
_Avoid_: Developer (too broad), Consumer

**Buyer**:
The person inside the customer org who signs the cheque. Usually the Operator's manager — CTO, head of infra, or security lead. Different motivations from the Operator (compliance, cost control, audit trail) and must be satisfied independently for a Pro/Enterprise deal to close.
_Avoid_: Client, customer (ambiguous with org-level "customer")

### Product tiers (2026-06-28 — see ADR-0003)

There are exactly **two** tiers and they are never named otherwise in
user-facing copy or code. The previous "Community / Pro / Team / Cloud"
four-tier ladder and the `$9 / $29 / hosted` pricing rungs were retired
on the date above — they signalled "hobby project" and let competitors
attack with "open-core greed". The replacement is one OSS surface plus
one contact-priced support contract.

**Open Source** (MIT, free, self-hosted):
The fully open-source single-binary deployment. No license check, all 14
ecosystem adapters available. Includes everything an Operator needs to
run Depsilo in production: cache + dashboard, upstream management, OSV
vulnerability scanning, **SBOM export (CycloneDX + SPDX)**, webhook
alerts, Prometheus metrics, single/multi-user access, **SSO / RBAC**,
and the T1 supply-chain wedge features (minimum release age, malicious
blocklist, freeze/snapshot, tamper detection) as those land. Goal:
zero-friction adoption by Operators. **No self-hosted necessity ever
gets paywalled** — especially not SSO/RBAC; the "SSO tax" pattern damages
trust badly and competitors give it free, so we do too.

**Pro** (Enterprise support contract, single tier, contact-priced):
A paid relationship gated by `entitlement.RequirePro` middleware,
unlocked by an Enterprise contract key. Currently encodes four
Buyer-facing capabilities: long-retention audit logs, the package
allow/deny rules engine + UI, the security intelligence dashboard
(cross-package vulnerability view + decision workflow), and multi-
project workspaces (team isolation). What the contract actually buys
is: support, SLA, compliance assistance (CRA / SBOM workflows), and
those four UI surfaces — **not** more storage, not more ecosystems,
not the wedge features (which stay open-source). No public price; the
trigger is a conversation with sales, not a credit-card form.

**Enterprise** is no longer a separate tier — the term used to imply a
future paywalled SSO/RBAC layer, which we explicitly do not want. When
copy needs a stronger word for "the Pro contract bundle for a large
buyer," use "Pro · Enterprise support" rather than promising a tier
that does not exist.

### Product surface

**Ecosystem**:
A package manager protocol that Depsilo proxies — `pypi`, `apt`, `npm`, `go`, `cargo`, `maven`, `rubygems`, `composer`, `nuget`, `conda`, `cran`, `helm`, `docker`. 13 of them as of 2026-05-18.
_Avoid_: Registry (overloaded with "Docker Registry"), Repo (overloaded with `apt` repo concept), Adapter (that's the code, not the product surface)

**Adapter**:
The Go implementation under `internal/adapter/<name>/` that translates an Ecosystem's protocol into Depsilo's unified cache flow. An Operator never sees this word; an Adapter is purely an internal artefact.
_Avoid_: Driver, Plugin (we don't have a plugin system)

**Upstream**:
A remote mirror Depsilo proxies for an Ecosystem (e.g. `tuna.tsinghua` for PyPI). Each Upstream has its own health, latency, and optional HTTP proxy.
_Avoid_: Mirror (Operator-facing the UI uses 上游源), Origin, Source

**Portal**:
The public-facing web UI at `/` — anonymous, shows Quick Start and live service status. Targets first-time Operators in the 10-minute deploy window.

**Admin**:
The authenticated web UI at `/admin` — for Operators after deployment. Hosts cache management, upstream config, project/security/audit views.

## Relationships

- A **Buyer** authorises money; an **Operator** runs the product; **End Users** consume it. All three must be satisfied for a sale to stick.
- The **Pro** tier exists to convert **Operators** of **Free** into paying **Buyers**.
- An **Ecosystem** is the product-language name; an **Adapter** is its implementation. One Ecosystem ↔ one Adapter.
- An **Upstream** belongs to exactly one **Ecosystem**.
- The **Portal** serves the first-90-seconds experience; the **Admin** serves everything after.

## Example dialogue

> **Designer:** "Should the audit log filter live on the Portal?"
> **Domain expert:** "No — auditing is a Pro feature, and the Portal is anonymous. Audit log review is what the Buyer cares about, but the UI lives in Admin because that's where Operators do work."

> **Engineer:** "Can we move OSV scanning into Pro?"
> **Domain expert:** "No — OSV scanning is the compliance wedge. Per ADR-0003 we keep the wedge open-source so the proxy itself is the buyer's compliance instrument. Pro buys the *management UI* around the wedge: the security-intelligence dashboard, the rules engine that acts on findings, the audit log that records the decisions."

> **Engineer:** "Should SSO be a Pro feature?"
> **Domain expert:** "No. 'SSO tax' kills enterprise adoption and competitors give it free. SSO/RBAC are open-source. Pro is governance/team/compliance UI + support, not commodity self-hosted infra."

## Flagged ambiguities

- "User" was used to mean both **Operator** and **End User** — resolved: these are distinct personas with different needs. Code in `internal/db/models.go` uses `User` to mean DB user records (Operators); UI strings should say "Operator" or 运维 in copy that targets the persona.
- "Customer" was used loosely to mean the org, the Buyer, and the Operator interchangeably — resolved: org-level = "customer org", money person = **Buyer**, hands-on person = **Operator**.
