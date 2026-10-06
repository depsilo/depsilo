# SIEM Audit Routing

> Published 2026-10-06 for [ADR-0004](adr/0004-supply-chain-enforcement-layer.md)
> ("SIEM-grade audit + multi-channel webhook routing") and ADR-0005's
> follow-up list. The audit log itself stays the source of truth; this feature
> forwards it.

## What is forwarded

Every row of the durable `audit_logs` table: proxy requests (`action` =
`download` / `metadata` with `cache_result` = hit / miss / error / blocked),
policy blocks recorded by the enforcement gates, and governance actions
written by admin handlers (snapshot lifecycle, audit exporter changes, …).

Alert-style events (malware block, quarantine, tamper, upstream down) keep
their own webhook channel; a `generic` webhook already posts those payloads to
any HTTP intake. Audit routing is the continuous record stream, not the alert
path.

## Delivery model

The audit log table is the buffer. Each exporter stores a **cursor**: the
highest `audit_logs.id` it has delivered.

- The forwarder polls on a 5-second tick, reads up to 200 rows per batch
  (`id > cursor`), drains up to 10 batches per tick, and posts one request per
  batch.
- A successful 2xx advances the cursor and increments the delivered counter.
  Delivery is therefore **at-least-once**: a crash between the collector
  accepting a batch and the cursor update re-sends that batch. Collectors
  should deduplicate on `id` (or `request_id`).
- A failure (transport error or non-2xx) records `last_error` and leaves the
  cursor untouched; the next attempt backs off exponentially from 10 seconds
  to 5 minutes. A restart resets the backoff, not the cursor.
- Rows filtered out by an exporter's event filter are skipped but still
  advance the cursor, so one exporter's narrow filter never blocks another's
  stream.
- A new exporter starts at the current audit head. Onboarding never replays
  months of history into a collector; historical ranges use the audit page's
  CSV export.

Nothing in this path runs inside a request: the audit logger writes the row
and returns, and the forwarder owns delivery.

## Collector formats

| Kind | Request | Auth |
| --- | --- | --- |
| `ndjson` | One JSON object per audit row, `application/x-ndjson`. Use for generic HTTP intakes (Elastic, Datadog, a small shipper). | `Authorization: Bearer <token>` when a token is set |
| `splunk_hec` | One Splunk HTTP Event Collector envelope per row: `{"time", "host", "source": "depsilo", "sourcetype": "depsilo:audit", "event": <row>}`. The URL is the full HEC event endpoint (`https://host:8088/services/collector/event`). | `Authorization: Splunk <token>` |

`source`/`sourcetype` give Splunk a stable search prefix
(`sourcetype="depsilo:audit"`); the host is the Depsilo machine's hostname.

## Event filter

`events` is `*` (default) or a comma-separated list matched against a row's
`action` **or** `cache_result`, case-insensitively:

- `download`, `metadata` — request classes
- `hit`, `miss`, `error`, `blocked` — outcomes
- governance actions such as `snapshot_create`, `snapshot_delete`,
  `snapshot_import`, `snapshot_use`, `exporter_test`

Example: a security feed that only wants refusals uses `blocked`; a full
compliance feed uses `*`.

## Tamper-evident chain

The audit log is hash-chained so a downstream SIEM can prove that what it
received is what Depsilo wrote, and an operator can prove that what is stored
now matches.

- Every row written since schema v8 carries `prev_hash` (the previous row's
  hash, `""` for the first chained row) and `hash`. `prev_hash` has a unique
  index, so two concurrent writers cannot fork the chain: the loser retries
  against the new head.
- The hash formula is versioned and reproducible:

  ```
  hash = SHA-256(JSON({
    "hash_version": "depsilo/audit-chain/v1",
    "prev_hash":   <previous row hash>,
    "ecosystem":   …, "package_name": …, "version": …, "action": …,
    "cache_result": …, "client_ip": …, "user_agent": …,
    "upstream_url": …, "latency_ms": …, "bytes_sent": …,
    "status_code": …, "created_at": <UTC RFC3339Nano>, "request_id": …
  }))
  ```

  Field order is the struct order above, the timestamp is normalized to UTC,
  and the database-assigned `id` is deliberately excluded: the chain binds
  content and order through `prev_hash`, and re-ordering, duplicating, or
  editing a row breaks the link at the next hash.
