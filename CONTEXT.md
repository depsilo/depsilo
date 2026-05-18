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

### Product tiers

**Free**:
The fully open-source single-binary deployment. No license check, all 13 ecosystem adapters available. Goal: zero-friction adoption by Operators.

**Pro**:
A paid tier gated by `license.RequirePro` middleware. Currently encodes: audit logs, rules engine, security/OSV scanning, project management + SBOM. Buyer-facing features.

**Enterprise**:
Not yet built. Reserved language for future tier covering org-wide concerns: LDAP/SSO, clustering, SLA, professional support. Decision pending — see ADR-0001 if/when created.

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

> **Engineer:** "Can we move OSV scanning out of Pro?"
> **Domain expert:** "OSV is the Buyer's reason to upgrade. Keep it in Pro."

## Flagged ambiguities

- "User" was used to mean both **Operator** and **End User** — resolved: these are distinct personas with different needs. Code in `internal/db/models.go` uses `User` to mean DB user records (Operators); UI strings should say "Operator" or 运维 in copy that targets the persona.
- "Customer" was used loosely to mean the org, the Buyer, and the Operator interchangeably — resolved: org-level = "customer org", money person = **Buyer**, hands-on person = **Operator**.
