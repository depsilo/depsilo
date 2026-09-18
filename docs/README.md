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
- [DESIGN.md](../DESIGN.md): current Portal and Admin design contract — the
  layers, token roles, component inventory, states, and what must not regress.
- [Design set](design/README.md): why the interface serves what it serves, why
  it looks like this, every value with the floors it must clear, and how
  components compose. Start here for any UI change.
- [Brand assets](brand/README.md): the mark and the palette it fixes, with the
  clear-space and minimum-size rules.
- [Deployment defaults](deployment.md): zero-config state paths, persistence,
  and advanced overrides.
- [Admin control plane](admin-control-plane.md): configuration/database
  authority and Admin HTTP contracts.
- [Package rules](package-rules.md): ecosystem-specific package identity,
  version capabilities, migration behavior, and enforcement limitations.
- [Compiler cache](compile-cache.md): ccache and sccache deployment contract.
- [Self-test checklist](self-test-checklist.md): manual deployed-service checks.
- [Release verification](release-verification.md): signed artifacts and
  immutable release inputs.
- [Trial guide](trial-guide.md): isolated installation, fault rehearsal, and
  feedback fields.
- [Security policy](../SECURITY.md): supported releases and reporting process.

## Decisions and historical evidence

- `docs/adr/` contains accepted architectural decisions. Read only the ADRs
  touching the module being changed.
- [The UI audit](refactor/shadcn-ui-audit.md) is the inventory of the legacy
  frontend that the shadcn/ui migration was argued from. Evidence, not
  instructions: everything in it has since been migrated or deleted.
- `docs/specs/` contains dated design snapshots. A shipped implementation may
  have evolved after its spec.
- `docs/research/` contains dated research records, not evergreen guidance.
- `docs/DIRECTION.md` is a historical strategy snapshot. `PRODUCT.md` is newer
  where the two disagree.

Completed task-by-task execution plans are intentionally not kept in the
working tree. Git history already preserves them, while leaving them in the
default search surface made fresh sessions follow obsolete file names,
commands, and product decisions.
