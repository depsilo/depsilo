# Threat Model

> Published 2026-10-06 for ADR-0004 T2 ("strong docs") and to feed the 1.0
> security review. Scope: the shipped `v0.x` line and the supported
> single-instance deployment (SQLite + local or S3 storage).
>
> Read together with [SECURITY.md](../SECURITY.md) (reporting process and
> explicitly accepted limitations), [docs/admin-control-plane.md](admin-control-plane.md)
> (control-plane authority), and [docs/compatibility.md](compatibility.md)
> (what stays stable). This file states what Depsilo defends, what it assumes,
> and what it knowingly does not defend.

## 1. Deployment shape and assumptions

Depsilo sits on the dependency request path: package clients reach its proxy
routes, Depsilo serves from its cache or fetches from the configured
upstreams, and every artifact request passes the enforcement gates
(known-malicious blocklist, snapshot-only mode, minimum release age).
Operators additionally use the authenticated Admin surface; the anonymous
Portal and the read-only MCP endpoint are separate surfaces.

Assumptions, all operator-controlled:

1. Depsilo runs inside the operator's network boundary; TLS terminates in
   front of it (nginx/Caddy/Traefik) as recommended in `SECURITY.md`.
2. The state directory (SQLite file, storage, config with secrets, signing
   keys) is readable only by the service account.
3. The host, container runtime, and reverse proxy are not compromised.
4. `config.toml`, `.dev-jwt-secret`, and runtime data are never committed.
5. A deployment serves one Depsilo process per SQLite database; HA is not a
   shipped capability.

## 2. Assets

| Asset | Where it lives | Why it matters | Primary controls |
| --- | --- | --- | --- |
| Cached artifact bytes | storage dir or S3 bucket | Builds install these bytes | Tamper detection (first-seen SHA-256, mismatch alert, first-seen copy kept), upstream provenance bindings, ecosystem checksums/signatures preserved end to end |
| Policy data (package rules, blocklist, snapshots, approvals, allow-lists) | SQLite | Decides what may be served | Authenticated admin writes, audited mutations, fail-closed gates |
| Admin credentials and API tokens | SQLite (bcrypt password hashes, hashed tokens) | Full control plane | Read/write capability groups, Pro entitlement gates, bootstrap-token flow |
| External Operator identities and sessions | SQLite | Account binding, admission, revocation | Stable source ownership plus exact issuer/subject identity; explicit password-proven binding; absolute session lifetime and local account checks |
| Per-source OIDC client secrets | Operator configuration/environment | Confidential provider client authentication | Write-only Admin input restricted to local credential authority; never returned; issuer-origin constrained requests; private configuration |
| Instance signing key | managed `data/secrets/jwt.key` relative to the config directory, or explicit environment/config secret | Mints admin sessions and signs npm / extra PyPI artifact URLs | Automatic 256-bit generation for new instances; mode-0700 directory and mode-0600 file; restart reuse; explicit-secret startup guard |
| Setup bootstrap token | current process and private `data/secrets/bootstrap.token` during initialization | Authorizes initial administrator creation | Mode-0600 file, refreshed before serving, removed after setup; local `depsilo setup token` requires the service to be awaiting setup; no public token-read endpoint |
| Ordinary Upstream and proxy URL credentials | SQLite | Upstream/proxy accounts | Full URLs are available to write-capable Principals; masked for other Principals |
| Docker registry credentials and extra-index keys | config file | Config-owned package sources | Private configuration; not exposed through Admin responses |
| Webhook and SIEM collector URLs | SQLite | Outbound data path | Full URLs are available to write-capable Principals; masked for other Principals |
| SIEM collector tokens (incl. Splunk HEC) | SQLite | Collector authentication | Write-only in the API; responses expose configured state, not the token |
| Cascade shared/peer tokens | config file | Authenticated parent relay | Write-only in the API; responses expose configured state, not the token |
| SBOM signing key (Ed25519) | operator-provided PEM file | Provenance of CRA exports | Never read into the DB; public key travels with each signature |
| License key | SQLite (plaintext by design) | Entitlement only, not a credential | Documented as a key identifier, masked in the UI |
| Audit log and quarantine events | SQLite | Accountability; SIEM source stream | Delivery cursor + lag/error status in Admin; at-least-once forwarding |
| Compile-cache objects and credentials | compile storage + SQLite | Source code and build output | Per-namespace credentials; plaintext returned once at creation and omitted from lists (see `docs/compile-cache.md`) |

