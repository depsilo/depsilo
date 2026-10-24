# ADR-0007: Separate Operator Accounts, Login Identities, and Permission Sources

**Status:** accepted
**Date:** 2026-10-22

Operators asked to use an existing identity system such as Casdoor and to bind
that login to an existing local account. Depsilo will support a narrow OIDC
Admin integration with explicit account linking and the existing two roles,
while keeping a local password administrator as the recovery authority. This
partially supersedes the blanket in-product SSO non-goal in
[ADR-0004](0004-supply-chain-enforcement-layer.md) and
[ADR-0005](0005-enforcement-first-until-1-0.md); their repository, HA, product
positioning, and broader identity-management non-goals remain unchanged.

## Decision

- An Operator Account has a stable local identity, a Permission Source, and
  separate Login Identities. Each external identity belongs to a stable
  Authentication Source and is identified by exact issuer and subject, never
  by source display name or email. Binding requires proof of the local password
  and a fresh provider authentication; it never merges
  accounts or transfers permissions.
- Multiple named Authentication Sources are configured by protocol; OIDC and LDAP are
  supported protocols; no provider product is assumed.
  OIDC uses Authorization Code with PKCE, state, nonce, ID-token verification,
  and matching UserInfo subject. Source configuration is file/environment-owned
  and applies on restart. The System management editor can patch the file;
  environment-owned fields cannot be overridden from the browser. Client
  secrets are write-only, never returned in configuration responses.
- Explicit provider groups admit a new provider-managed account as `admin`
  or `readonly`; unmapped identities are denied. A local account's OIDC
  session receives the lower of its local role and mapped provider role.
  A local disable always takes precedence.
- Provider-managed accounts cannot add a local password, change their own
  permission authority, unlink their required provider identity, or mint
  durable Admin/API or compiler-cache credentials. OIDC sessions on locally
  managed accounts are also excluded from those credential-management routes
  and from editing the system's authentication trust configuration.
  Proof of the local password still permits managing that account's own
  provider binding.
- Every provider session has a durable revocation record and a default
  30-minute absolute lifetime, configurable from 5 minutes to 1 hour. JWT
  refresh cannot extend that lifetime. Depsilo rechecks account state on each
  authenticated request; external group decisions are refreshed on login.
  Changed authentication settings, disabled, or removed source configuration invalidates that source's
  sessions at startup without revoking unchanged sources or contacting providers.
- A local account retains its password when unlinking. The last enabled local
  password administrator cannot be removed or demoted through Admin. A
  provider outage must leave local login and recovery available.

## Consequences

This avoids delegating account authority implicitly through email matching or
through a new login method. It also prevents a short external authorization
window from producing an unrestricted local credential, at the cost of asking
local administrators to perform credential operations through local login.

This version does not implement multiple clients sharing one issuer, account
merging, local-password removal, SCIM, directory synchronization, SAML,
custom roles, or project-level authorization. Remote disable/group changes
are bounded by the external session lifetime rather than promised to revoke
instantly; local disable provides immediate instance-level rejection. Logging
out of Depsilo does not promise to end the provider's own browser session.

The provider identity/session tables and account authority column require
schema version 12. Existing local account IDs and credentials remain intact.
Downgrades require the pre-upgrade database backup as well as the older
binary/image. See the [operator guide](../oidc.md) for configuration and
binding behavior.

## 2026-10-23 refinement: system and personal management

The initial file-only management choice made personal Account security carry
deployment details without an actionable administrator workflow. At the user's
request, System management now owns an administrator-only OIDC configuration
editor while Account security owns personal binding and unlinking. This revises
the initial prohibition on a browser editor; it does not add a second durable
configuration authority or hot reload. Only local credential-management
authority can save authentication configuration, preserving the separation
between an externally granted session and the authority that issues sessions.

## 2026-10-23 refinement: multiple protocol-based sources

