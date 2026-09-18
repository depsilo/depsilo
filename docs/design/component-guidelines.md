# Depsilo Component Guidelines

> Stage B step 00. How the component layers are meant to be used, what each
> component is for, and what must never be built again. Values are in
> [design-tokens.md](design-tokens.md); the current inventory is in
> [DESIGN.md](../../DESIGN.md) and `web/src/components/`.

## 1. The layers

```text
Page                admin/pages/*, portal/pages/*, setup/*
  ↓
Feature component   admin/components/*, portal/components/*
  ↓
Application component   components/app/*
  ↓
UI primitive        components/ui/*
  ↓
Base UI / native element
```

Five rules, enforced by the codebase rather than by intention:

1. `components/ui` holds generic primitives only. No product logic.
2. `components/app` holds Depsilo-wide components. No route data, no API calls.
3. `@base-ui/react` is imported **only** from `components/ui`. Nothing else in
   `src/` imports it, and a `rg` over the tree proves it.
4. Pages never import `components/ui` or `@base-ui/react` directly. They compose
   application and feature components.
5. One implementation per control. A second button, input, dialog, table, or
   badge is a defect even if it looks harmless.

## 2. Adding a primitive

```bash
cd web && npx shadcn@latest add <name>
```

Only when an owning call site exists. An unused primitive is not free: it is a
second way to do something, and it will be used.

When the generated source needs to change — as the generated `Tabs` did, when
it shadowed its own `orientation` prop — change it in `components/ui` with a
comment explaining why, so the reason survives the next `shadcn add`.

## 3. Command hierarchy

- **One primary action per view.** If two things look primary, neither is.
- Secondary commands are visually quieter; a view with three equal-weight
  buttons has not decided anything.
- Destructive commands are never the default focus and never adjacent-and-similar
  to a safe command.
- A command that changes server state disables itself while in flight and says
  so. It never closes its dialog on failure.

## 4. Component contracts

### `components/app`

