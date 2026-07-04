# Depsilo

A lightweight, single-binary supply-chain enforcement layer for package installs.
It proxies 14 package ecosystems, caches artifacts locally, and refuses to serve
packages that violate operator policy.

## Language

### Customer & user roles

**Operator**:
The hands-on engineer who installs and runs Depsilo inside a customer's network. Typically a DevOps / platform engineer in a 50–300 person Chinese tech company. The product's UX is optimised for this persona.
_Avoid_: User (ambiguous with End User), Admin (technical role, not persona), 小李

**End User**:
Anyone whose `pip install` / `npm install` / `mvn` resolves through Depsilo. Never touches the UI. Cares only that the proxy is fast and doesn't break their build.
_Avoid_: Developer (too broad), Consumer

**Buyer**:
The person inside the customer org who signs off on adoption. Usually the
Operator's manager — CTO, head of infra, or security lead. Different motivations
from the Operator (compliance, cost control, audit trail) and must be satisfied
independently for Depsilo to stay deployed.
_Avoid_: Client, customer (ambiguous with org-level "customer")

### Distribution posture

User-facing documentation should describe Depsilo as MIT-licensed,
self-hosted, and open-source. Do not introduce product-tier copy in docs. The
current product story is the enforcement layer itself: cache, upstream
management, audit logs, package allow/deny rules, security intelligence, SBOM
export, webhook alerts, Prometheus metrics, and supply-chain policy primitives
such as minimum release age, malicious blocklist, freeze/snapshot, and tamper
detection as they land.

### Product surface

**Ecosystem**:
A package manager protocol that Depsilo proxies — `pypi`, `apt`, `npm`, `go`,
`cargo`, `maven`, `rubygems`, `composer`, `nuget`, `conda`, `cran`, `helm`,
`alpine`, `docker`, and `huggingface`. 14 first-class install surfaces in the
product copy today; Docker is served via the OCI `/v2/` route rather than the
same adapter list used by the other HTTP path prefixes.
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
- An **Ecosystem** is the product-language name; an **Adapter** is its implementation. One Ecosystem ↔ one Adapter.
- An **Upstream** belongs to exactly one **Ecosystem**.
- The **Portal** serves the first-90-seconds experience; the **Admin** serves everything after.

## Example dialogue

> **Designer:** "Should the audit log filter live on the Portal?"
> **Domain expert:** "No — the Portal is anonymous, and audit log review is Operator work. Audit UI lives in Admin because that's where Operators do work."

> **Engineer:** "Can we make OSV scanning optional or hidden?"
> **Domain expert:** "No — OSV scanning, the rules engine, and the security intelligence dashboard are part of the self-hosted control surface. Governance primitives stay open so the proxy itself is the buyer's compliance instrument."

> **Engineer:** "Should we build SSO into Depsilo?"
> **Domain expert:** "No. The 'SSO tax' pattern damages trust. Recommend a mature OIDC reverse proxy rather than building a shallow in-product SSO layer."

## Flagged ambiguities

- "User" was used to mean both **Operator** and **End User** — resolved: these are distinct personas with different needs. Code in `internal/db/models.go` uses `User` to mean DB user records (Operators); UI strings should say "Operator" or 运维 in copy that targets the persona.
- "Customer" was used loosely to mean the org, the Buyer, and the Operator interchangeably — resolved: org-level = "customer org", money person = **Buyer**, hands-on person = **Operator**.
