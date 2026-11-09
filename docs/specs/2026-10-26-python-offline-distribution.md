# Python offline dependency distribution: first implementation contract

**Date:** 2026-10-26

**Status:** implementation plan and acceptance fixture. The catalog and the
full signed bundle export/import are implemented on `master` and documented in
[distribution.md](../distribution.md); offline serving, disconnected install,
and incremental transfer are not shipped. The implemented v1 format binds the
exact set manifest instead of the separate target, input, and source-evidence
field groups proposed below; those remain open for a later format version.

**Authority:** [ADR-0008](../adr/0008-dependency-cache-and-distribution.md)

This specification scopes the first complete offline delivery milestone. The
fixture under [`testground/offline-pypi`](../../testground/offline-pypi/README.md)
is implemented independently so subsequent work has a real client acceptance
target. Its existence does not imply an export/import command, durable
dependency set or offline serving mode already exists. The proposed format
below is not yet a public compatibility promise.

## Outcome and bounded support

An operator prepares the dependencies of a declared Python workload on an
internet-connected machine, carries a verified full bundle to an isolated
Depsilo instance, activates it, and installs that workload using native pip
against the isolated instance. The target has no path to a parent or public
repository; ordinary clients continue using a standard Python simple index.

The initial acceptance target is **Linux/amd64, CPython 3.11.15, pip 24.0** from
the existing repository's immutable Python image:

```text
python:3.11-slim@sha256:db3ff2e1800a8581e2c48a27c3995339d47bdf046da21c7627accd3d51053a93
```

The image was inspected on linux/amd64 to establish the exact Python and pip
versions. It supplies the environment and compatible wheel tags; a generic
“Python 3.11” label alone is insufficient. Preparation and validation use this
same target. ARM emulation may qualify behavior, not native amd64 performance.
The fixture uses historical versions as stable test inputs, not production
dependency recommendations.

Inputs are requirements/constraints for named public packages, optional extras
and environment markers. Native pip resolves them in the target environment;
the prepared result records exact versions, wheel hashes and the complete
transitive dependency set. The first milestone accepts **wheels only**. Reject
sdists, source builds, local paths, editable installs, VCS references, direct
URL requirements and undeclared extra indexes explicitly. Installing a wheel
does not guarantee the application can run arbitrary workloads without further
network access or operating-system libraries; those remain outside this scope.

The fixture covers pure Python dependencies, a compiled MarkupSafe wheel,
Requests' SOCKS extra, a Python-version marker and a Windows-only package that
must remain absent. Every selected distribution is version/hash pinned.

## Existing seams and limits

- `internal/cache` already streams local/S3 objects and coordinates downloads.
  Its entries are evictable; expiry and LRU do not promise dependency-set retention.
- `internal/snapshot` exports an approved identity/hash manifest. It does not
  transfer bytes or prove those bytes still exist. Preserve this meaning.
- PyPI indexes are rewritten before caching and contain instance-signed artifact
  references. Index cache keys also depend on the signing key. Copying a cache
  database/directory is not a portable transfer format, even if hostnames change.
- Cascade relays are online cache reuse. Cached artifacts can still involve
  metadata refreshes or redirects; a successful warm-cache installation is not
  durable disconnected installation evidence.

Add a focused `internal/distribution` module owning dependency sets, content
references, transfer verification, inventory and activation. Reuse storage
implementations with a separate root or restricted prefix **and independent
capacity accounting**. Ordinary cache GC must neither delete distribution
objects nor count them as evictable cache space. Defer sharing physical objects
across these two lifecycles until references and accounting can be proven safe.

CLI and authenticated Admin HTTP callers use the same module. Preparation runs
native pip in an operator-controlled matching container, not as an arbitrary
subprocess inside the Depsilo server. The package adapter owns the Python
protocol view and its integration with existing policy checks. No new resolver
is implemented in Go.

## Preparation, transfer and publication

1. **Declare:** record target image/architecture/runtime, roots, constraints,
   selected sources and input digests. Validate supported inputs before execution.
