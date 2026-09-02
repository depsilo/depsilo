# Package rules

Package allow/deny rules are interpreted by the selected package ecosystem.
Depsilo validates and normalizes the complete rule when it is created or
updated; request handling never falls back to a shared, best-effort version
comparator.

## Supported selectors

A package selector can be an exact name, `*`, or a single trailing prefix
wildcard such as `org.apache.*`. A version selector can be `*`, an exact
version, or—where the table permits it—one comparison using `<`, `<=`, `>`, or
`>=`. npm follows that same deliberately small selector grammar: Node-style
compound ranges such as `^1.2.3`, `~1.2`, `1.x`, or `>=1 <2` are rejected.

| Ecosystem | Package-name identity | Version rules |
| --- | --- | --- |
| PyPI | Lowercase; runs of `.`, `_`, and `-` normalize to `-` | PEP 440, including dev/pre/final/post releases and epoch; ranges supported |
| Cargo | Case-sensitive manifest package name | Strict SemVer (`major.minor.patch`); each release component is at most `uint64` (`18446744073709551615`); ranges supported |
| Go | Case-sensitive module path using the Go proxy's official escaping rules | Package-wide and exact-version rules only |
| Maven | Case-sensitive `groupId:artifactId` | Exact versions only; `ComparableVersion` equivalence is used, but ordered ranges are rejected |
| NuGet | Lowercase name | Package-wide and exact-version rules only |
| Conda | `channel/path/name`; every channel-path segment is case-sensitive and only the final package name normalizes to lowercase | Package-wide and exact-version rules only |
| CRAN | Case-sensitive package name | Package-wide and exact-version rules only |
| Alpine | Case-sensitive `branch/repository/architecture/name` coordinate | Package-wide and exact-version rules only |
| APT | Lowercase-only Debian package name | Package-wide rules only until complete versions can be derived from repository indexes |
| npm | Case-sensitive package identity, including historical uppercase names; scoped `@scope/name` coordinates are preserved | Strict SemVer exact versions and one `<`, `<=`, `>`, or `>=` comparison; enforced on authenticated `dist.tarball` URLs |
| Composer | ASCII `vendor/package` coordinate normalized to lowercase | Package-wide rules only because dist URLs may expose a normalized version or reference fallback rather than the declared pretty version |
| RubyGems, Helm | Artifact filenames do not expose an unambiguous package/version boundary | No Package Rule support |
| Docker | OCI registry/image/tag-or-digest identity | No Package Rule support; policy remains owned by the separate OCI control plane |
| Hugging Face | Hub repository identity | No Package Rule support; repository policy remains on the quarantine surface |

## Automatic OSV scan identities

Automatic vulnerability scans have a separate identity requirement from
Package Rules. Depsilo records an OSV vulnerability check—including a clean
zero-result check—only when the cached package name is authoritative enough to
query the intended package.

| Ecosystem | Automatic OSV scan | Identity status |
| --- | --- | --- |
| PyPI | Enabled for trusted identities | Simple-index keys and strict PEP 427 wheel or PEP 625 sdist filenames are normalized; other artifact filenames stay unidentified and are skipped |
| APT | Disabled | A `.deb` or `.udeb` identifies a binary package, while Debian OSV records use the source package; repository metadata keys identify neither one package nor that mapping |
| npm, Go, Maven | Enabled | Cache identity is accepted only from each adapter's authoritative route or key shape and then ecosystem-normalized |
| Composer | Enabled for trusted identities | Only exact `p2/<vendor>/<package>[~dev].json` metadata and reversible `dist/<vendor>/<package>/<reference>.<type>` cache keys establish `vendor/package`; `packages.json`, malformed `p2` paths, indexes, and arbitrary passthrough paths stay unidentified and are skipped |
| Cargo | Enabled for trusted artifact identities | Only exact `cargo/crates/<name>/<version>.crate` keys are scanned; sparse-index and other metadata keys stay unidentified and are skipped |
| CRAN | Enabled for trusted artifact identities | Only strict source, archive, Windows-binary, and macOS-binary artifact paths are scanned; `PACKAGES` indexes, installers, and ambiguous paths stay unidentified and are skipped |
| NuGet | Disabled | Flat-container paths lowercase the registry's canonical package ID |
| RubyGems | Disabled | Platform artifact filenames do not expose a reversible name/version/platform boundary |
| Conda, Alpine, Helm, Docker, Hugging Face | Disabled | No configured OSV ecosystem mapping |

