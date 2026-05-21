# Pricing surface + 14-day self-serve trial — Design

**Date:** 2026-05-21
**Status:** Approved design, ready for implementation plan
**Backlog source:** [docs/plans/2026-05-20-feature-backlog.md](../plans/2026-05-20-feature-backlog.md) §二 Tier A1 + A2
**Scope:** `depsilo/` + `depsilo-landingpage/` (two repos)

---

## 1. Summary

Turn the already-built Pro feature set (audit, SBOM, rules, security scan, projects) into a self-serve conversion funnel:

1. **Landing-side `/pro-trial` page** — replaces the current 404 that the existing Pricing.astro CTA already points to. Explains "install Depsilo → click Start trial in your admin." No email, no card, no signup.
2. **In-product `/admin/license` page** — single surface for entitlement: shows current status, lets admin start the 14-day trial, lets admin enter/change/remove a license key.
3. **Refined 402 paywall** — when a free user hits a Pro feature, return enough information for the frontend to surface a modal with a "Start trial" or "Buy Pro" CTA inline.
4. **Entitlement façade** — new thin layer combining license key + local trial state, so the rest of the codebase doesn't need to know about either source individually.

Payment integration is **explicitly deferred**. The trial state machine is purely local (no phone-home). Paid license keys continue to flow through the existing Lemon Squeezy validation client unchanged.

---

## 2. Motivation

The existing landing page already ships a polished 3-tier pricing section (Community / Pro / Team) with bilingual copy and a Pro CTA reading "Start 14-day free trial". That CTA links to `https://depsilo.com/pro-trial` — **a page that does not exist**. The conversion bottleneck today is not "no pricing UI" but "clicking the buy button goes nowhere."

In-product, the `RequirePro` middleware returns 402 with an upgrade URL but the frontend has no first-class flow for Pro-curious users to evaluate features. Users have to either find the landing page or guess at what Pro offers.

Goal of this iteration: close the loop with the minimum surface that lets a brand-new user go from "I see Depsilo has Pro features" to "I'm actively using all of them" in under 60 seconds, without touching a payment vendor.

---

## 3. Scope

### In scope

- New backend modules: `internal/trial/`, `internal/entitlement/`
- DB models: `TrialRecord`, `LicenseStorage`
- Extensions to existing `internal/license/license.go` (runtime key set/clear)
- New + modified admin API endpoints under `/api/v1/admin/license/*`
- New frontend page: `web/src/admin/pages/License.tsx`
- New frontend modal: `web/src/admin/components/ProRequiredModal.tsx`
- Sidebar nav entry + 402 axios interceptor
- New landing page: `depsilo-landingpage/src/pages/pro-trial.astro`
- i18n keys (~45 new keys × 2 locales) with placeholder validation via existing audit
- Backend unit + integration tests, frontend smoke tests

### Out of scope (deferred to later specs)

- Payment vendor integration (Lemon Squeezy vs Creem vs other) — keep current LS client untouched
- Email collection / lead capture
- Anti-abuse beyond "DB wipe re-grants trial" (machine fingerprint, server-side trial registry)
- Last-known-good license caching across restarts
- Per-feature tiering inside Pro (trial unlocks everything; future may carve a strict subset)
- Trial extensions, referral bonuses, education discounts (the Entitlement façade is the hook for these)
- Storybook / visual regression infra
- Auto-retry of original request after modal-driven trial activation

---

## 4. Architecture overview

### 4.1 Module map

