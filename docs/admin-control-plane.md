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
| Docker registries and extra indexes | `config.toml` | Restart-managed; absent from Admin Upstream CRUD. Admin may only initialize an empty Docker configuration with the Docker Hub default |
| Users, tokens, package rules, Webhooks, security scan policies | Database | Existing handler-specific runtime behavior |
| Minimum-release-age thresholds, blocklist/tamper settings | `config.toml` | Loaded at startup; restart to change |

## Upstream observations

Admin Upstream responses include `has_observations`, independently of the
`healthy` routing flag, and `runtime_sample_count`. Observation evidence means
the current runtime object has recorded request/probe outcomes or a persisted
probe timestamp is available. The count covers this runtime object only and
resets on restart or configuration replacement; it is neither lifetime usage
nor a 24-hour statistic. Passive-only evidence without a persisted probe can
be lost on restart. A false evidence flag means no available observation, not
that the source has never been used. Existing failures remain visible even
without evidence; new routing defaults do not establish measured health.

The Admin list refreshes status every minute while idle. Manual checks expose
their own result separately from runtime eligibility: a successful probe does
not clear protocol-integrity restrictions or guarantee artifact delivery.
The latest manual results are retained in the open page, not across navigation.

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

## Docker first connection

`GET /api/v1/admin/docker/bootstrap` reports the saved and running Docker
configuration, whether a restart is pending, and whether the file can be
written. It does not return registry endpoints or credentials. An admin may
call `POST /api/v1/admin/docker/bootstrap` to add Docker Hub only when no
Docker registries are configured and no Docker environment override is active.
This writes `config.toml` with private file permissions, preserves unrelated
settings, and requires a service restart before the proxy becomes active.
Existing custom registries remain operator-managed; neither this route nor
ordinary Upstream CRUD can modify them.

## Dashboard statistics and usage

`GET /api/v1/admin/dashboard` reports `usage` independently of the selected
statistics range. `state` is `never-used` only when observation covers the
instance's complete lifetime and no completed artifact download has been
observed. `used` records a completed artifact GET with a successful HTTP
status and a nonempty response body; metadata, HEAD, policy blocks, failed
transfers and internal maintenance requests do not establish this evidence.
`unknown` covers older installations and gaps in observation. The optional
frontend contract also treats an older server without `usage` as unknown.

Usage evidence survives request-log and cache retention. A
`first_successful_request_at` timestamp is reported only when the first
successful download is known across the instance's lifetime; otherwise it is
null, including an older instance's first newly observed download. An unclean
shutdown before durable evidence is recorded invalidates the claim that the
instance was never used. A later completed download can still establish
`used`. These facts are recorded separately from the lossy access-log queue.

Dashboard aggregates and authenticated `GET /api/v1/now` return HTTP 500 with
`code: DB_ERROR` when a required query fails. A successful query with no rows
still returns real zero counts. The frontend can therefore distinguish an
empty period from unavailable statistics. Database error details remain in
server logs.

## Cache-fill progress

`GET /api/v1/admin/cache/transfers` is an authenticated Admin read endpoint.
Its authority is the running cache manager, not SQLite: it reports up to 64
active or recent client-triggered PyPI/PyTorch cold-cache fills, with active
items first. Completed records are reclaimed before active records; if all 64
slots are active, additional fills proceed without a progress record. A service
restart clears these snapshots. Warm cache reads and other ecosystems are not
individual download jobs in this view.

Each item reports a safe file name, ecosystem, upstream name, downloaded and
expected bytes, average byte rate, resume attempts and last resume offset.
An unknown expected size is `-1`. States are `connecting`, `downloading`,
`resuming`, `saving`, `cached`, and `failed`. EOF moves a transfer to `saving`;
only a successful object and metadata commit makes it `cached`. `failed` means
that the cache fill did not complete, even when a client download could continue
after a storage failure. These are task outcomes, not a promise that a completed
object remains cached forever after cleanup or policy changes.

The endpoint omits raw cache keys, target URLs, credentials and upstream error
messages. Admin polls it every two seconds while visible, labels stale snapshots
on refresh failure, and refreshes cache listings after observing a completed
fill. This does not add pause/resume controls or persist partial files across a
restart; safe automatic resume still requires the existing strong-validator
and byte-range checks.

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

