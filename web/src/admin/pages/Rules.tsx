import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import Icon from '@/components/Icon'
import BadgeV2 from '@/components/Badge'
import ModalV2 from '@/components/Modal'
import DataTableV2 from '@/components/DataTable'
import SelectV2 from '@/components/Select'
import EcosystemIcon from '@/components/EcosystemIcon'

const ECOSYSTEM_OPTIONS = [{ value: '*', label: 'All (*)' }, { value: 'pypi', label: 'PyPI' }, { value: 'apt', label: 'APT' }, { value: 'npm', label: 'npm' }, { value: 'go', label: 'Go' }, { value: 'cargo', label: 'Cargo' }, { value: 'maven', label: 'Maven' }, { value: 'rubygems', label: 'RubyGems' }, { value: 'composer', label: 'Composer' }, { value: 'nuget', label: 'NuGet' }, { value: 'conda', label: 'Conda' }, { value: 'cran', label: 'CRAN' }, { value: 'helm', label: 'Helm' }]

interface RuleForm { ecosystem: string; package_name: string; version: string; action: 'allow' | 'deny'; reason: string }
const emptyForm: RuleForm = { ecosystem: '*', package_name: '', version: '*', action: 'deny', reason: '' }

function formatTime(t: string): string { if (!t) return '-'; const d = new Date(t); const now = new Date(); const diff = Math.floor((now.getTime() - d.getTime()) / 86400000); if (diff === 0) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }); if (diff < 30) return `${diff}d ago`; return `${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}` }

