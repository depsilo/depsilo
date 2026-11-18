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
| Admin credentials and API tokens | SQLite (bcrypt password hashes, hashed tokens) | Full control plane | Read/write capability groups, Pro entitlement gates, bootstrap-token flow; local `depsilo reset-password` recovery needs state-directory access and is audited (§5.7) |
| External Operator identities and sessions | SQLite | Account binding, admission, revocation | Stable source ownership plus exact issuer/subject identity; explicit password-proven binding; absolute session lifetime and local account checks |
| Per-source OIDC client secrets | Operator configuration/environment | Confidential provider client authentication | Write-only Admin input restricted to local credential authority; never returned; issuer-origin constrained requests; private configuration |
| Instance signing key | managed `data/secrets/jwt.key` relative to the config directory, or explicit environment/config secret | Mints admin sessions and signs npm / extra PyPI artifact URLs | Automatic 256-bit generation for new instances; mode-0700 directory and mode-0600 file; restart reuse; explicit-secret startup guard |
| Setup bootstrap token | current process and private `data/secrets/bootstrap.token` during initialization | Authorizes initial administrator creation | Mode-0600 file, refreshed before serving, removed after setup; local `depsilo setup token` requires the service to be awaiting setup; no public token-read endpoint; strength rule for generated and preset tokens (§5.7) |
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
| Any peer → addresses Depsilo writes | **Not authenticated.** The scheme and host of the addresses Depsilo writes into a response (rewritten npm, PyPI, Cargo, Composer, and NuGet metadata, a project's `proxy_url`, `/api/v1/discover`, the agent and integration prompts, and `reconnect_url` after setup) come from that request's `X-Forwarded-Proto` and `Host` headers. Depsilo accepts both from any peer by design; `[server] trusted_proxies` governs only the client address taken from `X-Forwarded-For` and `X-Real-IP`. The scheme is `https` on a direct TLS connection and otherwise only `http` or `https`, never another value. Cached metadata holds placeholders, so forged headers change only the response to the request that carried them. The operator keeps the listener reachable only through the reverse proxy and has the proxy set `Host` and `X-Forwarded-Proto` itself instead of passing a client's values on. |
| Admin browser/API → control plane | JWT bearer sessions; read/write and local credential-management capability groups; free-edition project and export allowances enforced in their handlers. OIDC sessions retain their independent expiry/revocation checks. |
| Depsilo → configured identity provider | Authorization Code + PKCE, state/nonce, signed ID-token verification and matching UserInfo subject; HTTPS and same-issuer-origin endpoints. Explicit isolated loopback HTTP is development-only. |
| MCP client → `/mcp` | Session JWT or API token with read capability, read-only tools, and resource reads never fetch caller-supplied hosts (SSRF contract test). |
| Portal → anonymous status | Public by design; exposes service health and guidance only. The integration prompt (`GET /api/v1/integration-prompt`) writes a service address into instructions an AI agent follows, so its `url` parameter accepts only a bare `http` or `https` origin; any other value is refused with `400 INVALID_URL` instead of reaching the prompt. |
| Monitoring and status clients → `/metrics`, `/api/v1/stats` | **Open by design.** Both answer without sign-in. `/metrics` exposes Prometheus series: requests per adapter, upstream requests per Upstream name, upstream exchanges per route (direct or peer), cache and compiler-cache size, policy-snapshot state, and process runtime. `/api/v1/stats` exposes the version, uptime, request and cache totals, a 5-minute request series, each Upstream's name, origin without credentials, and health, and the name, kind, and path of each configured extra index. Neither returns package names, client addresses, or credentials. The network restriction that `SECURITY.md` requires for the proxy covers both. |
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

Operators often run the offline commands `depsilo reset-password`,
`depsilo restore`, and `depsilo backup` as root, with `sudo` or as UID 0 in a
container, against a state directory that belongs to the service account.
On Linux and macOS, an entry that root creates in a directory owned by
another account gets that directory's owner and group; root in a root-owned
directory and every other user keep what they create, and Windows changes
no owner. The rule covers the lock directory `.depsilo-locks`, its lock
file, and a missing database directory, which `depsilo reset-password`,
`depsilo restore`, and a server start create; everything `depsilo restore` creates or replaces (the
restored `config.toml` and database, their `.pre-restore-<id>.bak` copies,
the restore journal, and missing directories); and an archive that
`depsilo backup --out` writes inside the state directory (the directory of
`config.toml` or of the database, or beneath one), with the directories
created for it. An archive written anywhere else stays owned by root, with
mode 0600. The owner always comes from the directory that receives the
entry, never from an archive. It is changed on the open entry before the
entry is renamed into place, so a link put at that name is not followed,
and a new directory is handed over only while it is still the empty
directory root created; otherwise the command refuses. When an owner cannot
be changed, the command removes the entry it created and fails, and a
restore stops before it replaces anything.

A Docker Registry `502`, which any client that reaches the proxy routes can
read, contains only the stage that failed, the origin (scheme, host and port)
of the registry, Bearer realm or redirect target that failed, a failure code
or the answer's status with at most three error codes from the OCI
distribution specification's closed list, a fixed reason for an egress proxy
failure, Depsilo's own refusal or remedy text, and the request ID. Depsilo
builds it from typed facts at each registry call and never from an error's
text, so no upstream free text reaches it: no answer body, status phrase,
registry error message or detail, Go transport error (which can quote a
server's malformed answer), certificate name, proxy address, or URL userinfo,
path or query, where signed CDN and object-storage URLs carry their
credentials. An unrecognized error reads `Docker registry request failed`.
The `500` for a client path that cannot form the registry URL carries a fixed
message too, because Go's parse error would quote that URL with any
userinfo. The server log keeps the full error under the same request ID; in
the URL that failed, and in every absolute `http` or `https` URL and every
double-quoted URL reference of the error text or the registry's answer,
userinfo is removed and each query value reads `REDACTED`.

**Residual:** Docker registry credentials live in the config file, and
`SECURITY.md` already calls out the bootstrap token appearing in logs during
first-run setup. It also covers the one case in which Depsilo generates the
initial administrator password, prints it once in the startup output and the
log, and keeps it valid until it is changed.

Root-run offline commands deliberately leave existing entries with their
owner. A `.depsilo-locks` directory or lock file that an earlier binary run
as root created, or that a root command left when it was killed before
changing the owner, stays owned by root, and the service then fails to start
until the operator runs `chown -R` on it for the service account. An
interrupted restore left by an earlier binary run as root is completed
without changing its owners, and a killed root backup inside the state
directory leaves a root-owned `.depsilo-backup-work-*` directory. Inside the
state directory, an archive goes to whoever owns the directory that
receives it, so an archive written into a subdirectory that belongs to a
third account gives that account the signing key it holds.

A Docker `502` still names the host of the hop that failed. That host can
reveal a bucket (`<bucket>.s3.<region>.amazonaws.com`) or an internal
storage host behind a private registry, and a registry chooses the host
names of its redirects, so it can place up to 253 characters of host-name
text in the message. The server log keeps up to 64 KiB of an error answer,
which can echo request details that the log redaction does not recognize:
text outside URL syntax, for example the signature an object store quotes in
a signature-mismatch error; a URL whose scheme is not `http` or `https`, such
as `s3://`; or a relative URL that is not the whole of a double-quoted
string, such as a link in an HTML or XML page or a path inside a quoted
sentence.

### 5.7 Privilege escalation

Every admin route is registered in an explicit read or write capability
group; Pro surfaces sit behind the entitlement checker; setup requires the
bootstrap token; a router AST contract test fails if a route is registered
outside those groups.

The bootstrap token authorizes creating the first administrator. A generated
token is 256 random bits and is replaced on every restart. A token preset
through `DEPSILO_BOOTSTRAP_TOKEN` is not rotated, so a start with setup
pending refuses one that is shorter than 24 characters, has surrounding
whitespace, or has fewer than 8 distinct characters. The variable is not read
once setup is complete. `POST /api/v1/setup/complete` has no attempt limit;
the token's entropy is the control.

An instance that has a `config.toml` and no administrator, and starts with
`DEPSILO_ADMIN_USERNAME` set and `DEPSILO_ADMIN_PASSWORD` unset, generates
the administrator password from 256 random bits. A drawn password that the
password policy rejects for that user name is replaced by a new draw, with
at most 16 draws in all, so the generated password passes the same policy as a chosen one; if every
draw is rejected, startup fails with the policy's error. A
password set through `DEPSILO_ADMIN_PASSWORD` that the policy rejects stops
startup.

`depsilo reset-password` sets a local account's password directly in the
SQLite database, for an administrator who can no longer sign in. It asks for
no Depsilo credential: whoever can run the binary with write access to the
state directory can use it, which is the access that already equals full
compromise (§4). The command runs only while the server is stopped, because
it holds the database lease a running server holds. It refuses disabled
accounts, accounts managed by an external identity provider, and a database
whose schema version differs from the binary's, and it never migrates. The
new password comes from a no-echo prompt or one line of standard input, never
from a flag, an argument, or an environment variable, and must pass the
server's password policy. One transaction stores the hash, increments the
account's credential version so that every signed-in session of the account
becomes invalid, and writes a `user.cli_reset` audit row that names the
operating-system user. The account's API tokens are not revoked.

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
A `depsilo reset-password` run leaves the account's API tokens valid; revoke
them in Admin when the account may be compromised. Its audit row records the
operating-system user the host reports, and the same state-directory access
allows editing the database directly.

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
check/use race), and rejects HTTPS→HTTP downgrades. A Docker registry without
its own proxy follows `[egress] upstream_proxy` and `no_proxy` as a package
upstream does, but routes each request by Depsilo's own answer for the
request's host: a private or loopback answer is dialed directly, by a dialer
that resolves the host again and dials only private or loopback addresses
that the registry's pinned scope allows, and only public and fake-IP answers
go through the proxy. Before a proxy carries a registry request, and before
the direct dialer takes it, Depsilo resolves the registry, Bearer realm, and
redirect host names locally and refuses a failed resolution, an answer with
an unsafe address or mixed network scopes, a literal special-purpose
address, an HTTPS downgrade, and a target outside the registry's pinned
network scope. A realm or redirect target of a registry configured by host
name that is written as a private or loopback IP address is left to that pin
instead of being refused by its form, because the checked address is the one
that is dialed or that a proxy connects to: a registry pinned private may
follow a private address, one pinned loopback also a loopback address, and
one pinned public neither. `localhost`, `.localhost` names and addresses
written with a trailing dot are still refused by their form, because a
resolver, a proxy's included, may answer them with any address.
One exception concerns fake-IP answers: a host name (not localhost, and not
an address in any notation) whose every DNS answer lies in the benchmarking
ranges `198.18.0.0/15` or `2001:2::/48`, from either family or both, counts
as public, because proxy software in TUN/fake-IP mode answers that way for
public hosts, and such an answer says nothing about the real address. When a
proxy carries the request, the proxy resolves the name itself. Where Depsilo
dials itself (package Upstreams, extra indexes and cascade parents without a
proxy, their checks, and Docker registries that no proxy carries), it dials
the placeholders, IPv4 ones first and each for at most 5 seconds, while
`[egress] allow_fake_ip` is on, which is the default: the proxy software's
TUN captures the connection, maps the placeholder back to the name, resolves
it and connects where its rules send it, as it does for every program on the
machine. Wherever Depsilo checks such an answer, it counts as public: any
origin may reach it, and an origin with such an answer reaches no target
whose local answer is private or loopback. The check covers only the local
answer; where the name itself connects is the proxy's or the TUN's decision
(see the residuals below). Where Depsilo dials itself, a fake-IP answer pins
the origin's network scope only once a connection to it succeeded.
An answer that mixes such addresses with any other address is still refused,
and so is a fake-IP answer that holds an address of this machine's network
interfaces, such as the TUN address of the proxy software, which would
otherwise reach a service on this machine; with `allow_fake_ip` off, a
fake-IP answer is refused wherever no proxy carries the request. Unique-local
fake-IP addresses get no exception, because they cannot be told from a
private network: the unique-local ranges that proxy software documents for
IPv6 fake-IP and for its own TUN, VIF and DNS addresses
(`fdfe:dcba:9876::/48`, `fd00:6152::/32`, `fc00::/18`) are unsafe for every
package Upstream, cascade parent and Docker registry, never private. A fake
answer from them used to pin a public Docker registry, with or
without a proxy, or a direct package Upstream as a private origin, after
which its Bearer realm or redirect could reach private addresses. A Docker
host name answered from these ranges, or with fake-IP addresses next to any
unique-local address, is refused before anything is dialed or sent to a
proxy; its `502` may name a documented range, but never a unique-local
address or any other unique-local prefix. MCP resource reads never fetch
caller-supplied hosts.

**Residual:** an operator-configured HTTP proxy owns DNS for proxied
upstreams, for every host of a Docker registry with its own proxy, and for
the hosts with public or fake-IP answers of a registry under
`[egress] upstream_proxy`; it is the only resolver for a registry host whose
local answer was fake-IP. Without a proxy, the proxy software whose TUN
carries a fake-IP connection is that resolver. Registry Bearer realms may be
cross-origin by protocol (both documented in `SECURITY.md`). The private and
loopback targets of a registry under `[egress] upstream_proxy` are dialed
directly, so the proxy's egress ACL does not apply to them: a registry whose
own answer is private may reach every private address directly, by name or
by address literal in a realm or redirect. Only the registry's own proxy
keeps those targets behind the proxy's ACL. The fake-IP exception has the
following limits:

- Without a proxy, `allow_fake_ip` delegates a fake-IP name to the proxy
  software whose TUN carries the connection, which resolves the name itself
  and applies its own rules, as `upstream_proxy` delegates it to a configured
  proxy, but now without configuration. The software's DNS policy and rules
  become part of the boundary for these names: a realm, redirect or Upstream
  host name with a fake-IP answer, a hostile one included, reaches whatever
  the software resolves it to under a rule that connects directly, loopback
  and private addresses of the TUN's host and network included, although
  its origin counts as public. Restricting loopback, link-local,
  metadata-service and unintended private destinations there is the
  operator's part, as for a configured proxy; `allow_fake_ip = false` keeps
  such names to a configured proxy.
- A network that really uses `198.18.0.0/15` or `2001:2::/48` is dialed for
  any host name whose answer lies there, including a hostile realm or
  redirect name that publishes such records, when no TUN captures the
  connection. `allow_fake_ip = false` (or `DEPSILO_EGRESS_ALLOW_FAKE_IP=false`)
  restores the refusal wherever no proxy carries the request.
- `2001:2::/48` is accepted exactly like `198.18.0.0/15`, so a hostile realm
  or redirect name can publish AAAA records there, and a proxy, or the TUN, is
  then asked to connect to that address.
- An address of Depsilo's own machine is refused in a fake-IP answer, read
  from its network interfaces at every check, and an answer that Depsilo
  cannot check because the interfaces cannot be listed is refused. Another
  host's own TUN address inside an accepted range, such as `198.18.0.1` or a
  user's `2001:2::1/126`, is not refused. A proxy on another machine can be
  asked to connect to it, and without a proxy Depsilo dials it itself when
  the TUN of another host carries its connections: inside a container, whose
  network interfaces do not hold the Docker host's TUN address, or behind a
  gateway that runs the proxy software. A name whose answer is that address,
  for example one that the proxy software's fake-ip-filter passes with its
  real record, then reaches that host's services that listen on all
  addresses or on the TUN address. Fake-IP pools do not hand out the TUN
  address, so only such a name answers with it; `allow_fake_ip = false`
  removes the direct case.
- A unique-local fake-IP range that an operator chose outside the documented
  list still counts as a private network, so a fake answer from it alone can
  still pin a public Docker registry or direct package Upstream as private.
- A kind network whose IPv6 /64 falls in `fc00::/18` is refused like proxy
  software's addresses; the default `kind` network, `fc00:f853:ccd:e793::/64`,
  lies outside it.

### 5.10 Denial of service and resource exhaustion

Fetch and idle timeouts bound upstream reads, artifacts stream instead of
buffering, imports are size-capped (snapshot manifest 256 MiB, simple index
16 MiB, index scan limits), byte ranges are validated, the cache enforces a
quota with LRU cleanup, and background loops are bounded (SIEM batches,
6-hour blocklist sync, 1 req/s OSV).

**Residual:** there is no in-product HTTP rate limiting. Exposed
deployments must rate-limit at the reverse proxy. The deployment guide's
reverse-proxy section covers forwarded headers, timeouts, and body limits; it
has no rate-limit recipe yet.

### 5.11 Multi-project data isolation

**Not provided.** Projects attribute usage and scope the SBOM export;
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
  are trusted egress components, and so, while `[egress] allow_fake_ip` is
  on, is the proxy software whose TUN carries Depsilo's connections to
  fake-IP answers.

## 7. Verification map

| Control | Evidence |
| --- | --- |
| Malware gate, overrides, dataset coverage | `internal/blocklist`, `internal/quarantine` tests; `docs/siem-audit-routing.md` |
| Minimum release age provenance | `internal/adapter/*/provenance_test.go` (including Docker's tag-resolve/observe/pin flow), `internal/quarantine/policy_capability_test.go`, `docs/specs/2026-10-06-min-release-age-provenance.md` |
| Tamper detection | `internal/tamper/recorder_test.go`, `internal/cache` tamper paths |
| Snapshot-only mode | `internal/snapshot`, `internal/quarantine/snapshot_gate_test.go`, `docs/specs/2026-10-06-freeze-snapshot.md` |
| SSRF and redirect guards | `internal/upstream/request` tests, `internal/upstream/egress_failure_test.go`, `internal/upstream/fake_ip_dial_test.go` (fake-IP answers dialed, not carried, or refused), `internal/egress/egress_test.go` (fake-IP and proxy-software ranges, local interface addresses), `internal/api/mcp_ssrf_test.go`, Docker resolver and egress tests (`internal/adapter/docker/egress_test.go`, `internal/adapter/docker/fake_ip_test.go`) |
| Docker `502` text and its log redaction | `internal/adapter/docker/bad_gateway_test.go`, `internal/adapter/docker/failure_test.go` |
| Capability groups and Pro gates | `internal/api/router_permissions_test.go`, `web/e2e` permission cases |
| Preset bootstrap token rule | `internal/config/loader_test.go` |
| Local password reset (`depsilo reset-password`) | `internal/cli/resetpassword_test.go` |
| Owner of entries root-run offline commands create | `internal/backup/lease_security_test.go`, `internal/backup/restore_ownership_test.go`, `internal/backup/backup_ownership_test.go` (the `*AsRoot` tests run only as root and are not part of an automated gate) |
| Scheme taken from `X-Forwarded-Proto` | `internal/requestorigin` tests |
| Integration prompt `url` validation | `internal/prompts/baseurl_test.go`, `internal/api/public/integration_prompt_test.go` |
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
