# Supply-chain controls

Depsilo applies supported policy decisions on dependency requests that pass
through the proxy. Coverage depends on the artifact identity and provenance
available to each ecosystem; it is not uniform across every install surface.
These governance controls are open source and independent: changing one does
not silently enable or disable another.

## Defaults

These are the defaults for a new instance, before operator configuration or
persisted policy is applied.

| Control | Default | Request behavior |
| --- | --- | --- |
| Known-malicious blocklist | On; `block`; sync every 6 hours | Refuses a matching imported advisory with `451 MALICIOUS_BLOCKED`. Requires a successful dataset sync first. |
| Minimum release age | Off; all thresholds `0` | When enabled for a supported ecosystem, quarantines artifacts younger than its threshold. Default mode is `block`, with `fail_closed = true`. |
| Freeze / golden snapshot | Off; no active snapshot | When activated, refuses artifact versions outside the snapshot with `451 SNAPSHOT_BLOCKED`. |
| Tamper detection | On; alert-only | Records first-seen hashes and alerts on changed immutable artifacts; it is not a download-blocking integrity gate. |
| Package allow / deny rules | No operator rules; unmatched requests allowed | Applies only the selectors and request surfaces supported by the Package Rule matrix. |
| Vulnerability auto-blocking | Safety-disabled for every ecosystem | OSV scan findings do not automatically create Package Rules. Reviewed manual rules remain available. |

A policy refusal means the request reached Depsilo. Cache hits, misses, policy
blocks and upstream errors have distinct outcomes in the Portal, Admin and
request logs; a failed install alone does not identify which occurred.

## Known-malicious blocklist

The blocklist imports OSV `MAL-*` advisories for npm, PyPI, Cargo, RubyGems,
Composer, NuGet, Go and Maven. It accepts explicit affected-version lists and
all-version advisories. Bounded ranges without an explicit version list are
skipped; this is not a promise to enforce every OSV range or cover every
ecosystem.

A fresh instance has no malicious-package coverage until the first successful
sync imports data. Later synchronization failures retain the last good data
for the affected ecosystem. An empty local dataset blocks nothing; a failed
sync does not shut down the proxy. Inspect synchronization status in Admin
before relying on coverage, and configure a reachable mirror or sync proxy
where necessary.

PyPI legacy archives and RubyGems platform gems resolve identity through their
registry indexes. When the dataset covers that ecosystem, an artifact whose
identity cannot be proven is refused rather than guessed. Those upstreams
must therefore serve the corresponding index metadata. PyPI reads the project
index through its cache, so an archive that is already cached stays available
while the upstream is unreachable.

The blocklist has its own `block` / `warn` mode. `warn` serves matching requests
and records `malware_warned`. In block mode, neither the release-age allow-list
nor a quarantine approval bypasses malware: an administrator must create a
reasoned, audited override, which expires after 24 hours.

## Minimum release age

Enable `supply_chain.min_release_age_enabled` and set a positive threshold for
each selected ecosystem. Keep `min_release_age.default = "0"`: with the gate
enabled, a positive default or an unsupported positive ecosystem threshold is
a startup error. Listing an unsupported ecosystem in an acknowledgement array
does not add implementation support.

There are three different timestamp guarantees:

| Capability | Meaning | Operator acknowledgement |
| --- | --- | --- |
| `source_bound` | Registry metadata binds the artifact identity, timestamp and serving upstream. | No additional acknowledgement |
| `approximate` | The serving upstream supplies a weaker timestamp, usually the artifact's `Last-Modified`. | Add the ecosystem to `supply_chain.approximate_sources` |
| `observed` | The clock starts when this instance first observes the exact digest, not when its publisher released it. | Add `docker` to `supply_chain.observation_sources` |

Current ecosystem coverage is:

| Ecosystem | Capability | Timestamp and important limitation |
| --- | --- | --- |
| npm | `source_bound` | Packument `time[version]`, carried in the authenticated tarball reference. |
| PyPI | `source_bound` | PEP 691 JSON simple-index `upload-time` from the upstream declaring the artifact. HTML-only metadata cannot supply this provenance. |
| Composer | `source_bound` | The p2 entry's own `time`; client fallback limits enforcement as described below. |
| NuGet | `source_bound` | v3 registration `published` for the package version; the 1900 unlisted sentinel counts as missing provenance. |
| Cargo | `source_bound` | Sparse-index `pubtime` with the exact version and `cksum` identity. |
| RubyGems | `source_bound` | Compact-index `created_at` and checksum for the exact `.gem`; ambiguous filenames cannot establish provenance. |
| Conda | `approximate` | The exact artifact's `Last-Modified`. |
| CRAN | `approximate` | Current source tarballs use DESCRIPTION `Date/Publication`; archives and binaries use `Last-Modified`. The ecosystem still requires approximate acknowledgement. |
| Maven, Alpine | `approximate` | The exact artifact's `Last-Modified` from the configured upstream. |
| Helm | `approximate` | Identity is resolved from the same upstream's `index.yaml`, then the exact chart supplies `Last-Modified`. |
| Docker | `observed` | Resolve a tag to a digest, record its first observation, and fetch the manifest pinned to that digest. Unseen digests remain quarantined until the threshold passes. |
| Go, APT, Hugging Face | Unsupported | Positive thresholds are rejected while the gate is enabled. |

