# Isolated LDAP interoperability fixture

This fixture runs pinned OpenLDAP on loopback ports 18389 (mandatory StartTLS)
and 18636 (LDAPS). It uses generated TLS certificates, a read-only directory
service account, and three users with admin, reader and unmapped groups.
All credentials, certificates, directory state and application runs live in
`~/.cache/depsilo/ldap` with private directory/file permissions. No credentials
are printed or committed. Starting again retains existing state; stopping does
not delete it. The image is a test dependency, not a deployed user directory.

```sh
python3 testground/ldap/fixture.py start
make build-dev-server
node testground/ldap/depsilo-smoke.mjs
python3 testground/ldap/fixture.py stop
```

Requires Docker Compose, Python 3, OpenSSL, Node and the repository's installed
Playwright Chromium. The harness refuses an occupied Depsilo port 18083, creates
an isolated database and recovery password, and stops only its own process.
Production authentication rate limits remain enabled; the harness waits for
their retry window. It exercises real TLS/binds/search, diagnostics without
durable identity writes, denied logins, binding/unlinking, explicit first-login
choice, role restrictions, rename/security restart effects and browser flows.
Results and screenshots are written to the private run directory. OpenLDAP
coverage does not claim verification against Microsoft Active Directory.

The directory image's default membership overlay is configured for standard
`groupOfNames`/`member`, then operational `memberOf` values are populated. Its
service-account ACL grants reads after the password-attribute deny rule. The
fixture verifies the published TLS endpoint against its CA and checks the
actual UUID and membership values before reporting ready.