2. **Prepare:** resolve/download through the connected Depsilo using native pip
   with empty client caches and wheel-only selection. Collect the selected
   artifact bytes, exact resolution and source evidence, not a history of
   unrelated requests observed on the instance.
3. **Check completeness:** verify every selected hash and wheel compatibility;
   include all advertised metadata sidecars. Run the clean native-client fixture
   against a locally served candidate set without external access. Persist the
   result with the target/input identity; changing either invalidates that result.
4. **Export:** stream the immutable manifest, its signature and referenced
   content into a full transfer bundle. Never export instance configuration,
   JWT/URL-signing keys, source credentials or unrelated cache objects.
5. **Import and verify:** validate format, trusted signer, target, manifest and
   every object's actual length/hash into unpublished storage. Invalid or missing
   content leaves the previous active dependency set unchanged.
6. **Activate:** recheck the complete object references, then publish the set
   and its active-version pointer in a SQLite transaction. A request resolves
   one immutable set generation; metadata cannot switch midway to a different
   artifact generation.
7. **Use and verify:** serve local index/artifact URLs and run disconnected native
   client acceptance. Display “content verified”, “active” and “client verified”
   separately. Import success alone cannot claim successful application install.

The module records preparation/import failures and retryable tasks. Repeated
imports reuse verified content by digest. Restart recovery reconciles unfinished
tasks and unreferenced staging objects; it must not publish a partly imported
set. Preserve old active references while replacements are incomplete. Test
process interruption explicitly. A local rename alone does not establish
power-loss durability; filesystem synchronization requirements must be verified
before making that stronger claim.

## Proposed v1 bundle and trust contract

Use an uncompressed streaming archive initially. Its bounded manifest and
detached signature appear before content entries, allowing signature/format
checks before writing artifact payloads. Content entries use digest-derived
names, independent of user filenames. The logical manifest contains:

| Field group | Required meaning |
| --- | --- |
| Format | Format version and declared required capabilities |
| Identity | Dependency-set revision, creation time and exact manifest identity |
| Target | OS/architecture, Python/ABI, client image digest and pip version |
| Inputs | Root/constraint digests, resolved names, versions, extras and markers |
| Artifacts | Filename, wheel compatibility, length and SHA-256 |
| Source evidence | Original source identity/URL, metadata digest, applicable package attributes and timestamp provenance |
| Objects | Exact inventory of artifact and required metadata content |
| Baseline | Absent for a full bundle; declared identity/inventory basis for an increment |

Sign the exact manifest bytes with a dedicated Ed25519 transfer key. The target
administrator explicitly installs trusted public keys; key IDs in a bundle do
not confer trust. This trust is separate from Admin login and instance URL
signing. Reject untrusted/revoked keys and unsupported required capabilities.
Unsigned manifests do not become trusted just because their content hashes match.

Set and test limits for manifest bytes, member count, individual/total declared
sizes and available storage before exposing the importer. Reject duplicate
manifest keys, duplicate/archive members, undeclared content, path traversal,
absolute paths, links, devices and inconsistent sizes. Read artifact payloads
incrementally with cancellation and bounded buffers. Never extract arbitrary
archive paths directly into a filesystem or S3 key space. Resource limits and
forward compatibility behavior must be included in importer tests before the
format is released.

A trusted preparer attests what it acquired; this is not a new upstream-native
signature. Preserve original signed bytes wherever applicable and distinguish
preparer-attested provenance from independently signed source information.
Retain the relation between selected artifact, source metadata and timestamp;
an import timestamp is never a release timestamp. Source URLs must have
credentials/query secrets removed or be represented by safe source identities.
Missing evidence remains missing and follows the target's existing policy.

## Offline serving and security behavior

The imported view is a standard simple index scoped to the selected immutable
dependency set. It advertises only included artifacts and valid metadata
sidecars, with correct names, versions, hashes, Requires-Python and yanked
attributes. The target generates its own local references; it never trusts or
reuses another instance's HMAC artifact URLs.

