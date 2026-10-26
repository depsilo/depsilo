# Depsilo

A self-hosted dependency cache and distribution service for reuse across
machines and networks. Current capabilities and planned work are recorded in
[PRODUCT.md](PRODUCT.md); this glossary defines their vocabulary.

## Language

### Customer & user roles

**Operator**:
The hands-on person who deploys and operates Depsilo, including its cache,
Upstreams, delivery workflows, and policy. Typically a Homelab operator,
technical founder, or DevOps/platform engineer without dedicated artifact staff.
_Avoid_: User (ambiguous with End User), Admin (technical role, not persona), 小李

**End User**:
A developer or CI worker whose dependencies are delivered through Depsilo.
They need dependable installs and may use connection guidance without managing
the service.
_Avoid_: Developer (too broad), Consumer

**Buyer**:
When a team, rather than an individual, adopts Depsilo, the person who signs off
on adoption. This may be a technical founder, engineering lead, or
infrastructure/security owner. Their motivations differ from the Operator's
(bandwidth cost, build continuity, transfer effort, and auditability), even when
one person holds both roles.
_Avoid_: Client, customer (ambiguous with org-level "customer")

### Operator access

**Operator Account**:
The durable Depsilo account through which an Operator accesses Admin. Its
permissions and enabled state belong to the account, independently of the
identity used to sign in.
_Avoid_: End User, Login Identity, SSO user

**Authentication Source**:
A named, independently configured connection through which Depsilo trusts an
external identity provider or user directory. Its durable identity is separate from its display
name and authentication protocol; it is not an Operator Account or a Permission
Source. Its saved settings, running settings, and a particular login-test
result are separate states; enabling a source does not prove its availability.
_Avoid_: Login Identity, Permission Source, Upstream, Casdoor (a provider product)

**Login Identity**:
A proven way to sign in to an Operator Account, such as its local password or
an identity issued through an Authentication Source. Linking adds a sign-in
method to the same account without merging accounts or transferring permissions.
_Avoid_: Operator Account, Permission Source

**Permission Source**:
The authority that determines an Operator Account's role: local administration
or its identity provider. Adding or removing a Login Identity does not transfer
that authority.
_Avoid_: Login method, group membership, project role

### Product surface

**Ecosystem**:
A package-manager protocol whose metadata and artifacts Depsilo serves, such
as PyPI, npm, or Maven.
_Avoid_: Registry (overloaded with "Docker Registry"), Repo (overloaded with `apt` repo concept), Adapter (that's the code, not the product surface)

**Adapter**:
The internal translator between an Ecosystem's protocol and Depsilo's shared
dependency-delivery behavior. Operators work with Ecosystems rather than Adapters.
_Avoid_: Driver, Plugin (we don't have a plugin system)

**Upstream**:
A remote mirror Depsilo proxies for an Ecosystem (e.g. `tuna.tsinghua` for PyPI). Each Upstream has its own health, latency, and optional HTTP proxy.
_Avoid_: Mirror (Operator-facing the UI uses 上游源), Origin, Source

**Cascade Peer**:
A parent Depsilo node that another node can use to reach an Upstream and reuse
cached responses. Each node retains its own cache and policy authority.
_Avoid_: Upstream (a peer is a Depsilo node, not a mirror), Proxy (that is the per-upstream HTTP proxy), Cluster (no shared control plane)

**Portal**:
The anonymous web surface for client connection guidance and service status.

**Admin**:
The authenticated web surface where Operators manage the service, inspect
dependency activity, and make policy decisions.

### Dependency distribution

**Dependency Set**:
The artifacts and metadata required for a declared workload and target
environment. Its scope includes relevant dependency versions and platform
requirements.
_Avoid_: Cache Snapshot, SBOM, entire cache

**Transfer Bundle**:
A verifiable delivery unit containing artifacts, metadata, and a manifest for
moving a Dependency Set, or an increment to one, between nodes.
_Avoid_: Instance Backup, Cache Snapshot, SBOM

**Baseline Inventory**:
The inventory of content a target node is confirmed to hold for a transfer.
It provides the basis for identifying content missing from that target.
_Avoid_: Latest export, source cache, file modification time

**Incremental Transfer**:
Delivery of the content needed beyond a target's Baseline Inventory, together
with the metadata needed for the resulting Dependency Set.
_Avoid_: Binary patch, complete cache copy, Cascade Peer

**Cache Snapshot**:
A named manifest of observed artifact identities and hashes used to select
approved versions. It does not contain the artifact bytes needed for transfer.
_Avoid_: Transfer Bundle, Instance Backup, complete Dependency Set

**Isolated Network**:
A target network that cannot reach public Upstreams and receives dependencies
through an approved offline transfer process.
_Avoid_: Slow network, temporarily unavailable Upstream, Cascade Peer

## Relationships

- A **Buyer** authorises money; an **Operator** runs the product; **End Users** consume it. All three must be satisfied for a sale to stick.
- An **Ecosystem** is the product-language name; an **Adapter** is its implementation. One Ecosystem ↔ one Adapter.
- An **Upstream** belongs to exactly one **Ecosystem**.
- An **Upstream** may egress through at most one **Cascade Peer**; the peer shares cache results, never the database or policy.
- The **Portal** serves the first-90-seconds experience; the **Admin** serves everything after.
- An **Operator Account** may have more than one **Login Identity** while retaining
  one **Permission Source**; using an external identity does not elevate a locally
  managed account's permissions.
- A **Transfer Bundle** carries content for a **Dependency Set**; an
  **Incremental Transfer** accounts for the target's **Baseline Inventory**.
- A **Cascade Peer** requires a network path between nodes; transfer into an
  **Isolated Network** uses an approved offline process.

## Example dialogue

> **Designer:** "Should the audit log filter live on the Portal?"
> **Domain expert:** "No — the Portal is anonymous, and audit log review is Operator work. Audit UI lives in Admin because that's where Operators do work."

> **Engineer:** "Does exporting a Cache Snapshot move the dependencies to another network?"
> **Domain expert:** "A Cache Snapshot carries a manifest. A Transfer Bundle must also carry the artifacts and metadata needed for the declared Dependency Set."

> **Engineer:** "Does binding a company login make the company directory authoritative for this account?"
> **Domain expert:** "No. Binding adds a Login Identity. The account's Permission Source is a separate choice, so its local permissions and recovery password remain local."
