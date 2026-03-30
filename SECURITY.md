# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |

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

- Default admin credentials (`admin:admin`) created on first run — this is documented and intended for initial setup only. Users are expected to change the password immediately.
- The `config.example.toml` contains placeholder values (`change-me-in-production`) — these are not real secrets.
- SQLite database is not encrypted at rest — use PostgreSQL with TLS for sensitive deployments.
- Proxy traffic between Depsilo and upstream sources is not independently verified beyond HTTPS — this is by design to preserve GPG signature chains (especially for APT).

## Security Best Practices

When deploying Depsilo in production:

1. **Change the default admin password** immediately after first login
2. **Set a strong `jwt_secret`** in your configuration — never use the default
3. **Use HTTPS** via a reverse proxy (nginx, Caddy, Traefik)
4. **Restrict network access** to the admin API (`/api/v1/admin/*`)
5. **Use PostgreSQL** instead of SQLite for multi-user deployments
6. **Review access logs** regularly via the admin dashboard

## Acknowledgments

We thank the following individuals for responsibly disclosing security issues:

- *(Your name could be here)*
