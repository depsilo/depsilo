# Compatibility Policy

> Published 2026-10-06 for [ADR-0005](adr/0005-enforcement-first-until-1-0.md)
> exit criterion 6. This file is the authority for what Depsilo promises to
> keep working, for how long, and how a removal is announced.

## What this covers

| Surface | Versioning unit | Authority |
| --- | --- | --- |
| Admin and public JSON HTTP API | `/api/v1` path prefix | this file |
| MCP endpoint | `/mcp` | this file |
| Health, readiness, and metrics | `/health`, `/live`, `/ready`, `/metrics` | this file |
| Compiler-cache HTTP APIs | `/ccache/v1`, `/sccache/v1` | this file |
| Configuration file | `config_version` + the schema in `config.example.toml` | this file |
| Database schema | numbered migration ledger | this file |
| Exported documents | format identifiers ending in `/vN` (`depsilo/snapshot/v1`, `depsilo/cra-technical-file/v1`) | this file |
| Webhook payloads, Prometheus metrics, CLI output | additive fields | this file |
| Package-proxy routes (`/pypi`, `/npm`, `/maven`, `/v2`, …) | the upstream client protocol | the package manager, not Depsilo |

Package-proxy routes exist so that `pip`, `npm`, `docker` and the other
clients keep working. Their compatibility is defined by those clients'
protocols; Depsilo does not add its own version suffix there.

## Release lines

- **Patch releases (`v0.x.y` → `v0.x.z`)** are compatible: no removal or
  behavior change for any surface in the table above.
- **Minor releases (`v0.x` → `v0.y`)** may contain breaking changes until
  `v1.0.0`. Every breaking change is listed under `Changed` or `Removed` in
  the changelog with a migration note, and — for the HTTP API and the
  configuration file — is deprecated for a full minor release first (see the
  window below).
- **From `v1.0.0` on**, the same rules apply with the longer window below and
  semantic versioning: breaking changes require a major release.

## Deprecation window

A removal or a breaking change to the HTTP API, the configuration file, a
`/vN` document format, a webhook payload, a metric name/label set, or a CLI
flag is announced and kept working for:

| Phase | Minimum window |
| --- | --- |
| Before `v1.0.0` (minor releases) | one full minor release **or 90 days, whichever is longer** |
| From `v1.0.0` on | two minor releases **or 180 days, whichever is longer** |

The window starts at the release that first announces the deprecation. During
the window the old behavior keeps working; where the surface can carry
signals, the deprecation is also visible there (HTTP responses carry
`Deprecation: true` and `Sunset: <date>` headers, the configuration loader
logs a startup warning naming the replacement, and CLI output prints the
same). Removal happens no earlier than the advertised `Sunset` date.

Security posture changes are the documented exception. When keeping the old
behavior would leave a known hole (for example failing open on a proven
malware match), the change ships immediately and is called out in the
changelog as a security change with the reason and any operator-visible
consequence.

## HTTP API

- Admin and public JSON API routes live under `/api/v1`; new JSON API routes
  are added there, and a future incompatible JSON API becomes `/api/v2`
  rather than changing `/api/v1`. The authenticated MCP endpoint (`POST /mcp`),
  health/readiness/metrics, compiler-cache, package-proxy, and cascade relay
  routes are separate HTTP surfaces, not unversioned JSON API routes.
- Within the window, changes are additive: new endpoints, new optional
  request fields, new response fields, and new enum values. Clients must
  ignore fields they do not know and treat unknown enum values as opaque.
- Existing field names, types, and status codes are stable. Error bodies keep
  the `{ "code": ..., "message": ... }` shape; `code` values are stable
  identifiers (for example `MALICIOUS_BLOCKED`, `SNAPSHOT_BLOCKED`).
- Deleting a field, renaming a field, changing a type, changing a default, or
  making an optional request field required is a breaking change and follows
  the deprecation window.
