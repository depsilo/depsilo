# Admin Control Plane

The Admin API is served under `/api/v1`. Authenticate with a JWT or API token and call
`GET /api/v1/auth/me` before rendering capabilities. JWTs are revalidated against the
current user on every request. API-token writes require an enabled owner whose current
role is `admin` and a token whose permission is `readwrite`.

## Authority

| Resource | Authority | Runtime application |
| --- | --- | --- |
| Settings | `config.toml` | Log level applies immediately; Cache/Auth changes require restart |
| Ordinary active Upstreams | Database | Registry atomically updates the next proxy request |
| Docker registries and extra indexes | `config.toml` | Restart-managed; absent from Admin Upstream CRUD |
| Users, tokens, rules, Webhooks, security policy | Database | Existing handler-specific runtime behavior |

## Permissions

Principal capability groups allow both `admin` and `readonly` principals to call every
Admin `GET` route, including CSV exports. Project list/detail/package and SBOM `GET`
routes additionally require an active Pro entitlement and otherwise return
`402 PRO_REQUIRED`. `POST /api/v1/admin/rules/test` is also read-only. Every other Admin
`POST`, `PUT`, `PATCH`, and `DELETE` requires a write-capable Principal. API tokens cannot
call `/api/v1/auth/refresh`. Readonly responses render credential-bearing URLs as
`scheme://host[:port]/***`; userinfo, paths, queries, and fragments are never returned.
Webhook URLs, proxy credentials, secrets, keys, and every other credential-bearing
response field are masked by the server.

The service rejects self-delete, self-disable, and self-demotion with `409 SELF_LOCKOUT`.
It also rejects removing the final enabled administrator with `409 LAST_ADMIN`.

## Operator credentials

Initial setup, Admin user creation, and Admin password changes share one password policy:
at least 12 characters and three character classes, or a passphrase of at least 20
characters, with a 72-byte bcrypt limit. Passwords cannot contain the username. Initial
setup separately applies its stricter administrator-username shape rules; Admin user
creation does not inherit first-run administrator existence checks.

JWTs carry the user's persistent credential version. A password change, role change, or
enabled-state change increments that version in the same transaction as the mutation, so
every older JWT is rejected immediately and cannot become valid again after re-enabling a
user. Schema version 2 initializes existing users at credential version 1; pre-upgrade
JWTs have no matching claim and are invalidated once. API tokens are intentionally not
bound to the JWT credential version and continue to use their own expiry and permissions
plus the owner's current enabled state and role.

## Contract compatibility

`GET /api/v1/admin/security/vulnerabilities` uses `package`; the deprecated `q` alias
remains accepted for one release only when `package` is absent. Security policy responses
and writes use `auto_block_enabled` and `min_cvss_score`, with the score constrained to
`0..10`.

`GET /api/v1/admin/audit-logs` and `GET /api/v1/admin/audit-logs/export` use `package`;
the deprecated `search` alias remains accepted for one release only when `package` is
absent. `GET /api/v1/admin/logs/export` accepts the same filters as the access-log list and
exports at most 10,000 matching rows.

Project package responses use `ecosystem`, `package_name`, `version`, `first_seen_at`,
`last_seen_at`, and `download_count`. Project proxy URLs fall back to `/p/{slug}`.

## Package policy test

`POST /api/v1/admin/rules/test` is a read-only Admin operation and is available to
readonly principals. It accepts `{ "ecosystem": "pypi", "package": "requests",
"version": "1.0.0" }` and returns the allow/deny decision, `winning_rule`, the
matching `candidates` in deterministic winner-first order, and each candidate's
`match_levels` and `specificity`. The selected candidate is also available as
`winner`. `reason` and `winner_reason` carry the winning rule's operator-authored
business reason; `precedence_reason` identifies the tuple dimension that selected
it. When the evaluator is degraded, `policy_status` reports whether a stale
last-known-good snapshot was used and its age.
When no rule matches, the response keeps `matched_rule: null`, returns an empty
`candidates` array, and reports the existing default-allow decision.

## Settings

`GET /api/v1/admin/settings` returns complete `configured` and `effective` snapshots plus
`sources`, `overrides`, `editable`, `pending_restart`, and `config_writable`. The editable
paths are `server.log_level`, `cache.max_size_gb`, `cache.ttl_index`, `cache.ttl_blob`,
`cache.lru_threshold`, and `auth.token_ttl`.

`PUT /api/v1/admin/settings` accepts a nested partial patch. Its response adds `changed`,
`applied_now`, `restart_required`, and `blocked_by_override`. An environment-overridden
field is still written to the file and returned in `changed`; among the application-result
lists it appears only in `blocked_by_override`, not `applied_now` or `restart_required`.
The service rejects malformed JSON with `400 BAD_REQUEST`, invalid values with
`422 INVALID_SETTING`, read-only configuration with `409 CONFIG_READ_ONLY`, and failed
durable replacement with `500 CONFIG_WRITE_FAILED`. A malformed or unreadable current
file returns `500 CONFIG_READ_FAILED`; no failure path changes the running level or the
Admin draft.

## Upstreams

The first upgraded start imports ordinary configured upstreams and records the active
ecosystems. After that seed, the database is authoritative for active ecosystems; restart
does not restore a row deleted through Admin. Adding config upstreams for a previously
inactive supported ecosystem activates and imports that ecosystem on the next restart.
Versioned endpoint repairs are the narrow exception: an upgrade may rewrite an exact
legacy built-in adapter/name/URL triple once, while preserving its ID, proxy, priority,
and probe settings. Custom URLs are never matched, and later Admin edits remain
authoritative. Because probe history belongs to the old target, a matched row starts a
new health history after the rewrite.

Active probe workers check once when they start and then continue on their configured
interval. Request selection always prefers the highest-priority healthy Upstream.
Only when none is healthy may an unhealthy passive Upstream receive one cooldown-limited
half-open request. A critical protocol-failure latch is never eligible for recovery.

`GET /api/v1/admin/upstreams` returns `{items,total}` runtime resources. Create and full
Update return the resulting runtime resource; Delete returns `{deleted_id,adapter_type}`.
Check returns `{upstream,check}` with HTTP 200 even when the network probe reports
`healthy:false`. An active ecosystem keeps at least one row (`409 LAST_UPSTREAM`), and
Admin cannot activate an inactive ecosystem (`409 ECOSYSTEM_NOT_ACTIVE`).

Upstream validation and lifecycle errors use `400 BAD_REQUEST`, `404 NOT_FOUND`,
`409 CONFLICT`, `409 LAST_UPSTREAM`, `409 ECOSYSTEM_NOT_ACTIVE`,
`422 INVALID_UPSTREAM`, `422 IMMUTABLE_ECOSYSTEM`, and
`500 REGISTRY_RECONCILE_FAILED`. A successful response means the committed database
records and the live Pool snapshot agree.

The unauthenticated `GET /api/v1/stats` response exposes only
`scheme://host[:port]` for each upstream. It never returns URL userinfo, paths, queries,
or fragments.

## Verification

Run the complete offline verification with:

```bash
make verify
```

For a faster edit loop, run only the relevant layers:

```bash
make test
make test-ui
make verify-web
```
