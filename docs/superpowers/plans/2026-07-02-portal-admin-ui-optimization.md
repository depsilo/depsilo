# Portal Admin UI Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve Portal onboarding and Admin usability without changing Depsilo's existing design language.

**Architecture:** Keep changes inside the React frontend. Add small shared presentation states, make Admin layout responsive, and add a Portal setup path that reuses existing ecosystem/configuration components.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind v4 utilities, existing design tokens in `web/src/index.css`.

---

### Task 1: Responsive Admin Shell

**Files:**
- Modify: `web/src/admin/components/MainLayout.tsx`

- [ ] Add mobile menu state and close the menu on navigation.
- [ ] Replace fixed desktop-only sidebar offsets with responsive classes.
- [ ] Add mobile overlay, drawer behavior, and compact topbar controls.

### Task 2: Dashboard Responsive States

**Files:**
- Modify: `web/src/admin/pages/Dashboard.tsx`
- Modify: `web/src/components/EmptyState.tsx`

- [ ] Add reusable loading/error/empty state variants.
- [ ] Make dashboard metric grids responsive.
- [ ] Add dashboard-level loading and error feedback.

### Task 3: Portal Onboarding Flow

**Files:**
- Modify: `web/src/portal/PortalApp.tsx`
- Modify: `web/src/portal/pages/QuickStart.tsx`

- [ ] Add a three-step setup summary before detailed ecosystem cards.
- [ ] Add copy-state feedback for endpoint and recommended commands.
- [ ] Preserve current portal content and routes.

### Task 4: Verify Frontend

**Files:**
- Run: `cd web && npm run build`

- [ ] Fix TypeScript or Vite errors introduced by the UI changes.
- [ ] Summarize modified files and verification result.
