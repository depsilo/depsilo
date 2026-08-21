# Documentation map

The documentation is organized by how often it should be read. Start with the
smallest document that answers the current question.

## Start and change the project

| Goal | Read |
| --- | --- |
| Install tools and start a local service | [Development quick start](development/quick-start.md) |
| Understand the runtime and state owners | [Architecture](development/architecture.md) |
| Find the files for a change | [Change map](development/change-map.md) |
| Choose the right tests | [Testing](development/testing.md) |
| Contribute a change | [Contributing](../CONTRIBUTING.md) |

Codex and other coding agents should begin at [AGENTS.md](../AGENTS.md), which
contains the stable repository rules and links into these guides.

## Current reference

- [PRODUCT.md](../PRODUCT.md): current users, product intent, constraints, and
  honest capability boundaries.
- [CONTEXT.md](../CONTEXT.md): domain vocabulary.
- [DESIGN.md](../DESIGN.md): current Portal and Admin design contract.
- [Deployment defaults](deployment.md): zero-config state paths, persistence,
  and advanced overrides.
- [Admin control plane](admin-control-plane.md): configuration/database
  authority and Admin HTTP contracts.
- [Compiler cache](compile-cache.md): ccache and sccache deployment contract.
- [Self-test checklist](self-test-checklist.md): manual deployed-service checks.
- [Release verification](release-verification.md): signed artifacts and
  immutable release inputs.
- [Security policy](../SECURITY.md): supported releases and reporting process.

## Decisions and historical evidence

- `docs/adr/` contains accepted architectural decisions. Read only the ADRs
  touching the module being changed.
- `docs/specs/` contains dated design snapshots. A shipped implementation may
  have evolved after its spec.
- `docs/research/` contains dated research records, not evergreen guidance.
- `docs/DIRECTION.md` is a historical strategy snapshot. `PRODUCT.md` is newer
  where the two disagree.

Completed task-by-task execution plans are intentionally not kept in the
working tree. Git history already preserves them, while leaving them in the
default search surface made fresh sessions follow obsolete file names,
commands, and product decisions.
