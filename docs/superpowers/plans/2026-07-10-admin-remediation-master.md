# Admin Control Plane Remediation Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Execute and integrate the four Admin remediation plans so Settings, Upstreams, permissions, API contracts, and all 13 Admin routes ship as one tested control plane without losing the existing dirty-worktree changes.

**Architecture:** The four detailed plans remain the implementation source for their subsystems; this plan is the serialization and release contract across their shared files. Backend authority and DTO work lands first, shared UI foundations land second, domain pages consume the completed contracts third, and documentation plus full-system verification close the work.

**Tech Stack:** Go 1.25.6, Gin 1.12, GORM 1.31, SQLite, zap 1.27, React 19, TypeScript 5.9, TanStack Query 5, Base UI 1.3, Playwright, axe-core, Git.

## Global Constraints

- The confirmed design in `docs/superpowers/specs/2026-07-10-admin-control-plane-ui-remediation-design.md` is the semantic authority. The four subsystem plans provide implementation detail but cannot weaken the design.
- Execute from the current worktree because it contains user changes that must be retained. Do not reset, checkout, clean, stash, or replace files from `HEAD`.
- Task 1 records the exact starting SHA, dirty paths, content hashes, normalized zero-context diff hashes, and diff summaries in `docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md`. A task may stage only its new hunks; use each subsystem plan's exact `git add -p` instructions and inspect `git diff --cached` before every commit.
- The Shared-File Order table is also the concurrency lock list. Never run two implementation agents concurrently when their tasks can touch the same listed path, and do not hand a path to its next owner until the prior owner's commit and focused gate pass.
- Settings persistence is file-authoritative; ordinary active Upstreams are database-authoritative; Docker and extra indexes remain config-authoritative.
- The server-issued Principal is the only permission authority. Frontend hiding is presentation, never authorization.
- Do not merge, rebase, or push during subsystem execution. Finish all gates and final review first; integration delivery is a separate explicit action.
- Every subsystem task keeps its own TDD cycle and commit. Do not squash intermediate review points while implementation is active.
- No new frontend runtime dependency is allowed. The only new frontend packages are the approved development dependencies `@playwright/test` and `@axe-core/playwright`.
- Do not clear unrelated lint debt or refactor untouched adapters. Modified files must be typed, formatted, and lint-clean within the declared scope.
- `web/admin-remediation-eslint-files.txt` is the sole frontend ESLint scope authority after Plan 04 Task 11 creates
  it. Master gates must consume its exact paths and must never reconstruct lint scope from Git diffs.

---

## Source Plans

1. `docs/superpowers/plans/2026-07-10-admin-remediation-01-contracts-permissions.md` - eight tasks for drift fixes, Principal, authorization, invariants, typed contracts, and `usePrincipal`.
2. `docs/superpowers/plans/2026-07-10-admin-remediation-02-settings-control-plane.md` - seven tasks for lossless TOML persistence, shared log level, Settings API/UI, and persistence verification.
3. `docs/superpowers/plans/2026-07-10-admin-remediation-03-upstream-registry.md` - nine tasks for bootstrap metadata, immutable Pools, Registry mutations/workers, active-only assembly, typed API/UI, and migration copy.
4. `docs/superpowers/plans/2026-07-10-admin-remediation-04-ui-system.md` - twelve tasks for browser fixtures, shared primitives, shell, responsive pages, truthful states, accessibility, and visual verification.

## Shared-File Order

| Shared path | Required owner order |
| --- | --- |
| `internal/api/router.go` | Plan 01 Task 6 -> Plan 02 Task 5 -> Plan 03 Task 7 |
| `internal/api/admin/upstream.go` | Plan 01 Task 6 credential mask -> Plan 03 Task 6 Registry handler |
| `internal/server/server.go` | Plan 02 Task 3 -> Plan 02 Task 5 -> Plan 03 Task 7 |
| `internal/config/loader.go` | Plan 02 Task 1 -> Plan 03 Task 1 |
| `internal/config/config.go` | Plan 03 Task 1, partial-staged around the starting worktree edit |
| `web/src/lib/adminApi.types.ts` | Plan 01 Task 8 -> Plan 02 Task 6 -> Plan 03 Task 8 |
| `web/src/lib/adminApi.types.type-test.ts` | Plan 01 Task 8 -> Plan 03 Task 8 |
| `web/src/lib/api.ts` | Plan 01 Task 8 -> Plan 02 Task 6 -> Plan 03 Task 8 |
| `web/src/admin/AdminApp.tsx` | Plan 01 Task 8 -> Plan 04 Task 6 -> Plan 04 Task 11 if lint requires an edit |
| `web/src/admin/pages/Security.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 3, 5, 8, 9, 10, 11 |
| `web/src/admin/components/MainLayout.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 6 and 11 |
| `web/src/admin/pages/Users.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 9, 10, and 11 |
| `web/src/admin/pages/AccessLogs.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 9, 10, and 11 |
| `web/src/admin/pages/AuditLogs.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 9, 10, and 11 |
| `web/src/admin/pages/Projects.tsx` | Plan 01 Task 8 -> Plan 04 Tasks 9, 10, and 11 |
| `web/src/admin/pages/Settings.tsx` | Plan 02 Task 6, partial-staged around the starting worktree edit; Plan 04 Task 7 owns only `WebhookTab.tsx` |
| `web/src/admin/pages/Upstreams.tsx` | Plan 03 Task 9 -> Plan 04 Tasks 9, 10, 11 |
| `web/src/i18n/en.ts`, `web/src/i18n/zh.ts` | Plan 04 Task 6 -> Plan 02 Task 6 -> Plan 04 Tasks 7, 9, and 11 if lint requires an edit |
| `config.example.toml` | Plan 02 Task 1 -> Plan 03 Task 9 -> Task 8 of this plan |
| `README.md`, `docs/README_zh.md` | Plan 03 Task 9 -> Task 8 of this plan |
| `DESIGN.md` | Plan 04 Task 12 -> Task 8 of this plan only if the final control-plane cross-link is missing |

This table lists every path handed between subsystem plans and every starting-dirty path a subsystem intentionally edits. Files repeated only inside one subsystem plan remain serialized by that plan's numbered task order; Plan 04 Task 11 runs after all earlier Plan 04 owners and may edit only files its touched-file ESLint gate reports.

### Starting-Dirty Overlap Ledger

The following paths are both dirty at the confirmed starting boundary and intentionally edited by this remediation. A path may use `preserved` when none of its starting hunks enter the task commit, `adopted` when all starting changes are deliberately integrated, or `mixed` when reviewed related hunks are integrated while unrelated starting hunks remain unstaged. Every actual commit touching one of these paths must append one review record to the baseline artifact before committing; an unrecorded touch fails Task 9.

| Starting-dirty path | Ordered remediation owners | Initial disposition | Required semantic guard |
| --- | --- | --- | --- |
| `internal/config/config.go` | Plan 03 Task 1 | `mixed` | SQLite remains the only documented driver; supply-chain tamper comments remain truthful |
| `internal/api/router.go` | Plan 01 Task 6 -> Plan 02 Task 5 -> Plan 03 Task 7 | `mixed` | transparent integration-prompt and open-source governance comments remain |
| `internal/server/server.go` | Plan 02 Tasks 3 and 5 -> Plan 03 Task 7 | `mixed` | tamper/LRU semantics remain while the shared log level and Registry are wired |
| `web/src/admin/pages/Settings.tsx` | Plan 02 Task 6 | `adopted` | PostgreSQL and `db_dsn` remain absent; the typed Settings contract replaces the old page |
| `web/src/lib/api.ts` | Plan 01 Task 8 -> Plan 02 Task 6 -> Plan 03 Task 8 | `mixed` | Audit remains open source and deprecated entitlement aliases remain compatibility-only |
| `web/src/i18n/en.ts` | Plan 04 Task 6 -> Plan 02 Task 6 -> Plan 04 Tasks 7, 9, and conditional 11 | `preserved` | current Community/Pro and 14-ecosystem copy remains |
| `web/src/i18n/zh.ts` | Plan 04 Task 6 -> Plan 02 Task 6 -> Plan 04 Tasks 7, 9, and conditional 11 | `preserved` | current Community/Pro and 14-ecosystem copy remains |
| `config.example.toml` | Plan 02 Task 1 -> Plan 03 Task 9 -> master Task 8 | `preserved` | SQLite-only, quarantine, blocklist, and tamper guidance remains alongside control-plane migration copy |
| `README.md` | Plan 03 Task 9 -> master Task 8 | `preserved` | Go 1.25.6, blocklist, tamper, setup persistence, and roadmap corrections remain |
| `docs/README_zh.md` | Plan 03 Task 9 -> master Task 8 | `preserved` | the equivalent Chinese release/setup/supply-chain corrections remain |
| `DESIGN.md` | Plan 04 Task 12 -> master Task 8 | `mixed` | the current Instrument reference remains and gains the Admin control-plane rules |
| `CHANGELOG.md` | master Task 8 | `preserved` | existing tamper/blocklist corrections remain alongside the remediation entry |
| `docs/self-test-checklist.md` | master Task 8 | `mixed` | the current deployment/metrics/backup rewrite remains while obsolete cold-Pool guidance is replaced |

The ledger follows the actual subagent-driven workflow in two phases. Before the implementer commit, record only `pending-review` evidence: the staged diff SHA, intended disposition, and guard. After that commit exists, run the specification reviewer and then the quality reviewer. Only when both approve does the controller append an `approved` row bound to the source commit SHA and create a separate bookkeeping commit. A path is not handed to its next owner before that approval commit lands.

```bash
bash -euo pipefail <<'BASH'
record_overlap_pending() {
  local path=$1 subject=$2 disposition=$3 guard_id=$4
  local artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
  local expected_guard staged_sha recorded_utc record

  case "$disposition" in preserved|adopted|mixed) ;; *) printf 'invalid overlap disposition: %s\n' "$disposition" >&2; return 1;; esac
  git diff --cached --quiet -- "$path" && { printf 'overlap path is not staged: %s\n' "$path" >&2; return 1; }
  expected_guard=$(awk -F '\t' -v path="$path" '$1 == path {print $4}' \
    < <(awk '/<!-- OVERLAP_PATHS_BEGIN -->/{capture=1;next}/<!-- OVERLAP_PATHS_END -->/{capture=0}capture' "$artifact"))
  [[ -n "$expected_guard" && "$guard_id" == "$expected_guard" ]] || { printf 'guard mismatch for %s: expected %s, got %s\n' "$path" "$expected_guard" "$guard_id" >&2; return 1; }
  if awk -F '\t' -v path="$path" -v subject="$subject" '$1 == path && $2 == subject {found=1} END {exit !found}' \
    < <(awk '/<!-- OVERLAP_PENDING_BEGIN -->/{capture=1;next}/<!-- OVERLAP_PENDING_END -->/{capture=0}capture' "$artifact"); then
    printf 'overlap pending evidence already recorded for %s in %s\n' "$path" "$subject" >&2
    return 1
  fi
  staged_sha=$(git diff --cached --binary -- "$path" | sha256sum | awk '{print $1}')
  recorded_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  record=$(printf '%s\t%s\t%s\t%s\t%s\tpending-review\t%s' "$path" "$subject" "$disposition" "$staged_sha" "$guard_id" "$recorded_utc")
  awk -v record="$record" '/<!-- OVERLAP_PENDING_END -->/{print record} {print}' "$artifact" >"$artifact.tmp"
  mv "$artifact.tmp" "$artifact"
  git add "$artifact"
  git diff --cached --check
  git diff --cached -- "$path" "$artifact"
}