`GET /api/v1/admin/security/vulnerabilities` uses `package`; the legacy `q` alias
remains accepted only when `package` is absent. No removal date has been announced.
Security policy responses and writes use `auto_block_enabled` and
`min_cvss_score`, with the score constrained to `0..10`.

`GET /api/v1/admin/audit-logs` and `GET /api/v1/admin/audit-logs/export` use `package`;
the legacy `search` alias remains accepted only when `package` is absent. No
removal date has been announced. `GET /api/v1/admin/logs/export` accepts the
same filters as the access-log list and exports at most 10,000 matching rows.

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

Create and Update accept an optional `via` field naming a cascade peer configured in
`[cascade]`. The value is stored with the upstream and points egress at that peer;
an unknown or malformed peer name fails with `422 INVALID_UPSTREAM` before anything
is committed. `GET /api/v1/admin/cascade` returns the deployment-level peer list
(names and URLs, never tokens); the Admin Cascade tab renders that topology together
with the `via` bindings from the upstream list.

`GET /api/v1/admin/cascade/config` returns the cascade section of `config.toml`
with `token_set` booleans instead of secrets and a `config_writable` flag.
`PUT` accepts a partial patch (`enabled`, `max_hops`, `max_ttl`,
`allow_insecure_http`, `shared_token`, and a full `peers` replacement), validates
the resulting document, and writes it atomically; every change is reported as
restart-required. The route sits on the Admin write group, so a read-only
principal receives `403`; a read-only file returns `409 CONFIG_READ_ONLY`, and
invalid values or a peer removal still referenced by an upstream return
`422 INVALID_SETTING` before the file is touched. Stored secrets are never
returned in any response.

Upstream validation and lifecycle errors use `400 BAD_REQUEST`, `404 NOT_FOUND`,
`409 CONFLICT`, `409 LAST_UPSTREAM`, `409 ECOSYSTEM_NOT_ACTIVE`,
`422 INVALID_UPSTREAM`, `422 IMMUTABLE_ECOSYSTEM`, and
`500 REGISTRY_RECONCILE_FAILED`. A successful response means the committed database
records and the live Pool snapshot agree.

The unauthenticated `GET /api/v1/stats` response exposes only
`scheme://host[:port]` for each upstream. It never returns URL userinfo, paths, queries,
or fragments.

## Client connection verification

The authenticated `/admin/connect` workspace is reusable. Its selected client,
configuration scope, shell, observation cursors, and results survive a reload
together; changing the selection or starting a new check creates a new baseline.
The service origin comes from the browser's current origin. Configuration is
applied on the client device, not on the Depsilo server.

`GET /api/v1/admin/onboarding/status` retains its audit `events`, numeric
`after_id`, and `has_more` contract. The additional `delivery_events`,
`delivery_after_id` / `next_delivery_after_id`, and `deliveries_has_more` fields
provide completed-response evidence. The delivery cursor is opaque and tracks
both independently committed log streams; clients must preserve it verbatim.
Joined records use the server request ID and the audit request-start timestamp,
so a response to a request that began before the observation cannot complete it.
This is an observation of related requests, not a unique client identity.

A successful download requires a GET artifact response, 2xx status, positive
bytes, a completed stream, and a hit/miss result. A 206 confirms delivery of the
requested range, not the entire artifact. Metadata hits, HEAD responses, policy
blocks, errors, and interrupted or unknown delivery results do not qualify.
Cache verification requires a successful hit for the same cache key as the
observed successful download; an initial successful hit also qualifies.

The onboarding `completed` / `skipped` preference is separate from durable
Dashboard usage evidence. The UI records completion after successful artifact
delivery, and the update endpoint never downgrades `completed` to `skipped`;
its response contains the effective persisted state. Returning to operations
remains possible when the preference API is unavailable.

Docker's current logs can associate an image manifest with its repository/tag
but do not bind blob deliveries to that tag. The workspace states this limit
and does not promote manifest hits to verified image downloads. pip and npm
provide isolated small-artifact download commands using the configured source;
other clients explicitly distinguish metadata checks from artifact downloads.

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