- Rows written before schema v8 have a NULL `prev_hash` and an empty `hash`.
  Verification reports them as a pre-chain prefix instead of pretending they
  are covered.

Verification entry points:

| Surface | Usage |
| --- | --- |
| Admin UI | The audit page shows the verification result (verified head, pre-chain rows, or the exact broken row) with a re-verify button. |
| Admin API | `GET /api/v1/admin/audit/integrity` returns the report (`ok`, `chained_rows`, `unchained_rows`, `first_chained_id`, `head_id`, `head_hash`, `broken_at_id`, `reason`). |
| CLI | `depsilo audit verify [--json]` recomputes the chain directly against the SQLite file, usable offline or after a restore. Exit code 1 means the chain is broken. |

Fully switched-on SIEM consumers can also verify continuously: the forwarded
rows carry `prev_hash`/`hash`, so the same reimplementation as above can
recompute each batch and alarm on the first mismatch.

### External anchoring

A chain alone cannot detect a rewrite that recomputes every hash. Anchoring
solves that by storing periodic checkpoints of the chain head outside the
database:

```toml
[audit]
checkpoint_file     = "/var/lib/depsilo-audit/anchors.ndjson"  # different volume/bucket
checkpoint_interval = "15m"
# or push directly, without relying on the operator to ship the file:
# checkpoint_url   = "https://worm.example/audit-anchors"
# checkpoint_token = "…"
```

Each line is one checkpoint:

```json
{"format":"depsilo/audit-anchor/v1","checked_at":"…","head_id":1200,"head_hash":"…"}
```

`depsilo audit verify` and `GET /api/v1/admin/audit/integrity` cross-check
every checkpointed `(head_id, head_hash)` pair against the stored row, so a
deleted or rewritten head — which an internally consistent rebuilt chain would
otherwise hide — is reported with its id. Ship the anchor file to WORM
storage or a log platform on a schedule; that copy is what makes a
full-database rewrite provable rather than merely suspicious. Malformed
checkpoint lines are skipped and counted instead of hiding the rest of the
file.

`checkpoint_url` removes the shipping step: the same NDJSON line is POSTed
(`Authorization: Bearer <checkpoint_token>` when set) whenever the head
changes, with per-sink deduplication, a 60-second retry after a failure, and
delivery status (`remote_head_id`, `remote_last_success_at`,
`remote_last_error`) surfaced in the integrity API and the audit page. A
remote delivery failure is a warning, not a chain contradiction: the local
chain still verifies, but the off-box copy is behind. RFC 3161 timestamp
authorities are out of scope; point the URL at a service that records arrival
time (most log platforms and WORM gateways do).

## Admin surface

The audit page (`/admin/audit`) lists exporters under **SIEM audit routing**
with their format, enabled state, delivered count, pending lag, and last
delivery error, and offers **Test** (a synthetic `exporter_test` row through
the real delivery path), enable/disable, and delete.

API (admin read/write capability groups, open source):

| Method | Path |
| --- | --- |
| GET | `/api/v1/admin/audit/exporters` |
| POST | `/api/v1/admin/audit/exporters` |
| PUT | `/api/v1/admin/audit/exporters/:id` |
| DELETE | `/api/v1/admin/audit/exporters/:id` |
| POST | `/api/v1/admin/audit/exporters/:id/test` |

Tokens are write-only: they are stored but never returned, and collector URLs
(which often embed credentials) are masked for principals without write
access.

## Out of scope for this iteration

- Syslog/UDP or CEF transports (use a local shipper with the NDJSON format).
- Per-event routing rules beyond the action/outcome filter.
- Mutually authenticated TLS (mTLS) to the collector; terminate that in a
  sidecar or reverse proxy.
- RFC 3161 timestamp authorities (see the anchoring section above for the
  supported HTTP push).
