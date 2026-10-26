# OIDC login and account binding

Depsilo can use multiple named OIDC authentication sources for Admin login.
Sources are added by protocol. This guide covers OIDC; directory authentication
is described in the [LDAP / LDAPS guide](ldap.md). Casdoor is one tested OIDC
provider, not a default source or a required identity system.
The integration keeps the existing `admin` and `readonly` roles. It is optional
and disabled by default; package clients and the anonymous Portal do not need
to authenticate through it.

## Choose who owns account permissions

| Account | Login methods | Permission authority |
| --- | --- | --- |
| Existing local account | Local password, optionally one bound identity per OIDC source | Local role; an OIDC session is also capped by its provider group mapping |
| Account first created through OIDC login | Original OIDC source only; cannot add another source or a local password | Explicit original-source group mapping, with local disable taking precedence |

Binding is a new sign-in method for the same account. It does not transfer
the account to company-directory management. Removing a local account's
provider group does not remove its local password or local permissions.
Use provider-managed accounts when the directory must own admission and roles.

Keep an enabled local password administrator for recovery and credential
operations. Depsilo prevents Admin from removing or demoting the last such
account. An OIDC administrator does not replace this recovery account.

## Configure authentication sources

Open **System management → Identity authentication → Add authentication
source**, fill in the **OpenID Connect (OIDC)** connection settings, and provide
a display name. The suggested stable source ID is editable under **Advanced
options** before the first save. The form creates no source until you save it. IDs contain 1–32 lowercase letters, digits, or hyphens and start with
a letter. The ID is used by callbacks and stored account bindings; it and the
protocol are immutable in the editor. Change the display name to rename a
source in the UI. The ID `oidc` is reserved for legacy configuration.

Create a confidential application at each identity provider with Authorization
Code flow, PKCE S256, and its exact source callback. For source `company`:

```text
https://depsilo.example/api/v1/auth/oidc/company/callback
```

Use the instance's browser-visible HTTPS origin. All enabled sources' callbacks
must share that origin. Both the browser and Depsilo server must be able to
reach each provider. A reverse proxy must preserve the browser's original
`Host` header for account proof requests. Discovery must identify the exact
configured issuer; authorization, token, UserInfo, and JWKS endpoints must share
that issuer's origin. Cross-origin discovery endpoints and HTTP redirects
during server-side provider requests are rejected.

Nonempty issuer values must be distinct across all configured sources,
including disabled ones. Disabled drafts may leave the issuer unset; enabling
a source requires it. Multiple clients for the same issuer are not supported. The identity
store preserves global exact `(issuer, subject)` ownership, so adding a source
cannot adopt an identity already bound through another source.

Local administrator password sessions or writable local administrator API
tokens may save changes; OIDC administrators may inspect them. The list and
editor distinguish saved configuration from effective configuration and show
pending restart and environment ownership. A newly added source has no running
configuration until restart. Saving changes neither restarts Depsilo nor runs
a connectivity test. Unsaved edits and secrets stay in component memory; navigation
prompts before discarding them, and browser reload/close uses the native warning.

Use **Check configuration** to validate the current draft, discovery metadata and
readability of the public-key document. This does not prove the client secret,
actual authentication or role mapping. Editing the form clears that observation.
Save a complete source, still disabled if desired, then use **Test sign-in** to
verify actual authentication and mapped role with the saved configuration. This
isolated flow does not create or link accounts, issue sessions, or replace the
administrator's current login. It reports only that test result, not permanent
provider health. Register the displayed callback with the provider before testing.
Enable and restart once the result is satisfactory.

Changes to a running source and source removal display current affected-account
and valid-session-record counts before confirmation. They are observations, not
online-user counts or a promise about the later restart. Source removal retains
accounts and bindings. **Undo pending removal** can restore the original saved
file configuration before restart only when that baseline was reliably captured;
it never materializes environment-only secrets.

Alternatively, add explicit source tables to the instance configuration:

```toml
[auth.sources.company]
protocol = "oidc"
enabled = true
display_name = "Company login"
issuer = "https://identity.example"
client_id = "depsilo"
redirect_url = "https://depsilo.example/api/v1/auth/oidc/company/callback"
groups_claim = "groups"
admin_groups = ["company/depsilo-admins"]
readonly_groups = ["company/depsilo-readers"]
session_ttl = "30m"
allow_insecure_loopback = false

[auth.sources.partner]
protocol = "oidc"
enabled = true
display_name = "Partner login"
issuer = "https://partner-identity.example"
client_id = "depsilo-partner"
redirect_url = "https://depsilo.example/api/v1/auth/oidc/partner/callback"
groups_claim = "groups"
admin_groups = []
readonly_groups = ["depsilo-readers"]
session_ttl = "30m"
allow_insecure_loopback = false
```