For Cargo, an ordered comparator admits a prerelease candidate only
when its target is also a prerelease of the same `major.minor.patch` release.
For example, `>= 1.0.0` does not match `1.1.0-alpha`. Numeric prerelease
identifiers are compared as arbitrary-precision decimal values rather than
machine integers. SemVer build metadata does not change precedence, so an exact
`1.0.0+build.1` rule also matches `1.0.0+build.2`.

Bare exact selectors use each dialect's version-precedence equality; they are
not aliases for additional range syntax such as PEP 440's `==` operator. Thus
PyPI `1.0` also matches `1.0.0`, while `1.0+local` remains a distinct exact
version. Operators other than `<`, `<=`, `>`, and `>=` are rejected.

Cargo artifact download URLs carry the case-sensitive crate name and are fully
enforced. Sparse-index filenames are lowercase, so their URL alone cannot
identify the original case-sensitive name; Depsilo skips package-rule
evaluation and automatic OSV scanning for that metadata request instead of
guessing. Only an exact `cargo/crates/<name>/<nonempty-version>.crate` cache key
establishes an automatic-scan identity. The subsequent artifact request is
still evaluated.

Cargo package identities follow the manifest grammar: Unicode alphanumeric
characters plus `-` and `_`, in any position, with no case folding. Depsilo
does not impose `cargo new`'s Rust-identifier checks or crates.io's stricter
ASCII publication policy on packages served by a configured private registry.

For Conda artifacts, every path segment before the architecture and filename is
part of the channel identity. For example, `pkgs/main/numpy` and
`pkgs/r/r-base` retain distinct `pkgs/main` and `pkgs/r` channel paths; they are
not collapsed to a shared `pkgs` channel.

CRAN source and binary artifacts carry an unambiguous `name_version` filename
only on the public `src/contrib`, `src/contrib/Archive/<package>`,
`bin/windows/contrib/<r>`, and `bin/macosx/.../contrib/<r>` route shapes. The
archive directory must name the same package as the artifact. Those strict
paths support exact-version rules and automatic scans; `PACKAGES`, compressed
indexes, R installers, and package-like filenames on any other path are skipped.
Maven artifact paths likewise carry an enforceable coordinate and directory
version regardless of packaging extension. Depsilo skips metadata and local
repository marker files such as `maven-metadata.xml`, `.lastUpdated`, and
`_remote.repositories`, whose paths do not establish one safe artifact target.

npm package identity is also case-sensitive. Historical registry entries such
as `Express` and `express` can denote different packages, so Depsilo validates
the legacy-compatible package-name grammar and never folds one into the other.
That grammar accepts npm's URL-safe legacy component characters
`A-Z a-z 0-9 . _ ~ ! ( ) * ' -`. Unscoped names cannot start with `.`, `_`,
or `-`, and the reserved names `node_modules` and `favicon.ico` are rejected
case-insensitively. Scoped names must be exactly `@scope/name`; the package
part cannot start with `.`, while legacy names such as `@scope/_pkg` remain
valid. Depsilo retains a 214-character maximum for the complete npm package
name, including `@scope/`. Within a package-rule selector, `*` is reserved for
the complete wildcard or one trailing prefix wildcard; it is not interpreted as
a literal npm package-name character.

A packument's `dist.tarball` may point to an arbitrarily named archive, so
Depsilo never reconstructs a release from the filename. Runtime metadata
instead emits an encrypted, HMAC-authenticated internal URL. The authoritative
release version is the validated key of the packument's `versions` map; it is
not inferred from a `dist` field or archive filename. Before metadata is
cached, the npm adapter validates every such key as strict SemVer and replaces
each tarball URL with an unsigned prepared reference. A fresh response-time
token binds all of the following provenance:

- the exact, case-sensitive package name and validated `versions` map key;
- the route audience (`global` or the exact project slug), so a project URL
  cannot be replayed on the global route or for another project;
- the stable ID of the selected metadata Upstream source;
- the exact resolved `dist.tarball` target, including its encoded path and
  query string, plus the filename derived from that target; and
- the packument's declared `integrity` and `shasum` values when present.

