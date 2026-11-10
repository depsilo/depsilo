# ADR-0009: Free Edition Includes Every Feature at a Bounded Scale

**Status:** accepted
**Date:** 2026-11-10
**Amends:** the entitlement consequence of
[ADR-0008](0008-dependency-cache-and-distribution.md): project SBOM export is no
longer Pro, and the commercial packaging of offline delivery is decided here.
**Companion documents:** [PRODUCT.md](../../PRODUCT.md),
[distribution guide](../distribution.md).

## Context

ADR-0008 kept runtime project SBOM export behind Pro and left the packaging of
offline delivery undecided. Signed transfer bundles now exist, and the first
users Depsilo can reach are small companies whose outbound bandwidth is
expensive or limited. They need to evaluate shared caching, SBOM export, and
offline delivery on their own network before a purchase conversation. A
feature paywall stops that evaluation before the value is visible.

The value of offline delivery grows with volume: one team's Python environment
fits in a few hundred megabytes, while routinely delivering many environments
does not. Imports frequently run on isolated instances that cannot reach a
license service.

## Decision

1. The free edition includes every feature. Pro removes scale limits; a limit
   bounds usage and never removes a capability.
2. Without Pro or an active trial:
   - One project, as before. Existing projects and their traffic stay usable
     after an entitlement expires.
   - Project SBOM export, including CRA mode, the technical-file preset, and
     signing, for that project.
   - Transfer bundle export: each bundle up to 500 MiB, and at most 5 GiB of
     exported bundles per UTC calendar month.
   - Imports, caching, cascade, and governance are not metered.
3. An export reserves its exact bundle size in a durable ledger (schema v16)
   before streaming, so concurrent exports cannot together exceed the monthly
   allowance. Only a completely streamed bundle stays counted; an interrupted
   stream, or a reservation left by a stopped process, is discarded. Exports
   made under Pro or a trial are recorded but not metered, so they do not
   consume the free allowance after expiry. The allowance renews at 00:00 UTC
   on the first day of each month.
4. A refused export returns `402` with `EXPORT_BUNDLE_LIMIT` or
   `EXPORT_MONTHLY_LIMIT` before any byte streams. The body states the bundle
   size, limits, used bytes, and renewal time.
   `GET /api/v1/admin/distribution/usage` and `depsilo distribution usage`
   report the current allowance.
5. The limits are product constants, not configuration. Changing them is a
   product decision recorded in an ADR.

## Consequences and alternatives

- Free users can complete an end-to-end offline delivery and SBOM workflow;
  Pro is purchased for more projects and for delivery volume.
- The limits are license terms enforced by a self-hosted instance, not a
  security boundary: an operator who edits the database can change the ledger.
  This is accepted for commercial packaging.
- Rejected: keeping offline delivery behind Pro, because seed users could not
  evaluate it. Rejected: metering imports, because isolated importers cannot be
  expected to validate a license, and the receiving side already depends on an
  export made elsewhere. Rejected: a per-bundle limit alone, because repeated
  full exports would be unbounded. Rejected: counting failed attempts, because
  an interrupted transfer does not deliver a usable bundle.
- Project SBOM export moves to the free edition. Compliance evidence is part of
  the evaluation path, and the one-project limit still bounds it.
