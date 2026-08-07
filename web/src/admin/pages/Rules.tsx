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
import type { RuleListResponse, RuleRecord, RuleRequest, RuleTestResponse } from '@/lib/adminApi.types'
import AdminPage from '@/admin/components/AdminPage'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'

const ECOSYSTEM_OPTIONS = [{ value: '*', label: 'All (*)' }, { value: 'pypi', label: 'PyPI' }, { value: 'apt', label: 'APT' }, { value: 'npm', label: 'npm' }, { value: 'go', label: 'Go' }, { value: 'cargo', label: 'Cargo' }, { value: 'maven', label: 'Maven' }, { value: 'rubygems', label: 'RubyGems' }, { value: 'composer', label: 'Composer' }, { value: 'nuget', label: 'NuGet' }, { value: 'conda', label: 'Conda' }, { value: 'cran', label: 'CRAN' }, { value: 'alpine', label: 'Alpine' }, { value: 'helm', label: 'Helm' }]

type RuleForm = RuleRequest
type RuleListPayload = RuleListResponse
type RuleTestState = RuleTestResponse | { error: string }
const emptyForm: RuleForm = { ecosystem: '*', package_name: '', version: '*', action: 'deny', reason: '' }

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
  const isSaving = createMutation.isPending || updateMutation.isPending
  const saveError = editId ? updateMutation.error : createMutation.error
  const apiError = getApiError(query.error)

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
      actions={canWrite ? (
        <>
          <ButtonV2 variant="ghost" size="sm" onClick={() => { setTestOpen(true); setTestResult(null) }}><Icon name="science" size="sm" />{t('rules.testRule')}</ButtonV2>
          <ButtonV2 onClick={openCreate}><Icon name="add" size="sm" />{t('rules.addRule')}</ButtonV2>
        </>
      ) : undefined}
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
          <SelectV2 label={t('rules.ecosystem')} value={form.ecosystem} onChange={(e) => setForm({ ...form, ecosystem: e.target.value })}>{ECOSYSTEM_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={form.package_name} onChange={(e) => setForm({ ...form, package_name: e.target.value })} placeholder={t('rules.packagePlaceholder')} required />
          <InputV2 label={t('rules.version')} mono value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} placeholder={t('rules.versionPlaceholder')} />
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
          <SelectV2 label={t('rules.ecosystem')} value={testForm.ecosystem} onChange={(e) => setTestForm({ ...testForm, ecosystem: e.target.value })}>{ECOSYSTEM_OPTIONS.filter(o => o.value !== '*').map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={testForm.package} onChange={(e) => setTestForm({ ...testForm, package: e.target.value })} placeholder={t('rules.packagePlaceholder')} />
          <InputV2 label={t('rules.version')} mono value={testForm.version} onChange={(e) => setTestForm({ ...testForm, version: e.target.value })} placeholder={t('rules.versionPlaceholder')} />
          <ButtonV2 onClick={handleTest} disabled={testLoading || !testForm.package} className="w-full">{testLoading ? t('rules.testing') : t('rules.testBtn')}</ButtonV2>
          {testResult && !('error' in testResult) && (
            <div className="rounded-[4px] p-4" style={{ background: testResult.allowed ? 'var(--ok-fill)' : 'var(--danger-fill)', border: `1px solid ${testResult.allowed ? 'var(--ok-border)' : 'var(--danger)'}` }}>
              <div className="flex items-center gap-2 mb-2"><Icon name={testResult.allowed ? 'check_circle' : 'cancel'} size="sm" style={{ color: testResult.allowed ? 'var(--ok-text)' : 'var(--danger)' }} /><span className="font-[400] text-[14px]" style={{ color: testResult.allowed ? 'var(--ok-text)' : 'var(--danger)' }}>{testResult.allowed ? t('rules.resultAllowed') : t('rules.resultDenied')}</span></div>
              {testResult.matched_rule ? (<div className="text-[12px] space-y-1" style={{ color: 'var(--text-soft)' }}><p className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.matchedRule')}:</p><p className="font-mono">{testResult.matched_rule.ecosystem}/{testResult.matched_rule.package_name}@{testResult.matched_rule.version} → {testResult.matched_rule.action}</p>{testResult.matched_rule.reason && <p>{testResult.matched_rule.reason}</p>}</div>) : (<p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{t('rules.noMatch')}</p>)}
            </div>
          )}
          {testResult && 'error' in testResult && <div className="rounded-[4px] p-4" style={{ background: 'var(--danger-fill)', border: '1px solid var(--danger)' }}><p className="text-[14px]" style={{ color: 'var(--danger)' }}>{testResult.error}</p></div>}
        </div>
      </ModalV2>
    </div>
    </AdminPage>
  )
}