The artifact endpoint verifies that token and resolves the recorded source by
its stable ID before evaluating Package Rules with the authenticated package
and version. A deny returns `403` before quarantine, cache access, or Upstream
network traffic; an integrity/evaluation failure returns `503`. A transient
rule-store failure follows `[policy] on_load_error`: the default uses the
last-known-good snapshot, while an explicit `allow` or `deny` mode applies its
configured no-snapshot decision. Allowed requests then enter
quarantine and malicious-package checks and fetch the recorded target through
that exact source. The Upstream selector is not called again and cannot fail
over to a different registry. Artifact cache identity is likewise bound to
the source ID, exact target, and declared digest fields, so equal-looking paths
from different sources or targets do not share an object. Binding
`integrity`/`shasum` authenticates what the packument declared and separates
the cache namespace; it is not a claim that Depsilo independently re-hashes
the downloaded payload. Unsigned/direct tarball URLs, including legacy npm
routes, return `404` without contacting an Upstream or evaluating a guessed
version.

Each token uses a fresh random nonce with authenticated encryption and an
independently derived outer MAC; normal request logs redact the bearer-token
path segment. With a persisted `auth.jwt_secret`, the signing root is
domain-separated from JWT signing, so a package-lock URL remains valid across
restarts with the same deployment secret; rotating that secret invalidates
existing signed URLs. The exact `change-me-in-production` placeholder is
accepted only for loopback development. In that mode artifact URLs use
process-local random signing keys and therefore become invalid after every
restart. A non-loopback listener rejects an empty, placeholder,
whitespace-padded, or shorter-than-32-byte root secret instead of producing a
forgeable artifact key. The global Package Rules middleware intentionally skips
all npm routes: metadata remains discoverable, and an opaque signed route cannot
be safely interpreted there. Instead, the npm adapter receives a narrow
immutable rule-checking capability through its request scope and evaluates
exact or single-comparator rules only after authenticating the signed
provenance.

Quarantine allow-list globs use the same concrete identity validation without
passing glob metacharacters through the concrete-name parser. Hugging Face
quarantine identities accept `repo`, `owner/repo`, `datasets/repo`, and
`datasets/owner/repo`; the `datasets/` transport namespace is retained and the
complete policy key normalizes to lowercase. The Hub `repo_id` beneath that
namespace is limited to 96 characters, cannot start or end with `-` or `.`,
cannot contain `--` or `..`, and its repository name cannot end in `.git`. A
single-segment identity named `datasets` remains a model repository, not the
dataset namespace. Docker remote names follow the distribution lowercase path
grammar; uppercase or malformed names are rejected rather than silently
lowercased. Alpine glob coordinates retain case, and Conda globs retain every
channel-path segment's case while normalizing only the final package-name
segment.

An ecosystem wildcard is deliberately narrower than a package wildcard. The
only accepted cross-ecosystem rule is `* / * / *`, which covers every ecosystem
listed in the Package Rule capability table above. It does not cover proxy
surfaces where Package Rules are unavailable (currently Docker, Hugging Face,
RubyGems, and Helm). Select a concrete ecosystem for every package-specific or
version-specific rule so its identity and version semantics are unambiguous.

## Request-path limitations

Dialect comparison is only one half of enforceability. Depsilo exposes a
version selector only when the real proxy request also carries an authoritative
package and complete version. A comparator passing conformance tests does not
make a guessed filename identity safe.

### APT

The APT dialect and its conformance tests correctly compare complete Debian
versions such as `1:1.0-1`. Debian `.deb` filenames omit the epoch, however, so
a download URL does not contain a complete version. Depsilo does not guess that
a missing epoch is zero.

Until an index-derived `Filename` to `Version` mapping is available, APT proxy
downloads reliably enforce package-wide rules only. Creation, update, and
migration therefore reject both exact-version and range selectors for APT.
Use a package-wide APT deny rule when enforcement is required.

Automatic APT OSV scanning is also safety-disabled. Debian advisories use the
source package identity, which is not necessarily the binary name in a `.deb`
or `.udeb` filename. Repository selectors and `Packages`/`Sources` metadata
cache keys are not treated as package identities. Scanning can be re-enabled
only after authenticated index data binds each served `Filename` and binary
package to its authoritative source package.

### npm

npm metadata remains discoverable even when a package-wide deny exists. The
global middleware defers every npm route because an artifact filename cannot
prove the selected packument release. End Users must fetch the authenticated
`dist.tarball` URL returned by metadata; the npm adapter then evaluates
package-wide, strict SemVer exact, and one-comparator (`<`, `<=`, `>`, or `>=`)
rules against the signed `versions` map key. A more-specific version rule can
therefore override a package-wide rule. Node-style compound ranges are outside
this grammar, and legacy hand-constructed tarball URLs are rejected.

