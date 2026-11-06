# Dependency-set preparation

Depsilo can retain a dependency-set draft and verify its declared content,
independently of the ordinary package and compiler caches. This is an opt-in
foundation for [offline delivery](specs/2026-10-26-python-offline-distribution.md),
not an offline repository: there is no bundle export/import, signature trust,
dependency resolver, installation endpoint, or Admin UI for it yet.

The current draft format accepts PyPI wheels and wheel metadata for an explicit
Linux/amd64 Python target. The target and source URL are operator declarations;
Depsilo does not execute the image, fetch the URL, or certify wheel compatibility.

## Enable retained storage

Add to the instance configuration and restart:

```toml
[distribution]
enabled = true
max_size_bytes = 21474836480
max_object_bytes = 1073741824

[distribution.storage]
type = "local"
path = "/srv/depsilo/data/distribution"
```

Use a dedicated directory outside the package and compiler cache roots. Equal,
nested, and overlapping roots are rejected, including existing symlink aliases.
Local directories and files use private permissions. Relative paths, if used,
are relative to the service working directory. S3 uses the same storage fields
as the package cache but requires a bucket name different from both caches,
even with different endpoints (aliases may identify the same service). A prefix
alone does not isolate capacity or cache cleanup. See
[config.example.toml](../config.example.toml).

Limits count retained storage, not estimated savings. A full store rejects new
bytes; it does not evict referenced content. Deleting a draft and reclaiming its
bytes are separate operations. Disabling the feature preserves existing data.
The default remains disabled; authenticated distribution requests then return
`503 DISTRIBUTION_DISABLED`.

Do not configure external lifecycle deletion on referenced S3 content. The quota
counts currently visible objects, not noncurrent versions or incomplete uploads;
use a bucket version/lifecycle policy that accounts for those separately.

## Prepare a draft

Obtain wheels with a native package manager in your declared target environment.
Keep a private preparation directory with this structure:

```text
prepared/
  manifest.json
  objects/
    <lowercase SHA-256 of the first file>
    <lowercase SHA-256 of the next file>
```

Object files contain the original wheel or metadata bytes. Their filenames in
`objects/` are their actual SHA-256 digests. Symlinks and nonregular files are
rejected by the CLI. The manifest records each original filename separately.

`manifest.json` is strict JSON with these fields:

| Field | Meaning |
| --- | --- |
| `format` | `depsilo/dependency-set-draft/v1` |
| `name`, `revision` | Immutable logical identity; lowercase letters, digits, `.`, `_`, `-`; maximum 128 characters |
| `ecosystem` | `pypi` |
| `target` | `os: linux`, `architecture: amd64`, explicit `python_version`, `python_abi`, `pip_version`, and `client_image` pinned with `@sha256:` |
| `objects` | Nonempty array, maximum 4,096 entries |
| Each object | `sha256`, positive byte `size`, `kind` (`wheel` or `metadata`), original `filename`, `package`, `version` |
| Optional `source_url` | Operator-declared HTTPS URL without credentials, query, or fragment; never fetched by this workflow |

The manifest is limited to 1 MiB. Unknown fields, duplicate fields, duplicate
object digests, and duplicate filenames are rejected. Wheel filenames end in
`.whl`; metadata filenames end in `.whl.metadata`. This format is a preparation
record, not the planned signed transfer-bundle format.

The set ID is the SHA-256 of the **exact manifest bytes**, including whitespace.
Reusing the same `name` and `revision` with changed bytes returns a conflict;
use a new revision when the declaration changes. Preserve the original manifest
when retrying. An object's digest can be shared across different sets.

## Upload and verify

Use a valid administrator credential through the existing CLI token mechanism:
`DEPSILO_TOKEN` or the protected `~/.config/depsilo/token` file. Set `DEPSILO_URL`
or pass `--url` for the intended instance. Keep credentials out of shell history
and checked-in files.

```bash
depsilo distribution stage --manifest prepared/manifest.json --objects prepared/objects --json
depsilo distribution list --json
depsilo distribution inspect <set-id> --json
depsilo distribution verify <set-id> --json
```

`stage` validates local file declarations, stages the manifest, streams every
object, and rechecks the complete set. The server validates the actual byte
length and SHA-256 before publishing content. Retry the same command after an
interruption: the declaration is idempotent, and valid existing objects are
reused. The current CLI retransmits them for input verification; this is not a
missing-object incremental transfer protocol.

Interpret the returned states separately:

| State | Meaning |
| --- | --- |
| `staged` | No complete-set verification has finished |
| `incomplete` | The last completed verification found missing or invalid content |
| `content_verified` | All declared objects matched length and SHA-256 at `checked_at` / `verified_at` |
| `provenance_status: operator_declared` | Source identity has not been independently authenticated |
| `install_validation: not_run` | No native installation or dependency-closure check has run |
| `serving: false` | This catalog is not exposed as a package repository |

`inspect` and `list` show the last completed check, not continuous storage health.
Run `verify` again after restoration or suspected corruption. Verification
reports `missing`, `size_mismatch`, or `hash_mismatch` for affected digests.
`stage` and `verify` return a nonzero CLI exit status if content is incomplete;
permission, transport, and storage errors also fail the command. Successful hash
verification alone does not prove provenance, safety, closure, or installation.

## Delete, reclaim, and recover

```bash
depsilo distribution delete <set-id>
depsilo distribution prune
```

Deletion removes the declaration and its references. `prune` explicitly removes
unreferenced owned content and abandoned local staging files; content referenced
by any remaining draft is retained. It can be retried after an interruption.
Neither action deletes package-cache or compiler-cache entries.

The catalog is in the instance SQLite database (schema 13); object bytes are in
the dedicated directory/bucket. Back up both while writes are stopped. The
existing `depsilo backup` includes database metadata but **does not include these
retained object bytes**. Restore the matching content separately, then verify.
Do not run an older binary against the migrated database; rollback requires its
matching pre-upgrade database and configuration backup.

Process interruption is covered by atomic object publication and transactional
catalog updates. This does not promise filesystem durability across power loss.
The internal active-version pointer is reverified before switching and reading,
but is deliberately unavailable through CLI/HTTP until offline serving and
policy checks exist. It does not change existing Snapshot behavior.

## Admin API

All routes are under `/api/v1/admin/distribution`. Reads use existing Admin read
permissions; mutations additionally require the `admin` role. Anonymous access
is denied. There are no public package-download routes for this catalog.

| Method and path | Action |
| --- | --- |
| `GET /sets?page=1&per_page=20` | Paginated summaries and storage usage |
| `GET /sets/:id` | Declaration and last verification status |
| `POST /sets` | Stage raw manifest JSON |
| `PUT /sets/:id/objects/:sha256` | Stream raw declared content, including chunked uploads |
| `POST /sets/:id/verify` | Re-read and verify every declared object |
| `DELETE /sets/:id` | Remove catalog references |
| `POST /prune` | Reclaim unreferenced owned bytes |

The server records successful mutations in the existing audit stream. Request
bodies, credentials, and local storage paths are not included in these entries.