A stable source ID names the trust connection; `protocol = "oidc"` identifies
its protocol. Display names may change without moving identity ownership.
Locally managed accounts may bind one external identity per source while
retaining their password and local Permission Source. Provider-managed
accounts remain tied to their original identity: adding another source must
not become an implicit permission-authority transfer.

New sources live in `[auth.sources.<id>]`. Existing `[auth.oidc]`, its source ID
`oidc`, callback URL, account bindings, and legacy configuration API remain
supported. Startup defaults do not fabricate a configured legacy source.
Source IDs and protocols cannot be renamed through the editor. Authentication
transactions, callbacks, and configuration fingerprints bind the chosen source;
unchanged legacy configuration retains compatible sessions through a verified fingerprint upgrade.

The existing identity tables already record a source ID. Reusing them avoids a
schema migration and preserves the global exact `(issuer, subject)` uniqueness
constraint. Consequently nonempty issuer values must be distinct across all
configured sources, including disabled sources; disabled drafts may leave the
issuer unset. Supporting several clients for the same
issuer would require a separate identity-ownership decision and migration;
removing that uniqueness constraint now could silently associate one identity
with multiple accounts.

Deleting a source removes configuration, not accounts or bindings. Its running
configuration remains visible as pending removal until restart, when its
sessions are revoked. A local account can explicitly unlink an unavailable
source with password proof. Recreating a deleted connection under a different
ID never adopts its old identities. Source removal does not bypass the existing
recovery restrictions on provider-managed accounts.


## 2026-10-23 refinement: explicit first login and isolated diagnostics

Authenticating an unknown provider identity no longer immediately creates a
provider-managed account. After verified group admission, Depsilo keeps a
short-lived browser-bound decision in memory. The Operator explicitly creates
a new provider-managed account or proves an existing local password to link it.
Successful decisions are consumed once; cancellation and expiry write nothing.
A linked login remains an external session with the lower local/provider role,
not an unrestricted local password session. This prevents an exploratory login
from unnecessarily occupying an identity while preserving the prohibition on
account merging and Permission Source transfer. Existing managed identities
are not migrated by this flow.

System management can inspect configuration/discovery/public-key readability
without persisting a draft and can test actual authentication and role mapping
through an isolated manager. A test never opens the durable account/session
write path or replaces the administrator's session. Saved settings, effective
settings, and a particular test result are separate observations; an enabled
flag is not a health check. Removal previews report linked accounts and valid
session records. Undo before restart restores only a reliable original file
snapshot, never effective environment-only secrets. Activation still requires
an explicit service restart.


## 2026-10-23 refinement: directory sources and presentation changes

LDAP sources use service-account search followed by a bind as the uniquely
matched user. LDAPS and mandatory StartTLS authenticate the server certificate;
there is no plaintext password bind or skip-verification option. Private CAs
can be supplied as PEM. The directory connection is an explicit, locally
administered trust configuration, so private-network directory addresses are
supported. It does not use package-Upstream selection or health authority.

A directory identity uses `entryUUID` or binary `objectGUID`, never a mutable
username, email, or DN. Its authority binds the source ID, directory endpoint,
search base and stable-ID attribute. Changing that authority cannot adopt old
bindings. Direct group attribute values determine the existing two roles; this
version does not recursively resolve groups, synchronize users, or implement
SASL/Kerberos. `ldap` is a distinct Permission Source and Login Method. The same
explicit account-choice, local-password binding, revocation and credential
restrictions apply to both external protocols. Directory passwords are used
only for the current request; only the service bind secret is persisted in
write-only configuration.

Display names are presentation, not authentication authority. New session
fingerprints exclude them; credential, endpoint, mapping and session-policy
changes still revoke affected sessions at restart. Startup upgrades a live
legacy OIDC fingerprint only when it exactly matches the full current old
configuration, source and issuer, in the same transaction as revocation. An
upgrade combined with a rename cannot prove that continuity from the old opaque
hash and conservatively revokes that session. No schema migration is required
for LDAP or this fingerprint refinement.
