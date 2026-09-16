# BoardUI design-system research — 2026-09-16

> Research record, frozen on 2026-09-16. Every design value below was read out
> of BoardUI's own shipped artifacts — the published theme source, the
> machine-readable registry, the GitHub export, and the npm package — rather
> than from marketing copy or third-party summaries. Retrieval method, HTTP
> status, and source URL is given for each section. Nothing here is a
> recommendation to adopt; it is the evidence a decision needs.

## Executive conclusion

- **BoardUI is a source-ownership design system, not a component dependency.**
  Components are copied into your project as `.tsx` files by a CLI. There is no
  runtime package to upgrade. Its stack is React 19 + Tailwind CSS v4
  (CSS-first `@theme`) + TypeScript + React Aria Components — which is close to
  Depsilo's existing frontend stack, so the *mechanism* would fit.
- **The free tier is MIT and usable in a public repository. The Pro tier is
  not.** The Pro licence explicitly forbids publishing the source in a public
  repository, and Depsilo is a public MIT repository. This is the single
  decisive constraint: chart cards, agent components, and the eight page
  templates are off-limits for `depsilo/depsilo` as it exists today.
- **The parts most relevant to Depsilo are the free ones anyway**: app shell /
  sidebar, tables, forms, chips, status dots, pagination, notification center,
  and the theme/typography token layers. The free tier is 63 registry items;
  the Pro tier is 36, with zero name overlap.
- **Depsilo has already borrowed the surface treatment, not the system.** The
  `BoardUI-inspired` block in `web/src/admin/admin-shell.css` moved a handful of
  colours and corners. The real BoardUI contribution would be the *semantic
  token vocabulary and the composite type scale*, which Depsilo currently does
  not have.
- **Three of BoardUI's defining choices conflict with written Depsilo design
  rules**: a second icon family (Remix Icon vs the mandated single Lucide
  wrapper), a second headless interaction library (React Aria Components vs
  `@base-ui/react`), and gradients/glass on primary controls versus the
  "no gradients, glass, decorative cards" constraint recorded for Admin.

## What BoardUI is

Retrieved 2026-09-16. `https://www.boardui.com/` → HTTP 200,
`<title>BoardUI - React Design System for Dashboards</title>`.

First-party self-description (`/llms.txt`, HTTP 200; and `Accept: text/markdown`
on `/`, `/docs/introduction`, `/installation` — every documentation URL answers
that content negotiation with clean markdown):

> BoardUI is a React + Tailwind CSS v4 design system for dashboards and agentic
> interfaces. It is distributed as source, not as a package: the CLI copies a
> component's `.tsx` files into your project and installs the npm packages that
> component actually needs.

Declared stack (`/docs/introduction`):

| Piece | BoardUI |
| --- | --- |
| UI runtime | React 19 |
| Styling | Tailwind CSS v4, CSS-first configuration via `@theme` |
| Language | TypeScript |
| Accessibility primitives | React Aria Components (Apache 2.0) |
| Icons | Remix Icon (`@remixicon/react`, Apache 2.0) |
| Charts | Recharts (MIT) |
| Animation | Motion (MIT) |
| Typefaces | Inter (OFL 1.1); JetBrains Mono for code |

The site itself is a Next.js App Router application deployed on Vercel (`_next`
asset paths, `dpl_` build id in the document element). Next.js is documented as
the first-class framework, with Vite, React Router, and Astro also supported.

Project scale as of retrieval: GitHub org `BoardUI` created 2026-09-02 with one
public repository; the free-source export `BoardUI/boardui` was created
2026-09-01, last pushed 2026-09-05, 459 stars, MIT `LICENSE` file present.
The `boardui` npm package (the CLI) was at v0.5.5 with ~2,435 downloads in the
2026-08-13 → 2026-09-11 window. **This is a young project — the entire public
history is roughly two weeks old at the research date.** That is not a quality
judgement, but it is a maintenance-risk fact.

### Counting discrepancies, reported rather than reconciled

The vendor's own numbers do not agree, and it is worth knowing which figure
comes from where:

- `/llms.txt`: "72 components, 17 chart cards, 8 full-page templates, and 400+
  design tokens".
- `/r/registry.json` (free, machine-readable): **63 items**.
- `/r/registry-pro.json` (Pro, machine-readable): **36 items**.
- `sitemap.xml`: 73 URLs under `/components/`.