| Component | Contract |
| --- | --- |
| `button` | `primary` / `secondary` / `ghost` / `danger`, `sm` / `md`. Does not default `type`, so browser submit behaviour is preserved. Every button that mutates carries `aria-busy` while pending |
| `icon-button`, `icon-button-control` | Mandatory label + tooltip; `data-icon-button`; **40×40 in every state**, including pending. This is asserted across every Admin route |
| `field` | Owns label + control + description/error and the `aria-describedby` merge. An error replaces the hint rather than stacking a second line |
| `input`, `textarea`, `select` | Compose `field`. 16px below `md` so a focused field cannot trigger mobile zoom |
| `select` | **Native `<select>`**, deliberately. OS typeahead, the platform picker on touch, and the `combobox` + value contract beat a styled popup for the filter bars and enum fields this product is made of |
| `checkbox`, `switch` | The label is part of the click target |
| `tabs` | `underline` for page-local destinations, `directory` for an indexed list. Selection is state, not decoration |
| `notice` | `success` / `warning` / `danger` / `info`. Warning and danger are different things and never merge |
| `error-state` | The one initial-error presentation: names the failure, keeps Retry beside it |
| `empty-state` | Only after a response has *proved* the collection is empty. Four registers: no data yet, no results for this filter, nothing to do, and unavailable |
| `section-header` | Title, optional hint, optional action. Do not restate the page title |
| `metric` | Label + tabular value + optional delta. Never wrapped in its own card; metrics group into a rail |
| `status-dot`, `badge` | The status axis. See [design-tokens.md](design-tokens.md#4-status-system) |
| `modal`, `drawer` | Base UI owns focus trap, `aria-modal`, and focus restoration. `closeDisabled` exists so an in-flight mutation cannot be abandoned |
| `toast` | Transient confirmation only. A failure the Operator must act on stays inline, in context |
| `data-table`, `table-viewport` | One column contract and one focusable scroll region. Rows are not focusable; the row's action is |
| `upstream-panel` | Shared by Portal Monitor and Admin Upstreams, so an Upstream's status vocabulary cannot diverge between the two surfaces |

### `components/ui`

Generic, no product meaning. A colour, a tone, or a label that only makes sense
for Depsilo belongs one layer up.

## 5. Form layout

- Labels above controls, always visible. A placeholder is not a label.
- One column by default. Two columns only when fields are genuinely paired
  (`sm` and up).
- Required, validation, and blocked-by-environment states are stated in text
  next to the field that failed. A disabled submit button is never the only
  explanation of what is wrong.
- A field blocked by an environment variable or a read-only config file says
  so, and says which one.

## 6. Navigation

Three navigation elements, each with one job.

### The workspace rail (Admin)

- Six workspace links, always visible from `lg` up, in one flat list. No child
  destinations and no disclosure controls: page-level destinations belong to the
  destination rail below the page title.
- **Exactly one item is active at a time**, marked by a single brand rule at the
  rail's own left edge plus a neutral fill and a weight change. The active
  workspace is *not* marked by tinting the whole row: a rail six items tall
  would spend the accent six times over, and position is what the accent marks.
- The icon inherits the item's colour. It does not sit in its own chip, tile, or
  square — that is a card inside a row.
- A workspace whose route is not its landing page carries `aria-current`
  `location`; the landing page carries `page`.
- The footer holds instance management, the signed-in Operator, and sign-out.
  The instance link is a **destination**, so it takes the same active treatment
  as a workspace. Without it, a rail whose instance group is hidden from the
  workspace list shows nothing selected at `/admin/settings`.
- Sign-out is revealed on row hover for pointing devices, and always visible to
  a keyboard user (`focus-visible`), because a control that exists only on hover
  does not exist for a keyboard.

### The destination rail

- Page-local destinations for the current workspace, projected from the route
  manifest. A workspace with one page renders no rail at all.
- An underline marks the current destination; the label gains weight and
  full-strength text colour. Nothing else about the row changes.
- The rail scrolls horizontally rather than wrapping, and never widens the
  document past the viewport.

### The mobile drawer

- The same six workspaces in a sheet, reached from the topbar trigger. Drawer
  and rail render the same component, so a workspace cannot exist in one and not
  the other.
- Escape closes it and returns focus to the trigger.

## 7. Tables

- Dense data is a table on wide viewports and a divided list on narrow ones.
  The list keeps the identifying value, the outcome, and the primary action
  together; it is not a horizontally scrolling table.
- Wide tables scroll inside their own named, focusable region. The document
  never gains horizontal scroll.
- Numbers are right-aligned and tabular; text is left-aligned.
- Column headers are labels, not sentences.
- Each row shows at most one status signal (see
  [design-tokens.md](design-tokens.md#43-one-signal-per-row)).

## 8. States

Every data region owns five, and they must be distinguishable from each other:

| State | Presentation |
| --- | --- |
| Initial pending | The region is `aria-busy`; skeletons are hidden from assistive technology; no empty message |
| Initial error | `error-state`, naming the failure, with Retry |
| Successful empty | `empty-state`, only after a success proves emptiness |
| Cached data + failed refresh | Cached data stays visible with a stale notice |
| Permission denied | Explicit denial, never rendered as an empty collection |

Independent sibling queries own independent states. One panel failing does not
erase another panel's data.

## 9. Accessibility, per component

- Every icon-only control: label, tooltip, 40px target, in every state.
- Focus is visible in both themes. Programmatically focused regions suppress the
  ring, because they are announcement targets rather than keyboard targets.
- Colour never carries meaning alone. Status is always colour **plus** text
  and/or shape.
- Live regions are used for genuinely live values only, and never wrap a region
  that updates often enough to become noise.
- Motion honours `prefers-reduced-motion`; the tests assert this by animation
  name.

## 10. Do not build

| Do not build | Instead |
| --- | --- |
| A second button/input/dialog/table | Extend the existing component's API |
| A page-local badge colour | A status role in `components/app` backed by a token |
| A one-off icon wrapper | `lucide-react` directly, or the runtime-glyph box in `components/app/icon.tsx` |
| A `Card` used to group two things that are never reordered | A section, or a data rail |
| A tooltip as the only explanation of a control | A visible label, with the tooltip as a reminder |
| A chart to fill a gap in a layout | An empty state, or nothing |
| A component-level colour value | A semantic token |

## 11. The check before merging a component change

1. Does it live in the right layer, and does it import only downwards?
2. Is it the *only* implementation of that control?
3. Does it work in light and dark, in Chinese and English, at 320px, with a
   keyboard, and with reduced motion?
4. Does it own all five query states if it renders data?
5. Do the focused Playwright specs and `make check` pass?