- Response ordering is not part of the contract unless an endpoint documents
  an order. Lists may return new items between calls; pagination is
  endpoint-specific.

## Configuration

- `config_version` records the schema generation of the document and is
  pinned in memory at load. A document with a version **newer** than the
  binary supports is rejected at startup instead of being partially
  interpreted; an older or unversioned document is migrated in memory to the
  current generation.
- Unknown keys are rejected at startup with the offending key named, so a
  typo or a key from a newer binary fails loudly instead of silently doing
  nothing.
- A deprecated key keeps working for the full window and logs a startup
  warning naming its replacement. It is removed only in a release whose
  changelog entry says so, and removal requires a `config_version` bump.
- The `custom` table is operator-owned: Depsilo preserves it when patching
  managed settings and never interprets its contents.
- Safety rejections are not deprecations. Values that cannot be enforced
  safely (for example a positive minimum-release-age threshold without bound
  provenance) keep failing at startup, with the fix named in the error.

## Database and backups

- Schema changes are numbered, one-way migrations applied exactly once and
  recorded in `schema_migrations`. A database written by a newer binary is
  rejected at startup instead of being mutated by an older binary.
- The supported downgrade path is restoring a backup taken before the
  upgrade with `depsilo restore`; migrations are not reversed in place.
- `depsilo backup` writes a checksummed `depsilo-backup` archive containing
  the configuration file and a transactionally consistent SQLite snapshot.
  Cached artifact bytes are deliberately excluded; copy the storage directory
  or bucket out-of-band if you need them.
- `depsilo restore` accepts the current archive version (`v2` today) and
  rejects other versions with an explicit error rather than guessing. When a
  new archive version is introduced, the previous reader stays available for
  the deprecation window above.
- Local and S3-compatible cache storage keep their on-disk layout across
  patch releases; cache eviction may remove entries at any time, so the cache
  is never part of the compatibility contract.

## Documents, webhooks, and metrics

- Documents that leave the process carry a format identifier with a version
  suffix (`depsilo/snapshot/v1`, `depsilo/cra-technical-file/v1`). A breaking
  shape change bumps the suffix; the previous reader stays available for the
  window and the changelog states the migration.
- SBOM output follows the published SPDX 2.3 and CycloneDX 1.5 schemas;
  Depsilo's additions are confined to documented fields/properties.
- Webhook payloads gain fields additively. Consumers must tolerate unknown
  fields and new event names.
- Prometheus metric names and existing label sets are stable. New metrics and
  new label *values* are additive; adding or renaming a label follows the
  deprecation window because it creates new series.

## How to deprecate something

1. Keep the old behavior working and add the replacement.
2. Announce in the changelog (under `Deprecated`) with the `Sunset` date, and
   document the migration in the owning guide (`config.example.toml`,
   `docs/`, or the API reference).
3. Make it visible at runtime: `Deprecation`/`Sunset` headers, a startup
   warning, or CLI output — whichever the surface supports.
4. Remove only after the window; list the removal under `Removed` with the
   replacement and, for configuration, bump `config_version`.

## Current deprecations

No removal has an announced `Sunset` date. The legacy Admin query aliases
`q` (vulnerabilities) and `search` (audit logs) still work when `package` is
absent; they must not be removed until a deprecation is announced with the
window above. The next formally announced deprecation is added here with its
announcement release and `Sunset` date.

## Enforcement

The mechanically checkable parts of this policy are covered by tests:

- `internal/api/api_version_contract_test.go` keeps `/api/` JSON routes under
  the versioned `/api/v1` group; separate HTTP surfaces retain their own paths.
- `internal/config/loader_test.go` covers unknown-key rejection,
  newer-`config_version` rejection, and that `config.example.toml` loads at
  the current version.
- `internal/db/schema_v1_test.go` covers the contiguous migration ledger and
  the fresh/upgraded schema convergence.
- `internal/snapshot` and `internal/sbom` tests assert their exported format
  identifiers keep the `/vN` suffix.