| Path | Status | Responsibility |
|---|---|---|
| `internal/license/` | extended | Owns paid license key + Lemon Squeezy validation. Gains `SetKey` / `ClearKey` for runtime mutation. |
| `internal/trial/` | **new** | Owns the local 14-day trial state machine. ~80 LOC. |
| `internal/entitlement/` | **new** | Façade combining license + trial. ~50 LOC. Exposes unified `IsPro()` and `Status()`. |
| `internal/db/models.go` | extended | Adds `TrialRecord` and `LicenseStorage` GORM models. |
| `internal/middleware/` (RequirePro lives in license.go currently) | extended | Switches dependency from `*license.Manager` to `*entitlement.Checker`. 402 body grows a `trial_available` field. |
| `internal/api/admin/license.go` | extended | `GetStatus` returns `entitlement.Status`. Adds `ActivateTrial`, `SetKey`, `ClearKey` handlers. |
| `internal/server/server.go` | edited | Wires up `trial.NewManager` + `entitlement.NewChecker`, injects into `api.Deps`. |
| `web/src/admin/pages/License.tsx` | **new** | Single page for entitlement UI. ~180 LOC. |
| `web/src/admin/components/ProRequiredModal.tsx` | **new** | Global 402 modal. |
| `web/src/admin/components/MainLayout.tsx` | edited | Adds License entry to sidebar (管理 group). |
| `web/src/lib/api.ts` | extended | Adds `licenseApi.{status, revalidate, activateTrial, setKey, clearKey}`. |
| `web/src/i18n/{zh,en}.ts` | extended | New `license.*` namespace, ~45 keys × 2. |
| `depsilo-landingpage/src/pages/pro-trial.astro` | **new** (other repo) | Closes the 404 left by `Pricing.astro` CTA. |
| `depsilo-landingpage/src/i18n/locales/{zh-CN,en}.json` | extended | `pro_trial_*` keys × 2. |

### 4.2 Entitlement state machine

```
                            +---- enters valid paid key ----+
                            v                               |
   free  -- click Start trial -->  trial-active             |
                                       | 14d wall clock     |
                                       v                    |
                                 trial-expired  ---- enters valid paid key ---+
                                       ^                                       |
                                       | (terminal once trial_used = true)     v
                                       +---- paid key revoked/expired ----  pro-paid
                                                                              ^
                                                                              | (free user
                                                                              |  enters key)
                                                                            free
```

**Source precedence in `Checker.Status()` when both could apply:**

```
paid (license valid)  >  trial (active)  >  none
```

I.e. if a user activates a paid key while their trial is still running, the badge shows "Pro · Paid" and the trial record stays as historical (`trial_used: true`) but its remaining days don't matter anymore.

### 4.3 Server boot sequence

```go
// internal/server/server.go (additions, simplified)
licenseMgr := license.NewManager(cfg.License, db)        // loads key from DB if present, else cfg
trialMgr   := trial.NewManager(db)                       // loads existing TrialRecord (≤1 row)
checker    := entitlement.NewChecker(licenseMgr, trialMgr)

go licenseMgr.Start(ctx)                                  // existing 24h revalidate loop

// api.Deps gets `Entitlement *entitlement.Checker`
// RequirePro middleware now takes *entitlement.Checker
```

### 4.4 Request flow — Pro endpoint blocked

```
GET /api/v1/admin/projects (Pro)
  └─ RequirePro middleware: checker.IsPro() == false
     └─ 402 {"code":"PRO_REQUIRED",
              "message":"...",
              "upgrade":"https://depsilo.com/#pricing",
              "trial_available": true}
        └─ frontend axios interceptor catches 402+PRO_REQUIRED
           └─ dispatches "pro-required" event
              └─ <ProRequiredModal /> mounts UI:
                 "This feature requires Pro"
                 [Start 14-day free trial]  (because trial_available)
                 [Learn more]
                    └─ user clicks "Start..."
                       └─ POST /api/v1/admin/license/trial/activate
                          └─ checker.IsPro() == true now
                             └─ toast "Trial activated — click your action again"
                                └─ modal dismisses; user manually retries
```

No automatic retry of the original request. Documented decision; see §15.

---

## 5. Backend: data model

Both models are single-row tables. Uniqueness is enforced by manager-layer `sync.Mutex` + count check rather than DB constraint, because Depsilo is a single-instance deploy. When HA arrives (Tier B in the backlog), swap to `UNIQUE(singleton_lock)` columns.

### 5.1 `TrialRecord`

```go
type TrialRecord struct {
    ID            uint      `gorm:"primarykey"` // expected = 1
    ActivatedAt   time.Time `gorm:"not null"`
    ExpiresAt     time.Time `gorm:"not null"`   // = ActivatedAt + 14 days
    ActivatedBy   uint      `gorm:"index"`      // User.ID of activating admin
    ActivatedFrom string    `gorm:"size:64"`    // client IP, reserved for future abuse analysis
    CreatedAt     time.Time
}
```

All timestamps stored as UTC (per CLAUDE.md timezone rule; see commit ba15f12).