# Plan 02 Task 6 intentionally integrates the related removal already present in Settings.
record_overlap_pending \
  web/src/admin/pages/Settings.tsx \
  'fix(admin): show truthful settings application state' \
  adopted \
  settings-no-postgres
BASH
```

Expected before the implementer commit: the staged source diff and baseline artifact show one `pending-review` row with the exact staged SHA. This row makes no claim about reviewer outcome.

After the owning commit and both successful reviewer turns, the controller runs:

```bash
bash -euo pipefail <<'BASH'
approve_overlap_commit() {
  local subject=$1 approval_subject=$2 spec_ref=$3 quality_ref=$4
  local artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
  local baseline_sha commit approved_utc pending_count=0
  [[ "$spec_ref" == spec-approved:* && "$quality_ref" == quality-approved:* ]]
  baseline_sha=$(awk '$1 == "Baseline" && $2 == "SHA:" {print $3}' "$artifact")
  mapfile -t commits < <(git log --format='%H%x09%s' --reverse "$baseline_sha"..HEAD | awk -F '\t' -v subject="$subject" '$2 == subject {print $1}')
  (( ${#commits[@]} == 1 )) || { printf 'expected one source commit for %s, found %d\n' "$subject" "${#commits[@]}" >&2; return 1; }
  commit=${commits[0]}
  approved_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)

  while IFS=$'\t' read -r path pending_subject disposition staged_sha guard_id state _recorded_utc; do
    [[ "$pending_subject" == "$subject" ]] || continue
    [[ "$state" == pending-review ]]
    commit_sha=$(git show --format= --binary "$commit" -- "$path" | sha256sum | awk '{print $1}')
    [[ "$commit_sha" == "$staged_sha" ]] || { printf 'committed diff does not match reviewed staged diff: %s\n' "$path" >&2; return 1; }
    if awk -F '\t' -v path="$path" -v subject="$subject" '$1 == path && $2 == subject {found=1} END {exit !found}' \
      < <(awk '/<!-- OVERLAP_APPROVALS_BEGIN -->/{capture=1;next}/<!-- OVERLAP_APPROVALS_END -->/{capture=0}capture' "$artifact"); then
      printf 'overlap approval already exists for %s in %s\n' "$path" "$subject" >&2
      return 1
    fi
    record=$(printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s' "$path" "$subject" "$approval_subject" "$disposition" "$commit" "$staged_sha" "$guard_id" "$spec_ref" "$quality_ref" "$approved_utc")
    awk -v record="$record" '/<!-- OVERLAP_APPROVALS_END -->/{print record} {print}' "$artifact" >"$artifact.tmp"
    mv "$artifact.tmp" "$artifact"
    ((pending_count += 1))
  done < <(awk '/<!-- OVERLAP_PENDING_BEGIN -->/{capture=1;next}/<!-- OVERLAP_PENDING_END -->/{capture=0}capture' "$artifact")

  (( pending_count > 0 )) || { printf 'no pending overlap rows for %s\n' "$subject" >&2; return 1; }
  git add "$artifact"
  git diff --cached --check
  git diff --cached -- "$artifact"
  git commit -m "$approval_subject"
}

approve_overlap_commit \
  'fix(admin): show truthful settings application state' \
  'chore(plan): approve Plan 02 Task 6 overlaps' \
  'spec-approved:Plan02-T6' \
  'quality-approved:Plan02-T6'
BASH
```

Expected after review: the source commit remains unchanged; every pending row for that subject has one approval row with the source commit SHA, matching staged SHA, guard, two distinct review references, and approval time; the bookkeeping commit contains only the baseline artifact.

Use these exact calls in the corresponding owner immediately before its commit. Run one call per path. The conditional Plan 04 Task 11 and master Task 8 rows run only when that path is present in `git diff --cached --name-only`; every other row is mandatory.

| Exact commit subject | Starting-dirty path | Disposition | Guard ID |
| --- | --- | --- | --- |
| `fix(auth): enforce admin route capabilities` | `internal/api/router.go` | `preserved` | `router-existing-semantics` |
| `fix(admin-ui): consume typed principal contracts` | `web/src/lib/api.ts` | `mixed` | `api-open-audit` |
| `feat(config): define validated admin settings model` | `config.example.toml` | `preserved` | `config-example-supply-chain` |
| `feat(logging): share atomic server log level` | `internal/server/server.go` | `preserved` | `server-tamper-semantics` |
| `feat(admin): expose truthful settings contract` | `internal/api/router.go` | `preserved` | `router-existing-semantics` |
| `feat(admin): expose truthful settings contract` | `internal/server/server.go` | `preserved` | `server-tamper-semantics` |
| `fix(admin): make shell status and navigation accessible` | `web/src/i18n/en.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): make shell status and navigation accessible` | `web/src/i18n/zh.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): show truthful settings application state` | `web/src/admin/pages/Settings.tsx` | `adopted` | `settings-no-postgres` |
| `fix(admin): show truthful settings application state` | `web/src/lib/api.ts` | `mixed` | `api-open-audit` |
| `fix(admin): show truthful settings application state` | `web/src/i18n/en.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): show truthful settings application state` | `web/src/i18n/zh.ts` | `preserved` | `i18n-current-product-copy` |
| `feat(upstream): persist registry seed state` | `internal/config/config.go` | `mixed` | `config-existing-semantics` |
| `feat(server): assemble active upstream registry routes` | `internal/api/router.go` | `mixed` | `router-existing-semantics` |
| `feat(server): assemble active upstream registry routes` | `internal/server/server.go` | `mixed` | `server-tamper-semantics` |
| `refactor(web): type upstream registry API` | `web/src/lib/api.ts` | `mixed` | `api-open-audit` |
| `fix(admin): reflect live upstream registry state` | `config.example.toml` | `preserved` | `config-example-supply-chain` |
| `fix(admin): reflect live upstream registry state` | `README.md` | `preserved` | `readme-supply-chain` |
| `fix(admin): reflect live upstream registry state` | `docs/README_zh.md` | `preserved` | `zh-readme-supply-chain` |
| `fix(admin): rebuild webhook workflow` | `web/src/i18n/en.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): rebuild webhook workflow` | `web/src/i18n/zh.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): make tables and row actions accessible` | `web/src/i18n/en.ts` | `preserved` | `i18n-current-product-copy` |
| `fix(admin): make tables and row actions accessible` | `web/src/i18n/zh.ts` | `preserved` | `i18n-current-product-copy` |
| `refactor(admin): type all remediated UI paths` | `web/src/i18n/en.ts` if staged | `preserved` | `i18n-current-product-copy` |
| `refactor(admin): type all remediated UI paths` | `web/src/i18n/zh.ts` if staged | `preserved` | `i18n-current-product-copy` |
| `test(admin): enforce accessibility and responsive matrix` | `DESIGN.md` | `mixed` | `design-instrument` |
| `docs: document admin control plane operations` | `README.md` | `preserved` | `readme-supply-chain` |
| `docs: document admin control plane operations` | `docs/README_zh.md` | `preserved` | `zh-readme-supply-chain` |
| `docs: document admin control plane operations` | `config.example.toml` if staged | `preserved` | `config-example-supply-chain` |
| `docs: document admin control plane operations` | `DESIGN.md` if staged | `mixed` | `design-instrument` |
| `docs: document admin control plane operations` | `docs/self-test-checklist.md` | `mixed` | `self-test-operator-truth` |
| `docs: document admin control plane operations` | `CHANGELOG.md` | `preserved` | `changelog-supply-chain` |

Use these exact approval packages after the two reviewer turns. The conditional Plan 04 Task 11 row is omitted when that commit stages neither i18n file.

| Source commit subject | Approval bookkeeping subject | Spec review ref | Quality review ref |
| --- | --- | --- | --- |
| `fix(auth): enforce admin route capabilities` | `chore(plan): approve Plan 01 Task 6 overlaps` | `spec-approved:Plan01-T6` | `quality-approved:Plan01-T6` |
| `fix(admin-ui): consume typed principal contracts` | `chore(plan): approve Plan 01 Task 8 overlaps` | `spec-approved:Plan01-T8` | `quality-approved:Plan01-T8` |
| `feat(config): define validated admin settings model` | `chore(plan): approve Plan 02 Task 1 overlaps` | `spec-approved:Plan02-T1` | `quality-approved:Plan02-T1` |
| `feat(logging): share atomic server log level` | `chore(plan): approve Plan 02 Task 3 overlaps` | `spec-approved:Plan02-T3` | `quality-approved:Plan02-T3` |
| `feat(admin): expose truthful settings contract` | `chore(plan): approve Plan 02 Task 5 overlaps` | `spec-approved:Plan02-T5` | `quality-approved:Plan02-T5` |
| `fix(admin): make shell status and navigation accessible` | `chore(plan): approve Plan 04 Task 6 overlaps` | `spec-approved:Plan04-T6` | `quality-approved:Plan04-T6` |
| `fix(admin): show truthful settings application state` | `chore(plan): approve Plan 02 Task 6 overlaps` | `spec-approved:Plan02-T6` | `quality-approved:Plan02-T6` |
| `feat(upstream): persist registry seed state` | `chore(plan): approve Plan 03 Task 1 overlaps` | `spec-approved:Plan03-T1` | `quality-approved:Plan03-T1` |
| `feat(server): assemble active upstream registry routes` | `chore(plan): approve Plan 03 Task 7 overlaps` | `spec-approved:Plan03-T7` | `quality-approved:Plan03-T7` |
| `refactor(web): type upstream registry API` | `chore(plan): approve Plan 03 Task 8 overlaps` | `spec-approved:Plan03-T8` | `quality-approved:Plan03-T8` |
| `fix(admin): reflect live upstream registry state` | `chore(plan): approve Plan 03 Task 9 overlaps` | `spec-approved:Plan03-T9` | `quality-approved:Plan03-T9` |
| `fix(admin): rebuild webhook workflow` | `chore(plan): approve Plan 04 Task 7 overlaps` | `spec-approved:Plan04-T7` | `quality-approved:Plan04-T7` |
| `fix(admin): make tables and row actions accessible` | `chore(plan): approve Plan 04 Task 9 overlaps` | `spec-approved:Plan04-T9` | `quality-approved:Plan04-T9` |
| `refactor(admin): type all remediated UI paths` | `chore(plan): approve Plan 04 Task 11 overlaps` | `spec-approved:Plan04-T11` | `quality-approved:Plan04-T11` |
| `test(admin): enforce accessibility and responsive matrix` | `chore(plan): approve Plan 04 Task 12 overlaps` | `spec-approved:Plan04-T12` | `quality-approved:Plan04-T12` |
| `docs: document admin control plane operations` | `chore(plan): approve master Task 8 overlaps` | `spec-approved:Master-T8` | `quality-approved:Master-T8` |

Expected: every source commit touching a listed overlap includes only `pending-review` rows. The controller creates the corresponding approval bookkeeping commit only after both reviewers explicitly confirm that unrelated starting hunks remain unstaged or every adopted starting hunk retains its intended semantics.

### Task 1: Freeze the execution baseline and prove the current starting point

**Files:**
- Create during execution: `docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md`

**Interfaces:**
- Produces: a committed baseline artifact containing the starting commit, complete dirty-path list, per-path content hash, normalized zero-context diff hash, and diff summary.
- Produces: the rule that only one task owns a shared path at a time.

- [ ] **Step 1: Load the execution skills before editing**

Read `subagent-driven-development`, `git-workflow-and-versioning`, and `test-driven-development`. Use a fresh implementation agent and two-stage review for each subsystem task; keep all agents in this worktree so the existing edits remain visible.

- [ ] **Step 2: Prove the index and starting worktree are auditable**

Run:

```bash
git rev-parse HEAD
git status --short
git diff --check
git rev-list --left-right --count origin/master...HEAD
git diff --cached --exit-code
git ls-files --error-unmatch \
  docs/superpowers/specs/2026-07-10-admin-control-plane-ui-remediation-design.md \
  docs/superpowers/plans/2026-07-10-admin-remediation-01-contracts-permissions.md \
  docs/superpowers/plans/2026-07-10-admin-remediation-02-settings-control-plane.md \
  docs/superpowers/plans/2026-07-10-admin-remediation-03-upstream-registry.md \
  docs/superpowers/plans/2026-07-10-admin-remediation-04-ui-system.md \
  docs/superpowers/plans/2026-07-10-admin-remediation-master.md
