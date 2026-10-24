# LDAP / LDAPS authentication

System management → Identity authentication → Add source → LDAP creates an
independent directory connection. Multiple LDAP and OIDC sources may coexist.
Only a local administrator password session or writable local administrator
API token can change authentication configuration. A local password session is
required to test a directory user's login. Saved changes apply after restart.

## Connection and identity

- `ldaps://directory.example:636` uses TLS immediately.
- `ldap://directory.example:389` requires StartTLS before any credentials are
  sent. Plaintext bind and skipping certificate verification are unsupported.
- The service account must search the user base and read the stable identity,
  display-name and group attributes. Give it only the required read access.
- The user filter contains exactly one `{username}` placeholder. Depsilo escapes
  the supplied value and accepts exactly one matching entry, then verifies the
  password by binding as that user. Empty passwords and referrals are rejected.
- Use `entryUUID` for OpenLDAP or binary `objectGUID` for Active Directory.
  Usernames, email addresses and DNs are not durable identity keys.
- Role mappings use direct group DNs from the configured user attribute,
  typically `memberOf`. No nested-group expansion or directory synchronization
  is performed. Configure exact directory groups, not display names.

OpenLDAP must expose a membership attribute such as the `memberof` overlay.
For Active Directory, a typical user filter is
`(&(objectClass=user)(sAMAccountName={username}))` and the stable attribute is
`objectGUID`. Actual directory schema, permissions and group behavior must be
checked against your deployment; the real integration fixture uses OpenLDAP.

## Configuration example

These are placeholders. Use the real directory endpoint, DNs, groups and CA.
The service bind password can be supplied through the named-source environment
override instead of writing it into TOML.

```toml
[auth.sources.directory]
protocol = "ldap"
enabled = true
display_name = "Company directory"
admin_groups = ["cn=depsilo-admins,ou=groups,dc=example,dc=com"]
readonly_groups = ["cn=depsilo-readers,ou=groups,dc=example,dc=com"]
session_ttl = "30m"

[auth.sources.directory.ldap]
url = "ldaps://directory.example:636"
bind_dn = "cn=depsilo-reader,ou=services,dc=example,dc=com"
# Supply DEPSILO_AUTH_SOURCES_DIRECTORY_LDAP_BIND_PASSWORD in the service environment.
user_base_dn = "ou=people,dc=example,dc=com"
user_filter = "(uid={username})"
subject_attribute = "entryUUID"
display_name_attribute = "displayName"
groups_attribute = "memberOf"
# ca_cert_pem = "...public PEM CA certificates..."
```

The CA field extends system trust; it contains certificates, never private
keys. Service credentials are write-only in Admin responses. User passwords
are used for the current request and are not stored in accounts or sessions.
Directory login, binding, diagnostics and submission of a new service bind
password in the browser require HTTPS, with a loopback HTTP exception for local
development. The UI checks before sending credentials; the API also rejects
insecure requests. Non-secret configuration changes remain available over HTTP.

When terminating TLS at a reverse proxy, keep the backend private and preserve
the original `Host`. The same-host HTTPS `Origin` check supports this trusted
proxy deployment; an arbitrary HTTP client can forge that header, so it is not
proof of end-to-end TLS. Do not expose the plain HTTP backend publicly. A
server-side rejection cannot undo credentials already sent by a client.

## Check, test and activate

1. Check the configuration to verify TLS, the service bind and search-base
   readability. This does not prove a specific user can log in.
2. Save the source, then test with a directory username and password. The result
   separates authentication from group admission. It creates no account, binding
   or login session and leaves the current administrator session intact.
3. Enable the source and restart Depsilo. Confirm its running state and retain
   the local administrator password for recovery.

An unknown admitted identity must explicitly choose between creating an
independent directory-managed account and proving an existing local password
to bind to that account. Names and emails never merge accounts automatically.
In Account security, a local account can bind one identity per source and unlink
it using its local password. Unlinking revokes that identity's external sessions
and preserves local password login. Directory-managed accounts cannot unlink
their managing identity, add a local password or move their permission source.

An external session receives the lower of its directory role and local account
role. It cannot manage authentication settings or mint durable credentials.
Sessions default to 30 minutes, configurable from 5 minutes to 1 hour; refresh
does not extend that deadline. Directory disable/group changes are observed on
the next login, not pushed to existing sessions. Local disable takes effect on
the next authenticated request.

Renaming a source preserves sessions after restart. Authentication-setting
changes, disabling and removal revoke its sessions. Changing the source ID,
directory endpoint, search base or stable-ID attribute changes the identity
authority and cannot silently adopt old bindings. Preserve the original source
or explicitly unlink/rebind local accounts; managed-account migration is not
implemented.

Protocol references: [OpenLDAP TLS](https://www.openldap.org/doc/admin26/tls.html),
[OpenLDAP access control](https://www.openldap.org/doc/admin26/access-control.html),
and the [Go LDAP client](https://github.com/go-ldap/ldap).