### 5.2 `LicenseStorage`

```go
type LicenseStorage struct {
    ID        uint      `gorm:"primarykey"` // expected = 1
    Key       string    `gorm:"size:256"`   // plaintext; license keys are not high-secrets
    UpdatedBy uint      `gorm:"index"`      // User.ID of admin who set it
    UpdatedAt time.Time
}
```

Stored in plaintext; license keys are not credentials (they're identifiers that get validated against the vendor). The mask is only for display in the UI to reduce shoulder-surfing leakage.

### 5.3 AutoMigrate

Both tables added to the existing `db.AutoMigrate(&...)` call list. Old installs upgrading: both tables created empty → trial is available, key falls back to `config.toml` if set.

---

## 6. Backend: `internal/trial/`

### 6.1 Manager

```go
type Manager struct {
    db     *gorm.DB
    mu     sync.RWMutex
    record *db.TrialRecord // cached after load; nil if not activated
}

func NewManager(database *gorm.DB) *Manager
    // SELECT TrialRecord LIMIT 1; cache into m.record

func (m *Manager) IsActive() bool
    // m.record != nil && time.Now().UTC() < m.record.ExpiresAt

func (m *Manager) IsUsed() bool
    // m.record != nil

func (m *Manager) Available() bool
    // !m.IsUsed()

func (m *Manager) Activate(ctx context.Context, userID uint, fromIP string) (*db.TrialRecord, error)
    // m.mu.Lock; defer Unlock
    // if m.record != nil → return ErrTrialAlreadyUsed
    // INSERT TrialRecord with ActivatedAt=now, ExpiresAt=now+14d
    // also: audit.Log(ctx, "trial.activated", ...)
    // m.record = newRecord; return it

func (m *Manager) Status() TrialStatus
    // internal struct used by entitlement.Checker; never serialized directly
```

### 6.2 Errors

```go
var (
    ErrTrialAlreadyUsed = errors.New("trial already used")
)
```

### 6.3 Audit hook

`Activate` writes an audit log entry via the existing `internal/audit.Logger`:

```
event:   "trial.activated"
actor:   userID
context: {"from_ip": "...", "expires_at": "..."}
```

---

## 7. Backend: `internal/entitlement/`

### 7.1 Source enum

```go
type Source string
const (
    SourceNone  Source = "none"
    SourceTrial Source = "trial"
    SourcePaid  Source = "paid"
)
```

### 7.2 Status struct

```go
type Status struct {
    IsPro              bool       `json:"is_pro"`
    Source             Source     `json:"source"`
    ExpiresAt          *time.Time `json:"expires_at,omitempty"`     // unified: paid expiry or trial expiry
    DaysLeft           int        `json:"days_left"`                // ceil from now to ExpiresAt; 0 if none
    TrialUsed          bool       `json:"trial_used"`
    TrialAvailable     bool       `json:"trial_available"`
    LicenseKeyMasked   string     `json:"license_key_masked,omitempty"`
    LicenseError       string     `json:"license_error,omitempty"`  // from license.Manager.status.Error
    LastChecked        time.Time  `json:"last_checked"`             // paid path's last validation time

    // Deprecated aliases for one-release backward compat. To be removed in 0.5.0.
    // See §16.2 for the deprecation timeline. Populated as:
    //   KeyMasked   = LicenseKeyMasked
    //   ActivatedAt = (paid path: license.activated_at) | (trial path: trial.ActivatedAt) | nil
    KeyMasked   string     `json:"key_masked,omitempty"`
    ActivatedAt *time.Time `json:"activated_at,omitempty"`
}
```

### 7.3 Checker

```go
type Checker struct {
    lic   *license.Manager
    trial *trial.Manager
}

func NewChecker(lic *license.Manager, t *trial.Manager) *Checker

func (c *Checker) IsPro() bool
    // c.lic.IsPro() || c.trial.IsActive()

func (c *Checker) Status() Status
    // Assemble unified view per precedence (paid > trial > none).
    // ExpiresAt and DaysLeft come from the active source.
    // TrialUsed / TrialAvailable always reflect trial state regardless of source.
```

`Checker` is the **only** dependency the rest of the code reaches for entitlement decisions. `RequirePro` middleware and admin handlers depend on `*Checker`, never on the underlying managers.

---

## 8. Backend: extensions to `internal/license/`

### 8.1 Constructor signature change

```go
// Before:
func NewManager(cfg config.LicenseConfig) *Manager

// After:
func NewManager(cfg config.LicenseConfig, db *gorm.DB) *Manager
```

Load order inside the constructor:

1. SELECT LicenseStorage LIMIT 1.
2. If a row exists → `m.key = row.Key`.
3. Else if `cfg.Key` is non-empty → `m.key = cfg.Key` (do **not** copy to DB).
4. Else → `m.key = ""` (free).

`DEPSILO_DEV_PRO=1` shortcut continues to work unchanged.

### 8.2 New methods

```go
func (m *Manager) SetKey(ctx context.Context, newKey string, userID uint) error
    // m.mu.Lock; defer Unlock
    // UPSERT LicenseStorage{ID:1, Key:newKey, UpdatedBy:userID, UpdatedAt:now}
    // m.key = newKey
    // m.doValidate() — synchronous; surfaces network error inline
    // audit.Log(ctx, "license.key_set", {actor: userID, masked: MaskKey(newKey)})
    // returns validation error if any, but the key is persisted regardless

func (m *Manager) ClearKey(ctx context.Context, userID uint) error
    // m.mu.Lock; defer Unlock
    // DELETE LicenseStorage WHERE ID=1
    // m.key = ""
    // m.status = LicenseStatus{IsPro:false, KeyMasked:"", LastChecked:now}
    // audit.Log(ctx, "license.key_cleared", {actor: userID})
```

`SetKey` returning a non-nil error while still persisting is intentional: lets the user save a key during a network outage and revalidate later. The HTTP handler turns this into a 200 response where the JSON body reflects the (failed) status.

### 8.3 `RequirePro` middleware change

```go
// Before: func RequirePro(mgr *Manager) gin.HandlerFunc
// After:  func RequirePro(checker *entitlement.Checker) gin.HandlerFunc

// 402 response body:
{
    "code":            "PRO_REQUIRED",
    "message":         "This feature requires Depsilo Pro.",
    "upgrade":         "https://depsilo.com/#pricing",
    "trial_available": checker.Status().TrialAvailable,
}
```

This function moves out of `internal/license/license.go` into `internal/entitlement/middleware.go` to follow the dependency direction.

---

## 9. Backend: API endpoints

All under `/api/v1/admin/license/` (existing prefix). All require admin role.

| Method | Path | Handler | Behavior |
|---|---|---|---|
| GET | `/status` | `GetStatus` (existing, rewritten) | Returns `entitlement.Status`. Backward-incompatible body change. |
| POST | `/revalidate` | `Revalidate` (existing) | Unchanged — fires `licenseMgr.Revalidate()`. |
| POST | `/trial/activate` | `ActivateTrial` (**new**) | Calls `trialMgr.Activate(...)`. Returns updated `Status`. See below for error mapping. |
| PUT | `/key` | `SetKey` (**new**) | Body `{"key":"depsilo-..."}`. Calls `licenseMgr.SetKey(...)`. Always returns 200 with updated `Status`. |
| DELETE | `/key` | `ClearKey` (**new**) | Calls `licenseMgr.ClearKey(...)`. Returns updated `Status`. |

### 9.1 Trial activate error mapping

```go
err := trialMgr.Activate(ctx, userID, c.ClientIP())
switch {
case errors.Is(err, trial.ErrTrialAlreadyUsed):
    c.JSON(409, {"code":"TRIAL_ALREADY_USED"})
case checker.Status().Source == SourcePaid: // pre-check before Activate
    c.JSON(409, {"code":"TRIAL_NOT_NEEDED"})
case err != nil:
    c.JSON(500, {"code":"INTERNAL"})
default:
    c.JSON(200, checker.Status())
}
```

The `TRIAL_NOT_NEEDED` pre-check happens before `Activate` is called, so a paid user accidentally hitting this endpoint doesn't consume their one-shot trial.

### 9.2 SetKey body validation

- `key` field required, non-empty after trim
- Length cap: 256 chars (matches DB column)
- No format validation beyond length — let the upstream validator reject malformed keys

---

## 10. Frontend: License page

Route: `/admin/license`. File: `web/src/admin/pages/License.tsx`. ~180 LOC.

### 10.1 Data fetching

Uses TanStack Query, key `["license", "status"]`. Refetch on window focus + every 60s.

Mutations:
- `activateTrial` → on success: invalidate status, toast "Trial activated"
- `setKey({ key })` → on success: invalidate status, toast based on resulting `is_pro`
- `clearKey` → on success: invalidate status, toast "Key removed"
- `revalidate` → on success: invalidate status

### 10.2 State-card visual contract

The page renders ONE of these four cards based on `status.source` × `status.trial_used`:

```
┌─ Free, trial available ──────────────────────────────────┐
│ Try Depsilo Pro free for 14 days                         │
│ Unlock audit logs, SBOM export, multi-project, security  │
│ scanning, and more. No credit card. No email.            │
│                                                           │
│  [Start 14-day free trial]                                │
│  Already have a license? Enter it below ↓                 │
│  Want to compare plans? [View pricing →]                  │
└──────────────────────────────────────────────────────────┘

┌─ Trial active ───────────────────────────────────────────┐
│ 🟢 Pro · Trial                                            │
│ {{count}} days remaining (expires {{date}})               │
│                                                           │
│ Enjoying it? [Buy Pro to keep access →]                   │
└──────────────────────────────────────────────────────────┘

┌─ Trial expired ──────────────────────────────────────────┐
│ ⚠️ Trial ended on {{date}}                                │
│ Pro features are now locked. Free tier features are       │
│ unaffected.                                               │
│                                                           │
│  [Buy Pro →]                                              │
└──────────────────────────────────────────────────────────┘

┌─ Paid Pro ───────────────────────────────────────────────┐
│ ✓ Pro · Activated                                         │
│ Key: depsilo-***   Expires: {{date}}                     │
│ Last checked: {{relative_time}}                           │
│                                                           │
│  [Revalidate]                                             │
└──────────────────────────────────────────────────────────┘
```

### 10.3 Key entry section

Collapsible. Default state by entitlement source:

- Free / Trial-expired: **expanded** (input visible, prominent)
- Trial active: **collapsed** (small "Already purchased? Activate your license [↓]" link)
- Paid: **collapsed** (small "[Change key] [Remove key]" link)

Form behavior:

1. User pastes key → submits. PUT `/license/key` always returns 200 with updated Status.
2. Two terminal states (no fuzzy "invalid vs network-error" distinction at the frontend):
   - **Success** (`status.is_pro === true`): clear input, refetch status, toast `license.key.success_toast`
   - **Saved-but-not-validated** (`status.is_pro === false`): keep input visible, inline notice `license.key.saved_pending_message` ("Key saved. Depsilo couldn't confirm it as Pro right now — this can mean an invalid key or a network issue. [Revalidate]"). If `status.license_error` is non-empty, append it verbatim in small text below the notice for operator diagnostics.
3. "Remove key" → AlertDialog confirmation (`license.key.remove_confirm_title` / `_body`) → on confirm, calls clearKey.

This collapses the distinction the backend can't reliably make (network failure vs invalid key — Lemon Squeezy returns 422 for both wrong key and unprocessable instance, etc.) into a single user-facing state with a revalidate escape hatch. Drops one i18n key (`license.key.invalid_message` is folded into `license.key.saved_pending_message`; `network_error_message` is no longer needed).

### 10.4 Feature comparison block

Static markup below the key section. Two columns (Free / Pro) listing 6-8 features each. Sourced from i18n (`license.features.free.*`, `license.features.pro.*`). Mirrors the landing page Pricing.astro feature lists but trimmed for in-product context (no marketing fluff).

---

## 11. Frontend: ProRequiredModal

File: `web/src/admin/components/ProRequiredModal.tsx`. Mounted once at AdminApp root.

### 11.1 Trigger plumbing

In `web/src/lib/api.ts`, extend the existing axios response interceptor:

```ts
api.interceptors.response.use(
  r => r,
  err => {
    if (err.response?.status === 401) { /* existing redirect to login */ }
    if (err.response?.status === 402 && err.response.data?.code === "PRO_REQUIRED") {
      window.dispatchEvent(new CustomEvent("depsilo:pro-required", {
        detail: err.response.data, // includes trial_available
      }))
    }
    return Promise.reject(err)
  }
)
```

`ProRequiredModal` listens for `depsilo:pro-required` via `useEffect(...window.addEventListener)`.

### 11.2 Modal content

Title: `license.paywall.title` ("This feature requires Depsilo Pro")

Body: `license.paywall.body` (one sentence explaining what Pro adds)

CTAs depend on `detail.trial_available`:

| `trial_available` | Primary CTA | Secondary CTA |
|---|---|---|
| `true` | "Start 14-day free trial" → call `activateTrial` → on success: dismiss + toast "Trial activated — please try your action again" | "Learn more" → router push `/admin/license` |
| `false` | "Buy Pro" → `window.open("https://depsilo.com/#pricing")` | "View license status" → router push `/admin/license` |

Always: "Dismiss" close button (top-right X + "Maybe later" text button).

### 11.3 No auto-retry

After successful trial activation from the modal, the modal closes and a toast tells the user to retry. The original failed request is **not** stored or re-issued. Rationale documented in §15.

---

## 12. Frontend: API client + i18n

### 12.1 `web/src/lib/api.ts` additions

```ts
export const licenseApi = {
  status:        () => api.get<EntitlementStatus>('/admin/license/status'),
  revalidate:    () => api.post('/admin/license/revalidate'),
  activateTrial: () => api.post<EntitlementStatus>('/admin/license/trial/activate'),
  setKey:        (key: string) => api.put<EntitlementStatus>('/admin/license/key', { key }),
  clearKey:      () => api.delete<EntitlementStatus>('/admin/license/key'),
}

export interface EntitlementStatus {
  is_pro: boolean
  source: 'none' | 'trial' | 'paid'
  expires_at?: string
  days_left: number
  trial_used: boolean
  trial_available: boolean
  license_key_masked?: string
  license_error?: string
  last_checked: string
}
```

### 12.2 i18n key inventory (new)

All keys must be added to both `web/src/i18n/zh.ts` and `web/src/i18n/en.ts`. Placeholder names (`{{count}}`, `{{date}}`, `{{relative_time}}`) must match across locales — enforced by the `make lint-i18n` placeholder check shipped in 973bcb7.

Approximate inventory (~45 keys):

```
license.title
license.subtitle

license.status.free
license.status.trial
license.status.trial_expired
license.status.pro

license.trial.start_button
license.trial.start_explainer
license.trial.days_left          ({{count}})
license.trial.expires_at         ({{date}})
license.trial.expired_message    ({{date}})

license.pro.activated
license.pro.key_label
license.pro.expires_at           ({{date}})
license.pro.last_checked         ({{relative_time}})
license.revalidate
license.buy_pro
license.view_pricing

license.key.title
license.key.placeholder
license.key.activate_button
license.key.change_button
license.key.remove_button
license.key.save_button
license.key.remove_confirm_title
license.key.remove_confirm_body
license.key.success_toast
license.key.saved_pending_message
license.key.try_revalidate

license.paywall.title
license.paywall.body
license.paywall.start_trial
license.paywall.buy_pro
license.paywall.learn_more
license.paywall.view_status
license.paywall.dismiss
license.paywall.trial_activated_toast

license.features.heading
license.features.free.* (6-8 items)
license.features.pro.* (6-8 items)
```

---

## 13. Landing page: `/pro-trial`

Repository: `depsilo-landingpage`. File: `src/pages/pro-trial.astro`. ~120 LOC.

### 13.1 Page structure

Reuses `BaseLayout.astro` (existing). Sections:

1. **Hero** — "Start your 14-day free trial" + subhead "All Pro features. No credit card. No email."
2. **Two-path block** — two side-by-side cards:
   - "Already running Depsilo?" → instruction: visit `http://<your-depsilo-host>/admin/license` → click "Start free trial"
   - "New here?" → `docker run -d ... -p 23333:23333 ...` command (copy-on-click button) → "Then come back to this page and follow the path above" + link to README
3. **What you'll get** — feature list (excerpted from `landing/Pricing.astro`'s Pro tier, 6-8 items)
4. **FAQ** (4 questions):
   - Why no email? (Because trial is 100% local; nothing phones home.)
   - What happens after 14 days? (Pro locks; Free tier unaffected.)
   - Can I try again? (One trial per install.)
   - How do I buy after the trial? (Link to landing #pricing.)

### 13.2 i18n additions

New keys in `src/i18n/locales/{zh-CN,en}.json` under `pro_trial_*` prefix:

```
pro_trial_hero_title
pro_trial_hero_subtitle
pro_trial_path_existing_title / _body / _cta
pro_trial_path_new_title / _body / _cta
pro_trial_features_heading + features.* list
pro_trial_faq_q1..q4 / a1..a4
```

### 13.3 No code change to `Pricing.astro`

The existing CTA `<a href="https://depsilo.com/pro-trial">` (line 158) just stops 404-ing once `pro-trial.astro` exists. No edits to `Pricing.astro` required.

---

## 14. Testing strategy

### 14.1 Backend unit tests (`*_test.go` colocated)

| File | Subject | Cases |
|---|---|---|
| `internal/trial/manager_test.go` | `trial.Manager` | First Activate succeeds; second returns `ErrTrialAlreadyUsed`; concurrent Activate via `sync.WaitGroup` — exactly one success; `IsActive` true if `ExpiresAt > now`, false if past, false if no record; `Available` mirrors `!IsUsed` |
| `internal/entitlement/checker_test.go` | `entitlement.Checker` | Source precedence table: (paid valid, trial active) → paid; (paid invalid, trial active) → trial; (paid valid, trial expired) → paid; (none, none) → none. Assembled `Status` fields correct in each. |
| `internal/license/license_test.go` | `license.Manager` SetKey / ClearKey | SetKey persists to DB, updates status, calls audit logger; SetKey with network-erroring validator still persists; ClearKey deletes row and resets status |

**Time handling:** No `Clock` interface. Tests construct `TrialRecord` rows with `ExpiresAt = now ± duration` and assert `IsActive` reads them correctly. No need to simulate 14-day passage.

### 14.2 Backend integration tests (`tests/integration/`)

1. **Full trial loop** — boot in-process Depsilo (existing harness) → POST `/trial/activate` → GET `/license/status` shows `source: "trial"` → call a `RequirePro`-protected endpoint (e.g. `POST /admin/projects` for SBOM) → 200
2. **Trial single-shot** — repeat activate → 409 `TRIAL_ALREADY_USED`
3. **Trial blocked while paid** — boot with mock-valid paid key (via test config) → POST `/trial/activate` → 409 `TRIAL_NOT_NEEDED`
4. **Key set/clear loop** — PUT `/license/key` (mock LS responds valid) → GET status `source: "paid"` → DELETE `/license/key` → GET status `source: "none"` (or `"trial"` if trial was active)
5. **402 trial_available flag** — free user (no trial used) hits Pro endpoint → 402 body includes `trial_available: true`. Then activate trial. Free user (trial used + expired) hits → 402 body includes `trial_available: false`.

### 14.3 Frontend tests

MVP scope: smoke tests via Playwright (existing in `testground/` ecosystem) against a real running Depsilo:

- License.tsx renders the correct card in each of 4 states (use API mock or actual backend in each state)
- Click "Start trial" from Free state → status card updates to Trial within 2s
- Submit invalid key → input retained + inline error shown
- 402 modal pops when a free user navigates to `/admin/projects` (Pro route) and triggers a protected API call

**No Storybook / no per-component snapshot tests in this iteration** — keep test surface minimal.

### 14.4 i18n CI

`make lint-i18n` runs as part of `make lint`. The placeholder mismatch check shipped in 973bcb7 will fail CI if any new `{{count}}` / `{{date}}` placeholder drifts between zh.ts and en.ts.

---

## 15. Edge cases & behavior table

| Scenario | Behavior |
|---|---|
| Already-paid user calls POST `/trial/activate` | 409 `TRIAL_NOT_NEEDED`; trial record not created |
| Trial in progress + valid paid key entered | Source flips to `paid`; trial record preserved with `trial_used=true`; trial days remaining ignored |
| Paid license expires after trial was used | `source=none, trial_used=true, trial_available=false`; UI shows "Trial used, license expired" + Buy CTA |
| User wipes DB volume and restarts | `TrialRecord` table empty → trial available again. **Accepted MVP behavior.** Will tighten when payment integration arrives. |
| `config.toml` has key X, UI sets key Y | DB wins. Y is used. To revert to X: ClearKey via UI, then restart (so X re-loads from config). |
| Boot with network down | License validation fails in background, `status.error` set, IsPro stays false until next validation succeeds. **Boot does not block.** |
| Server clock skew (NTP correction backward) | Trial may end early or late by the skew amount. Accepted; assume ops maintains NTP. |
| Trial activation succeeds | Writes audit log `trial.activated` with `actor`, `from_ip`, `expires_at` |
| SetKey / ClearKey | Writes audit log `license.key_set` / `license.key_cleared` with `actor` (and masked key for set, never plaintext) |
| User in 402 modal clicks "Start trial" while activation is already in flight (double-click) | Button disabled while mutation pending; subsequent 409 races land on already-active state, modal dismisses anyway |
| Failed request that caused 402 is not auto-retried | User sees toast "Trial activated — please try your action again". Decision: storing & replaying the original request adds significant complexity (queue, idempotency concerns, race with status refetch) for marginal UX gain. Revisit if user feedback flags this. |
| Frontend new, backend old (during rolling deploy) | New frontend reads new fields with `?.` optional chaining; old fields still present for backward shape compatibility where overlap exists |
| Backend new, frontend old | Old frontend reads `is_pro` and `key_masked` (kept as alias of `license_key_masked` for one release cycle) — see §16 |

---

## 16. Rollout & migration

### 16.1 DB migration

`AutoMigrate` creates `TrialRecord` + `LicenseStorage`. Empty by default. No data migration needed.

### 16.2 API response compat

`GET /api/v1/admin/license/status` body changes shape. To avoid breaking any hypothetical external script polling this endpoint, **include both old and new field names in the response for one release**:

```json
{
  "is_pro": true,
  "source": "paid",
  "expires_at": "...",
  "days_left": 89,
  "license_key_masked": "depsilo-***",
  "key_masked": "depsilo-***",        // alias, deprecated
  "activated_at": "...",              // legacy, deprecated
  "last_checked": "...",
  "trial_used": false,
  "trial_available": true
}
```

In a follow-up release (e.g. 0.5.0), drop `key_masked` and `activated_at`. CHANGELOG.md must note the deprecation.

### 16.3 Rollback

If a critical bug ships, rollback to prior version is safe:
- The two new DB tables are simply ignored by the old code (no FK references, no read paths)
- No user is harmed because no payment has happened
- Trial records become orphaned but inert; if you re-deploy the new version later they pick up where they left off

### 16.4 Release notes

CHANGELOG entry (v0.4.0 target):

> **Added:** `/admin/license` page for self-serve 14-day Pro trial activation and license key management.
> **Added:** `POST /api/v1/admin/license/trial/activate`, `PUT /api/v1/admin/license/key`, `DELETE /api/v1/admin/license/key`.
> **Changed:** `GET /api/v1/admin/license/status` response body. Old fields kept as aliases; will be removed in 0.5.0.
> **Changed:** 402 `PRO_REQUIRED` response now includes `trial_available` boolean.
> **Landing page:** New `/pro-trial` page (closes the existing 404 from Pricing CTA).

---

## 17. Future hooks (not in this iteration)

- `Entitlement.Checker` is the extension point for additional sources (promo codes, education licenses, site licenses). New sources implement the same `IsPro()`-contributing pattern and slot in via `NewChecker(...sources)` — minor refactor when needed.
- `TrialRecord.ActivatedFrom` IP is reserved for future abuse analysis (rate-limit trial activation by IP / subnet)
- `LicenseStorage` could gain `LastValidatedAt` + `LastValidatedStatus` columns to implement last-known-good caching across restarts
- Payment vendor integration (Creem or other) plugs into `license.Manager.validate` — only the client.go file changes
- Email capture, weekly trial-ending reminder email, post-trial nurture sequence — all server-side, all touch the trial.Manager via webhooks

---

## 18. Open questions

None blocking implementation. Items left for the implementation plan to surface:

- Exact React component patterns to match existing admin page style (look at Settings.tsx, Users.tsx)
- Whether to use a single `Dialog` from shadcn for the remove-key confirmation or a custom `AlertDialog`
- Exact `pro-trial.astro` styling — match landing page's existing typography scale and Stripe-style shadow tokens