```

Expected: `git diff --check` exits zero; the index is empty; the confirmed specification and all five plans are tracked; the branch is ahead only by those documentation commits; all pre-existing modified/deleted/untracked paths remain listed.

- [ ] **Step 3: Generate and commit the durable execution baseline**

Run this exact script from the repository root. It captures the worktree before creating the artifact, so the artifact never records itself as starting dirt.

````bash
bash -euo pipefail <<'BASH'
artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
tmp_paths=$(mktemp)
tmp_status=$(mktemp)
tmp_artifact=$(mktemp)
trap 'rm -f "$tmp_paths" "$tmp_status" "$tmp_artifact"' EXIT

git diff --cached --exit-code
git status --short >"$tmp_status"
{
  git diff --name-only -z --diff-filter=ACDMRTUXB HEAD
  git ls-files --others --exclude-standard -z
} | sort -zu >"$tmp_paths"

baseline_sha=$(git rev-parse HEAD)
status_sha=$(sha256sum "$tmp_status" | awk '{print $1}')
recorded_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)

overlap_paths=(
  internal/config/config.go
  internal/api/router.go
  internal/server/server.go
  web/src/admin/pages/Settings.tsx
  web/src/lib/api.ts
  web/src/i18n/en.ts
  web/src/i18n/zh.ts
  config.example.toml
  README.md
  docs/README_zh.md
  DESIGN.md
  CHANGELOG.md
  docs/self-test-checklist.md
)
declare -A overlap_owners overlap_initial overlap_guard baseline_dirty
overlap_owners[internal/config/config.go]=Plan03-T1
overlap_owners[internal/api/router.go]=Plan01-T6\>Plan02-T5\>Plan03-T7
overlap_owners[internal/server/server.go]=Plan02-T3\>Plan02-T5\>Plan03-T7
overlap_owners[web/src/admin/pages/Settings.tsx]=Plan02-T6
overlap_owners[web/src/lib/api.ts]=Plan01-T8\>Plan02-T6\>Plan03-T8
overlap_owners[web/src/i18n/en.ts]=Plan04-T6\>Plan02-T6\>Plan04-T7\>Plan04-T9\>Plan04-T11-if-edited
overlap_owners[web/src/i18n/zh.ts]=Plan04-T6\>Plan02-T6\>Plan04-T7\>Plan04-T9\>Plan04-T11-if-edited
overlap_owners[config.example.toml]=Plan02-T1\>Plan03-T9\>Master-T8
overlap_owners[README.md]=Plan03-T9\>Master-T8
overlap_owners[docs/README_zh.md]=Plan03-T9\>Master-T8
overlap_owners[DESIGN.md]=Plan04-T12\>Master-T8
overlap_owners[CHANGELOG.md]=Master-T8
overlap_owners[docs/self-test-checklist.md]=Master-T8

for path in "${overlap_paths[@]}"; do overlap_initial["$path"]=preserved; done
overlap_initial[internal/config/config.go]=mixed
overlap_initial[internal/api/router.go]=mixed
overlap_initial[internal/server/server.go]=mixed
overlap_initial[web/src/admin/pages/Settings.tsx]=adopted
overlap_initial[web/src/lib/api.ts]=mixed
overlap_initial[DESIGN.md]=mixed
overlap_initial[docs/self-test-checklist.md]=mixed

overlap_guard[internal/config/config.go]=config-existing-semantics
overlap_guard[internal/api/router.go]=router-existing-semantics
overlap_guard[internal/server/server.go]=server-tamper-semantics
overlap_guard[web/src/admin/pages/Settings.tsx]=settings-no-postgres
overlap_guard[web/src/lib/api.ts]=api-open-audit
overlap_guard[web/src/i18n/en.ts]=i18n-current-product-copy
overlap_guard[web/src/i18n/zh.ts]=i18n-current-product-copy
overlap_guard[config.example.toml]=config-example-supply-chain
overlap_guard[README.md]=readme-supply-chain
overlap_guard[docs/README_zh.md]=zh-readme-supply-chain
overlap_guard[DESIGN.md]=design-instrument
overlap_guard[CHANGELOG.md]=changelog-supply-chain
overlap_guard[docs/self-test-checklist.md]=self-test-operator-truth

while IFS= read -r -d '' path; do baseline_dirty["$path"]=1; done <"$tmp_paths"
for path in "${overlap_paths[@]}"; do
  [[ ${baseline_dirty[$path]+present} ]] || { printf 'declared overlap path is not starting dirty: %s\n' "$path" >&2; exit 1; }
done

{
  printf '# Admin Remediation Execution Baseline\n\n'
  printf 'Baseline SHA: %s\n' "$baseline_sha"
  printf 'Recorded UTC: %s\n' "$recorded_utc"
  printf 'Starting status SHA-256: %s\n' "$status_sha"
  printf '\n## Starting Status\n\n```text\n'
  cat "$tmp_status"
  printf '```\n\n## Dirty Paths\n\n<!-- DIRTY_PATHS_BEGIN -->\n'
  while IFS= read -r -d '' path; do
    [[ "$path" != *$'\n'* && "$path" != *$'\t'* ]] || { printf 'baseline path contains a tab or newline: %q\n' "$path" >&2; exit 1; }
    printf '%s\n' "$path"
  done <"$tmp_paths"
  printf '<!-- DIRTY_PATHS_END -->\n\n'
  printf '## Dirty Evidence\n\n'
  printf 'Fields are tab-separated: kind, status, content SHA-256, normalized diff SHA-256, diff summary, path.\n\n'
  printf '```text\n<!-- DIRTY_EVIDENCE_BEGIN -->\n'

  while IFS= read -r -d '' path; do
    status=$(git status --short -- "$path" | head -n 1 | cut -c 1-2)
    if git ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
      kind=tracked
      if [[ -e "$path" ]]; then
        content_sha=$(sha256sum -- "$path" | awk '{print $1}')
      else
        content_sha=DELETED
      fi
      diff_sha=$(
        git diff --no-color --unified=0 --binary HEAD -- "$path" |
          awk '/^diff --git / || /^index [0-9a-f]/ || /^--- (a\/|\/dev\/null)/ || /^\+\+\+ (b\/|\/dev\/null)/ || /^@@ / {next} {print}' |
          sha256sum | awk '{print $1}'
      )
      summary=$(git diff --numstat HEAD -- "$path" | awk -F '\t' '{printf "add=%s,del=%s;", $1, $2}')
      [[ -n "$summary" ]] || summary=no-numstat
    else
      kind=untracked
      content_sha=$(sha256sum -- "$path" | awk '{print $1}')
      diff_sha=$content_sha
      summary="untracked-bytes=$(wc -c <"$path" | tr -d ' ')"
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$kind" "$status" "$content_sha" "$diff_sha" "$summary" "$path"
  done <"$tmp_paths"

  printf '<!-- DIRTY_EVIDENCE_END -->\n```\n\n'
  printf '## Starting-Dirty Overlap Policies\n\n'
  printf 'Fields are tab-separated: path, ordered owners, initial disposition, semantic guard ID.\n\n'
  printf '```text\n<!-- OVERLAP_PATHS_BEGIN -->\n'
  for path in "${overlap_paths[@]}"; do
    printf '%s\t%s\t%s\t%s\n' "$path" "${overlap_owners[$path]}" "${overlap_initial[$path]}" "${overlap_guard[$path]}"
  done
  printf '<!-- OVERLAP_PATHS_END -->\n```\n\n'
  printf '## Pending Overlap Evidence\n\n'
  printf 'Before committing, each owner appends one row: path, exact subject, disposition, staged diff SHA-256, semantic guard ID, pending-review state, and UTC record time.\n\n'
  printf '```text\n<!-- OVERLAP_PENDING_BEGIN -->\n'
  printf '<!-- OVERLAP_PENDING_END -->\n```\n\n'
  printf '## Approved Overlap Reviews\n\n'
  printf 'After both reviewer turns approve, the controller appends one row: path, exact source subject, approval bookkeeping subject, disposition, source commit SHA, staged diff SHA-256, guard ID, spec review reference, quality review reference, and UTC approval time.\n\n'
  printf '```text\n<!-- OVERLAP_APPROVALS_BEGIN -->\n'
  printf '<!-- OVERLAP_APPROVALS_END -->\n```\n'
} >"$tmp_artifact"