export default function RulesV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false); const [editId, setEditId] = useState<number | null>(null); const [form, setForm] = useState<RuleForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null); const [testOpen, setTestOpen] = useState(false); const [testForm, setTestForm] = useState({ ecosystem: 'pypi', package: '', version: '' }); const [testResult, setTestResult] = useState<any>(null); const [testLoading, setTestLoading] = useState(false)

  const { data, isLoading, error } = useQuery({ queryKey: ['admin', 'rules'], queryFn: () => adminApi.listRules(), retry: false })
  const items: any[] = data?.data?.items || data?.data || []

  const createMutation = useMutation({ mutationFn: (d: any) => adminApi.createRule(d), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] }); closeDialog() } })
  const updateMutation = useMutation({ mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateRule(id, d), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] }); closeDialog() } })
  const deleteMutation = useMutation({ mutationFn: (id: number) => adminApi.deleteRule(id), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] }); setDeleteTarget(null) } })

  function closeDialog() { setDialogOpen(false); setEditId(null); setForm(emptyForm) }
  function openCreate() { setEditId(null); setForm({ ...emptyForm }); setDialogOpen(true) }
  function openEdit(rule: any) { setEditId(rule.id); setForm({ ecosystem: rule.ecosystem || '*', package_name: rule.package_name || '', version: rule.version || '*', action: rule.action || 'deny', reason: rule.reason || '' }); setDialogOpen(true) }
  function handleSubmit(e: React.FormEvent) { e.preventDefault(); if (editId) updateMutation.mutate({ id: editId, data: form }); else createMutation.mutate(form) }
  async function handleTest() { setTestLoading(true); setTestResult(null); try { const res = await adminApi.testRule(testForm); setTestResult(res.data) } catch (err: any) { setTestResult({ error: err?.response?.data?.message || 'Test failed' }) } finally { setTestLoading(false) } }
  const isSaving = createMutation.isPending || updateMutation.isPending

  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <div className="space-y-6">
        <div className="text-center py-12 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.04)', border: '1px solid var(--border-purple)' }}>
          <div className="flex flex-col items-center gap-4">
            <div className="flex items-center justify-center w-14 h-14 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.08)' }}><Icon name="shield" size="lg" style={{ color: 'var(--brand)' }} /></div>
            <h3 className="text-[18px] font-[300]" style={{ color: 'var(--text)' }}>{t('rules.proRequired')}</h3>
            <p className="text-[14px] max-w-md" style={{ color: 'var(--text-soft)' }}>{t('rules.proDesc')}</p>
            <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer"><ButtonV2>{t('rules.upgrade')}</ButtonV2></a>
          </div>
        </div>
      </div>
    )
  }

  const columns = [
    { key: 'ecosystem', label: t('rules.ecosystem'), render: (v: unknown) => <div className="flex items-center gap-1.5">{(v as string) !== '*' && <EcosystemIcon type={v as any} size={14} />}<BadgeV2 variant="ecosystem">{(v as string) === '*' ? t('rules.allEcosystems') : (v as string)?.toUpperCase()}</BadgeV2></div> },
    { key: 'package_name', label: t('rules.packageName'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
    { key: 'version', label: t('rules.version'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{(v as string) === '*' ? t('rules.allVersions') : (v as string)}</span> },
    { key: 'action', label: t('rules.action'), render: (v: unknown) => (v as string) === 'allow' ? <BadgeV2 variant="success">{t('rules.allow')}</BadgeV2> : <BadgeV2 variant="error">{t('rules.deny')}</BadgeV2> },
    { key: 'reason', label: t('rules.reason'), render: (v: unknown) => <span className="text-[12px] truncate block max-w-[200px]" style={{ color: 'var(--text-soft)' }} title={v as string}>{(v as string) || '—'}</span> },
    { key: 'created_at', label: t('users.createdAt'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
    { key: 'id', label: t('actions'), render: (_v: unknown, row: any) => (<div className="flex gap-1"><button onClick={() => openEdit(row)} className="bg-transparent cursor-pointer p-1.5 rounded-[4px]" style={{ color: 'var(--text-soft)' }}><Icon name="edit" size="sm" /></button><button onClick={() => setDeleteTarget(row.id)} className="bg-transparent cursor-pointer p-1.5 rounded-[4px]" style={{ color: 'var(--text-soft)' }}><Icon name="delete" size="sm" /></button></div>) },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div />
        <div className="flex gap-2">
          <ButtonV2 variant="ghost" size="sm" onClick={() => { setTestOpen(true); setTestResult(null) }}><Icon name="science" size="sm" />{t('rules.testRule')}</ButtonV2>
          <ButtonV2 onClick={openCreate}><Icon name="add" size="sm" />{t('rules.addRule')}</ButtonV2>
        </div>
      </div>
      <CardV2 noPad>{isLoading ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div> : items.length === 0 ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('rules.noRules')}</div> : <DataTableV2 columns={columns} data={items} />}</CardV2>

      <ModalV2 open={dialogOpen} onClose={closeDialog} title={editId ? t('rules.editRule') : t('rules.addRule')}>
        <form onSubmit={handleSubmit} className="space-y-4">
          <SelectV2 label={t('rules.ecosystem')} value={form.ecosystem} onChange={(e) => setForm({ ...form, ecosystem: e.target.value })}>{ECOSYSTEM_OPTIONS.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={form.package_name} onChange={(e) => setForm({ ...form, package_name: e.target.value })} placeholder={t('rules.packagePlaceholder')} required />
          <InputV2 label={t('rules.version')} mono value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} placeholder={t('rules.versionPlaceholder')} />
          <div>
            <label className="block text-[14px] font-[400] mb-1" style={{ color: 'var(--text-muted)' }}>{t('rules.action')}</label>
            <div className="flex gap-2">
              <button type="button" onClick={() => setForm({ ...form, action: 'allow' })} className="flex-1 py-2 text-[14px] font-[400] rounded-[4px] transition-colors cursor-pointer" style={{ background: form.action === 'allow' ? 'rgba(21,190,83,0.15)' : 'var(--bg-soft)', color: form.action === 'allow' ? 'var(--ok-text)' : 'var(--text-soft)', border: form.action === 'allow' ? '1px solid rgba(21,190,83,0.4)' : '1px solid var(--border)' }}>{t('rules.allow')}</button>
              <button type="button" onClick={() => setForm({ ...form, action: 'deny' })} className="flex-1 py-2 text-[14px] font-[400] rounded-[4px] transition-colors cursor-pointer" style={{ background: form.action === 'deny' ? 'var(--danger-fill)' : 'var(--bg-soft)', color: form.action === 'deny' ? 'var(--danger)' : 'var(--text-soft)', border: form.action === 'deny' ? '1px solid var(--danger)' : '1px solid var(--border)' }}>{t('rules.deny')}</button>
            </div>
          </div>
          <div><label className="block text-[14px] font-[400] mb-1" style={{ color: 'var(--text-muted)' }}>{t('rules.reason')}</label><textarea value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} placeholder={t('rules.reasonPlaceholder')} rows={2} className="w-full rounded-[4px] px-3 py-2.5 text-[16px] resize-none" style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)', outline: 'none' }} /></div>
          <div className="flex justify-end gap-3 pt-2"><ButtonV2 type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</ButtonV2><ButtonV2 type="submit" disabled={isSaving}>{isSaving ? t('saving') : t('save')}</ButtonV2></div>
        </form>
      </ModalV2>

      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('rules.confirmDelete')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('rules.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3"><ButtonV2 variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</ButtonV2><ButtonV2 variant="danger" disabled={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>{deleteMutation.isPending ? t('deleting') : t('delete')}</ButtonV2></div>
      </ModalV2>

      <ModalV2 open={testOpen} onClose={() => setTestOpen(false)} title={t('rules.testTitle')}>
        <div className="space-y-4">
          <SelectV2 label={t('rules.ecosystem')} value={testForm.ecosystem} onChange={(e) => setTestForm({ ...testForm, ecosystem: e.target.value })}>{ECOSYSTEM_OPTIONS.filter(o => o.value !== '*').map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}</SelectV2>
          <InputV2 label={t('rules.packageName')} mono value={testForm.package} onChange={(e) => setTestForm({ ...testForm, package: e.target.value })} placeholder={t('rules.packagePlaceholder')} />
          <InputV2 label={t('rules.version')} mono value={testForm.version} onChange={(e) => setTestForm({ ...testForm, version: e.target.value })} placeholder={t('rules.versionPlaceholder')} />
          <ButtonV2 onClick={handleTest} disabled={testLoading || !testForm.package} className="w-full">{testLoading ? t('rules.testing') : t('rules.testBtn')}</ButtonV2>
          {testResult && !testResult.error && (
            <div className="rounded-[4px] p-4" style={{ background: testResult.allowed ? 'rgba(21,190,83,0.1)' : 'var(--danger-fill)', border: `1px solid ${testResult.allowed ? 'rgba(21,190,83,0.3)' : 'var(--danger)'}` }}>
              <div className="flex items-center gap-2 mb-2"><Icon name={testResult.allowed ? 'check_circle' : 'cancel'} size="sm" style={{ color: testResult.allowed ? 'var(--ok)' : 'var(--danger)' }} /><span className="font-[400] text-[14px]" style={{ color: testResult.allowed ? 'var(--ok-text)' : 'var(--danger)' }}>{testResult.allowed ? t('rules.resultAllowed') : t('rules.resultDenied')}</span></div>
              {testResult.matched_rule ? (<div className="text-[12px] space-y-1" style={{ color: 'var(--text-soft)' }}><p className="font-[400]" style={{ color: 'var(--text)' }}>{t('rules.matchedRule')}:</p><p className="font-mono">{testResult.matched_rule.ecosystem}/{testResult.matched_rule.package_name}@{testResult.matched_rule.version} → {testResult.matched_rule.action}</p>{testResult.matched_rule.reason && <p>{testResult.matched_rule.reason}</p>}</div>) : (<p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{t('rules.noMatch')}</p>)}
            </div>
          )}
          {testResult?.error && <div className="rounded-[4px] p-4" style={{ background: 'var(--danger-fill)', border: '1px solid var(--danger)' }}><p className="text-[14px]" style={{ color: 'var(--danger)' }}>{testResult.error}</p></div>}
        </div>
      </ModalV2>
    </div>
  )
}
