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
- A tamper-evident hash chain over `audit_logs`. The cursor and delivery
  counters detect a stalled feed; they do not detect an operator editing the
  local database.