Supply the corresponding confidential application secrets through
`DEPSILO_AUTH_SOURCES_COMPANY_CLIENT_SECRET` and
`DEPSILO_AUTH_SOURCES_PARTNER_CLIENT_SECRET` before enabling these examples.
New sources default to disabled, `groups_claim = "groups"`, and a 30-minute
session lifetime; a complete effective configuration is required on enable.
No default source is inferred from those defaults.

Source fields have environment overrides named
`DEPSILO_AUTH_SOURCES_<UPPERCASE_ID_WITH_HYPHENS_AS_UNDERSCORES>_<FIELD>`.
For `company-sso`, the client-secret variable is
`DEPSILO_AUTH_SOURCES_COMPANY_SSO_CLIENT_SECRET`. Only IDs declared in the file
are enumerated; environment variables alone cannot introduce a new named source.
Group lists in the environment use comma-separated values. Environment-owned
fields must be changed through deployment configuration and cannot be edited
in the browser. Remove environment ownership and restart before deleting such
a source through Admin.

The `client_secret` TOML field is also supported; keep secret-bearing files
private. Responses expose only whether a secret is set. A blank secret input in
the editor preserves the saved value. Entered replacement secrets are cleared
after submission and not stored in browser storage or the request cache. Saving
a replacement secret restricts configuration-file permissions to its owner.
Never put secrets in compiled frontend configuration, screenshots, or shared
logs. Environment-only secrets remain in the environment, including when other
source fields are saved or the instance is backed up.

The file editor preserves unrelated TOML text and comments. Browser editing
requires explicit `[auth.sources.<id>]` tables. Inline tables or dotted-key
source declarations remain readable at runtime but make the source editor
read-only; normalize those declarations in deployment configuration first.

The group claim must be a string array. Depsilo reads UserInfo first and falls
back to the verified ID token if the configured claim is absent there. At least
one admission mapping is required, admin and readonly lists must be disjoint,
and unmapped identities are denied. Admin takes precedence if a person belongs
to groups in both lists. These mappings select Depsilo's two existing roles;
they do not import a provider's arbitrary policy or directory permissions.

Restart Depsilo to apply changes. Startup validates configuration without
contacting providers. Changing, disabling, or removing one source revokes its
incompatible sessions at startup while unchanged sources retain their valid
sessions. The list keeps removed but still-running sources visible as pending
removal until restart. Deleting configuration does not delete its accounts or
bindings; local password login and recovery remain available.

HTTP is permitted only for isolated loopback development with
`allow_insecure_loopback = true`. This does not permit insecure remote LAN
issuers or callbacks. Issuer URLs are exact: do not substitute `localhost` for
`127.0.0.1` or add/remove an issuer path when copying provider metadata.

### Existing single-source configurations

`[auth.oidc]` remains the legacy source with stable ID `oidc`, its existing
`DEPSILO_AUTH_OIDC_*` overrides, and callback
`/api/v1/auth/oidc/callback`. Existing explicit display names, including Casdoor,
are preserved; the fallback name is OIDC. This legacy source can coexist with
new sources using different issuers. Existing account bindings need no rewrite,
and unchanged configuration preserves existing valid session fingerprints.