63 + 36 = 99 registry items, of which 9 are templates. None of these
reconcile exactly with "72 components". The registry files are the
authoritative machine-readable source for "what can I install, and is it free";
the marketing counts should not be quoted as precise.

## Licensing and cost — the decisive constraint

Sources: `https://www.boardui.com/license` (HTTP 200),
`https://www.boardui.com/#pricing` (HTTP 200 after a 307 from `/pricing`), and
the homepage markdown rendering.

### Tier split

| Tier | What it is | Cost as published | Terms |
| --- | --- | --- | --- |
| Free | Base components and blocks, published on GitHub under MIT | €0, no account | MIT |
| Annual Access | Adds all Pro source, 1 year of updates, 1 developer | €179 list, shown at **€149**, one-off | Pro licence |
| Lifetime Access | Same, updates indefinitely, 1 developer | not published on the homepage | Pro licence |
| Unlimited for Startups | Up to 5 developers in one organisation | not published on the homepage | Pro licence |

Every plan is described as a one-off payment with no auto-renewal.

### The Pro licence terms that matter here

Quoted from `/license` (last updated 2 September 2026):

> **Free.** No purchase, no account. The Free source is published on GitHub
> under the MIT licence, and may be used under MIT terms wherever it came from.

> **What you cannot do.** Redistribute the Source, or any recognisable
> derivative of it, as source code. That includes selling it, giving it away,
> sublicensing it, or publishing it in a public repository, gist, template,
> starter kit or course.

> Use the Source to build anything that competes with BoardUI: a component
> library, UI kit, design system, template marketplace, theme, block collection
> or Figma kit offered to other people, paid or free.

Also stated: Build unlimited end products and sell them; modify anything; keep
the code in private repositories; hand end products to clients (the client does
not receive a licence). Seats are per human, not per machine. Governing law is
Dutch law.

**Consequence for Depsilo:** `depsilo/depsilo` is a public MIT repository.
Committing BoardUI Pro source into it would be publishing the Source in a
public repository, which the licence forbids. Pro chart cards, the composer /
agent components, and the eight templates cannot be vendored into this repo
without either changing the repository's visibility or not using Pro at all.
The MIT free tier carries no such restriction.

Two further licence-shaped observations, stated as facts rather than advice:

- The licence grants use of the *Source*. Independently re-implementing the
  design language (a green-accented token set, an information architecture,
  density choices) is a different activity from copying files, and the licence
  page does not purport to govern it. This document does not resolve where that
  line falls; if Depsilo ever intends to copy token *values* wholesale, that
  question deserves a deliberate decision rather than an implicit one.
- Depsilo is itself a supply-chain tool, so provenance of any third-party code
  it vendors is a product-credibility matter, not just a legal one.

## Distribution model

From `/docs/introduction`, `/installation`, and `/docs/cli` (all HTTP 200):

```bash
npx boardui@latest init          # Tailwind v4 wiring, theme tokens, globals, cx(), agent rules
npx boardui@latest add button    # copies source files, installs missing npm packages
npx boardui@latest list
npx boardui@latest login         # Pro licence activation
npx boardui@latest mcp
npx boardui@latest skill
```

`init` also writes always-on agent rules into `AGENTS.md` and
`.cursor/rules/boardui.mdc`; the `skill` command installs a `SKILL.md` plus
catalog/theming/pattern references into `.claude/skills/boardui`. Depsilo's own
`AGENTS.md` is a carefully curated short document, so that behaviour is worth
noting before running `init` unmodified.

Machine-readable surfaces, all HTTP 200 on 2026-09-16:

| Surface | Content |
| --- | --- |
| `/r/registry.json` | shadcn item schema index of all 63 free items |
| `/r/registry-pro.json` | metadata for the 36 Pro items — title, description, usage example; **never source** |
| `/r/<name>.json` | one free item with source inlined, pinned `dependencies`, and `registryDependencies` |
| `/openapi.json` | described API surface |
| `/llms.txt`, `/llms-full.txt` | site map; site map with every component inlined |
| `/mcp` | MCP over Streamable HTTP; read-only tools (`list_components`, `search_components`, `get_component`, `get_usage_examples`, `get_theme`) |
| `npx -y boardui@latest mcp` | MCP over stdio with write capability |
| `Accept: text/markdown` | clean markdown at every documentation URL |