mv "$tmp_artifact" "$artifact"
git add "$artifact"
git diff --cached --check
git diff --cached -- "$artifact"
git commit -m "chore(plan): record admin remediation execution baseline"
BASH
````

Expected: one commit containing only the baseline artifact. Every path from the pre-artifact `git status --short` appears between both marker pairs with concrete evidence values, and `git diff --cached --check` passes.

- [ ] **Step 4: Run narrow preflight builds without changing files**

Run:

```bash
go test ./internal/config ./internal/upstream ./internal/middleware ./internal/api/... ./internal/server
cd web
npm run type-check
npm run build
cd ..
python3 scripts/i18n-audit.py
```

Expected: PASS. If an existing dirty change causes a failure, record the exact command and output, debug it without reverting the dirty change, and do not attribute it to a remediation task until a failing task test reproduces it.

- [ ] **Step 5: Confirm no task commit is staged**

Run: `git diff --cached --exit-code`

Expected: exit zero. The only Task 1 commit is `chore(plan): record admin remediation execution baseline`.

### Task 2: Land contracts, Principal, permissions, and typed client foundation

**Files:**
- Execute: `docs/superpowers/plans/2026-07-10-admin-remediation-01-contracts-permissions.md`

**Interfaces:**
- Produces: explicit Security/Logs/Audit/Projects DTOs, canonical query aliases, log export, `middleware.Principal`, read/write groups, JWT-only refresh, self/last-admin invariants, `adminApi.types.ts`, and `usePrincipal().canWrite`.
- Required by: every later router mutation and every capability-aware page.

- [ ] **Step 1: Execute Plan 01 Tasks 1-4 in order**

Use the failing contract tests and exact implementations in Plan 01. Do not begin auth changes until all four drift tasks pass their focused Go tests.

- [ ] **Step 2: Execute Plan 01 Tasks 5-7 in order**

Preserve the exact route classification: every Admin GET and `POST /admin/rules/test` are read; every other Admin mutation is write. Keep `adminRead`, `adminWrite`, `proRead`, and `proWrite` as the route-group names consumed by Plan 03.

- [ ] **Step 3: Execute Plan 01 Task 8**

The hook's wire DTO field is `principal.can_write`; its application convenience field is `usePrincipal().canWrite`. Do not add Settings or Upstream DTOs yet.

- [ ] **Step 4: Run the Plan 01 gate**

Run:

```bash
go test -race ./internal/middleware ./internal/api/...
go test ./...
cd web
npm run type-check
npm run build
cd ..
python3 scripts/i18n-audit.py
```

Expected: PASS; readonly JWT reads return 200, readonly writes return 403, admin/readwrite token writes are allowed, disabled/stale-role JWTs are re-evaluated, and API tokens cannot refresh.

- [ ] **Step 5: Review the staged and committed delta**

Run:

```bash
bash -euo pipefail <<'BASH'
artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
baseline_sha=$(awk '$1 == "Baseline" && $2 == "SHA:" {print $3}' "$artifact")
git cat-file -e "$baseline_sha^{commit}"

expected=(
  "chore(plan): record admin remediation execution baseline"
  "fix(admin): align audit query contract"
  "fix(admin): make project responses explicit"
  "fix(admin): align security API contract"
  "feat(admin): add filtered access log export"
  "feat(auth): resolve current request principal"
  "fix(auth): enforce admin route capabilities"
  "chore(plan): approve Plan 01 Task 6 overlaps"
  "fix(admin): secure user responses and administrator invariants"
  "fix(admin-ui): consume typed principal contracts"
  "chore(plan): approve Plan 01 Task 8 overlaps"
  "docs(plan): reconcile executed admin subjects"
)
mapfile -t actual < <(git log --format=%s --reverse "$baseline_sha"..HEAD)
if (( ${#actual[@]} != ${#expected[@]} )); then
  printf 'expected %d commits after baseline, found %d\n' "${#expected[@]}" "${#actual[@]}" >&2
  printf 'actual subjects:\n%s\n' "${actual[*]}" >&2
  exit 1
fi
for i in "${!expected[@]}"; do
  [[ "${actual[$i]}" == "${expected[$i]}" ]] || {
    printf 'commit %d: expected %q, found %q\n' "$i" "${expected[$i]}" "${actual[$i]}" >&2
    exit 1
  }
done

unexpected=$(git diff --name-only "$baseline_sha"..HEAD -- internal/config internal/upstream internal/server web/src/admin/pages/Settings.tsx web/src/admin/pages/Upstreams.tsx)
[[ -z "$unexpected" ]] || { printf 'premature Settings/Registry paths:\n%s\n' "$unexpected" >&2; exit 1; }
printf 'Plan 01 exact business commit count: 8; overlap approval commits: 2; plan corrections: 1\n'
BASH
```

Expected: PASS with `Plan 01 exact business commit count: 8; overlap approval commits: 2; plan corrections: 1`; the eight business subjects remain exact and ordered, each overlap owner is followed by its artifact-only approval commit, the post-baseline plan correction is explicit, and no Settings/Registry implementation path has changed.

### Task 3: Land the Settings backend control plane

**Files:**
- Execute: `docs/superpowers/plans/2026-07-10-admin-remediation-02-settings-control-plane.md`, Tasks 1-5 only.

