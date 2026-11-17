# Security Policy

## Supported Versions

See [docs/threat-model.md](docs/threat-model.md) for the assets, trust
boundaries, mitigations, and deliberately accepted risks this policy is
written against.

The latest minor release line receives security fixes. Older lines do not:
upgrade to the latest release to get a fix. The latest release is the newest
entry on the [releases page](https://github.com/depsilo/depsilo/releases) and
the first versioned entry in [CHANGELOG.md](CHANGELOG.md).

A security fix may change behavior in a patch release when keeping the old
behavior would leave a known hole. The changelog lists such a change under
`Security`; [docs/compatibility.md](docs/compatibility.md#release-lines)
states the rule.

## Reporting a Vulnerability

If you discover a security vulnerability in Depsilo, please report it responsibly.

Report it through GitHub's private vulnerability reporting. This is the only
reporting channel, and it needs a GitHub account:

1. Open the [Security tab](https://github.com/depsilo/depsilo/security) of
   `github.com/depsilo/depsilo`.
2. Select **Report a vulnerability**.
3. Fill in the form and submit it.

The report is visible only to you and the maintainers. Do not report a
vulnerability in a public issue or discussion.

**Please include:**
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

**Response timeline:**
- **48 hours** — We will acknowledge your report
- **7 days** — We will provide an initial assessment
- **30 days** — We aim to release a fix for confirmed vulnerabilities

## Out of Scope

The following are known limitations, not vulnerabilities:

- The generated bootstrap token is printed in the server's startup output
  while first-run setup is pending, and a container runtime or service manager
  usually keeps that output as a log. Anyone who can read it during the
  bootstrap window may be able to claim the initial administrator. The same
  token is available to the service user through `depsilo setup token` and the
  private `data/secrets/bootstrap.token` runtime file, which is removed after
  setup. A generated token changes every time Depsilo restarts. A token preset
  through `DEPSILO_BOOTSTRAP_TOKEN` is not printed and does not change, so it
  stays valid until setup is complete. Protect logs and instance state and
  complete setup promptly. There is no default `admin/admin` credential.
- Depsilo generates the initial administrator password in one case: an
  instance that has a `config.toml` and no administrator starts with
  `DEPSILO_ADMIN_USERNAME` set and `DEPSILO_ADMIN_PASSWORD` unset. That start
  prints the password once, in its startup output and in a warning in the
  server log. The password is not one-time: it stays valid until someone
  changes it. Anyone who can read that output can sign in as the
  administrator, so sign in and change the password, and restrict who can
  read the log. A password supplied through `DEPSILO_ADMIN_PASSWORD` is never
  printed or logged.
- `config.example.toml` contains no secrets; its `jwt_secret` line is a
  commented-out placeholder. The value `change-me-in-production` is the
  built-in development placeholder for `auth.jwt_secret`, not a real secret.
  Depsilo accepts it only on a loopback listener and refuses to start with it
  on any other address.
- SQLite data is not encrypted at rest. Depsilo does not currently support
  PostgreSQL; use encrypted disks/filesystems and restrict access to the state directory.
- Depsilo preserves package-manager signatures and checksums end to end and
  relies on HTTPS for upstream transport. The tamper detector compares
  immutable bytes with the first-seen digest, but that first observation is not
  an independent proof of upstream authenticity.
- A forward proxy that carries Docker Registry requests (a registry's own
  `proxy`, or `[egress] upstream_proxy` for a registry without one) is a
  trusted egress component. Depsilo locally resolves and checks every
  registry, Bearer realm, and redirect target before handing the request to
  that proxy, but cannot require the proxy to receive the same DNS answer. A
  host name whose every local answer is a fake-IP address in `198.18.0.0/15`
  counts as public and is left to the proxy to resolve. Configure the proxy's
  DNS policy and ACLs to reject loopback, link-local, metadata, and unintended
  private targets. A configured registry may nominate a public cross-origin
  Bearer realm, so registry credentials also trust that authentication
  delegation; this is required for registries such as Docker Hub.

## Security Best Practices

When deploying Depsilo in production:

1. **Protect the bootstrap token** and choose a strong initial administrator
   password; remove broad access to bootstrap logs after setup
2. **Let Depsilo manage the signing key**: omit `jwt_secret`. A new instance
   then generates `data/secrets/jwt.key` beside its `config.toml` and reuses it
   on restart. Keep that file private and back it up with the database. If you
   supply `DEPSILO_AUTH_JWT_SECRET` or `jwt_secret` yourself, use at least 32
   random bytes with no surrounding whitespace, also when Depsilo binds to
   loopback behind a reverse proxy
3. **Use HTTPS** via a reverse proxy (nginx, Caddy, Traefik)
4. **Keep the listener on a trusted network.** Package routes, `/metrics`, and
   `/api/v1/stats` answer without sign-in by design, so anyone who can reach
   the port can download through Depsilo and read its usage statistics.
   Restrict who can reach the port, and restrict the admin API
   (`/api/v1/admin/*`) further where your reverse proxy or firewall allows
5. **Protect the SQLite state directory** with restrictive permissions,
   encrypted storage, and host-level backups
6. **Run a single Depsilo server per SQLite database**; multi-node/HA is not supported
7. **Review access logs and supply-chain events** regularly via the admin dashboard

## Acknowledgments

We thank the following individuals for responsibly disclosing security issues:

- *(Your name could be here)*