## Design tokens

Source: the complete `styles/theme.css` that `/installation` inlines in its
"Manual installation" section. It was extracted from the server-rendered
payload at that URL and read directly; the file is 907 lines. This is the same
artifact the `theme` registry item installs.

### Layer model

Four layers, exactly as Tailwind v4 expects:

1. `@theme { … }` — colour primitives plus ramp overrides, which become
   Tailwind utilities.
2. `:root { … }` — semantic tokens in light mode, referencing primitives.
3. `.dark { … }` — semantic overrides in dark mode.
4. `@theme inline { … }` — re-export of the semantic tokens so they become
   utilities; the `inline` form means Tailwind reads the *current computed
   value*, so light↔dark flips work without a rebuild.

This is structurally the same approach Depsilo already uses in
`web/src/index.css` (`@theme` plus `:root` plus theme overrides), which is why
the two systems can coexist without a styling-engine migration.

### Colour primitives

Almost the entire palette is Tailwind v4's default. The theme names exactly four
intentional deviations (plus a blue used by the accent ramp):

```css
--color-slate-200:   #ebeff5;
--color-neutral-100: #f7f7f7;
--color-neutral-200: #ebebeb;
--color-neutral-925: #121212;
--color-blue-400:    #3392ff;
```

### The accent ramp — how to make it green

This is the most directly transferable idea in the whole system, quoted from the
theme source:

> The CTA hue. Interactive/selection surfaces (primary button gradient, ghost +
> link buttons, checkbox, radio, switch, slider, tabs, sidebar selected, focus
> ring, date-range selection) reference `accent-*` instead of a raw hue, so the
> whole system re-tints by overriding these eleven variables at runtime… or in
> a customer project after importing this file. Defaults to the blue primitives
> above. Content/data blues (charts, status chips, calendar events, info
> notifications) deliberately keep referencing `blue-*`.

Eleven variables — `--color-accent-50` through `--color-accent-950` — control
every interactive surface. The shipped site exposes this through a runtime
accent engine (`components/application/theme/accent.ts`) that sets those
variables and an `data-accent` attribute from `localStorage` in a
before-interactive script.

For Depsilo, whose signal green is the primary command colour, this is the
cleanest adoption path: the system is *designed* to be re-hued, and the data /
status colours are deliberately kept out of the accent ramp so that a green CTA
does not turn cache-health semantics green as well.

### Semantic token namespaces

Every colour reference in component code is a semantic token. The namespaces
observed in the shipped theme:

- `text-*` — `text-primary`, `text-secondary`, `text-tertiary`,
  `text-placeholder`, `text-disabled`, `text-error-primary`
- `background-*` — `background-full`, `background-primary-default` / `-hover` /
  `-active` / `-disabled`, `background-secondary-default` / `-hover`,
  `background-tertiary-*`, `background-quaternary-*`, plus per-component
  surfaces such as `background-secondary-default`
- `border-*` — `border-button-default` / `-hover` / `-active`,
  `border-checkbox-*`, `border-table`, `border-button-group`,
  `border-focus-ring`, `separator-border` / `-strong`
- `foreground-icon-*` — `foreground-icon-primary` through
  `-quaternary`, `-error`, `-disabled`
- `state-*` — `state-success-text`, `state-success-base`
- `chart-1` … `chart-9` plus `-active` variants, `chart-track`,
  `chart-cursor`, `chart-neutral`
- component-scoped families: `notification-*`, `badge-*`, `status-*`
  (lime / yellow / rose / cyan / blue / purple, each with `-background` and
  `-text`), `calendar-event-*`, `segmented-control-*`, `switch-*`,
  `theme-toggle-sidebar-*`, `composer-*`, `kbd-*`, `docs-*`

### Light and dark values for the core roles

Extracted from the `:root` and `.dark` blocks of the same file:

| Role | Light | Dark |
| --- | --- | --- |
| `background-full` (page ground) | `white` | `neutral-925` (`#121212`) |
| `background-primary-default` | `white` | `neutral-800` |
| `background-secondary-default` | `neutral-100` (`#f7f7f7`) | `neutral-900` |
| `background-tertiary-default` | `neutral-200` (`#ebebeb`) | `neutral-800` |
| `text-primary` | `neutral-950` | `neutral-50` |
| `text-secondary` | `neutral-500` | (inherits the ramp) |
| `text-tertiary` | `neutral-400` | `neutral-600` |
| `border-table` | `neutral-200` (`#ebebeb`) | `neutral-800` |
| `border-button-default` | `neutral-200` | `neutral-700` |
| `border-focus-ring` | `accent-500` | `accent-500` |
| `foreground-full` | `white` | `neutral-700` |