Do not move a legacy configuration into `[auth.sources.<another-id>]` as a
renaming operation: a different source ID does not inherit its bindings.
Use the display name to relabel it. The old Admin authentication GET/PUT
endpoint remains limited to the legacy source; new management uses the source
collection described in the [control-plane guide](admin-control-plane.md#external-operator-identities).

## Casdoor setup and isolated test instance

The repository's [Casdoor fixture](../testground/oidc-casdoor/README.md) provides
an independent local provider, example admin/readonly/unmapped/disabled
identities, and private generated credentials. Its configured group values are
`depsilo-test/depsilo-admins` and `depsilo-test/depsilo-readers`. The fixture uses
`JWT-Standard` tokens with group IDs; use the actual group values returned by
your deployment rather than treating display names as identifiers.

The Depsilo client requests `openid profile email`, verifies the ID token, and
calls UserInfo. Email is not an account-linking key. The fixture's independent
browser probe validates the provider itself; it is not evidence that Depsilo
login, authorization, binding, or deployment verification has passed. Consult
the recorded test output for those separate checks.

## Bind an existing local account

1. Sign in to the intended Depsilo account with its local password and open
   **Account security → Sign-in methods**.
2. Choose the intended authentication source's **Link** action and prove the
   account's current password. Complete that source's provider login.
3. Return to the account page and confirm the new login method is listed.
   The existing account, local role, and password remain unchanged.
4. Repeat for another source if needed. Each local account can bind one identity
   per source; changing the identity within a source requires unlinking first.

When a standalone provider login authenticates a previously unknown identity,
Depsilo now asks you to choose explicitly: prove an existing local username and
password to link that account, or create a new provider-managed account. No
account, identity or session is written before that choice. Cancelling discards
the short-lived proof. Linking from this flow creates an OIDC session capped by
both the local role and provider mapping; it is not a local password session
and does not grant credential-management authority.

An identity already
bound to any account returns `IDENTITY_ALREADY_BOUND`; this version does not
merge accounts or move a provider-managed identity between accounts. Matching
usernames, display names, and email addresses never establish a binding.

Binding requires both local-password proof and provider admission. A disabled
local account or a changed local credential version during the redirect round
trip prevents completion. API-token sessions cannot bind or unbind identities.

## Unbind a login method

On a locally managed account, choose **Unlink** and confirm its local password.
Depsilo removes that provider association and revokes sessions established
through it. The local password remains available, including when the provider
is unavailable or its source configuration has been removed. Other bound
sources remain available and their sessions are unchanged. Unbinding affects
Depsilo only; it does not delete the provider account or end its browser session.

A provider-managed account cannot bind another source, unbind its required
login identity, add a local password, or change its Permission Source. A local
administrator can disable such an account in **Users**. Local role/password edits and deletion are
rejected so a later provider login cannot silently recreate a disabled account.

## Sessions, permissions, and revocation

- An external session lasts 30 minutes by default, configurable from 5 minutes
  to 1 hour. JWT refresh cannot move that absolute deadline; sign in again
  when it expires. If `auth.token_ttl` is shorter, individual JWTs may expire
  sooner.
- Account enabled state, credential version, identity ownership, and external
  session revocation/expiry are checked on each authenticated request. Local
  disable invalidates access on the next request. Role changes or missing
  admission groups observed during a new provider login revoke the affected
  older external sessions.
- Depsilo does not continuously poll directory membership or implement SCIM,
  back-channel logout, or immediate remote revocation. A remote disable or
  group removal may leave an existing session usable until its deadline.
  Disable the Depsilo account locally when immediate instance-level rejection
  is needed.
- An OIDC session can perform ordinary operations allowed by its effective
  role. User mutations, API-token mutations, compiler-cache credential
  mutations, Cascade configuration writes, and authentication configuration
  writes require a local administrator password session or an existing local
  administrator readwrite API token.
  This also applies to an OIDC session on a locally managed account. Proof of
  that account's password separately permits managing its own binding.
- Signing out revokes the current external Depsilo session. It does not promise
  provider-wide sign-out; another browser session can remain signed in there.
  Existing local password JWTs retain their client-discard logout semantics.

## Recovery and upgrades

If the provider fails, use the local password login and inspect the provider's
configuration and availability. Depsilo does not make provider network calls
while starting. Disabling one source and restarting revokes that source's
sessions while leaving other sources, local accounts, and credentials intact.

Source ID, issuer, and subject identify an external binding. Changing the source
ID or configured issuer does not migrate old associations. Deleting a source
keeps those associations, so creating another ID for the same issuer does not
adopt them. A local account may explicitly unbind an unavailable identity and
then bind again using its password. Provider-managed account merging or
authority migration is outside this version's supported workflows.

The original OIDC integration introduced schema version 12 for login identities,
external sessions, and the Permission Source field. Multiple sources reuse
those tables and require no further schema migration. Existing source ID `oidc`,
local account IDs, password hashes, API tokens, and credential versions are
preserved. Downgrading across the original schema-12 migration requires the old
binary and compatible pre-migration configuration/database backups. Older
single-source binaries do not understand `auth.sources`; any rollback also
requires compatible configuration. Do not delete identity rows or reset
credentials as a substitute for the normal recovery flow.

Backups contain persisted identity/session state and the saved configuration.
An OIDC secret supplied only through the environment is not materialized into
the archive; restore that service environment separately. The JWT signing key
continues to follow the existing [backup contract](compatibility.md#database-and-backups).
Keep backups and local test credentials private.

The OIDC integration does not add multiple clients sharing one issuer, SAML,
SMTP authentication, directory synchronization, custom roles, or project-level
access control. LDAP is available as a separate protocol; see [LDAP / LDAPS](ldap.md). The integration does not turn the anonymous
package proxy into an authenticated artifact service; continue restricting proxy
network reachability as documented.


### Display-name changes and existing sessions

A display-name-only edit applies after restart and keeps current external
sessions. Changing authentication settings, disabling or removing a source
still invalidates its sessions at startup. The editor's `sessions_will_reset`
flag describes the saved effective change after environment precedence; a new
unsaved security edit is shown conservatively until saved.

The first upgrade from the original fingerprint algorithm preserves unrevoked,
unexpired sessions only when the old fingerprint matches the complete current
source configuration and issuer. Combining that upgrade with a rename cannot
prove continuity from the old opaque fingerprint, so those sessions expire.
Subsequent name-only edits preserve sessions. No extra schema migration is used.

For directory username/password authentication, add an independent
[LDAP / LDAPS source](ldap.md) rather than configuring it as OIDC.
