# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read only what the task needs

- **`PRODUCT.md`** for current users, product intent, and capability claims.
- **`CONTEXT.md`** when naming domain concepts or user roles.
- **`docs/adr/`** for decisions touching the module being changed.
- **`docs/DIRECTION.md`** only for historical strategy context; it is not the
  current backlog where it conflicts with `PRODUCT.md`.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo:

```
/
├── PRODUCT.md
├── CONTEXT.md
├── docs/
│   ├── DIRECTION.md
│   └── adr/
│       ├── 0001-pools-map.md
│       ├── 0002-access-log-rollup.md
│       ├── 0003-supply-chain-control-point.md
│       └── 0004-supply-chain-enforcement-layer.md
├── internal/
└── web/src/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal,
a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Do not drift
to synonyms the glossary explicitly avoids. Product claims still come from
`PRODUCT.md`; the glossary is not a release-status document.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because..._