**Interfaces:**
- Produces: canonical Settings snapshots/patches, lossless atomic TOML writes, shared `zap.AtomicLevel`, `config.Store`, strict Settings handler, and `api.Deps.ConfigStore`.
- Required by: Plan 02 Task 6 and the strict Playwright Settings fixture.

- [ ] **Step 1: Execute Plan 02 Tasks 1-2**

Keep `github.com/pelletier/go-toml/v2` at `v2.2.4`; source-range usage is isolated in `toml_patch.go`. Verify
unchanged bytes, comments, CRLF, unknown sections, mode bits, atomic rename, pre-rename failure non-mutation, and the
explicit post-rename outcome: a directory-sync error is `committed=true`, not a false write failure.

- [ ] **Step 2: Execute Plan 02 Tasks 3-4**

Every server entry point must receive the same `zap.AtomicLevel` used by the process logger. Only a non-overridden
`server.log_level` update calls it; Cache and Auth remain boot-effective. When rename committed but directory sync
reports a durability warning, the Store returns the committed Settings result and aligns this same runtime level;
only a proven pre-rename failure maps to `CONFIG_WRITE_FAILED`.

- [ ] **Step 3: Execute Plan 02 Task 5**

Merge the Settings handler into Plan 01's capability groups. `GET` stays on `adminRead`; `PUT` stays on `adminWrite`.

- [ ] **Step 4: Run the Settings backend gate**

Run:

```bash
go test -race ./internal/config ./internal/api/admin ./internal/server
go test ./...
```

Expected: PASS. Exact error mappings include 400 malformed JSON, 409 `CONFIG_READ_ONLY`, 422 `INVALID_SETTING`, and
500 read failures or pre-rename write failures; invalid/pre-rename-failed patches never alter disk or runtime state,
while a post-rename directory-sync warning returns committed success with disk and runtime aligned.

### Task 4: Land the dynamic Upstream Registry backend

**Files:**
- Execute: `docs/superpowers/plans/2026-07-10-admin-remediation-03-upstream-registry.md`, Tasks 1-7 only.

**Interfaces:**
- Consumes: Plan 01 `adminRead`/`adminWrite` and readonly URL masking; Plan 02's final server signature and `api.Deps` shape.
- Produces: bootstrap metadata, active ecosystems, immutable Pools, Registry CRUD/workers, runtime DTO handler, active-only routes, and true proxy-switch tests.

- [ ] **Step 1: Execute Plan 03 Tasks 1-2**

Prove seed marker and active JSON are transactional, config never repopulates an already active ecosystem after first seed, and `Pool.Snapshot()` is race-free. Bootstrap input must distinguish keys explicitly present in `config.toml` from Viper defaults, so the built-in Alpine fallback cannot activate an ecosystem the operator never configured.

- [ ] **Step 2: Execute Plan 03 Tasks 3-5**

Registry workers must have independent cancellation and join semantics. Mutation publication must remain validate -> transaction target -> prebuild -> commit -> atomic Store -> worker plan -> invariant check. Test a real injected commit failure, and compare the live Pool snapshot against committed records after publication; never validate the prepared snapshot against its own source records.

- [ ] **Step 3: Execute Plan 03 Task 6**

Readonly List responses must continue masking URL userinfo and proxy credentials. Network-unhealthy Check remains HTTP 200 with a persisted `upstream_id`; write authorization still prevents readonly callers from invoking Check.

- [ ] **Step 4: Execute Plan 03 Task 7**

Use the existing `adminRead` and `adminWrite` variables and retain Plan 02's `zap.AtomicLevel` server input plus `api.Deps.ConfigStore`. Build all 14 ordinary adapter factories and their standard/project routes only for active ecosystems; leave Docker and extras on their existing config paths.

- [ ] **Step 5: Run the Registry gate**

Run:

```bash
go test -race ./internal/upstream ./internal/api/admin ./internal/api/public ./internal/server
go test ./...
```

Expected: PASS with no races; the integration test proves the next real proxy request switches upstream while the adapter and Pool pointer remain stable.

### Task 5: Establish the browser harness and shared UI foundation

**Files:**
- Execute: `docs/superpowers/plans/2026-07-10-admin-remediation-04-ui-system.md`, Tasks 1-6 only.

**Interfaces:**
- Consumes: Plan 01 contracts and Principal hook.
- Produces: strict Playwright fixture, Instrument tokens, fields, Switch, Dialog, Drawer, Tooltip, IconButton, Toast/notices, Tabs, table viewport, responsive shell, and truthful NowStrip.
- Required by: Settings UI, Webhook, Upstreams presentation, and all later route-state tests.

- [ ] **Step 1: Execute Plan 04 Task 1**

Install only the two approved dev dependencies and make unmatched `/api/v1/**` requests fail the test. Keep the fixture response shapes exact to Plans 01-03.

- [ ] **Step 2: Execute Plan 04 Tasks 2-5 in order**

Do not import Base UI directly from pages. Keep Tabs' vertical root at `180px minmax(0,1fr)` and horizontal list locally scrollable so Settings can use one tablist at every width.

- [ ] **Step 3: Execute Plan 04 Task 6**

Use `principal.can_write` only when reading the DTO and `canWrite` when reading the hook. Preserve readable navigation for readonly principals.

- [ ] **Step 4: Run the foundation gate**

Run:

```bash
cd web
npm run type-check
npm run build
npm run test:e2e -- admin-smoke.spec.ts admin-contrast.spec.ts admin-forms.spec.ts admin-dialog-actions.spec.ts admin-layout-primitives.spec.ts admin-shell.spec.ts
cd ..
python3 scripts/i18n-audit.py
```

Expected: PASS with no unmatched API request, page-level horizontal overflow, unnamed primitive action, or false Healthy fallback.

### Task 6: Connect Settings and Upstreams to their truthful runtime contracts

**Files:**
- Execute: Plan 02 Task 6.
- Execute: Plan 03 Tasks 8-9.

**Interfaces:**
- Consumes: Plan 04 fixture/components and completed backend DTOs.
- Produces: typed Settings/Upstream Axios APIs, response-driven forms/cache updates, active-only Upstream choices, and migration documentation.
- Required by: Webhook composition and Plan 04's final page migrations.

- [ ] **Step 1: Execute Plan 02 Task 6**

Settings sends only dirty whitelist leaves. It displays configured/effective differences, environment variables, pending restart, applied-now, read-only configuration, stale data, and mutation errors based on server responses.

- [ ] **Step 2: Execute Plan 03 Task 8**

Append Upstream DTOs to the same `adminApi.types.ts`; retain every Plan 01 and Plan 02 export. Axios methods must be exact and non-`any`.

- [ ] **Step 3: Execute Plan 03 Task 9**

Upstreams derives choices from the active runtime envelope and updates TanStack Query cache with Create/Update/Delete/Check responses. Keep all visual primitive and feedback work for Plan 04.

- [ ] **Step 4: Run both domain gates**

Run:

```bash
cd web
npm run type-check
npm run build
npm run test:e2e -- admin-settings.spec.ts
cd ..
python3 scripts/i18n-audit.py
go test -race ./internal/config ./internal/upstream ./internal/api/... ./internal/server
```

Expected: PASS; Settings never reports inferred success, and Upstream mutations update the runtime row returned by
the server. Touched-file ESLint is intentionally deferred until Plan 04 Task 11 creates the sole authoritative
`web/admin-remediation-eslint-files.txt` manifest.

### Task 7: Finish every Admin route and UI state

**Files:**
- Execute: `docs/superpowers/plans/2026-07-10-admin-remediation-04-ui-system.md`, Tasks 7-12.
- Verify: Plan 02 Task 7 and Plan 03 Final Verification.

**Interfaces:**
- Produces: rebuilt Webhook, responsive grids/tables/actions, route-wide query/mutation states, touched-file lint closure, axe coverage, and the complete visual matrix.

- [ ] **Step 1: Execute Plan 04 Tasks 7-9**

Run each focused Playwright spec before its implementation, then after. Plan 04 may change Upstreams presentation after Plan 03, but it must not change Registry DTO or cache semantics.

- [ ] **Step 2: Execute Plan 04 Tasks 10-11**

Initial error, empty success, stale cached data, 403, pending mutation, mutation failure, and mutation success remain distinct. Remove all nonzero letter spacing in the touched Admin surface.

- [ ] **Step 3: Execute Plan 04 Task 12**

Run axe across all 13 routes at 390/1440, both themes, both locales; run targeted 320/768/1024 cases; regress Portal `/` and `/monitor` after global token changes.

- [ ] **Step 4: Run the domain final verifications**

Execute every checkbox under Plan 02 Task 7 and Plan 03 Final Verification. Do not depend on a process left running by an earlier task. The persistence, restart, permission, seed, and live-proxy smoke evidence is produced by the self-contained Go and Playwright fixtures in Task 9 Step 3; those fixtures create and tear down their own temporary files, databases, servers, and browser contexts.

### Task 8: Synchronize operator, API, design, test, and release documentation