Two things stand out against Depsilo's current values. BoardUI's light page
ground is pure white with a very light neutral secondary surface — the same
family as the `#FFFFFF` canvas plus `#F6F7F5` rail that `admin-shell.css`
already approximates. And BoardUI's dark ground is a **neutral** `#121212`, not
a charcoal-green; Depsilo's dark Admin canvas is `#141915`, which is a
deliberate brand tint rather than BoardUI's neutral.

### Radii and shadows

BoardUI adds only two radius tokens to Tailwind v4's defaults:

```css
--radius-2lg: 10px;              /* Figma radius/2lg — used by medium buttons */
--radius-2-5xl: 20px;            /* large inset panels */
--radius-notification-card: 10px;
```

Everything else is the Tailwind default ladder (`sm` 2px, `md` 6px, `lg` 8px,
`xl` 12px, `2xl` 16px). Depsilo's current scale is a different, smaller ladder —
`--radius-pill: 4px`, `--radius-tag: 6px`, `--radius-card: 10px`,
`--radius-shell: 14px`, with Admin controls forced to 6px. BoardUI corners are
meaningfully rounder than Depsilo's Admin today.

Shadows are documented as copied 1:1 from Figma effect styles, including:

```css
--shadow-sidebar:
  0 1px 0 0 rgb(0 0 0 / 0.0196),
  0 1px 12px 0 rgb(0 0 0 / 0.0588),
  0 0 1px 0 rgb(0 0 0 / 0.3216);
--shadow-nav-selected:
  0 0 0 1px var(--color-accent-500),
  inset 0 1px 0 0 rgb(255 255 255 / 0.251);
--shadow-card: 0 1px 1px 0 rgb(0 0 0 / 0.05);
--shadow-dropdown: 0 1px 1px 0 rgb(0 0 0 / 0.04), 0 4px 4px 0 rgb(0 0 0 / 0.02);
```

`--shadow-nav-selected` is a useful, small idea: selected navigation is
expressed as an accent ring plus an inner top highlight, rather than a filled
tint. That is a pattern Depsilo could evaluate independently of the licence
question.

### Typography

Source: the `typography` registry item — `styles/typography.css`, 11,066
characters, read in full.

The whole ramp is Inter, registered as Tailwind v4 text utilities so that one
class sets size, line-height, letter-spacing, and weight together. Naming is
`text-<family>-<weight>` in lowercase kebab: `text-title-1-semibold`,
`text-headline-medium`, `text-body-regular`, `text-caption-2-bold`.

Weight mapping is `regular` 400, `medium` 500, `semibold` 600, `bold` 700.
The ramp opens at Large Title 64/80 and Display 1 56/72. The file notes that
Figma marks some styles "Inter Display" but BoardUI deliberately maps everything
to Inter because of the variable optical-size axis — Depsilo, by contrast,
currently ships both `Inter Variable` and `Inter Tight Variable`.

Code uses JetBrains Mono; the theme comments that Figma's spec actually calls
for IBM Plex Mono and JetBrains Mono is "the closest already-bundled face".

### Spacing and motion

- Spacing is Tailwind's default scale, with an explicit instruction to prefer
  flex/grid `gap` over per-child margins and to avoid arbitrary values.
- Button transitions run on `--button-transition-ms: 150ms`; inputs on
  `--input-transition-ms: 150ms`.
- Primary and danger buttons use a **gradient fill** with a `::before`
  pseudo-element carrying the hover gradient cross-faded by opacity, because
  CSS cannot interpolate `background-image`. The utility is registered with
  `@utility bg-button-primary` / `bg-button-danger`.
- There is a shared press-motion utility: `transform: scale(0.98)` on press,
  with the return animated at 420ms, and an explicit
  `@media (prefers-reduced-motion: reduce)` guard that disables both.

The reduced-motion guard and the stable-dimension discipline are the parts that
match Depsilo's existing interaction rules; the gradients do not.