## 3. Adversaries considered

| Adversary | Capability | In scope |
| --- | --- | --- |
| Malicious package publisher | Publishes malware, typosquats, or a rushed fresh release | Yes |
| Compromised or malicious upstream/mirror | Serves altered bytes or metadata for an existing version | Yes |
| On-path network attacker | Reads/modifies traffic between clients, Depsilo, and upstreams | Yes |
| Authenticated but low-privilege operator | Read-only admin, valid client credentials | Yes |
| Rogue administrator | Can change policy, create overrides, read logs | Yes (audited; host-level trust is assumed) |
| Supply-chain attacker on Depsilo releases | Alters binaries/images/SBOMs | Yes (signing and verification, §5.12) |
| Host root / hypervisor attacker | Reads the SQLite file and config secrets directly | No — accepted risk, see §6 |

## 4. Trust boundaries

| Boundary | Enforcement |
| --- | --- |
| Package client → proxy routes | **Open by design.** Package managers cannot present Depsilo credentials on every fetch, so proxy routes are not access-controlled. A `Bearer depsilo_proj_*` token is optional and only attributes usage; a missing or unknown token still serves, untracked. `SECURITY.md` therefore requires restricting *network reachability* of the proxy. |
| Admin browser/API → control plane | JWT bearer sessions; read/write and local credential-management capability groups; `RequirePro` for the multi-project and SBOM surfaces. OIDC sessions retain their independent expiry/revocation checks. |
| Depsilo → configured identity provider | Authorization Code + PKCE, state/nonce, signed ID-token verification and matching UserInfo subject; HTTPS and same-issuer-origin endpoints. Explicit isolated loopback HTTP is development-only. |
| MCP client → `/mcp` | JWT + read capability, read-only tools, and resource reads never fetch caller-supplied hosts (SSRF contract test). |
| Portal → anonymous status | Public by design; exposes service health and guidance only. |
| Depsilo → upstream | HTTPS; per-ecosystem provenance binding to the serving upstream; HTTPS→HTTP downgrade rejected; guarded dialer re-checks the resolved IP at connect time (loopback, link-local, multicast, unspecified rejected). |
| Depsilo → collector (webhook / SIEM) | Operator-configured destinations; audit forwarding is at-least-once with a per-exporter cursor. |
| Process ↔ local disk/DB | The state directory is the trust root; file-level access equals full compromise. |

## 5. Threats and controls

### 5.1 Installing known malware or typosquats

The known-malicious dataset (OSV `MAL-*`, synced every 6 h) is consulted
before everything else in the gate, for npm, PyPI, Cargo, RubyGems, Composer,
NuGet, Go, and Maven. A match returns `451 MALICIOUS_BLOCKED`, fires the
webhook and writes an audited event; the only exemption is a time-boxed,
reasoned admin override. PyPI legacy archive formats and RubyGems platform
artifacts resolve identity from registry metadata and are refused when the
identity cannot be proven.

**Residual:** the dataset is third-party and lagging by up to one sync
interval; ecosystems outside the eight are not covered.

### 5.2 Fresh-release attack (compromised release, then removal)

Minimum release age quarantines versions younger than the configured
threshold. Each decision is bound to the same upstream that will serve the
artifact: npm/PyPI/Composer/NuGet/Cargo/RubyGems carry a signed,
source-bound timestamp; Conda/CRAN/Maven/Alpine/Helm use an operator-
acknowledged approximate `Last-Modified`. Docker registries expose no usable
publish time, so an armed Docker threshold measures first-observation age: the
tag is resolved to a digest with a registry-token-aware HEAD, the digest's
first sighting by this instance is recorded once and never updated, and the
manifest is then fetched pinned to that digest, so a tag moved between the
decision and the fetch cannot serve unchecked bytes. The operator
acknowledges the semantics through `supply_chain.observation_sources`. Missing
provenance fails closed while a threshold is active; a registry that omits
`Docker-Content-Digest` is refused rather than guessed.

**Residual:** approximate ecosystems trust the serving upstream's headers;
Docker's observation age starts when this instance first pulls the digest, not
when the publisher released it, so a digest is quarantined only on its first
sighted pull and a pre-existing installation clock starts late. Tags without
the gate remain mutable.

### 5.3 Republished artifact (same version, different bytes)

Tamper detection records the first-seen SHA-256 per immutable artifact and
alerts when a refresh returns different bytes, keeping the trusted copy.