**Files:**
- Create: `docs/admin-control-plane.md`
- Modify: `README.md`
- Modify: `docs/README_zh.md`
- Modify: `config.example.toml`
- Modify: `DESIGN.md`
- Modify: `docs/self-test-checklist.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the implemented response/error semantics; documentation must describe observed behavior, not the pre-implementation design alone.
- Produces: one operator/API reference and consistent English/Chinese migration and verification copy.

- [ ] **Step 1: Add the operator/API reference**

Create `docs/admin-control-plane.md` with this exact content, updating no field or status without a corresponding passing contract test:

````markdown
# Admin Control Plane

The Admin API is served under `/api/v1`. Authenticate with a JWT or API token and call
`GET /api/v1/auth/me` before rendering capabilities. JWTs are revalidated against the
current user on every request. API-token writes require an enabled owner whose current
role is `admin` and a token whose permission is `readwrite`.

## Authority

| Resource | Authority | Runtime application |
| --- | --- | --- |
| Settings | `config.toml` | Log level applies immediately; Cache/Auth changes require restart |
| Ordinary active Upstreams | Database | Registry atomically updates the next proxy request |
| Docker registries and extra indexes | `config.toml` | Restart-managed; absent from Admin Upstream CRUD |
| Users, tokens, rules, Webhooks, security policy | Database | Existing handler-specific runtime behavior |

## Permissions

All Admin `GET` routes, including CSV and SBOM export, are readable by `admin` and
`readonly` principals. `POST /api/v1/admin/rules/test` is also read-only. Every other
Admin `POST`, `PUT`, `PATCH`, and `DELETE` requires a write-capable Principal. API tokens
cannot call `/api/v1/auth/refresh`. Readonly responses mask Webhook URLs, URL userinfo,
proxy credentials, secrets, keys, and every other credential-bearing response field.

The service rejects self-delete, self-disable, and self-demotion with `409 SELF_LOCKOUT`.
It also rejects removing the final enabled administrator with `409 LAST_ADMIN`.

## Contract compatibility

`GET /api/v1/admin/security/vulnerabilities` uses `package`; the deprecated `q` alias
remains accepted for one release only when `package` is absent. Security policy responses
and writes use `auto_block_enabled` and `min_cvss_score`, with the score constrained to
`0..10`.

`GET /api/v1/admin/audit-logs` and `GET /api/v1/admin/audit-logs/export` use `package`;
the deprecated `search` alias remains accepted for one release only when `package` is
absent. `GET /api/v1/admin/logs/export` accepts the same filters as the access-log list and
exports at most 10,000 matching rows.

Project package responses use `ecosystem`, `package_name`, `version`, `first_seen_at`,
`last_seen_at`, and `download_count`. Project proxy URLs fall back to `/p/{slug}`.

## Settings

`GET /api/v1/admin/settings` returns complete `configured` and `effective` snapshots plus
`sources`, `overrides`, `editable`, `pending_restart`, and `config_writable`. The editable
paths are `server.log_level`, `cache.max_size_gb`, `cache.ttl_index`, `cache.ttl_blob`,
`cache.lru_threshold`, and `auth.token_ttl`.

`PUT /api/v1/admin/settings` accepts a nested partial patch. Its response adds `changed`,
`applied_now`, `restart_required`, and `blocked_by_override`. An environment-overridden
field is still written to the file, but it appears only in `blocked_by_override`. The
service rejects malformed JSON with `400 BAD_REQUEST`, invalid values with
`422 INVALID_SETTING`, read-only configuration with `409 CONFIG_READ_ONLY`, and failed
durable replacement with `500 CONFIG_WRITE_FAILED`. A malformed or unreadable current
file returns `500 CONFIG_READ_FAILED`; no failure path changes the running level or the
Admin draft.

## Upstreams

The first upgraded start imports ordinary configured upstreams and records the active
ecosystems. After that seed, the database is authoritative for active ecosystems; restart
does not restore a row deleted through Admin. Adding config upstreams for a previously
inactive supported ecosystem activates and imports that ecosystem on the next restart.

`GET /api/v1/admin/upstreams` returns `{items,total}` runtime resources. Create and full
Update return the resulting runtime resource; Delete returns `{deleted_id,adapter_type}`.
Check returns `{upstream,check}` with HTTP 200 even when the network probe reports
`healthy:false`. An active ecosystem keeps at least one row (`409 LAST_UPSTREAM`), and
Admin cannot activate an inactive ecosystem (`409 ECOSYSTEM_NOT_ACTIVE`).

Upstream validation and lifecycle errors use `400 BAD_REQUEST`, `404 NOT_FOUND`,
`409 CONFLICT`, `409 LAST_UPSTREAM`, `409 ECOSYSTEM_NOT_ACTIVE`,
`422 INVALID_UPSTREAM`, `422 IMMUTABLE_ECOSYSTEM`, and
`500 REGISTRY_RECONCILE_FAILED`. A successful response means the committed database
records and the live Pool snapshot agree.

## Verification

Run backend race and contract tests with:

```bash
go test -race ./internal/config ./internal/upstream ./internal/middleware ./internal/api/... ./internal/server
go test ./...
```

Run frontend contracts and accessibility checks with:

```bash
cd web
npm run type-check
npm run type-check:e2e
npm run build
npm run test:e2e
cd ..
python3 scripts/i18n-audit.py
```
````

- [ ] **Step 2: Cross-link the reference in both READMEs**

Add this English sentence beside the Admin overview in `README.md`:

```markdown
See [Admin Control Plane](docs/admin-control-plane.md) for Settings persistence, live Upstream mutation, Principal permissions, response semantics, and operator verification.
```

Add this Chinese sentence beside the Admin overview in `docs/README_zh.md`:

```markdown
Settings 持久化、上游源实时变更、Principal 权限、响应语义和运维验证见 [Admin 控制面说明](admin-control-plane.md)。
```

- [ ] **Step 3: Replace the obsolete self-test statement**

Replace the two-line claim that Admin Upstream CRUD does not hot-rebuild the Pool with:

```markdown
- [ ] Admin Upstream 新增、修改或删除成功后，下一次真实代理请求立即使用数据库中的新 Pool 快照；无需重启
- [ ] 删除普通上游源后重启，确认首次 seed 已完成的生态不会被 `config.toml` 重新回填
- [ ] 删除 active ecosystem 的最后一个上游源返回 `409 LAST_UPSTREAM`
```

Add Settings checks for comment/mode preservation, log-level immediate application, Cache/Auth restart status, environment override naming, and read-only config failure.

- [ ] **Step 4: Reconcile config and design documentation**

Verify `config.example.toml` contains Plan 02's log-level default and Plan 03's one-time seed/database-authority block. Verify `DESIGN.md` contains Plan 04's shared component, breakpoint, query-state, focus, and 40x40 action rules plus the authority table from this plan. Edit only missing statements; do not duplicate the same migration block.

- [ ] **Step 5: Add an Unreleased changelog entry**

Append under `[Unreleased] -> Added`:

```markdown
- **Admin control plane remediation**: Settings now atomically preserves and patches
  `config.toml` with explicit immediate/restart/environment-override results; ordinary
  upstream CRUD is database-authoritative and atomically updates live proxy Pools; fresh
  Principal authorization, exact Admin DTOs, responsive Base UI wrappers, and Playwright/
  axe regression coverage make all 13 Admin routes permission-aware and truthful about
  loading, empty, stale, error, and mutation states.
```

- [ ] **Step 6: Run documentation checks and commit only new documentation hunks**

Run:

```bash
bash -euo pipefail <<'BASH'
rg -q '^# Admin Control Plane$' docs/admin-control-plane.md
rg -q 'GET /api/v1/admin/security/vulnerabilities|Security vulnerability search uses `package`' docs/admin-control-plane.md
rg -q 'deprecated `q` alias' docs/admin-control-plane.md
rg -q 'deprecated `search` alias' docs/admin-control-plane.md
rg -q 'GET /api/v1/admin/logs/export' docs/admin-control-plane.md
rg -q 'package_name.*first_seen_at|Project package responses use' docs/admin-control-plane.md
rg -Uq 'Webhook URLs,[[:space:][:print:]]*secrets, keys' docs/admin-control-plane.md
rg -q 'CONFIG_READ_ONLY' docs/admin-control-plane.md
rg -q 'LAST_UPSTREAM' docs/admin-control-plane.md

rg -q '\[Admin Control Plane\]\(docs/admin-control-plane\.md\)' README.md
rg -q '\[Admin 控制面说明\]\(admin-control-plane\.md\)' docs/README_zh.md
rg -q 'server\.log_level' config.example.toml
rg -qi 'database.*authoritative|database-authoritative' config.example.toml
rg -q '数据库成为权威' docs/README_zh.md
rg -q 'Authority|权威' DESIGN.md
rg -q '40x40|40×40' DESIGN.md
rg -q '下一次真实代理请求' docs/self-test-checklist.md
rg -q 'CONFIG_READ_ONLY|只读' docs/self-test-checklist.md
rg -q 'Admin control plane remediation' CHANGELOG.md

git diff --check
python3 scripts/i18n-audit.py
BASH
```

Expected: every file-specific assertion passes, tracked documentation has no whitespace error, and i18n leaves remain balanced.

Stage the new file normally and all pre-existing dirty documentation with partial staging:

```bash
git add docs/admin-control-plane.md
git add -p -- README.md docs/README_zh.md config.example.toml DESIGN.md docs/self-test-checklist.md CHANGELOG.md
git diff --cached --check
bash -euo pipefail <<'BASH'
allowed=(
  docs/admin-control-plane.md
  README.md
  docs/README_zh.md
  config.example.toml
  DESIGN.md
  docs/self-test-checklist.md
  CHANGELOG.md
)
declare -A allowed_path=()
for path in "${allowed[@]}"; do allowed_path["$path"]=1; done
while IFS= read -r path; do
  [[ ${allowed_path[$path]+present} ]] || { printf 'unexpected staged path: %s\n' "$path" >&2; exit 1; }