### Composer

Composer package routes establish the normalized `vendor/package` identity,
so package-wide rules remain enforceable. Dist URLs are not authoritative for
the operator-visible version: they may contain `version_normalized`, or a
reference fallback when no version token is present. Creation, update, and
migration therefore reject every Composer version selector.

Automatic OSV scans use the same strict identity boundary. A per-package p2
metadata route must be exactly `p2/<vendor>/<package>.json` (or the Composer
`~dev` variant), and a cached dist mirror key must retain exactly the two
coordinate segments plus a non-empty artifact reference and type. Global
`packages.json`, incomplete coordinates such as `p2/not-a-package`, indexes,
and arbitrary passthrough paths are cacheable but never receive a package name
or a clean OSV receipt. Existing rows from a release that guessed a p2 name
must be re-derived from these shapes or cleared during the next schema
migration before Composer scanning resumes.

### PyPI

Range enforcement is limited to artifact paths whose identity follows a
strict wheel filename or PEP 625 `.tar.gz` sdist shape. Legacy multi-hyphen
sdists and alternate archive formats cannot be split safely by searching for a
digit-like suffix; they are never assigned a guessed name or version.

For an artifact with incomplete identity, Depsilo first derives the
unconditional fallback from the highest-specificity PyPI-wide or global
wildcard rule; without one, the fallback is allow. The fallback is applied when
every potentially matching specific rule has the same action. Evaluation fails
closed with HTTP `503` and code `PACKAGE_POLICY_UNEVALUABLE` only when a more
specific rule could change that result. Consequently, a wildcard deny returns
`403`, while an opposing specific override makes the artifact unevaluable.
This behavior is identical for the built-in PyPI route, configured preset
channels, and configured extra PyPI-compatible routes. PEP 658 sidecar files
ending in `.metadata` are metadata rather than artifacts and do not enter this
ambiguity path.

The automatic OSV scanner uses the same strict PyPI artifact identity seam.
Package simple-index cache rows remain trustworthy scan sources. A strict wheel
or PEP 625 sdist can also trigger a scan, but legacy sdists, `.zip` archives,
alternate formats, malformed filenames, and sidecars receive an empty cached
package identity. They therefore neither enqueue an immediate scan nor
participate in a periodic cache scan, and can never produce a guessed clean
check. Schema v3 recomputes PyPI cache identities from their keys, clears these
ambiguous legacy artifact identities, and invalidates all old PyPI vulnerability
checks before trusted rows are scanned again.

### RubyGems and Helm

RubyGems platform artifacts use filenames such as
`nokogiri-1.16.5-x86_64-linux.gem`. Versions and platforms may both contain
dashes, and gem names may also contain dashes, so the request path does not
provide a reliable package/version/platform boundary. Helm chart filenames
have the same package/version boundary problem: both chart names and
prerelease versions may contain dashes.

Depsilo does not expose either ecosystem in the Package Rule form or API, and
does not synthesize a quarantine identity from those ambiguous artifact
filenames. Reliable enforcement requires index or gemspec-derived provenance.
For the same reason, automatic RubyGems vulnerability scans are safety-disabled:
neither an artifact-triggered background scan nor a periodic cache scan records
a filename-derived identity as clean. Existing pre-v3 RubyGems vulnerability
checks, advisories, dismissals, and project-package identities are invalidated
during the schema-v3 upgrade; cached gem bytes remain available.
Docker is also outside the Package Rule seam; its registry/image/tag-or-digest
identity remains owned by the separate OCI control plane.
Hugging Face repository identities remain on the quarantine surface and are
not selectable as Package Rules.

## Validation and runtime safety

Rules retain both the operator-entered values and the normalized values used by
the evaluator. Creates and updates validate the entire resulting rule before
one atomic database write. Unsupported ranges and malformed versions return a
client error instead of being guessed at request time.

### Deterministic winner selection

When more than one rule matches, the Engine compares an explicit lexicographic
tuple, from left to right. Higher values win at the first differing component:

```text
(priority, ecosystem, package, version, action, id)
```

The current schema has no operator-facing priority column, so `priority` is a
reserved extension point and is `0` for every persisted rule. Selector ranks
are exact ecosystem `2` > wildcard ecosystem `1`; exact package `2` > trailing
prefix `1` > wildcard `0`; and exact version `2` > range `1` > wildcard `0`.
At equal selector specificity, `deny` wins (`action=1`), and the higher
database ID wins the final stable tie-break. The ID rule preserves the
historical newest-row behavior without depending on query order.

