# Depsilo Component Guidelines

> How the component layers are meant to be used, what each component is for,
> and what must never be built again. Values are in
> [design-tokens.md](design-tokens.md); the current inventory is in
> [DESIGN.md](../../DESIGN.md) and `web/src/components/`. Start at the
> [design set index](README.md).

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
| `button` | `primary` / `secondary` / `ghost` / `destructive`, `sm` / `md`. Does not default `type`, so browser submit behaviour is preserved. Every button that mutates carries `aria-busy` while pending |
| `icon-button`, `icon-button-control` | Mandatory label + tooltip; `data-icon-button`; **40×40 in every state**, including pending. This is asserted across every Admin route |
| `field` | Owns label + control + description/error and the `aria-describedby` merge. An error replaces the hint rather than stacking a second line |
| `input`, `textarea`, `select` | Compose `field`. 16px below `md` so a focused field cannot trigger mobile zoom |
| `select` | **Native `<select>`**, deliberately. OS typeahead, the platform picker on touch, and the `combobox` + value contract beat a styled popup for the filter bars and enum fields this product is made of |
| `checkbox`, `switch` | The label is part of the click target |
| `tabs` | `underline` for page-local destinations, `directory` for an indexed list. Selection is state, not decoration |
| `notice` | `success` / `warning` / `destructive` / `info`. Warning and destructive are different things and never merge. The tone names match the tokens and `components/ui`, so one word means one thing across the stack |
| `error-state` | The one initial-error presentation: names the failure, keeps Retry beside it |
| `empty-state` | Only after a response has *proved* the collection is empty. Four registers: no data yet, no results for this filter, nothing to do, and unavailable |
| `section-header` | Title, optional hint, optional action. Do not restate the page title |
| `metric` | Label + tabular value + optional delta. Never wrapped in its own card; metrics group into a rail |
| `status-dot`, `badge` | The status axis. See [design-tokens.md](design-tokens.md#4-status-system) |
| `modal`, `drawer` | Base UI owns focus trap, `aria-modal`, and focus restoration. `closeDisabled` exists so an in-flight mutation cannot be abandoned |
| `toast` | Transient confirmation only. A failure the Operator must act on stays inline, in context |
| `data-table`, `table-viewport` | One column contract and one focusable scroll region. Rows are not focusable; the row's action is. A page marks a measured column with `align="end"` rather than styling cells |
| `copy-button` | One copy control with two presentations: the labelled action beside a code block, and the compact action beside a read-only value. Projects and the Portal both used to have their own, which meant a copy could behave two ways |
| `setting-section` | A titled group whose rows are divided. It owns the list rhythm; each row's internal arrangement belongs to `Field` |
| `upstream-panel` | Shared by Portal Monitor and Admin Upstreams, so an Upstream's status vocabulary cannot diverge between the two surfaces |

### `components/ui`

Generic, no product meaning. A colour, a tone, or a label that only makes sense
for Depsilo belongs one layer up.

## 5. Form layout

- **`Field` owns the label.** One component renders label, control, hint, and
  error, and computes the `aria-describedby` merge. A page never assembles its
  own label + control + message, and never repositions a label with a
  descendant selector — that is styling another component's internals, and it
  breaks silently when those internals change. Where the label sits is a
  `layout` prop: `stacked` above the control (dialogs, two-column forms) or
  `row` beside it (settings lists, where the labels form a scannable column).
- **One column by default.** Two columns only when fields are genuinely paired,
  and only from `sm` up.
- **A placeholder is not a label.** Every control has a visible label. The two
  exceptions are a `sr-only` label for a control that is visually named by
  adjacent content, and `<span>` for a read-only value that is not a form
  control — a `<label>` pointing at nothing is announced as a label with no
  control.
- **Errors are inline and specific.** Required, validation, and
  blocked-by-environment states are stated in text next to the field that
  failed, naming the environment variable or the read-only config file. A
  disabled submit button is never the only explanation of what is wrong.
- **Control height is 36px.** Fields and standard buttons share it, so a button
  sitting beside a select in a filter bar lines up instead of sitting 4px
  short. The 40px floor in [design-tokens.md](design-tokens.md#6-space-size-radius)
  is the *target* floor for icon-only controls and the row rhythm, not the
  height of a text field.
- **A row's rhythm belongs to its container.** `SettingSection` owns the
  separators and the vertical spacing between rows; a row does not pad itself
  into alignment with its neighbours.

## 6. Page layout

## 7. Navigation

Every Admin route renders through the same frame, and the frame owns the
decisions a page must not re-make.

| Element | Contract |
| --- | --- |
| Outlet | The **one** width cap (1840px, border-box) and the padding ladder (16 / 24 / 32px at base / `sm` / `lg`). A banner rendered above the page frame aligns with the page because the cap lives here |
| Page | `fluid` inherits the outlet's cap; `readable` caps itself at 768px for a page whose content is prose rather than data |
| Header | One `h1` per page, a description capped at `72ch`, and actions that wrap rather than shrink |
| Destination rail | Page-local navigation, or nothing when the workspace has one page |
| Content | Edge-aligned with the page header. The Dashboard snapshot spec asserts this to the pixel |

Three composition rules:

1. **A page's sections are separated by space, not by frames.** A titled
   region uses `SectionHeader`; a rule under a title is a separator, not a
   container. Nothing wraps a whole page in a card.
2. **One main column, optionally one supporting rail.** The rail's width is
   decided once (320px from `xl`, 380px from `2xl`) and both arrangements —
   rail first or rail last — draw from it. Four hand-written grid templates for
   two arrangements is the drift this replaces.
3. **A page does not pad itself.** The frame owns the horizontal gutter; a page
   that adds its own padding breaks edge alignment with the header above it.

Rhythm inside a section is the section's own business, drawn from the 4px
ladder. The page-level rhythm is one value per page rather than a different gap
between every pair of sections; pages that use eight different vertical gaps
are describing a layout that has not been decided yet.

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

## 8. Tables

**A table scrolls; it does not become a list.** At 390px a log row is compared
column by column with the rows above it, and a stacked list destroys the
comparison — so the table scrolls horizontally inside its own named, focusable
region and the document never gains horizontal scroll. This corrects an earlier
version of this document, which said dense data always becomes a list on narrow
viewports. It does not, and the specification has always asserted the
opposite.

**The exception is a row whose action is essential.** Quarantine decisions and
metadata-refresh episodes switch to a divided list below `sm`, because there
the action has to stay reachable and a horizontally scrolling row would push it
off screen. The list keeps the identifying value, the outcome, and the primary
action together; it is not a scrolling table in a different shape.

**Numbers end-align; text starts-align.** `9 ms` and `1 240 ms` left-aligned
put their units in different places and defeat the column; end-aligned, the
digits and the unit line up. The header follows its own column's alignment —
a header labelling a right-aligned column from the left edge is the same bug
one row up. Measured columns use the mono face with tabular figures as well.

**Every header carries `scope="col"`.** This is not a WCAG A/AA failure, so
axe does not report it, which is exactly why it has to be a rule rather than a
review note.

**Rows are never focusable; the row's action is.** Asserted across every table.

**Each row shows at most one status signal** (see
[design-tokens.md](design-tokens.md#43-one-signal-per-row)). Column headers are
labels, not sentences.

## 9. States

The contract itself — the five states, plus mutation pending and permission
denied — is [DESIGN.md § Query-State Contract](../../DESIGN.md#query-state-contract).
What belongs here is the component-level consequence:

- A component that fetches owns all of them. It never renders an empty state
  before a response has proved emptiness, and never renders a zero as a failure.
- Independent sibling queries own independent states; one panel failing does not
  erase another panel's data.
- `empty-state` and `error-state` are the only two presentations for those
  cases. A page-local "nothing here" paragraph is a second implementation of
  one of them.

## 10. Accessibility, per component

- Every icon-only control: label, tooltip, 40px target, in every state.
- Focus is visible in both themes. Programmatically focused regions suppress the
  ring, because they are announcement targets rather than keyboard targets.
- Colour never carries meaning alone. Status is always colour **plus** text
  and/or shape.
- Live regions are used for genuinely live values only, and never wrap a region
  that updates often enough to become noise.
- Motion honours `prefers-reduced-motion`; the tests assert this by animation
  name.

## 11. Do not build

| Do not build | Instead |
| --- | --- |
| A second button/input/dialog/table | Extend the existing component's API |
| A page-local badge colour | A status role in `components/app` backed by a token |
| A one-off icon wrapper | `lucide-react` directly, or the runtime-glyph box in `components/app/icon.tsx` |
| A `Card` used to group two things that are never reordered | A section, or a data rail |
| A tooltip as the only explanation of a control | A visible label, with the tooltip as a reminder |
| A chart to fill a gap in a layout | An empty state, or nothing |
| A component-level colour value | A semantic token |

## 12. The check before merging a component change

1. Does it live in the right layer, and does it import only downwards?
2. Is it the *only* implementation of that control?
3. Does it work in light and dark, in Chinese and English, at 320px, with a
   keyboard, and with reduced motion?
4. Does it own all five query states if it renders data?
5. Do the focused Playwright specs and `make check` pass?