With an active threshold and the default block/fail-closed settings, missing
provenance is refused, including PyPI HTML-only metadata, missing Cargo times
and Docker registries that omit the required digest. The age policy supports
`warn` mode for observation without refusal; it does not change the independent
blocklist or snapshot mode. First-observation age is not release age, and an
upstream `Last-Modified` header is not proof of the publisher's release time.

Existing configurations with an explicit threshold table and no enable switch
retain their legacy enabled interpretation. Set the switch explicitly when
changing policy. See the [provenance implementation record](specs/2026-10-06-min-release-age-provenance.md#implementation-status)
for the binding details and rollout evidence.

## Composer fallback

Composer can fall back from a refused mirrored distribution to its original
distribution URL after a `451`. Depsilo's gate then provides best-effort
blocking and an audit trail, not guaranteed prevention of that install.
Hard enforcement also requires controlling direct client access to the origin.
Source-bound metadata alone does not prevent a client from bypassing a proxy.

## Package rules and vulnerability findings

Use the [Package Rule capability matrix](package-rules.md#supported-selectors)
as the authority for package names, exact versions, ranges and request-path
limitations. Unsupported surfaces do not acquire guessed identities. The
[policy test endpoint](admin-control-plane.md#package-policy-test) explains the
winning rule without changing policy.

OSV vulnerability scanning has its own [identity coverage](package-rules.md#automatic-osv-scan-identities).
It is distinct from the malicious-package blocklist. Automatic conversion of
OSV affected intervals into allow/deny selectors is
[safety-disabled](package-rules.md#vulnerability-auto-block-policies), because
the supported selector grammar cannot represent those intervals losslessly.

Findings are matched by package, not by the cached version: a listed advisory
may only affect versions this instance never served, so check its affected
ranges before acting. Severity comes from the advisory's CVSS v3 vector, scored
with the CVSS v3.1 base-score formula. Advisories without a v3 vector use
GitHub's qualitative rating when present, without a numeric score (CVSS v4
vectors are not scored); the remainder, such as Go vulnerability database
entries, stay `unknown`. Severity suggestions list scored advisories only.

The periodic scan lists advisory IDs through OSV's batch endpoint and fetches a
package's complete records only when the listing names an advisory that is
new, changed, or stored by an older parser. A failed fetch keeps the stored
records and retries on the next scan. Releases before this behavior stored
every advisory as `unknown` and could replace stored details with bare IDs;
upgrading marks every package for a fresh scan.

## Snapshots and tamper detection

A golden snapshot is a named set built from recorded artifact identities and
first-seen hashes. It can be promoted, exported, imported and explicitly
activated in Admin. Activation enforces membership before release-age checks;
it does not bypass malware decisions. Missing membership or a failed
membership lookup is refused. Metadata remains discoverable.

Snapshot manifests contain metadata and hashes, not artifact bytes. Importing
one does not populate the cache or make every download available offline.
Imported hashes seed tamper baselines; membership enforcement is not a
byte-for-byte verification gate. See the [snapshot format and API](specs/2026-10-06-freeze-snapshot.md#export--import).

Tamper detection compares immutable artifacts against their first-seen SHA-256
when they are naturally fetched again. A background-refresh mismatch keeps
the existing cached copy, rejects its replacement and emits an alert. A cache
miss after eviction can alert but cannot restore evicted bytes. Detection is
alert-only, and a first-seen hash is not independent authenticity proof.

## Configuration and operations

Use [`config.example.toml`](../config.example.toml) for the full schema:
`[supply_chain]` and `[supply_chain.min_release_age]` control age policy;
`[supply_chain.blocklist]` controls malware sync and decisions;
`[supply_chain.tamper_detection]` controls hash recording and alerts.
These file settings load at startup and require a restart to change.

Admin provides package rules, quarantine events and approvals, malware sync
status and overrides, and snapshot lifecycle operations. Their durable state
is separate from the startup configuration; see [Admin authority](admin-control-plane.md#authority).
Review the [threat model](threat-model.md) for accepted limits and
[SIEM audit routing](siem-audit-routing.md) for forwarding decisions and alerts.