## Component inventory: free versus Pro

Source: `/r/registry.json` (63 free items) and `/r/registry-pro.json` (36 Pro
items), plus the free-component descriptions in `/llms.txt`.

**Free (MIT, 63 items)** — foundations (`theme`, `typography`, `globals`,
`cx`, `rules`, `logo`, `chevrons`), hooks (`use-dismiss-on-outside-press`,
`use-count-up`), form and control primitives (`button`, `icon-button`,
`link-button`, `button-group`, `close-button`, `input`, `textarea`, `checkbox`,
`checkbox-card`, `radio`, `radio-card`, `switch`, `switch-card`, `select`,
`slider`, `segmented-control`, `date-picker`, `date-range-picker`,
`meeting-scheduler`, `file-upload`, `input-otp`, `social-button`), data display
(`table`, `data-table`, `chip`, `status-dot`, `badge`, `kbd`, `avatar`,
`breadcrumb`, `pagination`, `carousel`, `divider`, `tooltip`, `dropdown`,
`tabs`), and application blocks (`sidebar`, `app-shell`, `settings-modal`,
`auth-card`, `notification`, `notification-center`, `announcement`,
`theme-toggle`, `stat-cards`, `revenue-chart-card`, `orders-chart-card`,
`important-alerts-card`, `patient-info-card`, `agent-chat`, `agent-runtime`,
`agent-thinking`, `agent-log`, `composer-loader`).

**Pro (36 items)** — `web-search`, `task-list`, `composer`,
`composer-panel`, `composer-attachments`, `agent-progress`,
`agent-limits-card`, `questionnaire`, `calendar`, `project-board`, seventeen
chart cards (`earnings`, `line`, `contributions`, `radar`, `radial`, `funnel`,
`sankey`, `stage-bars`, `bar-list`, `area`, `combo`, `scatter`, `heatmap`,
`steps`, `sleep-score`, `activity-rings`, `most-active-days`), and nine
templates (`home-dashboard`, `project-board`, `marketing`,
`finance`, `hr`, `medical-profile`, `ai-chat`, `ai-profile`,
`ai-image-generation`). The ten agent/application items and the named
project-board, plus seventeen chart cards and nine templates, account for all
36.

### What the free blocks actually contain

Inspected by fetching `/r/<name>.json` for each (all HTTP 200):

| Item | Files | Source size | npm dependencies added | Notable `registryDependencies` |
| --- | --- | --- | --- | --- |
| `sidebar` | 3 | 20,934 + 8,888 + 7,791 chars | `@remixicon/react`, `react-aria-components` | `avatar`, `badge`, `button`, `chevrons`, `close-button`, `cx`, `globals`, `kbd`, `settings-modal`, `theme-toggle` |
| `app-shell` | 24 | 7,296 chars entry | `@internationalized/date`, `@remixicon/react`, `motion`, `react-aria-components` | 40 items — effectively the whole free catalogue |
| `segmented-control` | 1 | 4,844 chars | `react-aria-components` | `cx`, `globals` |
| `stat-cards` | 1 | 9,546 chars | `@remixicon/react`, `react-aria-components` | `chip`, `cx`, `tooltip` |
| `data-table` | 1 | 21,235 chars | `@remixicon/react`, `@tanstack/react-table`, `react-aria-components` | 14 items |

Two caveats that a "just install it" plan would hit:

1. The free `data-table` item's single file is
   `components/application/docs/examples/data-table-example.tsx` — it is the
   **documentation example composition**, not a drop-in primitive. Adopting it
   means adopting TanStack Table plus fourteen sibling registry items.
2. `app-shell` is a marketing-site shell: among its 24 files are
   `components-catalog.tsx`, `pro-offer-card.tsx`, and
   `starter-notifications.ts`. Installing it into Depsilo would import
   BoardUI's own product surfaces, not a neutral dashboard shell.

The genuinely reusable, tightly-scoped free items are the ones with one file and
one dependency: `segmented-control`, `button`, `input`, `select`, `switch`,
`tooltip`, `badge`, `chip`, `status-dot`, `pagination`, `tabs`.

## Patterns worth adopting

Ordered by value to Depsilo, with the evidence each rests on.