Requests use the target's current blocklist, snapshot membership and release-age
rules as applicable. Integrate verified imported provenance at the existing
policy seam, rather than skipping checks for imported bytes. A rollback selects
old content but cannot relax current policy. Preserve denied-request audit data.

Missing artifact/metadata, wrong target, unavailable storage and invalid baseline
return specific failures. This serving path cannot fetch a parent, public
Upstream or another configured source, including after TTL expiry. The isolated
deployment profile also disables outbound health/synchronization tasks and
reports stale or absent security data accurately. Tests must assert absence of
attempted fetches, not merely eventual network timeouts. Container/network
isolation supplies independent acceptance evidence.

## Incremental milestone, after full transfer passes

An increment includes the complete new dependency-set manifest and only whole
objects missing from the declared baseline. Do not introduce binary patches.

For environments allowing inventory return, export target-confirmed object
presence/integrity with the set identity. A past successful-import receipt or
file modification time is not proof of today's inventory. Before activation,
the target rechecks every referenced object, including reused baseline objects.

For one-way environments, allow an explicitly selected prior delivered revision
as an **assumed source-side baseline**. Record this weaker basis; the receiver
still validates local possession. A missing/incompatible baseline prevents
activation and emits a missing-object report. Where reports cannot leave the
isolated network, provide periodic full bundles. Never silently activate an
incomplete increment or restore external connectivity.

Keep retained versions until explicit deletion. Distribution GC reclaims only
objects unreferenced by retained/pending sets, with concurrent import/activation
protection. Deleting an active version requires selecting another complete
version first. Show durable distribution storage separately from ordinary cache.

## Acceptance gates and implementation order

| Delivery | Required evidence |
| --- | --- |
| Catalog and durable content | Ordinary cache expiry/LRU cannot remove a retained set; local and S3 tests; bounded streaming and capacity accounting |
| Full export/import | Trusted full bundle succeeds; bad signer/hash/length/path/format fails; repeated import and interrupted import preserve previous active state |
| Local Python serving | Correct local index/metadata URLs, wheel compatibility and target policy; no fetch attempted on missing objects or expired metadata |
| Disconnected install | Exact pinned fixture in a new venv/client, both client and server isolated, import/version/native-code checks and `pip check`; target logs show artifact delivery |
| Negative completeness | Missing wheel/advertised sidecar, wrong platform, unsatisfied extra/marker and policy denial fail clearly; empty/warm client caches cannot mask failure |
| Incremental transfer | New/reused byte counts, stale/missing baseline rejection, retry/idempotence, object corruption and old-version selection with current policy |
| Admin experience | Same task lifecycle as CLI; permission checks, progress, cancellation, diagnostics and retained-version storage |

The included runner checks dedicated internal-network topology before/after,
target and client egress probes, exact installed versions, local artifact origin,
fresh client state and native imports. Its result is intentionally labeled
`disconnected-native-install-only`: it cannot by itself prove import trust,
retention, recovery or absence of attempted application egress. Those require
the server-side tests above. Do not report the entire milestone complete from a
successful runner against a static wheel directory or ordinary warm cache.

Implement the catalog, full bundle, offline serving and real-client gates before
incremental transfer or broad Admin work. Keep the current free-project and
security defaults; this specification does not change commercial entitlement.

## Protocol references

- Native pip supports dependency resolution/download and explicit target
  platform options: [pip download](https://pip.pypa.io/en/stable/cli/pip_download/).
- Hash-required installation and machine-readable results provide client
  evidence: [pip install](https://pip.pypa.io/en/stable/cli/pip_install/).
- The target serves the standard Python index contract:
  [Simple Repository API](https://packaging.python.org/en/latest/specifications/simple-repository-api/).
- The fixture uses Docker's external-network restriction:
  [internal networks](https://docs.docker.com/reference/cli/docker/network/create/#network-internal-mode---internal).
