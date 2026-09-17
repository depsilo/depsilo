# Depsilo Stage B — UI Redesign Proposal

> Status: proposal for discussion. **No code changes.** Stage A is complete and
> the architecture it produced is frozen; everything below is a plan for the
> engagement that follows it.
>
> Companion documents: [Stage A audit](shadcn-ui-audit.md),
> [Stage A plan](shadcn-ui-plan.md), the current UI contract
> [`DESIGN.md`](../../DESIGN.md).

> **Step 00 has since been written.** The design set this proposal calls for is
> in [docs/design/](../design/): principles, visual direction, token contract,
> component guidelines, and the page order. Read those for the substance; this
> document records the reasoning that produced them and the questions that
> remain open.

## 1. What Stage A left behind

Stage B starts from a substrate, not a blank page. The relevant properties:

| Property | Value |
| --- | --- |
| Primitive layer | `components/ui` — 15 shadcn primitives over Base UI |
| Application layer | `components/app` — 29 Depsilo components |
| Token layer | One `:root`, one `.dark`, `@theme inline`; semantic roles only |
| Stylesheet | `index.css` is 376 lines: imports, tokens, base, icon box |
| Icons | Lucide, direct imports |
| Legacy surface | Zero legacy tokens, zero hand-written component classes, zero legacy primitives |
| Motion | Named animations with a global `prefers-reduced-motion` switch |
| Enforcement | 263 Playwright tests, 41 unit tests, axe on every Admin route |

This means Stage B can change *values and composition* without touching
architecture. A palette change is a token edit; a density change is a class
edit; neither requires a component rewrite.

## 2. Mandate and hard constraints

From the brief:

- Everything is open: brand, logo, colour, typography, spacing, radius,
  sidebar, navigation, dashboard, cards, tables, forms, status system, charts,
  empty states, onboarding, portal, landing language.
- Stage B must **not** look like a typical AI SaaS product: no giant gradients,
  purple-blue glow, glassmorphism, floating cards, huge radii, everything-in-a-
  card, huge headings, excessive whitespace, or decorative dashboards.
- The target register is `precise, technical, minimal, calm, dense,
  professional, premium, developer-first`.
- Reference products to study (not copy): Linear, Vercel, GitHub, Cloudflare,
  Railway, Sentry, Grafana, Supabase, Resend, Stripe Dashboard.

What the brief does not say, and what I am treating as binding until you rule
otherwise:

1. **Behaviour is frozen.** The Playwright suite encodes the product's
   interaction contract — query states, permission denials, mutation
   in-flight rules, focus restoration, responsive floors, i18n parity. A
   redesign may change how a state *looks*; it may not delete a state or
   weaken an invariant.
2. **Accessibility floors are frozen.** Every icon-only control keeps a 40px
   target, every Admin route stays axe-clean, no negative letter spacing, the
   document never scrolls horizontally at 320px, and colour is never the only
   carrier of meaning.
3. **CJK is a first-class locale.** Any typographic decision is evaluated in
   Chinese before it is accepted. The brief's own rules make this structural:
   negative tracking and tight display faces are unavailable to us.
4. **Depsilo is infrastructure, not a marketing site.** The Portal is a setup
   workbench and the Admin is a control plane. Neither becomes a landing page.

## 3. The design problem, stated concretely

Stripped of adjectives, Depsilo has to make four things legible:

1. **Is the service working right now?** Health, upstream availability, cache
   pressure, error rate. Read many times a day, usually in a glance.
2. **What happened to this request?** Three independent outcome dimensions —
   cache result, policy decision, delivery result — plus latency, size, origin.
   Read while investigating.
3. **What is happening over time?** Trends, bandwidth, top packages, refresh
   history. Read when something looks wrong or when capacity is planned.
4. **What am I allowed to change, and what did changing it do?** Settings,
   upstreams, rules, users, tokens. Read rarely, and dangerous when wrong.