1. **One re-tintable accent ramp separate from data colours.** Eleven
   `accent-*` variables drive every interactive surface, while charts and status
   chips keep their own hues. Depsilo currently has `--brand` / `--hit` doing
   double duty as both "primary command" and "cache hit", which is exactly the
   coupling BoardUI's comment says it avoided. *Evidence: theme source accent
   ramp comment; `DESIGN.md` state-semantics table.*
2. **A composite type scale.** `text-title-1-medium` beats stacking
   `text-xl font-medium leading-7`, and it makes the "did this page use the
   right level?" question reviewable. Depsilo's `DESIGN.md` specifies type
   behaviour in prose but has no composite utilities. *Evidence:
   `styles/typography.css`.*
3. **Selected state as ring plus inner highlight rather than a filled slab.**
   `--shadow-nav-selected` and the sidebar's selection treatment give hierarchy
   without a large tinted block. *Evidence: theme shadows; sidebar item JSON.*
4. **Explicit light/dark token pairing with no `dark:` colour variants in
   component code.** The rule "if a token pair looks wrong in dark mode, pick a
   different token, not a literal" is a cheap review rule that prevents the
   theme drift Depsilo's Admin already works around with a scoped override
   block. *Evidence: `docs/agent-rules.md` in the `rules` item.*
5. **Status as a dot plus text, not a filled badge**, already Depsilo's rule for
   Dashboard panel headers — BoardUI's `status-dot` and table conventions agree.
6. **Agent-facing machine-readable surfaces.** Registry JSON, MCP, `llms.txt`,
   and `Accept: text/markdown` on every docs page. Depsilo has an
   `agent-prompt` endpoint and an MCP read surface of its own; BoardUI is a
   useful reference for what that can look like. *Evidence: `/llms.txt`,
   `/mcp`, `/docs/introduction`.*
7. **Copy the reduced-motion and stable-dimension guardrails, not the
   gradients.** The press-motion utility's `prefers-reduced-motion` block and
   the fixed control geometry are consistent with Depsilo's existing rules;
   the gradient button fill and "liquid-glass" composer treatment are not.

## Compatibility with Depsilo today

Checked against `web/package.json` and `web/src/index.css`.

| Dimension | BoardUI | Depsilo | Fit |
| --- | --- | --- | --- |
| React | 19+ required | 19.2.8 | compatible |
| Tailwind | v4, CSS-first `@theme` | v4.2.2, CSS-first `@theme` | compatible — same mechanism |
| Class merging | `tailwind-merge` | `tailwind-merge` 3.5 already present | compatible |
| Bundler | Vite supported (Next.js first-class) | Vite 8 | compatible |
| Charts | Recharts (MIT) | Recharts 3.8 already present | compatible |
| Typefaces | Inter, JetBrains Mono | Inter Variable, Inter Tight Variable, JetBrains Mono | near-identical |
| Dark mode selector | `.dark` on `<html>`, toggled by a before-interactive script; no system-preference dependency | `html.dark`, `[data-theme="dark"]`, **and** `prefers-color-scheme` | needs reconciliation |
| Icon family | `@remixicon/react` | `lucide-react` behind a `components/Icon.tsx` wrapper | conflict |
| Headless primitives | React Aria Components | `@base-ui/react` | conflict — would be two interaction libraries |
| Radius scale | Tailwind defaults + 10px / 20px additions | 4/6/10/14px custom ladder | conflict — BoardUI is rounder |
| Primary control treatment | gradient fill, press scale | flat fill; Admin already at 6px corners | conflict |
| Resulting new npm deps | `react-aria-components`, `@remixicon/react`, `@internationalized/date`, `motion`, `@tanstack/react-table` | none of these present | additive |

## Conflicts with the current DESIGN.md

`DESIGN.md` is the declared source of truth for UI work, so any adoption has to
settle these first. Each is a direct quote from the current document:

