import { useState } from 'react'
import type { AxiosResponse } from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import TextareaV2 from '@/components/Textarea'
import Icon from '@/components/Icon'
import BadgeV2 from '@/components/Badge'
import ModalV2 from '@/components/Modal'
import DataTableV2 from '@/components/DataTable'
import SelectV2 from '@/components/Select'
import EcosystemIcon from '@/components/EcosystemIcon'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type {
  RuleListResponse,
  RuleRecord,
  RuleRequest,
  RuleTestCandidate,
  RuleTestMatchLevels,
  RuleTestResponse,
  RuleTestSpecificity,
} from '@/lib/adminApi.types'
import AdminPage from '@/admin/components/AdminPage'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import { packageRuleEcosystems, supportsPackageRuleRanges, supportsPackageRuleVersions } from '@/admin/operatorEcosystems'

const ECOSYSTEM_OPTIONS = packageRuleEcosystems.map(ecosystem => ({ value: ecosystem.id, label: ecosystem.label }))

type RuleForm = RuleRequest
type RuleListPayload = RuleListResponse
type RuleTestState = RuleTestResponse | { error: string }
const emptyForm: RuleForm = { ecosystem: 'pypi', package_name: '', version: '*', action: 'deny', reason: '' }

const MATCH_LEVEL_TRANSLATION_KEYS: Record<keyof RuleTestMatchLevels, Record<string, string>> = {
  ecosystem: { exact: 'rules.levelEcosystemExact', wildcard: 'rules.levelWildcard' },
  package: { exact: 'rules.levelPackageExact', prefix: 'rules.levelPackagePrefix', wildcard: 'rules.levelWildcard' },
  version: { exact: 'rules.levelVersionExact', range: 'rules.levelVersionRange', wildcard: 'rules.levelWildcard' },
}

const PRECEDENCE_TRANSLATION_KEYS: Record<string, string> = {
  default_allow: 'rules.precedenceDefaultAllow',
  only_matching_rule: 'rules.precedenceOnlyMatching',
  priority: 'rules.precedencePriority',
  ecosystem_specificity: 'rules.precedenceEcosystem',
  package_specificity: 'rules.precedencePackage',
  version_specificity: 'rules.precedenceVersion',
  deny_tie_break: 'rules.precedenceDeny',
  id_tie_break: 'rules.precedenceID',
  policy_fallback_deny: 'rules.precedenceFallbackDeny',
  stable_order: 'rules.precedenceStableOrder',
}

function ruleSelector(rule: RuleRecord | null | undefined) {
  if (!rule) return ''
  return `${rule.ecosystem}/${rule.package_name}@${rule.version}`
}

function matchLevelLabel(
  translate: (key: string) => string,
  dimension: keyof RuleTestMatchLevels,
  value: string | undefined,
) {
  if (!value) return '—'
  return translate(MATCH_LEVEL_TRANSLATION_KEYS[dimension][value] ?? value)
}

function specificityParts(specificity: RuleTestSpecificity | number | undefined) {
  if (typeof specificity === 'number') return [`${specificity}`]
  if (!specificity) return []
  return [
    `P${specificity.priority}`,
    `E${specificity.ecosystem}`,
    `K${specificity.package}`,
    `V${specificity.version}`,
    `A${specificity.action}`,
    `#${specificity.id}`,
  ]
}

function candidateAction(candidate: RuleTestCandidate) {
  return candidate.rule.action === 'allow' ? 'allow' : 'deny'
}