**Residual:** alert-only, and the first observation is not independent
authenticity proof (`SECURITY.md`).

### 5.4 Enforcement bypass through alternate request surfaces

Gates run before the cache lookup, so previously cached bytes cannot dodge a
new decision. Identity is path-derived where the layout is authoritative and
metadata-derived otherwise; every refusal is audited with its identity.

**Residual, documented:** metadata/index requests are not gated (clients see
which versions exist); artifacts whose identity cannot be established skip
identity-based gates; Docker tags are mutable, which is why the observation
gate resolves and pins the digest before deciding.

### 5.5 Cache poisoning via a compromised mirror

Artifact provenance is bound to the serving upstream (signed references for
npm/PyPI), `extra:` PyPI indexes canonicalize into the PyPI blocklist
namespace, and PyPI serves the legacy HTML shape for clients that reject JSON
so the signed path is never silently bypassed.

**Residual:** ecosystems without a metadata checksum trust upstream bytes at
first fetch; tamper detection only notices later changes.

### 5.6 Credential leakage through the API/UI

Passwords are bcrypt hashes; API tokens are stored hashed. Saved SIEM collector
tokens, Cascade shared/peer tokens, Docker/extra-index credentials, and the OIDC
client secret are not returned through Admin read responses. Compiler-cache
tokens are returned once when created and are not available from later lists.

Ordinary Upstream/proxy, Webhook, and SIEM collector **URLs** follow the existing
`CanWrite` visibility rule: write-capable administrators, including OIDC
administrators, can read their complete URLs and any embedded credentials.
Other Principals receive masked URLs. `can_manage_credentials` restricts
credential mutations; it does not replace this existing URL visibility rule.
The license key is masked in the UI. The JWT secret has a startup guard.
New instances without an explicit secret generate and persist a random
256-bit signing key; an existing database with no key or a malformed key file
fails startup instead of silently rotating sessions and signed artifact URLs.
Explicit environment/config secrets remain supported. The legacy development
placeholder forces a loopback-only server with process-local artifact keys.

CLI backups include the effective signing key in their archived configuration,
including managed-file and environment-provided keys, so backup access also
grants signing authority. Restore retains that key rather than generating a
replacement.

**Residual:** Docker registry credentials live in the config file, and
`SECURITY.md` already calls out the bootstrap token appearing in logs during
first-run setup.

### 5.7 Privilege escalation

Every admin route is registered in an explicit read or write capability
group; Pro surfaces sit behind the entitlement checker; setup requires the
bootstrap token; a router AST contract test fails if a route is registered
outside those groups.

Optional OIDC and LDAP integrations separate account permissions from login
identities ([ADR-0007](adr/0007-operator-oidc-identities.md)). Each binding belongs
to a stable authentication source and exact issuer/subject identity. Source
display names and authentication protocol names are not identity keys;
email/name matching cannot bind or merge accounts. Nonempty issuer values must
be distinct across all configured sources, including disabled sources. Global
issuer/subject uniqueness is retained, so deleting a source and adding another
ID cannot adopt its old identities. Linking requires a local password and a
fresh provider authentication, and local enabled state/credential version are
checked again at callback.
Unknown admitted identities first receive a short-lived browser-bound account
choice; no account, binding or session exists before explicit creation or
local-password-proven linking. Decisions are consumed once, with cancellation
and expiry respected before durable writes. The linked result remains an OIDC
session with capped permissions. Existing managed identities cannot be moved by
this mechanism. Isolated administrator test logins use the same protocol checks
but never open the durable user/binding/session write path or replace an existing
JWT. Configuration diagnostics require local credential authority and retain
issuer-origin and bounded-response protections.
Local accounts may bind one identity per source. Unlinking preserves the local
password and revokes only that identity's sessions, including when its source
is unavailable or removed. Provider-managed accounts cannot add another source,
remove their required identity, gain a local password, or be deleted and
recreated through Admin to escape local disable. Deleting source configuration
preserves those identity and account records.

Provider-group admission maps only to `admin` or `readonly`. A local account's
external session receives the lower of the local and provider roles. Each JWT
is checked against the current account and its nonrevoked, unexpired external
session; refresh keeps the same absolute deadline (30 minutes by default,
configurable from 5 minutes to 1 hour). External sessions cannot mutate users,
API tokens, compiler-cache credentials, Cascade credentials/configuration, or
authentication configuration, even when their operational role is admin. Local password administrators and
their existing readwrite API tokens retain credential management.