| Current Depsilo rule | BoardUI's actual behaviour |
| --- | --- |
| "Do not import a second general icon family or inline an SVG for a symbol already present in `components/Icon.tsx`." | Every component ships with `@remixicon/react` icons. |
| "No gradients, glass, decorative cards, invented state, or backend rewrite" (recorded constraint on the Admin shell brief) and the Instrument description of surfaces as "neutral gray/green-black, with restrained borders and shadows". | `bg-button-primary` / `bg-button-danger` are gradient fills; the free `composer-loader` is described as an iridescent band. |
| "Do not use the old purple/OKLCH examples, `/status` route, **shadcn components**, `CardV2`, or `MetricCardV2`." | BoardUI's registry uses the *shadcn item schema* and can install via the shadcn CLI. The schema is a distribution format, not the shadcn component library, but the wording of the existing rule would have to be clarified before a CLI-driven install. |
| Admin controls use 6px corners and 40px targets. | BoardUI's control ladder includes `--radius-2lg: 10px` for medium buttons. |
| Instrument green is `#0FA86F` (hit/brand) and `#0A8654` (primary command). | Default accent is Tailwind blue; the accent ramp is designed to be overridden, so green is achievable, but the shipped defaults are blue. |

## Not verifiable / caveats

Stated plainly, so nothing here is mistaken for a fact:

- **BoardUI's Pro component source was not inspected.** `/r/registry-pro.json`
  returns metadata and usage examples only, explicitly "never source". Claims
  about Pro code quality would be guesses. Only live previews and the public
  docs descriptions were available.
- **The Figma master file was not inspected.** The vendor states Figma is the
  source of truth and that components are generated from it; the file itself is
  not public. Every token value here comes from the shipped CSS, not Figma.
- **Quality, security, and stability of the code were not evaluated.** Only file
  counts, sizes, and declared dependency lists were read. No installation into
  a scratch project, no build, no runtime test, and no audit of the shipped
  React was performed. The free catalogue's real-world maturity is unverified.
- **The npm package's `repository` field is stale or wrong.** `boardui@0.5.5`
  points at `github.com/mertcanesmergul/boardui` (`directory: "cli"`), and that
  repository returns 404 from the GitHub API on 2026-09-16. The live org is
  `github.com/BoardUI/boardui`. This is a provenance wart worth resolving before
  any dependency on the CLI.
- **Pricing and its terms are inconsistent across the vendor's own surfaces.**
  `/llms.txt` and the homepage markdown say "one-time purchase with lifetime
  updates"; the homepage pricing card for the €149 tier says "1 year of Pro
  updates". The `/license` page describes three plans with a one-year window on
  Annual Access and indefinite updates on Lifetime Access. Treat any specific
  price as volatile; `store` links and EUR figures are marketing state, not a
  contract, and this document does not restate them as stable.
- **Aggregate counts do not reconcile** (see the counting-discrepancies section
  above). The registry JSON files are trustworthy per item; the headline totals
  are not.
- **No accessibility conformance claim was verified.** React Aria Components is
  a credible accessibility foundation and is Apache-2.0 licensed, but BoardUI
  publishes no VPAT, WCAG conformance statement, or axe results that were found
  during this research. Depsilo's own Admin axe coverage is currently stronger
  evidence of accessibility than anything on the BoardUI site.
- **Longevity is unproven.** Both the GitHub org and the source export are days
  old at the research date, with a single public repository and a few thousand
  monthly CLI downloads.
- **This document is research, not legal advice.** The licence reading above
  quotes the vendor's own text; a decision to copy anything non-MIT should be
  confirmed deliberately rather than inferred from a summary.

## Relevance to Depsilo

### What is already borrowed

`web/src/admin/admin-shell.css` (added in `23eb652 refactor(admin): apply
boardui-inspired shell styling`, 2026-09-15) scopes a colour and radius override
block to `[data-admin-shell]`: a white light canvas, a `#F6F7F5` rail, a
`#141915` dark canvas with `#181F1A` navigation, and 6px control corners. That
is a surface treatment. It does not adopt BoardUI's token vocabulary, its type
scale, its accent-ramp separation, or any component.

`DESIGN.md` records the same change in prose. Note that the block redeclares
shared role variables (`--bg-page`, `--bg-card`, `--text`, `--brand`, `--btn`,
`--focus-ring`) inside a scoped selector, which is a second palette living
alongside the Instrument tokens in `web/src/index.css`. The `DESIGN.md`
instruction immediately below it already forbids "hard-coded parallel
palettes", so the current state is a tension the next design change should
resolve either way.

### The three realistic paths

1. **Free-tier adoption (MIT).** Vendor or install selected free items —
   `segmented-control`, `button`, `input`, `select`, `switch`, `tooltip`,
   `badge`, `chip`, `status-dot`, `pagination`, `tabs`, `table`, `sidebar` —
   and adopt the token layering, accent-ramp split, and composite type scale.
   Keeps the repository public and MIT. Requires deciding on the second icon
   family and the second headless library, since those arrive with the
   components whether they are wanted or not.
