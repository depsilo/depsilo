# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.9.x   | :white_check_mark: |
| < 0.9   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in Depsilo, please report it responsibly.

**Email:** security@depsilo.dev

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

- The one-time bootstrap token and any generated initial password are printed
  to the server log during setup. Anyone who can read that log during the
  bootstrap window may be able to claim or access the initial administrator.
  Protect logs and complete setup promptly. There is no default `admin/admin`
  credential.
- The `config.example.toml` contains placeholder values (`change-me-in-production`) — these are not real secrets.
- SQLite data is not encrypted at rest. Depsilo does not currently support
  PostgreSQL; use encrypted disks/filesystems and restrict access to the state directory.
- Depsilo preserves package-manager signatures and checksums end to end and
  relies on HTTPS for upstream transport. The tamper detector compares
  immutable bytes with the first-seen digest, but that first observation is not
  an independent proof of upstream authenticity.
- An explicitly configured Docker Registry forward proxy is a trusted egress
  component. Depsilo locally resolves and checks every registry, Bearer realm,
  and redirect target before handing the request to that proxy, but cannot
  require the proxy to receive the same DNS answer. Configure the proxy's DNS
  policy and ACLs to reject loopback, link-local, metadata, and unintended
  private targets. A configured registry may nominate a public cross-origin
  Bearer realm, so registry credentials also trust that authentication
  delegation; this is required for registries such as Docker Hub.

## Security Best Practices

When deploying Depsilo in production:

1. **Protect the bootstrap token** and choose a strong initial administrator
   password; remove broad access to bootstrap logs after setup
2. **Set a strong `jwt_secret`** of at least 32 random bytes in your
   configuration — never use the example placeholder. This also applies when
   Depsilo binds to loopback behind a reverse proxy
3. **Use HTTPS** via a reverse proxy (nginx, Caddy, Traefik)
4. **Restrict network access** to the admin API (`/api/v1/admin/*`)
5. **Protect the SQLite state directory** with restrictive permissions,
   encrypted storage, and host-level backups
6. **Run a single Depsilo server per SQLite database**; multi-node/HA is not supported
7. **Review access logs and supply-chain events** regularly via the admin dashboard

## Acknowledgments

We thank the following individuals for responsibly disclosing security issues:

- *(Your name could be here)*