done < <(git diff --cached --name-only)
BASH
git diff --cached -- docs/admin-control-plane.md README.md docs/README_zh.md config.example.toml DESIGN.md docs/self-test-checklist.md CHANGELOG.md
git commit -m "docs: document admin control plane operations"
```

Expected: the staged whitespace check includes the newly added reference, every staged path belongs to the seven-file documentation allowlist, the staged diff contains only Admin remediation documentation, and all unrelated starting documentation edits remain unstaged and intact.

### Task 9: Run the complete release gate and adversarial review

**Files:**
- Verify only; fixes belong to the owning subsystem task and require a focused regression test plus a new commit.

**Interfaces:**
- Produces: evidence that backend, frontend, docs, runtime migration, permissions, responsive UI, and repository state meet all acceptance criteria.

- [ ] **Step 1: Run formatting, static analysis, race, unit, integration, and build gates**

Run:

```bash
bash -euo pipefail <<'BASH'
artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
baseline_sha=$(awk '$1 == "Baseline" && $2 == "SHA:" {print $3}' "$artifact")
[[ "$baseline_sha" =~ ^[0-9a-f]{40}$ ]]
git cat-file -e "$baseline_sha^{commit}"
git diff --check "$baseline_sha"..HEAD
git diff --check

declare -A starting_dirty=()
while IFS= read -r path; do
  [[ -n "$path" ]] && starting_dirty["$path"]=1
done < <(awk '/<!-- DIRTY_PATHS_BEGIN -->/{capture=1;next}/<!-- DIRTY_PATHS_END -->/{capture=0}capture' "$artifact")

mapfile -d '' -t candidates < <(git diff --name-only -z --diff-filter=ACMR "$baseline_sha"..HEAD -- '*.go')
go_files=()
for path in "${candidates[@]}"; do
  [[ ${starting_dirty[$path]+present} ]] || go_files+=("$path")
done