The authentication source collection and compatibility configuration endpoint
require an effective administrator role for reads and local
credential-management authority for writes. Stable IDs and protocols cannot be
renamed through the editor. New sources select OIDC or LDAP; no default provider
product is trusted. File updates preserve unrelated settings and environment-owned secrets; replaced
client secrets are saved with owner-only file permissions. Responses expose
only whether a secret is set. The browser editor keeps an entered replacement
only in component/request memory, clears it after submission, and never echoes
server error text that could contain that value. Updates do not alter the live
sources; activation remains a deployment restart with local recovery available.
Startup revocation is scoped to changed, disabled, or removed sources. Unchanged
sources keep their valid sessions; source deletion never purges stored bindings.
Environment-only secrets are not persisted by configuration writes or backups.

Login transactions use short-lived, browser-bound HttpOnly cookies, single-use
state/completion, and JSON/same-origin guards. Transactions and callbacks bind
an explicit source ID; a callback cannot complete another source's flow. Enabled
sources must share a callback origin. HTTPS cookies use the `__Host-` prefix.
JWTs and provider tokens are not placed in redirect URLs; provider
payloads and credentials are not included in public errors or identity audit
details. Server request logs omit query strings. Discovery and token/UserInfo
requests are bounded and cannot follow redirects or use another issuer origin.

LDAP directory authentication uses verified LDAPS or mandatory StartTLS before
sending bind credentials. Service-account search escapes user input, rejects
ambiguous matches and referrals, and requires an immutable UUID/GUID identity.
User binding is followed by a current group read; no directory password is
stored in accounts, sessions, cookies or browser storage. Private directory
networks are explicitly supported; only local credential-management authority
may configure them or submit connection diagnostics. Same-origin HTTPS (or
loopback development HTTP), bounded request size/time and login rate limits
protect directory credential endpoints. The browser checks before sending
passwords, including replacement service credentials; the API checks the same
transport boundary. HTTPS Origin handling assumes a trusted TLS reverse proxy
preserving Host and a private backend. It cannot prove TLS for arbitrary clients
that forge headers, or undo a password already sent over plaintext HTTP.
No recursive group expansion or directory synchronization is performed.

**Residual:** directory changes are not continuously polled. Remote disable or
group removal can leave existing external sessions usable until their absolute
deadline; a new login that proves loss of admission revokes affected sessions,
and local disable rejects the next request. SCIM, back-channel logout,
provider-wide browser logout, custom roles, and project authorization are not
implemented. The provider and its group issuance remain trusted authorities.
The last enabled local password administrator remains necessary for recovery.

### 5.8 Audit tampering or loss

Audit rows are written by the request path and by governance handlers; SIEM
forwarding is at-least-once with a visible cursor, lag, and last error, so a
stalled or silently broken feed is detectable from Admin. Rows are
hash-chained (schema v8): the chain is recomputed by the audit page, the
`GET /api/v1/admin/audit/integrity` endpoint, and the offline
`depsilo audit verify` CLI, and every edited, deleted, reordered, or unchained
row is reported with its id. Rows written before the chain keep a NULL
`prev_hash` and are reported as a pre-chain prefix.

**Residual:** the chain has no external anchor — an attacker with database
access can recompute the entire chain from the first row. The audit chain
anchor (`[audit] checkpoint_file`) closes most of that: periodic chain-head
checkpoints are written outside the database and verified by the audit page,
the integrity API, and `depsilo audit verify`, so a rewritten or truncated
chain contradicts a copy the operator already holds. What remains is
operational: the anchor copy must live where a database attacker cannot
rewrite it. `checkpoint_url` pushes each checkpoint directly to a WORM
gateway or log platform, so this no longer depends on a person shipping the
local file.

### 5.9 SSRF through redirects, upstreams, or MCP

The guarded dialer rejects loopback/link-local/multicast/unspecified targets,
re-checks the resolved address at connect time (closing the DNS
check/use race), and rejects HTTPS→HTTP downgrades. Docker registry
resolution checks registries, Bearer realms, and redirect targets before
handing the request to a configured forward proxy. MCP resource reads never
fetch caller-supplied hosts.

**Residual:** an operator-configured HTTP proxy owns DNS for proxied
upstreams, and registry Bearer realms may be cross-origin by protocol
(both documented in `SECURITY.md`).

### 5.10 Denial of service and resource exhaustion