Stage A gave all four the same neutral treatment. Stage B's job is to give each
one a distinct, appropriate density and a shared visual grammar, so an Operator
can tell at a glance which of the four they are looking at.

## 4. Proposed visual direction

**Working thesis: "Instrument panel, not dashboard."**

A dependency proxy is a measuring instrument. The interface should read like
calibrated equipment: quiet surfaces, hairline structure, one accent reserved
for action, and data that is dense without being cramped. Nothing floats;
nothing glows; depth comes from material contrast and keylines rather than
shadow.

Concretely, in order of how much they change the product:

### 4.1 One accent, spent carefully

Keep exactly one saturated brand colour and spend it on: primary commands,
focus, active navigation, and the "hit" signal. Everything else is neutral.
Status is a separate axis with its own four values (see §7) that are *not*
derived from the brand hue, so a healthy system never looks like a branded
system and a failure never looks like a call to action.

### 4.2 Structure from hairlines, not from cards

Dense operational data reads better as one continuous surface divided by rules
than as a field of floating cards. Proposal: a single **data rail** pattern
(already prototyped in the Stage A KPI grid) used consistently for KPI rows,
summary strips, and metadata blocks. Cards remain for things that are genuinely
discrete and movable, which is a small set.

### 4.3 A three-step density scale, chosen per surface

Rather than arbitrary paddings:

| Step | Row height | Use |
| --- | --- | --- |
| `comfortable` | 48px | Settings, License, onboarding, empty states |
| `compact` | 40px | Dashboard, Upstreams, Projects, Security summaries |
| `dense` | 32px | Log tables, audit trails, quarantine events, rule lists |

Density is a documented property of a surface, not a per-component accident.
The 40px interactive floor still applies to any control a pointer or finger
must hit, so `dense` applies to *rows*, not to buttons.

### 4.4 Numbers are a first-class type

Latency, sizes, rates, and versions get one tabular numeric treatment with
documented widths, so a value changing from `9 ms` to `1 240 ms` cannot move
the column beside it. This is a Stage A invariant (mono + `tabular-nums`); Stage
B turns it into a small, named type scale rather than a habit.

### 4.5 Motion stays functional

Entrances on newly keyed live rows, and nothing else. No page-level fades, no
scroll reveals, no hover theatre. The Stage A reduced-motion switch already
disables everything by name; Stage B keeps that property.

### 4.6 Charts get a real categorical palette

Stage A deliberately rendered the trend chart with status roles. Stage B should
introduce a five-step categorical ramp that is distinct from the status axis,
readable in both themes, distinguishable in greyscale, and never used to imply
health.

## 5. Brand

The Stage A mark is a neutral placeholder, as agreed. Stage B owns the identity.

Two candidate directions, to be chosen before any page work:

1. **Converging layers.** Direct and transitive dependencies resolving into one
   indexed store — a dependency graph compressed to its essential gesture.
   Technically honest, easy to render at 16px, and it survives being reduced to
   a monochrome mark.
2. **Spine and shelf.** The cache as an ordered, indexed structure: a
   continuous spine with ranked layers feeding it. Reads as infrastructure
   rather than as a graph.

Both are viable; the decision should be made on the mark at 16 of pixels, not at
presentation size, because that is where it will actually live — browser tab,
sidebar header, terminal-adjacent documentation.

## 6. Deliverables, in order

The brief's own order, restated as work products. Step 0 is not optional.

