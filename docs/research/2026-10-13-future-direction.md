# Future direction: current status, 2026-10-13

> Repository assessment at `6c40924` (`master`), not a roadmap decision or a
> release qualification. [PRODUCT.md](../../PRODUCT.md) and
> [ADR-0005](../adr/0005-enforcement-first-until-1-0.md) remain authoritative.
> External product comparisons are deliberately excluded because they change
> quickly. This note replaces the intermediate 2026-10-07 research drafts.

## Shipped and in progress

- The latest tag in this checkout is `v0.11.0`. It shipped the minimum-release-
  age gate with different provenance strengths, malicious-package data for the
  eight ecosystems named in ADR-0005, snapshot-only mode, and CRA-mode SBOM
  export. The product has 14 standard package routes plus a separate Docker
  OCI route, not 15 standard ecosystems. See [PRODUCT.md](../../PRODUCT.md),
  the [ecosystem catalog](../../internal/ecosystem/catalog.go), and
  [CHANGELOG.md](../../CHANGELOG.md).
- Current `master` also contains unreleased cascade peering and Admin changes
  (see `Unreleased` in [CHANGELOG.md](../../CHANGELOG.md)). It includes an
  in-process macOS/Linux tray entry and release-build jobs. The tray opens the
  web UI in a browser; it is not a new native desktop UI or a Windows installer.
  Release-workflow presence is not evidence of a published desktop release.

## 1.0 readiness

Against the **six actual exit criteria** in [ADR-0005, section 5](../adr/0005-enforcement-first-until-1-0.md):

| Criterion | Repository status |
| --- | --- |
| 1. Release-age provenance | Implemented where enabled, with explicitly approximate or first-observation modes elsewhere. Composer remains best-effort because its client can fall back to an upstream dist URL. |
| 2. Malicious-package data | All **eight named ecosystems** are covered. Ecosystems outside that list lack the same coverage; they are a documented limitation, **not** an additional ADR-0005 exit requirement. |
| 3. Freeze/snapshot MVP | Shipped in `v0.11.0`; manifest transfer does not transfer artifact bytes. |
| 4. CRA-mode SBOM | Implemented to the ADR's field/signing/preset contract; output is SPDX 2.3 or CycloneDX 1.5. This does not establish legal compliance. |
| 5. Candidate release evidence | **Open.** Checks and release automation exist, but this checkout has no `v1.0.0` candidate run showing the required signed artifacts, S3 contract, upgrade/rollback, and real-client results. |
| 6. Compatibility policy | Published with an explicit window. The [current list](../compatibility.md#current-deprecations) now names the still-working `q` and `search` aliases and says neither has an announced Sunset date. Runtime deprecation signaling should be verified before announcing a removal. |

Therefore, do not call 1.0 qualified until criterion 5 is evidenced on the
candidate and remaining contract concerns are reviewed. The old research's
"five of six met, only one test left" was internally inconsistent; its claim
that ecosystems outside the named eight block criterion 2 was also a misreading.

## Next decisions

1. **Before 1.0:** verify end-to-end release-age semantics on the serving
   upstream and the actual client path. Composer currently reads its `time`
   field, CRAN reads DESCRIPTION `Date/Publication` or Last-Modified, and npm
   needs a full publish-time map for age decisions. Check these paths against
   current upstream metadata and retain explicit best-effort/approximate
   labels. Then record the complete candidate qualification described in the
   [release checklist](../../RELEASE_CHECKLIST.md).
2. **After 1.0, subject to a new decision:** evaluate upstream publisher
   attestations as an enforceable signal, VEX and SBOM interoperability, and
   operator-facing coexistence guidance. These are proposals, not shipped
   features or a committed sequence. Source-bound timestamps are **not**
   cryptographic publisher attestations.
3. **Only with user evidence and a superseding ADR:** reconsider a
   general-purpose artifact repository. Until then, the single-instance
   SQLite plus local/S3 deployment and the existing ecosystem scope stand.

**Regulatory boundary:** EU CRA manufacturer reporting started on
2026-09-11; open-source software stewards' reporting starts on 2027-12-11,
according to the [European Commission's reporting guidance](https://digital-strategy.ec.europa.eu/en/policies/cra-reporting).
Neither the proxy role nor MIT licensing alone decides whether obligations
apply to Depsilo as a product. Do not claim compliance or exemption without a
separate legal assessment.