2. **Design-language adoption without code.** Keep Depsilo's own components and
   take the structural ideas: separate the command accent from the health
   signal, add composite type utilities, use ring-plus-highlight selection,
   tighten the light/dark token pairing. This is the smallest diff against the
   current `DESIGN.md` and needs no licence decision beyond "did we copy files
   or values?".
3. **Pro adoption.** Only viable if Depsilo's repository stops being public, or
   if Pro material stays entirely outside the repository. Given that Depsilo is
   MIT-licensed and self-hosted by design, and that its Admin already has its
   own chart and table implementations, the Pro tier's incremental value here
   looks like the smallest of the three against the largest constraint.

### Open questions this research does not answer

- Does Depsilo want a second headless interaction library, or would it rather
  port patterns onto `@base-ui/react`?
- Is a single-icon-family rule still worth keeping if BoardUI components are
  the source of most new UI?
- Which surface is actually being redesigned — Admin only, Portal, Setup, or all
  three? `DESIGN.md` currently treats Portal/Setup and Admin as one system with
  a scoped Admin override, and the four existing `.impeccable/mocks/` comps are
  all Admin.
- Is there an appetite to pay for Pro at all, given the repo is public?

## Sources

All URLs retrieved 2026-09-16.

- `https://www.boardui.com/` — HTTP 200; title, pricing card, product claims.
  Also retrieved with `Accept: text/markdown` (HTTP 200).
- `https://www.boardui.com/license` — HTTP 200; licence text, plan definitions,
  third-party licence list, governing law.
- `https://www.boardui.com/terms` — HTTP 200.
- `https://www.boardui.com/privacy` — HTTP 200 (not read in detail).
- `https://www.boardui.com/docs/introduction` — HTTP 200; also HTTP 200 as
  markdown. Stack, distribution model, free-vs-Pro split, source-not-dependency
  statement.
- `https://www.boardui.com/installation` — HTTP 200; also HTTP 200 as markdown.
  The complete 907-line `styles/theme.css` and the manual setup steps are
  inlined in the server-rendered payload and were extracted from there.
- `https://www.boardui.com/docs/cli` — HTTP 200.
- `https://www.boardui.com/pricing` — HTTP 307 → `/#pricing` HTTP 200.
- `https://www.boardui.com/components` — HTTP 200; gallery.
- `https://www.boardui.com/mcp` — HTTP 200.
- `https://www.boardui.com/skill` — HTTP 200.
- `https://www.boardui.com/llms.txt` — HTTP 200; free/Pro inventory with
  descriptions, machine-readable surface list.
- `https://www.boardui.com/sitemap.xml` — HTTP 200; 91 URLs.
- `https://www.boardui.com/robots.txt` — HTTP 200.
- `https://www.boardui.com/r/registry.json` — HTTP 200; 63 free items.
- `https://www.boardui.com/r/registry-pro.json` — HTTP 200; 36 Pro items.
- `https://www.boardui.com/r/theme.json`, `/r/typography.json`, `/r/rules.json`,
  `/r/button.json`, `/r/sidebar.json`, `/r/data-table.json`,
  `/r/stat-cards.json`, `/r/app-shell.json`, `/r/segmented-control.json` —
  HTTP 200 each; item source, dependencies, registry dependencies.
- `https://github.com/BoardUI/boardui` and its GitHub API record — HTTP 200;
  MIT licence, 459 stars, created 2026-09-01, last push 2026-09-05.
- `https://api.github.com/orgs/BoardUI` — HTTP 200; org created 2026-09-02.
- `https://registry.npmjs.org/boardui/latest` — HTTP 200; v0.5.5, MIT, CLI
  dependencies, and the unresolved `repository` URL.
- `https://api.npmjs.org/downloads/point/last-month/boardui` — HTTP 200;
  2,435 downloads, 2026-08-13 → 2026-09-11.

Secondary or local sources used only for comparison, never as the basis of a
BoardUI claim: `web/package.json`, `web/src/index.css`,
`web/src/admin/admin-shell.css`, `DESIGN.md`,
`.impeccable/surfaces/web-src-admin-components-mainlayout-tsx.md`, and
`git show 23eb652`.