The Store's `created_at DESC, id DESC` ordering is useful for presentation and
snapshot loading only; it is not policy semantics. Reordering rows cannot
change the decision.

The authenticated `POST /api/v1/admin/rules/test` endpoint evaluates one
`ecosystem`/`package`/`version` coordinate and returns the decision, the
winning rule, every matching candidate in winner-first order, each candidate's
`match_levels` (`exact`, `prefix`, `range`, or `wildcard`), its numeric
specificity tuple, and `precedence_reason`. `reason` remains the operator's
business/audit reason; `precedence_reason` explains which tuple dimension made
the winner prevail. A request with no matching rule returns `candidates: []`,
`precedence_reason: "default_allow"`, and the existing default-allow decision.

If persisted raw and normalized values disagree, or a matching request version
cannot be interpreted by its ecosystem dialect, the package request fails
closed with HTTP `503` and code `PACKAGE_POLICY_UNEVALUABLE`. A transient rules
database read failure follows the explicit `[policy] on_load_error` policy. By
default, `use_stale_then_allow` keeps evaluating the last successful immutable
snapshot, so a temporary storage outage does not become a package outage;
`use_stale_then_deny`, `allow`, and `deny` provide the other documented
fallback choices. If no snapshot has ever loaded, the two
`use_stale_then_*` modes select their final allow/deny behavior. Semantic,
malformed-row, and integrity errors never use these availability fallbacks and
fail closed.

## Policy refresh failures and observability

The evaluator keeps a last-known-good compiled snapshot after its 30-second
freshness window. A successful database read atomically publishes a new
snapshot and clears the degraded state. When a refresh cannot reach the rule
store, a `use_stale_then_*` mode continues to evaluate that old snapshot,
marks policy `degraded`, and records the snapshot age; it never silently turns
the rule engine off. A process with no successful snapshot uses the configured
final allow/deny choice and reports an `unavailable` policy status (with the
degraded metric set) until a read succeeds.

Set the behavior explicitly in `config.toml`:

```toml
[policy]
on_load_error = "use_stale_then_allow" # use_stale_then_allow | use_stale_then_deny | allow | deny
```

Each refresh failure is logged with the error, selected mode, and snapshot age.
Prometheus exposes `depsilo_policy_snapshot_loaded_timestamp`,
`depsilo_policy_snapshot_age_seconds`, `depsilo_policy_refresh_failures_total`,
and `depsilo_policy_degraded`. The authenticated
`GET /api/v1/admin/policy/status` endpoint returns the same state for operators;
the Admin shell displays a warning such as “Policy rules are using a stale
snapshot. Last successful refresh: 12 minutes ago.” The public `/ready`
response keeps policy degradation informational (the service can remain ready)
and includes `policy.status` plus `policy.using_stale_snapshot` when the
policy provider is present.

## Upgrading existing databases

Schema v3 deliberately does not guess what a legacy package-specific or
version-specific selector meant. The old evaluator case-folded every package
name and exact version and discarded ecosystem qualifiers in ordered
comparisons, so those rows have no uniquely recoverable dialect-v1 meaning.
Only rules whose package and version are both `*` and whose ecosystem still
supports Package Rules are eligible for automatic migration, together with a
fully global `* / * / *` rule. Every legacy row with a concrete package or
version selector must be reviewed and deleted under the previous release.
This includes v0.9.1 auto-block rows: `created_by = security-scanner`, the
generated reason text, and an advisory association are not collision-free
machine provenance because `security-scanner` was also a valid human username.
The migration therefore never deletes a Package Rule based on those fields.
Ecosystem-wide RubyGems, Helm, Docker, and other now-unavailable ecosystem rows
must also be deleted; migration will not silently reinterpret them as global
rules.

After validating the remaining `* / *` rows, the migration disables stored
automatic-block policies and backfills normalized values in one transaction.
If it finds any package/version-specific row or another invalid field such as
an unsupported action, the data changes and schema-version record are rolled
back together. Startup reports every offending rule ID. A schema-v2 database
remains at schema v2, so the previous release can still open it.

Before upgrading:

1. Back up the SQLite database and record the intent of every existing package
   rule.
