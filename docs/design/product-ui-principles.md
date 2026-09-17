# Depsilo UI Principles

> Stage B step 00. This document states what the interface is *for* before
> anything is drawn. Values and composition follow in
> [visual-direction.md](visual-direction.md) and
> [design-tokens.md](design-tokens.md).
>
> Sources: [PRODUCT.md](../../PRODUCT.md), [CONTEXT.md](../../CONTEXT.md),
> [docs/adr](../adr/), and the Stage A
> [audit](../refactor/shadcn-ui-audit.md).

## 1. Who the interface serves

| Persona | Opens the UI? | What they need from it |
| --- | --- | --- |
| **Operator** | Yes, repeatedly | To run the service: configure Upstreams, read health, investigate requests, review policy decisions, manage Operators and tokens |
| **Buyer** | Rarely, sometimes never | Evidence that the product is trustworthy: auditability, honest state, no surprises |
| **End User** | No | Only that their `pip install` / `npm install` is fast and correct |

The primary design target is the **Operator**: an individual developer,
technical founder, or platform engineer in a small team, who is not an
artifact-infrastructure specialist and does not want to become one.

This has a direct consequence for every screen: **the UI has to answer "what do
I do next" without a manual.** An Operator who has to read documentation to
understand a screen has been failed by the screen.

End Users never see the interface. Anything the UI does to make the *request
path* slower, noisier, or more fragile is a cost paid by people who cannot even
see the product.

## 2. What the interface is for

Four jobs, in the order an Operator meets them:

1. **Connect.** Get one package manager pointed at Depsilo in minutes, from the
   Portal, before any account exists.
2. **Know the state.** Answer "is this working right now, and is anything about
   to break" in a glance, many times a day.
3. **Investigate.** Reconstruct what happened to a specific request: was it a
   cache hit, was it allowed, did it arrive.
4. **Change things safely.** Adjust settings, Upstreams, rules, and access
   without being able to break the request path by accident, and be able to see
   what changed afterwards.

## 3. Principles

These translate the product principles in
[PRODUCT.md](../../PRODUCT.md#product-principles) into interface terms. When a
visual decision and a principle disagree, the principle wins.

### 3.1 Small-team operability first

Every surface justifies its setup, maintenance, and cognitive cost. Prefer one
obvious control over two clever ones. Prefer a default that is right for a
single-instance deployment over one that scales to a fleet that does not exist.

- No screen requires specialist artifact-infrastructure knowledge.
- Configuration exposes what it changes and what it costs.
- A feature that cannot be explained on its own screen is not ready to ship.

### 3.2 The request path is the product

The interface exists to protect installs, not to be looked at. Interface weight
is not free: a heavier Portal is a heavier binary, a slower first paint, and
worse behaviour on the small machines this product is meant to run on.

- No decorative payload, no ornamental data, no chart that exists to fill space.
- Every number on screen must be a number the Operator can act on.

### 3.3 Enforce transparently

A refusal is an explanation, not an error. When Depsilo blocks a package, the
UI's job is to say *what* was refused, *by which rule*, and *what an Operator
can do next*.

- A policy decision is never presented as a bare "blocked".
- Every security decision is auditable and attributable.
- Reversing a decision is an explicit Operator action, never a side effect.

### 3.4 Honest state, always

The interface never claims more than it knows. This is the principle most
easily lost in a redesign, because ambiguity is usually prettier than
precision.

- Not yet observed, unknown, and zero are three different things and must look
  different.
- A failed refresh never renders as healthy. Stale data is labelled stale.
- A capability that is not shipped is not shown as if it were. The
  minimum-release-age gate is safety-disabled today: the UI must not present it
  as an available switch.

### 3.5 Preserve self-hosted trust

The Operator runs this on their own hardware, with their own data, and can read
every line of it.

- No telemetry-shaped UI, no "share anonymous usage".
- No upsell inside the control plane. The commercial model is undecided; a
  self-hosted Operator's own admin panel is not a place to test pricing
  hypotheses.
- No dark patterns: nothing is destructive without saying so, and nothing
  important is hidden to make a metric look better.

## 4. Rhythm: one row, not three

Admin uses **one row and control rhythm at 40px**. There is no comfortable /
compact / dense tier.

This is derived, not arbitrary:

1. The accessibility contract requires every icon-only control to be at least
   40×40 in every state, and it is enforced across every Admin route by
   `e2e/admin-axe.spec.ts`.
2. Depsilo's data rows carry icon actions — 17 of them across the log, user,
   rule, quarantine, project, and upstream surfaces.
3. A 32px row cannot contain a 40px control. A "dense" tier would therefore
   either violate the contract or be silently re-expanded to 40px by its own
   contents.

So rows and controls share one number, and the interface gains its sense of
space from **page padding and section rhythm**, not from variable row heights.
A setup surface (Portal Quick Start, Setup) is airy because of its page
padding; an operational surface (Logs, Dashboard) is tight because of its
grouping. Neither needs a second row height.

If a future read-only surface genuinely benefits from 32px, it must carry text
actions only, and the decision is made then, on that surface, with that
constraint.

## 5. What this interface is not

| Not this | Because |
| --- | --- |
| A marketing landing page | The Portal is a setup workbench. Its first screen is Quick Start, not a hero. |
| A BI dashboard | Charts support a decision; they are not the product. |
| A monitoring product | Service health is one input to an Operator's day, not a wall of graphs. |
| An enterprise console | The deployment is single-instance and self-hosted; scale theatre is dishonest. |
| A showcase for visual effects | Effects compete with the data for attention. |

## 6. Product-meaning rules

These belong in the interface layer (`components/app`), not in the generic
primitives (`components/ui`):

- The difference between a cache hit and a cache miss.
- The difference between "policy refused this" and "the upstream failed".
- The difference between "not observed yet" and "observed as zero".
- Ownership and attribution of a security decision.
- The entitlement boundary, stated precisely and without sales language.

If a colour or a badge exists to express one of these, it is a product token and
it is documented in [design-tokens.md](design-tokens.md) as such.

## 7. How to tell whether a screen obeys this document

For any screen, answer these without hedging:

1. What is the Operator here to do, in one sentence?
2. What does the first viewport prove about the service?
3. If the data is missing, stale, denied, or partially failed, what does the
   screen say — and is it different from "empty" and from "zero"?
4. Which single element is the primary action, and what makes it unmistakable?
5. Does it work at 320px, in Chinese, in light and dark, with a keyboard, and
   with reduced motion?

A screen that cannot answer all five is not finished, regardless of how it
looks.
