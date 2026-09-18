# Depsilo Design Set

> The reasoning layer of the design system. [DESIGN.md](../../DESIGN.md) is the
> one-page entry point and the current implementation contract; this set
> explains *why* each value and rule is what it is, so a future change can be
> argued against the reason rather than against the number.

## Where each fact lives

One home per fact. If two documents disagree, the one listed here as
authoritative wins and the other is corrected in the same change.

| Fact | Authoritative document |
| --- | --- |
| What the system is right now: layers, token roles, component inventory, states, verification commands | [DESIGN.md](../../DESIGN.md) |
| What the interface is for, who it serves, and what each surface must prove in its first viewport | [product-ui-principles.md](product-ui-principles.md) |
| Why it looks the way it does, what it borrows from which references, and what it refuses to be | [visual-direction.md](visual-direction.md) |
| Every value: colour, type, space, radius, elevation, motion, charts, status semantics, and the gates that hold them | [design-tokens.md](design-tokens.md) |
| How components compose, what each layer may do, and what must never be built again | [component-guidelines.md](component-guidelines.md) |
| The mark, the palette it fixes, and where the masters live | [../brand/README.md](../brand/README.md) |

The values are in `web/src/index.css`; where this set and the stylesheet
disagree, the stylesheet is right and this set is what gets corrected.

## Reading order

1. [product-ui-principles.md](product-ui-principles.md) — what the interface is
   for, before anything is drawn.
2. [visual-direction.md](visual-direction.md) — the direction, and the refusals
   that keep it from drifting into a template.
3. [design-tokens.md](design-tokens.md) — the values and the floors they must
   clear.
4. [component-guidelines.md](component-guidelines.md) — how a surface is
   assembled from the layers.
5. [DESIGN.md](../../DESIGN.md) — the same system stated as a contract,
   including what must not regress.

## How the system is enforced

The standard is not a style preference: most of it is a test. The contrast
floors, and the gate that holds them, are in
[design-tokens.md §9](design-tokens.md#9-verification-gates); the invariants
that must survive any change, each next to the spec that enforces it, are in
[DESIGN.md](../../DESIGN.md#what-must-not-regress). Nothing in this set is
enforced *here* — a rule that is only written down is a rule that drifts.

## How a change lands

1. Find the fact's home in the table above and change it there.
2. Change the stylesheet or component that implements it in the same commit.
3. Correct any document that now disagrees — including this set. A stale
   document is a defect, not a leftover.
4. Run `make check`; run `make verify` for anything that moves a token, a
   primitive, or a page frame.

For a component change specifically, the review checklist is
[component-guidelines.md §12](component-guidelines.md#12-the-check-before-merging-a-component-change).

Completed step-by-step plans are deliberately **not** kept here. Git history
holds them; keeping them in the tree made fresh sessions follow obsolete file
names and decisions. See [docs/README.md](../README.md).
