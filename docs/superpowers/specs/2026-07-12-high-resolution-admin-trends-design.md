# High-Resolution Admin Trends Design

**Date:** 2026-07-12
**Status:** Approved for implementation planning

## 1. Goal

Make the Admin dashboard trends feel near-real-time and materially more precise without returning one row per request or repeatedly scanning several days of raw access logs.

The chart will keep a bounded payload of approximately 300 to 360 points for every range:

| Range | Display bucket | Maximum points | Refresh interval | Primary source |
| --- | --- | ---: | ---: | --- |
| 1 hour | 10 seconds | 360 | 5 seconds | `access_logs` |
| 24 hours | 5 minutes | 288 | 15 seconds | `access_log_five_minutely` |
| 7 days | 30 minutes | 336 | 30 seconds | `access_log_five_minutely` |
| 30 days | 2 hours | 360 | 60 seconds | `access_log_hourly` |

Per-request points are intentionally excluded. They scale with traffic, create visual noise, and make response size unpredictable.

## 2. Storage Model

Add an `AccessLogFiveMinutely` model mapped to `access_log_five_minutely`. Its composite key is:

- `bucket_start`: UTC Unix seconds aligned to a five-minute boundary
- `adapter_type`
- `hit`
- `upstream`

Each row stores `request_count`, `total_bytes`, `sum_latency_ms`, `error_count`, and `updated_at`. It has the same aggregate dimensions as `AccessLogHourly`, so dashboard calculations can share a common projection without introducing package-level cardinality.

The batched access-log recorder will aggregate five-minute counters in memory alongside its existing raw, hourly, and package-daily buffers. Every normal recorder flush will upsert all four outputs independently. A failure in the new rollup write is logged and does not block proxy requests or prevent the other stores from being updated.

The five-minute table retains eight days. Existing raw and long-term rollup retention behavior remains unchanged.

## 3. Migration And Backfill

`db.AutoMigrate` creates the new table and indexes without changing existing tables.

At startup, a dedicated idempotent backfill populates only the trailing seven days from `access_logs`. The backfill:

- skips work only when the `access_log_five_minutely_v1` completion marker exists in `control_plane_states`;
- groups timestamps into UTC five-minute boundaries;
- replaces the trailing seven-day rows and writes the completion marker in one database transaction, so a failure rolls back and the next startup retries the full window;
- runs before the dashboard server begins accepting requests, matching the current rollup bootstrap lifecycle;
- logs its start, completion, duration, and failure.

A backfill failure does not prevent server startup. Until fine-grained rows become available, `24h` groups indexed raw logs into five-minute buckets and `7d` returns the existing hourly rollup. These fallbacks produce valid, possibly coarser data rather than an empty chart.

## 4. Trends API

The existing endpoint and response contract remain stable:

```text
GET /api/v1/admin/dashboard/trends?range=1h|24h|7d|30d
```

Each returned point continues to include the current fields: bucket, requests, hits, misses, hit rate, byte counters, latency totals and averages, and errors.

Source and grouping rules are:

- `1h`: scan indexed raw rows from the trailing hour and group by 10-second UTC bucket;
- `24h`: read trailing five-minute rollups without further aggregation;
- `7d`: read trailing five-minute rollups and combine six consecutive rows into 30-minute buckets;
- `30d`: read hourly rollups and combine pairs into two-hour buckets.

Every range includes the current incomplete bucket so recent requests appear on the next refresh. Empty intervals are emitted as zero-valued points. Bucket alignment is deterministic in UTC; the browser continues formatting timestamps in its local timezone.

Invalid range values continue to use the existing `7d` fallback behavior for compatibility.

## 5. Frontend Presentation

`TrendsCard` will render all API points directly instead of rebucketing 7-day and 30-day data into local calendar days. The API is authoritative for display granularity.

Presentation changes:

- chart height increases from 180 px to 240 px on desktop, with a responsive minimum suitable for mobile;
- time-series strokes use linear interpolation instead of `monotone`, avoiding curves that imply unobserved values;
- data remains full resolution, while X-axis ticks are sampled independently to prevent label collisions;
- desktop shows approximately six to eight time labels and mobile approximately three to four;
- tooltips show the exact local bucket start and the existing metric values;
- tabs continue switching locally without another request;
- range-specific React Query refresh intervals are 5, 15, 30, and 60 seconds respectively;
- a failed background refresh keeps the last successful chart visible and shows the existing stale-data warning.

The chart remains an aggregate interval chart. UI text will not claim that a plotted point represents a single request.

## 6. Performance Boundaries

API responses are bounded to at most 360 points. The only raw-table query covers one hour and uses the existing `created_at` index. Multi-day views use rollups.

The new table's expected upper bound is approximately:

```text
288 buckets/day x 8 days x adapter/hit/upstream combinations
```

This is materially smaller than per-request storage and has a fixed retention horizon.

Recorder ingestion remains non-blocking. The new in-memory aggregation must not add database work to the proxy request goroutine.

## 7. Compatibility And Failure Handling

- The endpoint path and JSON point shape do not change.
- Existing hourly and daily tables remain authoritative for their current consumers.
- Rollup-disabled mode derives requested series from raw logs, preserving behavior at a higher query cost selected by the operator.
- Missing five-minute data makes `24h` fall back to five-minute grouping over indexed raw logs and `7d` fall back to hourly rollups.
- Database or API errors use existing structured logging and query-state UI patterns.

## 8. Testing

Backend tests will cover:

- five-minute key alignment across boundaries and UTC conversion;
- recorder aggregation and idempotent upsert behavior;
- seven-day backfill, empty-table detection, and repeat execution;
- eight-day retention;
- exact bucket count and alignment for every range;
- inclusion of the current partial bucket and zero-filled gaps;
- hit rate, error rate, bandwidth, and weighted latency calculations;
- fallback behavior when five-minute rollups are unavailable or disabled.

Frontend tests will cover:

- direct rendering without daily rebucketing;
- range-specific refresh intervals;
- linear chart interpolation;
- bounded responsive X-axis ticks;
- exact tooltip timestamp formatting;
- stale data preservation after a background refresh failure;
- desktop and mobile chart layout without overflow or label overlap.

## 9. Delivery Sequence

1. Add and migrate the five-minute storage model.
2. Extend recorder aggregation, upsert, backfill, and retention.
3. Implement range-aware trend queries and fallbacks.
4. Update frontend rendering and refresh behavior.
5. Run focused backend, frontend, integration, and browser visual verification before committing the implementation.
