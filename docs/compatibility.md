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
- Optional `[auth.sources.<id>]` configuration declares stable source IDs and
  a protocol (`oidc` or `ldap`). No external source is created by defaults.
  Sources use the existing file/environment precedence, are restart-managed,
  and are preserved by ordinary settings writes. Enabling a source requires a
  complete valid effective configuration. File-only validation permits omitted
  fields supplied by the service environment; runtime loading validates the
  combined result. Per-source environment keys use
  `DEPSILO_AUTH_SOURCES_<UPPERCASE_ID_WITH_HYPHENS_AS_UNDERSCORES>_<FIELD>`;
  only source IDs declared in TOML are enumerated. Nonempty issuer values must
  be distinct across configured sources, including disabled ones; disabled
  drafts may leave the issuer unset. Enabled callbacks
  must share one instance origin. See [OIDC configuration](oidc.md#configure-authentication-sources).
- Existing `[auth.oidc]` remains source ID `oidc`, with its old callback path and
  `DEPSILO_AUTH_OIDC_*` overrides. Explicit names are preserved; the fallback
  name is OIDC. The reserved ID cannot be used by a new named source. Moving
  configuration to another ID does not migrate account bindings.

## Operator authentication

- OIDC and LDAP add public provider/login endpoints and authenticated self-service
  binding endpoints under `/api/v1/auth`; existing local password login remains
  available. `auth/me` adds `permission_source`, `login_method`, and
  `can_manage_credentials`. Clients should use those capabilities rather than
  assuming every administrator session can manage long-lived credentials.
- System management is visible only to administrators; personal Account
  security remains available to every authenticated browser account. The
  existing Users, Settings, and License URLs are preserved. The new
  `/api/v1/admin/authentication/sources` collection supports listing, creation,
  per-source updates, and removal; reads require an administrator role and
  writes additionally require local credential-management authority. The old
  GET/PUT `/api/v1/admin/authentication` remains legacy-only. Client
  secrets are write-only, environment overrides cannot be edited, and saved
  configuration applies after restart. Removed running sources remain listed
  with null configured state until restart; removal preserves account bindings.
- Public providers include `id`, `name`, and `protocol`; login and linking accept
  `provider_id`. Existing legacy callback and completion paths remain. Requests
  without a source select the legacy source, or a sole enabled named source if
  legacy is absent; ambiguous multi-source requests require explicit selection.
  Self-service identities add `providers` while retaining the old `provider`
  field when a legacy or sole source supplies an unambiguous value.
- Existing local password JWTs without an external session claim retain their
  local authentication behavior when the account is enabled, has a password,
  and its credential version and role still match. External accounts and
  external sessions cannot use that legacy path.
- External sessions have a separate absolute expiry and revocation record; refresh
  never converts them into local sessions or extends the provider admission
  window. Existing locally owned API tokens keep their role/expiry behavior;
  provider-managed accounts cannot use API tokens to bypass that window.
  Startup invalidation is source-scoped: changing, disabling, or removing one
  source does not revoke valid sessions for unchanged sources. An unchanged
  legacy OIDC configuration upgrades its exact old fingerprint transactionally
  while preserving valid sessions. Subsequent display-name-only changes also
  preserve sessions. A simultaneous rename during that first legacy upgrade
  cannot prove the old configuration and revokes those sessions conservatively.
- Linking and unlinking are explicit account operations and do not merge
  usernames or emails. Local accounts may bind one identity per source and
  unlink each separately. Provider-managed accounts cannot add another source;
  their deletion, password/role edits, and removal of the required identity are
  rejected. These identity capabilities do not change the authority of existing
  local accounts.

## Database and backups

- Schema changes are numbered, one-way migrations applied exactly once and
  recorded in `schema_migrations`. A database written by a newer binary is
  rejected at startup instead of being mutated by an older binary.
- Schema version 12 adds external login identities, sessions, and a local
  default for account Permission Source. Existing account IDs, password hashes,
  credential versions, and API-token rows are preserved. Multiple OIDC and LDAP sources
  reuse version 12 tables and the global `(issuer,subject)` unique key, without
  an additional database migration. Downgrading across
  this boundary requires the pre-upgrade SQLite backup as well as the older
  binary/image.
- The supported downgrade path is restoring a backup taken before the
  upgrade with `depsilo restore`; migrations are not reversed in place.
- `depsilo backup` writes a checksummed `depsilo-backup` archive containing
  a portable configuration snapshot and a transactionally consistent SQLite
  snapshot. The archived configuration includes the effective JWT signing
  secret, including a key managed in `data/secrets/jwt.key` or supplied through
  `DEPSILO_AUTH_JWT_SECRET`; the source configuration is not modified. Treat
  archives as credentials and keep them private. Restore preserves the signing
  key as `auth.jwt_secret` in the restored configuration, so restoring to a new
  directory does not require a separate secret-file copy. An unreadable,
  missing, or corrupt managed key fails backup rather than generating a
  replacement key or publishing an incomplete archive. Explicit legacy inline
  keys remain supported when archiving older state.
  Cached artifact bytes are deliberately excluded; copy the storage directory
  or bucket out-of-band if you need them.
  OIDC client secrets supplied only through the service environment are not
  copied into the archive; restore the corresponding environment separately.
  Inline OIDC configuration and persisted identity/session records remain part
  of the saved configuration/database and must be protected as private state.
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


LDAP source settings are nested under `[auth.sources.<id>.ldap]`; see
[LDAP configuration](ldap.md). Existing OIDC paths, source IDs and source
configuration remain compatible. The shared browser account-choice endpoints
also have `/api/v1/auth/identity/{complete,resolve,cancel}` aliases. LDAP adds
`ldap` to `permission_source`, `login_method` and provider protocol values.
Presentation-only source renames retain external sessions after restart. On the
first upgrade from the old full-config OIDC fingerprint, only an exact current
configuration match preserves sessions; changing configuration during that
upgrade remains fail-closed. This refinement and LDAP reuse schema version 12.
