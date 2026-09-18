# Depsilo Page Redesign Plan

> Stage B step 00. The order of work, what each surface must achieve, and what
> must not regress. Principles are in
> [product-ui-principles.md](product-ui-principles.md).
>
> Scope: the Portal (anonymous), Setup (first run), and all 18 authenticated
> Admin routes.

## 1. Order

The system comes first and is proved on one vertical slice before any page sweep
begins. Two amendments to the brief's sequence are marked **◆**.

| Step | Work | Why here | Exit criteria |
| --- | --- | --- | --- |
| 0 | This document set | Decisions that are expensive to change later | Reviewed |
| 1 | Design tokens | Everything else reads them | Both themes pass the gates in [design-tokens.md](design-tokens.md#9-verification-gates) |
| 2 | Typography | Latin and Chinese together, including the numeric scale | Every text role has one token; no negative tracking |
| 3 | Navigation | The rail, the destination rail, the mobile sheet | Switching workspaces never loses the Operator's place |
| 4 | Page layout | Page header, section rhythm, rail vs fluid width | One layout grammar for all 18 routes |
| 5 | **◆ Status system** | The most product-specific surface, and the one most likely to be wrong | One signal per row, and every refusal explains itself |
| 6 | Data tables | The densest surface, and the one the status system renders into | Wide = table, narrow = list, both with identical information |
| 7 | Forms | The most-used interactive surface after tables | Every field composes from `field`; every failure is inline |
| 8 | **◆ Dashboard — review gate** | One page that exercises the KPI rail, the request flow, status, charts, and a feed | The system is signed off before 10 pages are rebuilt on it |
| 9 | Requests: Access Logs, Audit Logs, Metadata Refreshes | Investigative work: filter, scan, open a detail | Filters survive reload and Back |
| 10 | Upstreams | Operational inventory and health | Same status vocabulary as Portal Monitor |
| 11 | Policy: Package Rules, Security, Quarantine | The enforcement story | A refusal is explainable from any surface that shows it |
| 12 | Cache: Artifacts, Index Cache, Compiler Cache | Storage operations, highest-risk destructive actions | Destructive actions are confirmed and reported honestly |
| 13 | Instance: Settings, Users, License | Rarely visited, high consequence | Configured vs effective, and what needs a restart, are unmistakable |
| 14 | Portal and Setup | Anonymous surfaces; Setup is a live multi-step submit | Both meet the Admin accessibility standard |
| 15 | Brand | Last, because it depends on everything above | Resolves the conflict in [visual-direction.md](visual-direction.md#8-brand) |

**Why step 5 moved ahead of 6.** The status system is what the tables render.
Designing tables first means designing the cell that holds a status before
deciding what a status is — and Depsilo's status is three independent
dimensions, not one, so the cell is the hard part.

**Why step 8 is a gate.** Dashboard compresses every hard problem into one
screen: a divided KPI rail, a three-stage request flow, per-series chart
legibility, a live feed, and an attention queue at 320px. If the system cannot
carry that page, it cannot carry nineteen. Fixing it there is one page of
rework; discovering it later is ten.

## 2. Per-surface intent

What each surface must achieve, in the Operator's terms.

### Portal (anonymous)

| Surface | Intent | First viewport must show |
| --- | --- | --- |
| Quick Start `/` | Get one package manager pointed at Depsilo in minutes, with no account | The ecosystem/manager choice, the resulting configuration, and how to verify it |
| Monitor `/monitor` | Is the service healthy, and are my Upstreams reachable | Health, hit rate, and the Upstreams needing attention |

The Portal is a workbench, not a landing page. It is the first 90 seconds of
the product and is read by someone who has never seen Depsilo.

### Setup (first run)

A single-page security gate: verify the bootstrap token if required, create the
first administrator, write the durable configuration, restart. It is not a
product tour, and it does not get a welcome step.

### Admin

| Step | Surface | Intent | First viewport must show |
| --- | --- | --- | --- |
| 8 | Dashboard `/admin` | Is anything wrong right now, and what is the request path doing | Service state, the four KPIs, and the first thing needing attention |
| 8 | Bandwidth `/admin/bandwidth` | Where is the traffic and what is it costing | Saved bytes and the top consumers |
| 9 | Access Logs `/admin/logs` | What happened to this request | Filters, then rows that can be opened |
| 9 | Audit Logs `/admin/audit` | The same, in domain terms, with policy outcomes | Filters, then outcomes |
| 9 | Metadata Refreshes `/admin/upstream-updates` | Which upstream metadata refreshes are failing | Result filter and episode history |
| 10 | Upstreams `/admin/upstreams` | Which Upstreams exist, are they reachable, and what do I change | Search, health filter, and the inventory |
| 11 | Package Rules `/admin/rules` | What am I refusing, and why | Rules and a way to test one |
| 11 | Security `/admin/security` | What does Depsilo know about vulnerability in what I serve | The intelligence view and its score |
| 11 | Quarantine `/admin/quarantine` | What was withheld, and what needs approval | Events needing a decision |
| 12 | Artifacts `/admin/cache` | What is cached, and what can I safely remove | Size, pressure, and the destructive actions with their consequences |
| 12 | Index Cache `/admin/indexes` | Which package indexes are cached | Per-ecosystem cache state |
| 12 | Compiler Cache `/admin/compile-cache` | Is ccache/sccache working, and who can use it | Status, hit rate, and credentials |
| 13 | Settings `/admin/settings` | What is configured, what is effective, what needs a restart | The distinction between configured and applied |
| 13 | Users `/admin/users` | Who can act, and with what tokens | Operators and their access |
| 13 | License `/admin/license` | What this deployment is entitled to | The entitlement, stated plainly |
| 6 | Projects `/admin/projects` | Which projects connect here, and how | The project list |
| 8 | Attention `/admin/attention` | The merged queue behind the Dashboard's attention rail | Nothing needing attention, or what does |
| — | Connect `/admin/connect` | First-project onboarding loop | The chosen ecosystem's configuration and its verification |

## 3. Product honesty requirements

These come from [PRODUCT.md](../../PRODUCT.md) and are interface obligations,
not copy preferences:

- The **minimum-release-age gate is safety-disabled**. It must not appear as an
  available switch, and a positive threshold must not read as usable.
- The **commercial model is undecided**. Licence and Pro surfaces state the
  current entitlement precisely and carry no pricing, trial, or tier language
  presented as durable truth.
- **General-purpose artifact repository support has not shipped.** It is not
  presented as a current capability.
- **Delivered, unreleased, and planned capabilities are described by actual
  release state**, never as uniformly available.

## 4. What must not regress

The redesign changes how states look. It does not remove states, weaken
invariants, or change behaviour that the suite encodes.

| Invariant | Where it is enforced |
| --- | --- |
| Five query states per data region, distinguishable | `admin-query-states.spec.ts` |
| A pending mutation cannot be abandoned | `admin-dialog-actions.spec.ts` |
| Every icon-only control is ≥40×40 in every state | `fixtures/a11y.ts`, asserted by the Admin, Portal, and Setup specs |
| No letter spacing, anywhere, in both locales | `fixtures/a11y.ts`, asserted by the Admin, Portal, and Setup specs |
| No horizontal document scroll at 320px | `fixtures/a11y.ts`, responsive specs |
| Filters and pagination survive reload, Back, and deep links | log and settings workspace specs |
| The same health rule drives every health display | Portal Monitor and Admin Upstreams specs |
| Chinese and English stay in parity | `make lint-i18n` |
| An Admin route never falls through to the Portal SPA | routing specs |

## 5. Verification per step

| Step | Focused | Before handoff |
| --- | --- | --- |
| 1–2 | token contrast script, axe on one route | `make check` |
| 3–4 | `admin-shell`, `admin-page-layout`, `admin-responsive-grids` | `make check` |
| 5 | `admin-tables-actions`, `admin-policy-status`, `admin-quarantine-mobile` | `make check` |
| 6 | `admin-tables-actions`, `admin-query-states` | `make check` |
| 7 | `admin-forms`, `admin-settings` | `make check` |
| 8 | `admin-contrast`, `admin-responsive-grids`, `admin-page-layout`, `admin-recent-downloads` | `make verify` |
| 9–13 | the owning spec per page | `make check`, then full `make test-ui` |
| 14 | `portal-axe`, `portal-redesign`, `portal-monitor`, `auth-setup-state` | `make verify` |
| 15 | `light-theme-canvas`, brand asset checks | `make verify` |

Every step also re-runs the accessibility contract (`e2e/admin-axe.spec.ts` for
Admin, `e2e/portal-axe.spec.ts` for the Portal and Setup, both built on
`e2e/fixtures/a11y.ts`), because it is the check that catches a redesign
quietly breaking a floor.

## 6. Commit strategy

One commit per step, and never a migration commit mixed with a redesign commit:

```text
design(ui): establish design tokens
design(type): establish the typographic scale
design(shell): redesign navigation
design(shell): redesign page layout
design(status): establish the status system
design(tables): redesign data tables
design(forms): redesign form composition
design(dashboard): redesign the overview surface
design(admin): redesign the request and log surfaces
design(admin): redesign the upstream surfaces
design(admin): redesign the policy surfaces
design(admin): redesign the cache surfaces
design(admin): redesign the instance surfaces
design(portal): redesign the portal and setup
design(brand): resolve the Depsilo identity
```

## 7. Definition of done for Stage B

- Every surface in section 2 is redesigned against
  [visual-direction.md](visual-direction.md), not against the old UI.
- No legacy token, class, or component is reintroduced.
- All invariants in section 4 still hold, verified by `make verify`.
- The design documents match what shipped; where they do not, the documents are
  corrected in the same change.
- The brand conflict in [visual-direction.md](visual-direction.md#8-brand) is
  resolved and `PRODUCT.md`, `docs/brand/`, and the code agree.
- `DESIGN.md` is rewritten a final time to describe the redesigned system, and
  this `docs/design/` set becomes the working reference.
