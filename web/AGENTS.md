# Frontend guide

These instructions apply under `web/`. Read the root `AGENTS.md` first.

## Product surfaces

- `src/portal`: anonymous connection guidance and service status.
- `src/admin`: authenticated operational work; route ownership starts in
  `src/admin/routes.ts`.
- `src/setup`: first-run configuration before Portal/Admin dispatch.
- `src/components`: shared primitives; prefer these over page-local variants.
- `src/lib`: typed transport contracts and pure domain helpers.

`DESIGN.md` records the current UI contract. `PRODUCT.md` governs claims and
personas. The implementation and tokens in `src/index.css` are final truth when
a screenshot or old spec disagrees.

## Frontend invariants

- Portal never exposes Admin logs, decisions, credentials, or operational
  history.
- Use TanStack Query for server state. Preserve visible loading, initial error,
  stale-refresh, empty, permission, and mutation failure states.
- Keep route paths in the route manifest rather than duplicating strings.
- Derive the service origin from `window.location.origin`.
- Update `src/i18n/zh.ts` and `src/i18n/en.ts` together; placeholders must match.
- Every interaction works with keyboard and visible focus. Icon-only controls
  have accessible names and at least a 40px target.
- Responsive tables scroll inside a named region; the document must not gain
  horizontal overflow at 320px.
- Respect reduced motion. Do not hide state or meaning behind color alone.
- No new runtime dependency without a demonstrated need.

## Test placement

- Pure catalogs, reducers, state models, and route functions: `unit/` (Vitest).
- Rendered behavior, navigation, focus, network states, layout, and axe: `e2e/`
  (Playwright with `fixtures/admin-api.ts`).
- Do not use Playwright for a pure array or function assertion.

```bash
npm --prefix web run lint
npm --prefix web run test:unit
npm --prefix web run test:ui -- <focused-spec>
make lint-i18n
make check
```

See `docs/development/testing.md` for the full verification matrix.