function RuleTestResultView({ result }: { result: RuleTestResponse }) {
  const { t } = useTranslation()
  const hasCandidateData = Array.isArray(result.candidates)
  const candidates = hasCandidateData ? result.candidates ?? [] : []
  const winner = result.winning_rule ?? result.matched_rule ?? result.winner?.rule
  const winnerID = winner?.id
  // `reason` is the stable business/audit explanation. Some early server
  // builds used `winner_reason` for a ranking explanation, so keep it as a
  // fallback rather than allowing that diagnostic text to mask the rule's
  // operator-facing reason.
  const winnerReason = result.reason || winner?.reason || result.winner_reason
  const displayReason = result.reason === 'no matching rule; default allow' || result.reason === 'policy load fallback denied request'
    ? ''
    : result.reason
  const precedenceReason = result.precedence_reason
    ? t(PRECEDENCE_TRANSLATION_KEYS[result.precedence_reason] ?? result.precedence_reason)
    : ''
  const usingStaleSnapshot = result.policy_status?.using_stale_snapshot === true
  const degradedPolicy = result.policy_status?.degraded === true
    || result.policy_status?.status === 'degraded'
    || result.policy_status?.status === 'unavailable'

  return (
    <div className="space-y-3" data-rule-test-result role="status" aria-live="polite">
      <div
        className="rounded-[4px] p-4"
        data-rule-test-decision={result.allowed ? 'allow' : 'deny'}
        style={{
          background: result.allowed ? 'var(--ok-fill)' : 'var(--danger-fill)',
          border: `1px solid ${result.allowed ? 'var(--ok-border)' : 'var(--danger)'}`,
        }}
      >
        <div className="mb-2 flex items-center gap-2">
          <Icon
            name={result.allowed ? 'check_circle' : 'cancel'}
            size="sm"
            style={{ color: result.allowed ? 'var(--ok-text)' : 'var(--danger)' }}
          />
          <span className="text-[14px] font-[400]" style={{ color: result.allowed ? 'var(--ok-text)' : 'var(--danger)' }}>
            {result.allowed ? t('rules.resultAllowed') : t('rules.resultDenied')}
          </span>
        </div>

        {winner ? (
          <div className="space-y-1 text-[12px]" data-rule-test-winner style={{ color: 'var(--text-soft)' }}>
            <p className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.winningRule')}</p>
            <p className="break-all font-mono" data-rule-test-winner-selector>
              {ruleSelector(winner)} → {winner.action === 'allow' ? t('rules.allow') : t('rules.deny')}
            </p>
            {winnerReason && <p data-rule-test-reason><span className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.decisionReason')}:</span> {winnerReason}</p>}
            {precedenceReason && <p data-rule-test-precedence><span className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.precedenceReason')}:</span> {precedenceReason}</p>}
          </div>
        ) : (
          <div className="space-y-1 text-[12px]" style={{ color: 'var(--text-soft)' }}>
            <p data-rule-test-no-match>{result.allowed ? t('rules.noMatch') : t('rules.noMatchDenied')}</p>
            {displayReason && <p data-rule-test-reason>{displayReason}</p>}
            {precedenceReason && <p data-rule-test-precedence><span className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.precedenceReason')}:</span> {precedenceReason}</p>}
          </div>
        )}
      </div>

      {(usingStaleSnapshot || degradedPolicy) && (
        <InlineNotice tone="warning" title={t('rules.policySnapshotNotice')}>
          {usingStaleSnapshot && result.policy_status?.snapshot_age_seconds
            ? t('rules.policySnapshotAge', { seconds: Math.round(result.policy_status.snapshot_age_seconds) })
            : usingStaleSnapshot
              ? t('rules.policySnapshotUsingStale')
              : t('rules.policySnapshotDegraded')}
        </InlineNotice>
      )}

      {!hasCandidateData ? null : candidates.length > 0 ? (
        <section aria-label={t('rules.candidateRules')} data-rule-test-candidates>
          <div className="mb-2 flex items-center justify-between gap-2">
            <h3 className="text-[13px] font-[500]" style={{ color: 'var(--text)' }}>{t('rules.candidateRules')}</h3>
            <BadgeV2 variant="neutral">{candidates.length}</BadgeV2>
          </div>
          <ol className="space-y-2">
            {candidates.map((candidate, index) => {
              const levels = candidate.match_levels
              const selected = candidate.selected === true || (winnerID !== undefined && candidate.rule.id === winnerID)
              const matched = candidate.matched !== false
              const action = candidateAction(candidate)
              const specificity = specificityParts(candidate.specificity).join(' · ')
              return (
                <li
                  key={`${candidate.rule.id}-${index}`}
                  className="min-w-0 border-b py-3 first:pt-0 last:border-b-0"
                  data-rule-test-candidate
                  data-selected={selected ? 'true' : 'false'}
                  style={{
                    borderColor: 'var(--border)',
                    background: selected ? 'var(--brand-soft)' : undefined,
                    borderLeft: selected ? '3px solid var(--brand)' : undefined,
                    paddingLeft: selected ? '9px' : undefined,
                  }}
                >
                  <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                    <span className="min-w-0 break-all font-mono text-[12px]" style={{ color: 'var(--text)' }}>
                      {ruleSelector(candidate.rule)}
                    </span>
                    <div className="flex shrink-0 flex-wrap items-center gap-1">
                      <BadgeV2 variant={action === 'allow' ? 'success' : 'error'}>
                        {action === 'allow' ? t('rules.allow') : t('rules.deny')}
                      </BadgeV2>
                      {selected && <BadgeV2 variant="success">{t('rules.selected')}</BadgeV2>}
                      {!matched && <BadgeV2 variant="neutral">{t('rules.notMatched')}</BadgeV2>}
                    </div>
                  </div>
                  <div className="mt-2 grid min-w-0 grid-cols-1 gap-1 text-[11px] sm:grid-cols-3" data-rule-test-levels>
                    <span className="min-w-0 break-words" style={{ color: 'var(--text-soft)' }}>
                      <span style={{ color: 'var(--text-muted)' }}>{t('rules.levelEcosystem')}:</span>{' '}
                      {matchLevelLabel(t, 'ecosystem', levels?.ecosystem)}
                    </span>
                    <span className="min-w-0 break-words" style={{ color: 'var(--text-soft)' }}>
                      <span style={{ color: 'var(--text-muted)' }}>{t('rules.levelPackage')}:</span>{' '}
                      {matchLevelLabel(t, 'package', levels?.package)}
                    </span>
                    <span className="min-w-0 break-words" style={{ color: 'var(--text-soft)' }}>
                      <span style={{ color: 'var(--text-muted)' }}>{t('rules.levelVersion')}:</span>{' '}
                      {matchLevelLabel(t, 'version', levels?.version)}
                    </span>
                  </div>
                  {specificity && (
                    <p className="mt-1 break-words text-[11px]" data-rule-test-specificity style={{ color: 'var(--text-muted)' }}>
                      {t('rules.specificity')}: <span className="font-mono">{specificity}</span>
                    </p>
                  )}
                </li>
              )
            })}
          </ol>
        </section>
      ) : (
        <p className="text-[12px]" data-rule-test-candidates-empty style={{ color: 'var(--text-soft)' }}>
          {t('rules.noCandidates')}
        </p>
      )}
    </div>
  )
}

function upsertRule(current: AxiosResponse<RuleListPayload> | undefined, response: AxiosResponse<RuleRecord>): AxiosResponse<RuleListPayload> {
  const rule = response.data
  if (!current) return { ...response, data: [rule] }
  const items = Array.isArray(current.data) ? current.data : current.data.items
  const next = [...items.filter((item) => item.id !== rule.id), rule]
  return { ...current, data: Array.isArray(current.data) ? next : { ...current.data, items: next } }
}

export default function RulesV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const [dialogOpen, setDialogOpen] = useState(false); const [editId, setEditId] = useState<number | null>(null); const [form, setForm] = useState<RuleForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<RuleRecord | null>(null); const [testOpen, setTestOpen] = useState(false); const [testForm, setTestForm] = useState({ ecosystem: 'pypi', package: '', version: '' }); const [testResult, setTestResult] = useState<RuleTestState | null>(null); const [testLoading, setTestLoading] = useState(false)

  const query = useQuery({ queryKey: ['admin', 'rules'], queryFn: ({ signal }) => adminApi.listRules({ signal }), retry: false })
  const { data } = query
  const rulePayload = data?.data
  const items = rulePayload ? (Array.isArray(rulePayload) ? rulePayload : rulePayload.items) : []

  const createMutation = useMutation({
    mutationFn: (input: RuleForm) => adminApi.createRule(input),
    onSuccess: (response) => {
      queryClient.setQueryData<AxiosResponse<RuleListPayload>>(['admin', 'rules'], (current) => upsertRule(current, response))
      closeDialog()
    },
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, data: input }: { id: number; data: RuleForm }) => adminApi.updateRule(id, input),
    onSuccess: (response) => {
      queryClient.setQueryData<AxiosResponse<RuleListPayload>>(['admin', 'rules'], (current) => upsertRule(current, response))
      closeDialog()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteRule(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] })
      setDeleteTarget(null)
    },
  })

  function closeDialog() { setDialogOpen(false); setEditId(null); setForm(emptyForm) }
  function openCreate() { createMutation.reset(); setEditId(null); setForm({ ...emptyForm }); setDialogOpen(true) }
  function openEdit(rule: RuleRecord) { updateMutation.reset(); setEditId(rule.id); setForm({ ecosystem: rule.ecosystem || '*', package_name: rule.package_name || '', version: rule.version || '*', action: rule.action || 'deny', reason: rule.reason || '' }); setDialogOpen(true) }
  function openDeleteDialog(rule: RuleRecord) { deleteMutation.reset(); setDeleteTarget(rule) }
  function closeDeleteDialog() { if (deleteMutation.isPending) return; deleteMutation.reset(); setDeleteTarget(null) }
  function handleSubmit(e: React.FormEvent) { e.preventDefault(); if (!canWrite) return; if (editId) updateMutation.mutate({ id: editId, data: form }); else createMutation.mutate(form) }
  async function handleTest() { setTestLoading(true); setTestResult(null); try { const res = await adminApi.testRule(testForm); setTestResult(res.data) } catch (error: unknown) { setTestResult({ error: getApiError(error).message }) } finally { setTestLoading(false) } }
  function updateTestField(field: 'ecosystem' | 'package' | 'version', value: string) {
    setTestForm(current => ({ ...current, [field]: value }))
    // A previous decision belongs to the previous coordinate; never leave it
    // visible while the operator edits a new one.
    setTestResult(null)
  }
  const isSaving = createMutation.isPending || updateMutation.isPending
  const saveError = editId ? updateMutation.error : createMutation.error
  const apiError = getApiError(query.error)
  const globalRule = form.ecosystem === '*'
  const versionsSupported = supportsPackageRuleVersions(form.ecosystem)
  const rangesSupported = supportsPackageRuleRanges(form.ecosystem)
  const packagePlaceholder = form.ecosystem === 'maven' ? t('rules.mavenPackagePlaceholder') : t('rules.packagePlaceholder')
  const testPackagePlaceholder = testForm.ecosystem === 'maven' ? t('rules.mavenPackagePlaceholder') : t('rules.packagePlaceholder')

  // Rules engine moved to open-source on 2026-06-28 — the page no longer
  // 402s, so there is no Pro paywall branch to render.

  const columns = [
    { key: 'ecosystem', label: t('rules.ecosystem'), render: (v: unknown) => { const ecosystem = typeof v === 'string' ? v : ''; return <div className="flex items-center gap-1.5">{isAdminEcosystem(ecosystem) && <EcosystemIcon type={ecosystem} size={14} />}<BadgeV2 variant="ecosystem">{ecosystem === '*' ? t('rules.allEcosystems') : ecosystem.toUpperCase()}</BadgeV2></div> } },
    { key: 'package_name', label: t('rules.packageName'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
    { key: 'version', label: t('rules.version'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{(v as string) === '*' ? t('rules.allVersions') : (v as string)}</span> },
    { key: 'action', label: t('rules.action'), render: (v: unknown) => (v as string) === 'allow' ? <BadgeV2 variant="success">{t('rules.allow')}</BadgeV2> : <BadgeV2 variant="error">{t('rules.deny')}</BadgeV2> },
    { key: 'reason', label: t('rules.reason'), render: (v: unknown) => <span className="text-[12px] truncate block max-w-[200px]" style={{ color: 'var(--text-soft)' }} title={v as string}>{(v as string) || '-'}</span> },
    { key: 'created_at', label: t('users.createdAt'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string, 'relative')}</span> },
    { key: 'id', label: t('actions'), render: (_v: unknown, row: RuleRecord & Record<string, unknown>) => canWrite ? (<div className="flex gap-1"><IconButton icon="edit" label={t('rules.editNamed', { name: row.package_name })} onClick={() => openEdit(row)} /><IconButton icon="delete" label={t('rules.deleteNamed', { name: row.package_name })} tone="danger" onClick={() => openDeleteDialog(row)} /></div>) : null },
  ]

  return (
    <AdminPage
      description={t('rules.subtitle')}
      actions={(
        <>
          <ButtonV2 variant="ghost" size="sm" onClick={() => { setTestOpen(true); setTestResult(null) }}><Icon name="science" size="sm" />{t('rules.testRule')}</ButtonV2>
          {canWrite && <ButtonV2 onClick={openCreate}><Icon name="add" size="sm" />{t('rules.addRule')}</ButtonV2>}
        </>
      )}
    >
    <div className="space-y-6">
      {query.isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></div>
      ) : query.isError && !data ? (
        <QueryErrorState message={apiError.status === 403 ? t('common.permissionDenied') : apiError.message} onRetry={() => { void query.refetch() }} />
      ) : (
        <div className="space-y-3">
        {data && query.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
        {items.length === 0 ? <EmptyState icon="rule" title={t('rules.noRules')} minHeight={200} /> : <DataTableV2
          columns={columns}
          data={items.map((rule) => ({ ...rule }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('rules.table')}
          minWidth={860}
        />}
        </div>
      )}

      <ModalV2 open={dialogOpen} onClose={closeDialog} title={editId ? t('rules.editRule') : t('rules.addRule')} closeDisabled={isSaving}>
        <form onSubmit={handleSubmit} className="space-y-4">
          <SelectV2 label={t('rules.ecosystem')} value={form.ecosystem} onChange={(e) => { const ecosystem = e.target.value; setForm({ ...form, ecosystem, ...(ecosystem === '*' ? { package_name: '*', version: '*' } : !supportsPackageRuleVersions(ecosystem) ? { version: '*' } : {}) }) }}><option value="*">{t('rules.allEcosystems')} (*)</option>{ECOSYSTEM_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={form.package_name} onChange={(e) => setForm({ ...form, package_name: e.target.value })} placeholder={packagePlaceholder} required disabled={globalRule} />
          <InputV2 label={t('rules.version')} mono value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} placeholder={rangesSupported ? t('rules.versionPlaceholder') : t('rules.exactVersionPlaceholder')} disabled={globalRule || !versionsSupported} />
          {globalRule && <InlineNotice tone="warning">{t('rules.globalRuleHint')}</InlineNotice>}
          {!globalRule && versionsSupported && !rangesSupported && <InlineNotice tone="warning">{t('rules.exactOnlyHint')}</InlineNotice>}
          {form.ecosystem === 'apt' && <InlineNotice tone="warning">{t('rules.aptVersionHint')}</InlineNotice>}
          {form.ecosystem === 'npm' && <InlineNotice tone="warning">{t('rules.npmVersionHint')}</InlineNotice>}
          {form.ecosystem === 'composer' && <InlineNotice tone="warning">{t('rules.composerVersionHint')}</InlineNotice>}
          <fieldset>
            <legend className="mb-1 block text-[14px] font-[400] text-[var(--text-muted)]">{t('rules.action')}</legend>
            <div className="flex gap-2">
              <button
                type="button"
                aria-pressed={form.action === 'allow'}
                onClick={() => setForm({ ...form, action: 'allow' })}
                className="stripe-focus-ring flex-1 cursor-pointer rounded-[4px] py-2 text-[14px] font-[400] transition-colors"
                style={{ background: form.action === 'allow' ? 'var(--ok-fill)' : 'var(--bg-soft)', color: form.action === 'allow' ? 'var(--ok-text)' : 'var(--text-soft)', border: form.action === 'allow' ? '1px solid var(--ok-border)' : '1px solid var(--border)' }}
              >
                {t('rules.allow')}
              </button>
              <button
                type="button"
                aria-pressed={form.action === 'deny'}
                onClick={() => setForm({ ...form, action: 'deny' })}
                className="stripe-focus-ring flex-1 cursor-pointer rounded-[4px] py-2 text-[14px] font-[400] transition-colors"
                style={{ background: form.action === 'deny' ? 'var(--danger-fill)' : 'var(--bg-soft)', color: form.action === 'deny' ? 'var(--danger)' : 'var(--text-soft)', border: form.action === 'deny' ? '1px solid var(--danger)' : '1px solid var(--border)' }}
              >
                {t('rules.deny')}
              </button>
            </div>
          </fieldset>
          <TextareaV2 label={t('rules.reason')} value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} placeholder={t('rules.reasonPlaceholder')} rows={2} className="resize-none" />
          {saveError && <InlineNotice tone="danger">{getApiError(saveError).message}</InlineNotice>}
          <div className="flex justify-end gap-3 pt-2"><ButtonV2 type="button" variant="secondary" disabled={isSaving} onClick={closeDialog}>{t('cancel')}</ButtonV2><ButtonV2 type="submit" aria-busy={isSaving || undefined} disabled={isSaving || !canWrite}>{isSaving ? t('saving') : t('save')}</ButtonV2></div>
        </form>
      </ModalV2>

      <ConfirmActionDialog
        open={deleteTarget !== null}
        title={t('rules.confirmDelete')}
        description={t('rules.confirmDeleteMsg')}
        details={deleteTarget ? [
          { label: t('rules.packageName'), value: deleteTarget.package_name, mono: true },
          { label: t('rules.ecosystem'), value: deleteTarget.ecosystem, mono: true },
          { label: t('rules.version'), value: deleteTarget.version, mono: true },
        ] : []}
        cancelLabel={t('cancel')}
        confirmLabel={t('delete')}
        pendingLabel={t('deleting')}
        pending={deleteMutation.isPending}
        errorMessage={deleteTarget && deleteMutation.isError ? getApiError(deleteMutation.error).message : null}
        onClose={closeDeleteDialog}
        onConfirm={() => {
          if (deleteTarget && canWrite) deleteMutation.mutate(deleteTarget.id)
        }}
      />

      <ModalV2 open={testOpen} onClose={() => setTestOpen(false)} title={t('rules.testTitle')} closeDisabled={testLoading}>
        <div className="space-y-4">
          <SelectV2 label={t('rules.ecosystem')} value={testForm.ecosystem} disabled={testLoading} onChange={(e) => updateTestField('ecosystem', e.target.value)}>{ECOSYSTEM_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={testForm.package} disabled={testLoading} onChange={(e) => updateTestField('package', e.target.value)} placeholder={testPackagePlaceholder} />
          <InputV2 label={t('rules.version')} mono value={testForm.version} disabled={testLoading} onChange={(e) => updateTestField('version', e.target.value)} placeholder={t('rules.testVersionPlaceholder')} />
          <ButtonV2 type="button" onClick={handleTest} aria-busy={testLoading || undefined} disabled={testLoading || !testForm.package} className="w-full">{testLoading ? t('rules.testing') : t('rules.testBtn')}</ButtonV2>
          {testResult && !('error' in testResult) && <RuleTestResultView result={testResult} />}
          {testResult && 'error' in testResult && <div role="alert" className="rounded-[4px] p-4" style={{ background: 'var(--danger-fill)', border: '1px solid var(--danger)' }}><p className="text-[14px]" style={{ color: 'var(--danger)' }}>{testResult.error}</p></div>}
        </div>
      </ModalV2>
    </div>
    </AdminPage>
  )
}
