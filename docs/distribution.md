# Dependency-set preparation

Depsilo can retain a dependency-set draft and verify its declared content,
independently of the ordinary package and compiler caches. This is an opt-in
foundation for [offline delivery](specs/2026-10-26-python-offline-distribution.md),
not an offline repository. It can export and import signed full
[transfer bundles](#transfer-bundles), but there is no dependency resolver,
offline package-serving or installation endpoint, incremental transfer, or
Admin UI for it yet.

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

The catalog is in the instance SQLite database (schema 13, with import
provenance added in schema 14); object bytes are in
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

## Transfer bundles

A transfer bundle moves one content-verified set between instances. Export and
import are full transfers: every object is carried, and no baseline or
increment is negotiated. Import never activates or serves the set; it ends in
the same `content_verified` / `serving: false` state as a local `stage`.

### Keys and trust

Transfer signing uses a dedicated Ed25519 key, separate from Admin login, JWT,
URL-signing, and SBOM compliance keys. Generate one on a trusted machine:

```bash
depsilo distribution keygen --out /etc/depsilo/transfer-signing-key.pem
```

The command refuses to replace an existing file, writes it with mode `0600`,
and prints the public key (`ed25519:<base64>`) and its key ID (the SHA-256 of
the raw public key). Configure each instance and restart:

```toml
# Exporting (connected) instance
[distribution]
transfer_signing_key_file = "/etc/depsilo/transfer-signing-key.pem"

# Importing (isolated) instance
[distribution]
trusted_transfer_keys = ["ed25519:..."]
```

Trust is restart-managed configuration. Startup fails on a malformed or
duplicate trusted key or an unreadable signing key. The key ID inside a bundle
only selects a configured key; it never makes a signer trusted. To revoke a
signer, remove its key and restart. Sets already imported keep the record of
the signer that was trusted at import time.

### Export

```bash
depsilo distribution export <set-id> --out python-v1.depsilo-bundle.tar --json
```

The set must be `content_verified`. The server signs a transfer manifest and
streams the bundle with an exact `Content-Length`, verifying each object's
length and SHA-256 again while reading. If content fails mid-stream, the body
ends short. The CLI writes to a private temporary file beside the destination,
checks the archive structure and every object hash, compares the received
length, and publishes the file only if all checks pass. It never replaces an
existing file. Where the destination filesystem lacks hard links, as on
exFAT/FAT32 removable media and many network shares, it publishes by rename
after rechecking that the destination is still absent. The server audits
`dist_export` when streaming begins, because signed material leaves with the
first bytes, and `dist_export_incomplete` if the stream fails. Its `signature_verified: false` output is expected: only the
importing instance makes a trust decision.

#### Export allowance

Without Pro or an active trial, each bundle may be at most 500 MiB, and the
bundles exported in one UTC calendar month may total at most 5 GiB
([ADR-0009](adr/0009-usage-limited-free-edition.md)). The check runs before
any byte streams: a bundle beyond either limit returns `402` with
`EXPORT_BUNDLE_LIMIT` or `EXPORT_MONTHLY_LIMIT`, its size, the limits, the bytes
already used, and the renewal time. Only completely streamed bundles count; an
interrupted download is not charged. Exports made under Pro or a trial are not
metered. Imports are never limited.

```bash
depsilo distribution usage --json
```

reports `used_bytes`, `bundle_limit_bytes`, `monthly_limit_bytes`, and
`resets_at`, or `"unlimited": true` under Pro.

### Import

```bash
depsilo distribution import --bundle python-v1.depsilo-bundle.tar --json
```

The importer authenticates the signature before reading any object, then
checks that `set.json` matches the signed set identity and declarations, and
that the declared size fits the current storage and object limits. Objects are
then streamed into storage through the same length and SHA-256 verification
as an upload. A failed import reports the set identity, object counts, and any
verification issues when they are known (the CLI prints them as `import`), and
is audited as `dist_import_failed`. The set remains `staged` or `incomplete`,
never becomes active, and is not attributed to the signer. Repeat the same
import to resume; objects already verified are reused by digest. Only after
every object arrived, the archive ended exactly, and stored content verified
does the set report `provenance_status: preparer_attested` and a `provenance`
record with the signer key ID. This attests what a trusted preparer declared; it is
not an upstream signature, a release timestamp, or install evidence.

### Format v1

A bundle is an uncompressed tar archive. Its members appear in this exact
order, as regular USTAR files:

| Member | Content |
| --- | --- |
| `transfer.json` | Signed transfer manifest, at most 64 KiB: `format` (`depsilo/transfer-bundle/v1`), `required_capabilities` (must be empty in v1), `kind` (`full`), `set_id`, `name`, `revision`, `set_manifest_size`, `object_count`, `total_bytes`, `created_at` |
| `transfer.sig` | Detached signature, at most 4 KiB: `format` (`depsilo/transfer-signature/v1`), `algorithm` (`ed25519`), `key_id`, base64 `signature` over the exact `transfer.json` bytes |
| `set.json` | The exact dependency-set manifest bytes; their SHA-256 is `set_id` |
| `objects/<sha256>` | One member per declared object, in manifest order, containing the original bytes |

The importer rejects unknown formats, required capabilities, or fields; any
reordered, missing, duplicate, or undeclared member; non-regular entries such
as links, devices, and directories; PAX extended records; size mismatches; a
missing end-of-archive marker; and anything other than zero padding after it.
Bundles never contain instance configuration, credentials, signing keys,
access tokens, or unrelated cache objects. A single object must be smaller
than 8 GiB, the USTAR member limit.

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
| `GET /sets/:id/bundle` | Stream a signed full transfer bundle (`admin` role; metered without Pro) |
| `GET /usage` | This UTC month's export allowance |
| `POST /import` | Stream a transfer bundle as the request body (`application/x-tar`) |

The server records successful mutations in the existing audit stream. Request
bodies, credentials, and local storage paths are not included in these entries.