| # | Deliverable | Output |
| --- | --- | --- |
| 00 | Design system documents | `docs/design/{product-ui-principles,visual-direction,design-tokens,component-guidelines,page-redesign-plan}.md` |
| 01 | Design tokens | Token layer rewritten; light and dark; contrast-verified |
| 02 | Typography | Latin + CJK scale, numeric scale, display rules |
| 03 | Navigation / sidebar | Rail width, workspace switching, destination rail, mobile sheet |
| 04 | Page layout | Page header, section rhythm, rail-vs-fluid widths, empty/loading/error frames |
| 05 | Buttons and forms | Command hierarchy, form layout, validation presentation |
| 06 | Data tables | Column rhythm, header treatment, row density, pagination, toolbar |
| 07 | Status system | The four status roles, plus how the three outcome dimensions combine |
| 08 | Dashboard | Health, flowline, KPI rail, trends, attention queue |
| 09–13 | Requests, Upstreams, Policies, Security, Settings | Page by page |
| 14 | Portal | Quick Start workbench, Monitor |
| 15 | Logo / brand refinement | Final mark, favicon, wordmark |

Each step ships as its own commit and is verified with `make check`, with
`make verify` at each milestone.

## 7. The status system deserves its own design

This is Depsilo's most product-specific surface and the easiest to get wrong,
so it should be designed explicitly rather than inherited from a component
library.

Stage A established that the product reports three *independent* outcome
dimensions per request:

| Dimension | Normal | Attention |
| --- | --- | --- |
| Cache | `hit` (success) · `miss`, `unknown` (neutral — the request was still served) | — |
| Policy | `allow` (normal) | `deny` (explicit refusal) |
| Delivery | `upstream`, `completed` (normal) | `failed` (failure); `cancelled`, partial (warning) |

They combine. A request can be a cache miss, policy-allowed, and delivered
successfully — three different facts that must not collapse into one green
tick. Stage B should design:

- a compact **outcome triple** that fits in one table cell without becoming
  three chips;
- a rule for what the *dominant* signal is when a row is scanned;
- a documented mapping from each dimension to colour, icon, and text, so a
  future dimension cannot invent a fourth colour.

## 8. What Stage B must not break

These are the invariants the existing suite will enforce, written down here so
they are design inputs rather than surprises:

- **Query-state contract.** Every data region owns honest initial loading,
  initial error with Retry, successful-empty, stale-with-cached-data, and
  permission-denied states. A redesign may restyle them; it may not show an
  empty state while a query is pending, or present a failed refresh as healthy.
- **Mutation contract.** A pending mutation cannot be abandoned: destructive
  dialogs stay open until the request resolves, and failures stay in context.
- **Target sizes.** Every icon-only control is at least 40×40 in every state,
  including pending.
- **Layout floors.** 320px is a supported width on every surface; tables scroll
  inside their own named region; the document never scrolls horizontally.
- **Locales.** `zh` and `en` stay in parity, and the Admin is evaluated in
  Chinese as well as English.
- **Focus.** Visible focus in both themes; dialogs restore focus to their
  trigger; programmatically focused regions stay unringed.

## 9. Open questions for you

I have deliberately not decided these, because they are product calls rather
than design calls, and each one changes the work:

1. **Dark mode default.** Depsilo currently defaults to dark. Does Stage B keep
   that, or move to light-first with a dark companion?
2. **Default density.** Should Admin default to `compact`, or to `dense` with
   `compact` as an option?
3. **Brand equity.** Is there anything in the current identity worth carrying
   forward — the green, the wordmark, the name's "silo" metaphor — or is the
   whole identity open?
4. **Pro / License surfaces.** How much brand presence do entitlement and
   upgrade surfaces get, given the product is self-hosted and the Admin is an
   operational tool?
5. **Charts.** Does Stage B own a chart treatment (axes, tooltips, legend,
   empty/partial series), or is `recharts` styling out of scope for the first
   pass?
6. **Scope check.** Fifteen steps is a lot. Do you want the full sequence, or
   a first tranche (00–07: the system, the shell, and the data surfaces) with
   the page-by-page work re-planned once the system is visible in situ?

## 10. Proposed first commit

Nothing visual. The first Stage B commit should be the same kind of artifact as
the Stage A audit: the five `docs/design/` documents, written against the
audit's constraints and the questions above, so the visual decisions are
reviewable before any of them is implemented.
