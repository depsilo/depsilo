# Freeze / Golden Snapshot (MVP)

> Design snapshot, 2026-10-06. Implements [ADR-0005](../adr/0005-enforcement-first-until-1-0.md)
> build order item 4. `PRODUCT.md` and the ADRs remain the authority for
> product intent and decisions.

## Goal

Serve a reproducible, approved artifact set: promote the current cache into a
named snapshot, export/import its manifest, and — when snapshot-only mode is
active — refuse every artifact that is not pinned by that snapshot.

## Data

Snapshots are built from `tamper_record`, so every item carries the artifact
identity (`ecosystem`, `package`, `version`), the cache key, the first-seen
SHA-256, and the size. Artifacts that were never fetched through this proxy
(metadata-only cache entries, or artifacts fetched while tamper detection was
off) are not part of any snapshot; promotion refuses an empty set instead of
creating a snapshot that would block everything.

Schema v6 adds:

- `snapshots`: name (unique), note, creator, artifact count, total bytes.
- `snapshot_items`: one row per artifact, unique on
  `(snapshot_id, ecosystem, package, version, cache_key)`.

The active snapshot ID lives in `control_plane_state`
(`snapshot_active_id`); the store keeps it in memory and restores it at
startup, clearing it when the snapshot no longer exists.

## Enforcement

The quarantine checker consults a snapshot gate after the malicious blocklist
and before the minimum-release-age policy:

- membership is an indexed lookup on the exact identity tuple;
- a version outside the active snapshot is refused with `451
  SNAPSHOT_BLOCKED` and a `snapshot_blocked` event;
- a membership lookup failure fails closed, because serving would silently
  break the pin;
- metadata/index requests are not gated, so clients still see which versions
  the upstream offers; only artifact downloads are refused.

## Export / import

`depsilo/snapshot/v1` is a JSON manifest with the snapshot metadata and one
entry per artifact (identity, cache key, SHA-256, size). Export streams the
manifest. Import validates the format, cap (200k items), non-empty identity
fields, and 64-hex SHA-256 values, deduplicates identical artifacts, accepts a
name override, and seeds missing tamper baselines from the manifest so a first
fetch whose bytes differ from the manifest raises a tamper alert.

The manifest carries metadata, not artifact bytes. Moving bytes between
instances uses the existing `depsilo backup` / `restore` (SQLite plus storage)
path.

## Admin API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/snapshots` | list + active state |
| POST | `/api/v1/admin/snapshots` | promote the current cache |
| GET | `/api/v1/admin/snapshots/:id` | detail + first page of items |
| GET | `/api/v1/admin/snapshots/:id/export` | download the manifest |
| POST | `/api/v1/admin/snapshots/import?name=` | create from a manifest |
| PUT | `/api/v1/admin/snapshots/active` | activate (`snapshot_id`) or disable (`0`) |
| DELETE | `/api/v1/admin/snapshots/:id` | delete a non-active snapshot |

Snapshot mode is open source (governance primitive), not a Pro feature.
Lifecycle changes are written to the audit stream.

## Out of scope for the MVP

- Byte-level import/export of artifacts (use backup/restore).
- Per-project snapshots and snapshot diffing.
- Automatic pinning of metadata responses.
- Verifying served bytes against the imported manifest at cache-write time;
  the imported hash becomes the tamper baseline, so later changes alert.