2. While still running the previous Depsilo release, review and delete every
   rule whose package or version is not `*`, including v0.9.1 automatic-block
   rows shown as created by `security-scanner`. Also delete every
   ecosystem-wide rule for an ecosystem no longer listed in the
   supported-selector table. Do not edit it into a supposedly equivalent
   legacy selector.
3. Stop the previous release, upgrade, and let schema v3 migrate the remaining
   ecosystem-wide or global rules.
4. Recreate the reviewed package/version-specific rules through the current
   Admin UI or API. The new dialect may reject an old spelling or selector that
   cannot be enforced safely.

Do not manually populate the normalized columns. Creates and updates prepare
the raw and normalized values atomically under the current dialect revision.

Schema v3 also canonicalizes permanent quarantine approvals with the selected
ecosystem dialect and collapses equivalent aliases to the newest operator
decision. An invalid legacy approval aborts the migration atomically with its
row ID; revoke that record under the prior release instead of guessing its
meaning. Schema v3 re-derives PyPI, Go, Cargo, CRAN, and Maven cache package
identities from authoritative keys and clears identities on metadata or
ambiguous keys. npm rows from the old case-folded cache namespace cannot seed a
scan. APT cache package identities are cleared because a binary filename does
not prove Debian's source-package identity; NuGet cache identities are cleared
because the transport spelling cannot recover registry casing.

Legacy vulnerability tables do not record whether an advisory came from an
automatic fetch or an operator import. Schema v3 therefore invalidates every
stored advisory, check, dismissal, and project-package row for npm, PyPI, APT,
Go, Cargo, Maven, NuGet, CRAN, RubyGems, and Composer rather than risk
attaching a trusted decision to the wrong package. Composer cache identities
are re-derived from the strict p2/dist route shapes and ambiguous rows are
cleared. Re-import reviewed advisory files after the upgrade; trusted cache
identities will be scanned again. Cached package objects remain available.
Hugging Face cache metadata and ref pins are invalidated so legacy case aliases
cannot retain stale private bytes.

### npm identity data

Schema v3 also makes npm identity exact-case end to end. New npm metadata and
artifact cache keys use the `npm-exact-v1/` namespace, so cache entries written
under the old case-folded namespace are never read by the npm adapter and can
age out through normal retention.

Depsilo also deliberately does not reuse v0.9 npm metadata or artifact cache
entries. Those entries contain unsigned internal tarball URLs and no proof of
which selected Upstream supplied the packument, exact target, or digest
declarations. Reinterpreting them would reintroduce cross-source selection and
cache-collision risks. After upgrading, the Upstream must therefore be online
for Depsilo to fetch fresh metadata, prepare source-bound references, and emit
new signed URLs; artifacts are then fetched into the new provenance-bound
cache namespace. Previously emitted unsigned URLs return `404`. If the
original source recorded by a new signed URL is no longer configured or
available, the request fails closed rather than falling back to another source.

The original spelling cannot be recovered from existing npm malware data.
During the same transaction, schema v3 deletes legacy npm malicious-package
rows and npm malware overrides. It also removes rows and overrides for datasets
that are no longer enforced end to end. Recoverable Cargo, Composer, NuGet, Go,
and Maven rows are dialect-normalized and deduplicated; malformed version data
for an otherwise valid identity remains an all-version block until resync. The
migration clears the blocklist's last-success marker, records the retained row
count, and requests a full source resync. Review and recreate any required npm
override after confirming the package's exact spelling; overrides are not
reconstructed automatically.

## Vulnerability auto-block policies

Automatic vulnerability blocking is safety-disabled for every ecosystem. An
OSV affected set is the union of its ordered ranges and explicitly listed
versions, and its range timeline uses version precedence directly. Package Rule
comparators instead follow each package manager's requirement semantics. For
example, Cargo comparators exclude unmentioned prereleases, and PEP 440 `<`
comparators exclude some prereleases of a final target. Reducing an OSV
`introduced: "0"` / `fixed` interval to `< fixed` can therefore leave affected
prereleases unblocked; disjoint events, `last_affected`, `limit`, and explicit
version entries introduce additional shapes that the current selector grammar
cannot represent losslessly.

The scanner continues to retain advisory and vulnerability-check data, but it
does not create Package Rules from that data. Use any reviewed manual selector
permitted by the capability table, including single comparators for PyPI,
Cargo, and npm. If an older database contains an enabled automatic-block
policy, the Admin UI permits only turning it off. Re-enabling requires a
separate OSV interval policy seam rather than reusing Operator-authored
dependency comparator semantics.