if (( ${#go_files[@]} > 0 )); then
  unformatted=$(printf '%s\0' "${go_files[@]}" | xargs -0 -r gofmt -l)
  [[ -z "$unformatted" ]] || { printf 'unformatted remediation Go files:\n%s\n' "$unformatted" >&2; exit 1; }
fi
printf 'gofmt checked %d remediation-owned Go files; starting dirty paths were excluded\n' "${#go_files[@]}"
BASH
go vet ./...
go test -race ./internal/config ./internal/upstream ./internal/middleware ./internal/api/... ./internal/server
go test ./...
go test -tags integration ./tests/integration/ -count=1
go build -o /tmp/depsilo-admin-remediation ./cmd/depsilo
```

Expected: every command exits zero with no race report; `gofmt -l` prints nothing.

- [ ] **Step 2: Run the complete frontend gate**

Run:

```bash
bash -euo pipefail <<'BASH'
cd web
npm run type-check
npm run type-check:e2e
npm run build
npm run test:e2e
manifest=admin-remediation-eslint-files.txt
[[ -s "$manifest" ]]
mapfile -t ui_files < "$manifest"
[[ ${#ui_files[@]} -gt 0 ]]
[[ -z $(printf '%s\n' "${ui_files[@]}" | sort | uniq -d) ]]
for path in "${ui_files[@]}"; do
  [[ "$path" =~ ^[A-Za-z0-9._/-]+$ && "$path" != /* && "$path" != ../* && "$path" != */../* ]]
  [[ -f "$path" ]] || { printf 'missing ESLint manifest path: %s\n' "$path" >&2; exit 1; }
done
printf '%s\0' "${ui_files[@]}" | xargs -0 -r npx --no-install eslint
printf 'eslint checked %d exact manifest paths\n' "${#ui_files[@]}"
cd ..
BASH
python3 scripts/i18n-audit.py
```

Expected: every command exits zero; application and E2E fixture TypeScript projects both type-check; ESLint reads
only the exact, duplicate-free paths in `web/admin-remediation-eslint-files.txt` and never derives scope from Git;
all 13 Admin routes pass the light/dark, zh/en, mobile/desktop axe matrix; Portal `/` and `/monitor` have no token
regression.

- [ ] **Step 3: Run the reproducible permission and control-plane smoke matrix**

Use the named self-contained fixtures below. Go tests create temporary config files, SQLite databases, Gin engines, and `httptest` upstreams and release them through `t.Cleanup`, `defer`, or `TestMain`. Playwright starts its configured Vite server when none is running, intercepts the declared Admin API routes, and closes its browser contexts. No database, API process, or temporary file from an earlier task is reused.

```bash
go test -race ./internal/middleware ./internal/api/... \
  -run 'Test(JWTUsesCurrentRoleAndRejectsDisabledUser|APITokenPermissionMatrix|JWTOnlyRejectsAPIToken|AuthMeAndRefreshUseCurrentPrincipal|AdminRoutesUseExplicitCapabilityGroups|CredentialURLMasking|WebhookListMasksURLForReadonlyPrincipal|UpstreamHandlerMasksCredentialsForReadonlyResponses|UserCannotLockOutSelfButCanChangePassword|ConcurrentAdminDemotionsLeaveOneEnabledAdmin)$' \
  -count=1

go test -race ./internal/config ./internal/api/admin \
  -run 'Test(Store|Settings|OSAtomicFileWriter)' \
  -count=1

go test -race ./internal/upstream ./internal/server \
  -run 'Test(ReconcileBootstrap_|Registry|StandardEcosystemDefinitions|SeedSources|RegisterActiveAdapters)' \
  -count=1

cd web
npm run test:e2e -- admin-contracts.spec.ts admin-settings.spec.ts admin-query-states.spec.ts admin-tables-actions.spec.ts
cd ..
```

Expected: PASS. The first command proves readonly/readwrite JWT and API-token routing, refresh rejection, credential
masking, self-lockout, and final-admin behavior. The second proves exact Settings response sets, disk bytes,
pre-rename non-mutation, post-rename committed/runtime alignment, override classification, immediate log level, restart
reload, and cleanup. The third proves one-time seed, restart persistence, last-source protection, commit failure, worker
replacement, live invariant recovery, and the next real proxy request switch. Playwright proves the same contracts and
mutation affordances through the browser fixture with no unmatched API request.

- [ ] **Step 4: Request two-stage code review**

Run a specification-compliance review first, then a code-quality/security/race/accessibility review. Findings must include exact file and line references. Fix every blocker/high finding in the owning task, rerun its focused test, then repeat the full gate.

- [ ] **Step 5: Audit the final Git boundary**

Run:

```bash
bash -euo pipefail <<'BASH'
artifact=docs/superpowers/plans/2026-07-10-admin-remediation-execution-baseline.md
baseline_sha=$(awk '$1 == "Baseline" && $2 == "SHA:" {print $3}' "$artifact")
[[ "$baseline_sha" =~ ^[0-9a-f]{40}$ ]]
git cat-file -e "$baseline_sha^{commit}"
git ls-files --error-unmatch "$artifact" >/dev/null
git diff --exit-code -- "$artifact"
git diff --cached --exit-code

declare -A starting_dirty=()
declare -A remediation_path=()
declare -A current_dirty=()
declare -A overlap_path=()
declare -A overlap_guard=()
declare -A overlap_rank=()
declare -A overlap_mode=()
declare -A pending_sha=()
declare -A pending_disposition=()
declare -A pending_guard=()
declare -A approval_commit=()
declare -A approval_seen=()
declare -A actual_seen=()
mapfile -d '' -t remediation_paths < <(git diff --name-only -z --diff-filter=ACDMRTUXB "$baseline_sha"..HEAD)
for path in "${remediation_paths[@]}"; do remediation_path["$path"]=1; done

while IFS=$'\t' read -r path _owners _initial guard_id; do
  [[ -n "$path" ]] || continue
  overlap_path["$path"]=1
  overlap_guard["$path"]=$guard_id
  overlap_rank["$path"]=0
  overlap_mode["$path"]=preserved
done < <(awk '/<!-- OVERLAP_PATHS_BEGIN -->/{capture=1;next}/<!-- OVERLAP_PATHS_END -->/{capture=0}capture' "$artifact")

while IFS=$'\t' read -r path subject disposition staged_sha guard_id state _recorded_utc; do
  [[ -n "$path" ]] || continue
  key="${path}"$'\034'"${subject}"
  [[ ${overlap_path[$path]+present} && "$guard_id" == "${overlap_guard[$path]}" && "$state" == pending-review ]]
  [[ "$staged_sha" =~ ^[0-9a-f]{64}$ ]]
  [[ ! ${pending_sha[$key]+present} ]] || { printf 'duplicate pending overlap row: %s / %s\n' "$path" "$subject" >&2; exit 1; }
  pending_sha["$key"]=$staged_sha
  pending_disposition["$key"]=$disposition
  pending_guard["$key"]=$guard_id
done < <(awk '/<!-- OVERLAP_PENDING_BEGIN -->/{capture=1;next}/<!-- OVERLAP_PENDING_END -->/{capture=0}capture' "$artifact")

while IFS=$'\t' read -r path subject approval_subject disposition commit staged_sha guard_id spec_ref quality_ref _approved_utc; do
  [[ -n "$path" ]] || continue
  key="${path}"$'\034'"${subject}"
  [[ ${pending_sha[$key]+present} ]]
  [[ "$disposition" == "${pending_disposition[$key]}" && "$staged_sha" == "${pending_sha[$key]}" && "$guard_id" == "${pending_guard[$key]}" ]]
  [[ "$commit" =~ ^[0-9a-f]{40}$ && "$spec_ref" == spec-approved:* && "$quality_ref" == quality-approved:* ]]
  [[ ! ${approval_seen[$key]+present} ]] || { printf 'duplicate overlap approval: %s / %s\n' "$path" "$subject" >&2; exit 1; }
  git cat-file -e "$commit^{commit}"
  [[ "$(git show -s --format=%s "$commit")" == "$subject" ]]
  mapfile -t approval_commits < <(git log --format='%H%x09%s' --reverse "$baseline_sha"..HEAD | awk -F '\t' -v subject="$approval_subject" '$2 == subject {print $1}')
  (( ${#approval_commits[@]} == 1 )) || { printf 'expected one approval bookkeeping commit for %s\n' "$approval_subject" >&2; exit 1; }
  mapfile -t approval_paths < <(git diff-tree --no-commit-id --name-only -r "${approval_commits[0]}")
  (( ${#approval_paths[@]} == 1 )) && [[ "${approval_paths[0]}" == "$artifact" ]] || { printf 'approval commit is not artifact-only: %s\n' "$approval_subject" >&2; exit 1; }
  committed_sha=$(git show --format= --binary "$commit" -- "$path" | sha256sum | awk '{print $1}')
  [[ "$committed_sha" == "$staged_sha" ]] || { printf 'approved commit diff mismatch: %s / %s\n' "$path" "$subject" >&2; exit 1; }
  approval_commit["$key"]=$commit
  approval_seen["$key"]=1
  case "$disposition" in preserved) rank=0;; mixed) rank=1;; adopted) rank=2;; *) printf 'invalid approved disposition: %s\n' "$disposition" >&2; exit 1;; esac
  current_rank=${overlap_rank[$path]}
  if (( rank > current_rank )); then overlap_rank["$path"]=$rank; overlap_mode["$path"]=$disposition; fi
done < <(awk '/<!-- OVERLAP_APPROVALS_BEGIN -->/{capture=1;next}/<!-- OVERLAP_APPROVALS_END -->/{capture=0}capture' "$artifact")

for path in "${!overlap_path[@]}"; do
  while IFS=$'\t' read -r commit subject; do
    [[ -n "$commit" ]] || continue
    key="${path}"$'\034'"${subject}"
    [[ ${pending_sha[$key]+present} && ${approval_seen[$key]+present} && "${approval_commit[$key]}" == "$commit" ]] || {
      printf 'unreviewed overlap commit: %s / %s / %s\n' "$path" "$commit" "$subject" >&2
      exit 1
    }
    actual_seen["$key"]=1
  done < <(git log --format='%H%x09%s' --reverse "$baseline_sha"..HEAD -- "$path")
done
for key in "${!pending_sha[@]}"; do
  [[ ${approval_seen[$key]+present} && ${actual_seen[$key]+present} ]] || { printf 'pending overlap lacks matching approval/source commit: %q\n' "$key" >&2; exit 1; }
done

tmp_current=$(mktemp)
trap 'rm -f "$tmp_current"' EXIT
{
  git diff --name-only -z --diff-filter=ACDMRTUXB HEAD
  git ls-files --others --exclude-standard -z
} | sort -zu >"$tmp_current"
while IFS= read -r -d '' path; do current_dirty["$path"]=1; done <"$tmp_current"

failures=0
evidence_count=0
while IFS=$'\t' read -r kind _status content_sha diff_sha _summary path; do
  [[ -n "$path" ]] || continue
  starting_dirty["$path"]=1
  ((evidence_count += 1))

  if [[ "$kind" == tracked ]]; then
    if ! git ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
      printf 'starting tracked path is no longer tracked: %s\n' "$path" >&2
      ((failures += 1))
      continue
    fi
    mode=${overlap_mode[$path]:-preserved}
    if [[ "$mode" == adopted ]]; then
      if [[ ${current_dirty[$path]+present} ]]; then
        printf 'fully adopted starting path remains dirty: %s\n' "$path" >&2
        ((failures += 1))
      fi
      continue
    fi
    if [[ "$mode" == mixed ]]; then
      continue
    fi
    if [[ ! ${current_dirty[$path]+present} ]]; then
      printf 'strictly preserved starting path is no longer dirty: %s\n' "$path" >&2
      ((failures += 1))
      continue
    fi
    current_diff_sha=$(
      git diff --no-color --unified=0 --binary HEAD -- "$path" |
        awk '/^diff --git / || /^index [0-9a-f]/ || /^--- (a\/|\/dev\/null)/ || /^\+\+\+ (b\/|\/dev\/null)/ || /^@@ / {next} {print}' |
        sha256sum | awk '{print $1}'
    )
    if [[ "$current_diff_sha" != "$diff_sha" ]]; then
      printf 'starting diff payload changed: %s (baseline %s, current %s)\n' "$path" "$diff_sha" "$current_diff_sha" >&2
      ((failures += 1))
    fi
    if [[ ! ${remediation_path[$path]+present} ]]; then
      if [[ -e "$path" ]]; then current_content_sha=$(sha256sum -- "$path" | awk '{print $1}'); else current_content_sha=DELETED; fi
      if [[ "$current_content_sha" != "$content_sha" ]]; then
        printf 'untouched starting file content changed: %s\n' "$path" >&2
        ((failures += 1))
      fi
    fi
  elif [[ "$kind" == untracked ]]; then
    if [[ ! ${current_dirty[$path]+present} ]]; then
      printf 'starting untracked path is missing: %s\n' "$path" >&2
      ((failures += 1))
      continue
    fi
    if git ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
      printf 'starting untracked path was committed: %s\n' "$path" >&2
      ((failures += 1))
      continue
    fi
    current_content_sha=$(sha256sum -- "$path" | awk '{print $1}')
    if [[ "$current_content_sha" != "$content_sha" ]]; then
      printf 'starting untracked content changed: %s\n' "$path" >&2
      ((failures += 1))
    fi
  else
    printf 'unknown baseline evidence kind %s for %s\n' "$kind" "$path" >&2
    ((failures += 1))
  fi
done < <(awk '/<!-- DIRTY_EVIDENCE_BEGIN -->/{capture=1;next}/<!-- DIRTY_EVIDENCE_END -->/{capture=0}capture' "$artifact")

for path in "${!current_dirty[@]}"; do
  if [[ ! ${starting_dirty[$path]+present} ]]; then
    printf 'new uncommitted path after remediation: %s\n' "$path" >&2
    ((failures += 1))
  fi
done

(( failures == 0 )) || exit 1

rg -q 'only implemented driver|only supported database driver' internal/config/config.go
rg -q 'tamper detection' internal/config/config.go
rg -q 'Transparent project-integration prompt' internal/api/router.go
rg -q 'open-source' internal/api/router.go
rg -q 'LRU miss.*alert|evicted bytes' internal/server/server.go
! rg -q 'PostgreSQL|db_dsn' web/src/admin/pages/Settings.tsx
rg -q 'Audit Logs \(open source\)' web/src/lib/api.ts
rg -q 'Deprecated aliases retained' web/src/lib/api.ts
rg -Fq "licenseFeature12eco: 'All 14 ecosystems + Docker OCI'" web/src/i18n/en.ts
rg -Fq "licenseFeature12eco: '14 种生态代理 + Docker OCI'" web/src/i18n/zh.ts
rg -q 'currently the only supported database driver' config.example.toml
rg -q 'bounded ranges without a version list are skipped' config.example.toml
rg -q 'Go-1\.25\.6|Go 1\.25\.6' README.md
rg -q 'Known-malicious blocklist' README.md
rg -q 'Tamper detection' README.md
rg -q 'Go-1\.25\.6' docs/README_zh.md
rg -q '已知恶意包阻断' docs/README_zh.md
rg -q '篡改检测' docs/README_zh.md
rg -q 'Instrument Language' DESIGN.md
rg -q '40x40|40×40' DESIGN.md
rg -q 'Known-malicious blocklist' CHANGELOG.md
rg -q 'Tamper detection' CHANGELOG.md
rg -q 'Admin control plane remediation' CHANGELOG.md
rg -q '下一次真实代理请求' docs/self-test-checklist.md
! rg -q '运行中 pool 不会热重建' docs/self-test-checklist.md

[[ -z "$(git rev-list --merges "$baseline_sha"..HEAD)" ]]
printf 'baseline and overlap-ledger audit passed for %d starting dirty paths\n' "$evidence_count"
git status --short
git log --oneline --decorate "$baseline_sha"..HEAD
BASH
```

Expected: the artifact is committed and clean; every actual overlap commit has matching pending and approved rows tied to its exact source commit and staged diff SHA; fully adopted paths are clean; mixed paths pass their semantic guards; untouched and `preserved` paths retain their normalized diff/content hashes; starting untracked files retain their content hashes; no new dirty path or merge commit exists; remediation and review-bookkeeping commits remain independently reviewable.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-10-admin-remediation-master.md`. Two execution options:

1. **Subagent-Driven (recommended)** - dispatch a fresh implementation agent per task with specification and quality review between commits.
2. **Inline Execution** - use `superpowers:executing-plans` in this workspace with checkpoints after each ordered group above.