Fetch and idle timeouts bound upstream reads, artifacts stream instead of
buffering, imports are size-capped (snapshot manifest 256 MiB, simple index
16 MiB, index scan limits), byte ranges are validated, the cache enforces a
quota with LRU cleanup, and background loops are bounded (SIEM batches,
6-hour blocklist sync, 1 req/s OSV).

**Residual:** there is no in-product HTTP rate limiting. Exposed
deployments must rate-limit at the reverse proxy; the deployment guide is
the place to make that concrete.

### 5.11 Multi-project data isolation

**Not provided.** Projects attribute usage and scope the Pro SBOM export;
they do not partition the cache. Any client that can reach the proxy can
fetch any cached artifact through the normal package routes. Treat a Depsilo
instance as one trust/tenant domain.

### 5.12 Supply chain of Depsilo itself

Tagged releases are built in CI with cosign keyless signing (checksums,
archives, container images, SBOM attestations) and verified through
`docs/release-verification.md`. `make security` runs govulncheck and npm
audit; `make verify` is the offline gate.

**Residual:** build-platform trust and no bit-for-bit reproducibility claim
for the `v0.x` line.

## 6. Accepted risks and non-goals

These are deliberate, documented, and not vulnerabilities:

- Multiple optional OIDC sources with distinct issuers and LDAP sources with
  the existing two-role mapping are supported under ADR-0007. Multiple OIDC
  clients sharing one issuer, SAML, SMTP authentication, SCIM, custom/project roles, account merging, and
  immediate remote revocation remain outside the integration.
- SQLite and cached bytes are not encrypted at rest; protect the directory
  and disks (`SECURITY.md`).
- The bootstrap token is printed to the log during first-run setup.
- The proxy surface is unauthenticated; restrict network reachability.
- Approximate provenance ecosystems trust upstream headers.
- Docker's age gate is opt-in and measures first-observation age, not publish
  time; without `supply_chain.observation_sources` a positive Docker
  threshold is rejected at startup. Go and APT have no publish-time authority
  and no gate.
- Tamper detection is alert-only and does not prove upstream authenticity.
- The audit chain has no external anchor; a full-database rewrite is
  detectable with the checkpoint file or a copy that left the box (SIEM,
  backup). Anchoring without shipping the file off-box adds little.
  (`checkpoint_url` covers the push case; RFC 3161 timestamping is out of
  scope.)
- Login starts and local password proofs have bounded in-process attempt
  limits. General traffic/abuse rate limiting remains a reverse-proxy concern.
- A configured HTTP forward proxy and cross-origin registry Bearer realms
  are trusted egress components.

## 7. Verification map

| Control | Evidence |
| --- | --- |
| Malware gate, overrides, dataset coverage | `internal/blocklist`, `internal/quarantine` tests; `docs/siem-audit-routing.md` |
| Minimum release age provenance | `internal/adapter/*/provenance_test.go` (including Docker's tag-resolve/observe/pin flow), `internal/quarantine/policy_capability_test.go`, `docs/specs/2026-10-06-min-release-age-provenance.md` |
| Tamper detection | `internal/tamper/recorder_test.go`, `internal/cache` tamper paths |
| Snapshot-only mode | `internal/snapshot`, `internal/quarantine/snapshot_gate_test.go`, `docs/specs/2026-10-06-freeze-snapshot.md` |
| SSRF and redirect guards | `internal/upstream/request` tests, `internal/api/mcp_ssrf_test.go`, Docker resolver tests |
| Capability groups and Pro gates | `internal/api/router_permissions_test.go`, `web/e2e` permission cases |
| External login, explicit binding, and bounded sessions | `internal/identity`, `internal/api/oidc.go`, `internal/api/ldap.go`, `internal/middleware/identity_auth_test.go`; isolated fixtures under `testground/oidc-casdoor` and `testground/ldap` |
| Audit forwarding integrity of delivery | `internal/audit/forwarder_test.go`, `internal/api/admin/audit_exporters_test.go` |
| Dependencies and known CVEs | `make security` (govulncheck + npm audit) |
| Release artifacts | `docs/release-verification.md`, release workflow tests in `make verify` |

## 8. Open items

1. An RFC 3161 timestamp-authority client, if operators need a third-party
   cryptographic timestamp rather than an arrival time at their own endpoint.
   For Docker that would upgrade first-observation age into a
   third-party-attested one.
2. A published reverse-proxy recipe covering rate limiting, admin-API
   network restriction, and header hygiene.
3. Optional mTLS to SIEM collectors (currently sidecar/reverse-proxy
   territory).
