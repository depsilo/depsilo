import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Button from '@/components/Button'
import Input from '@/components/Input'
import Icon from '@/components/Icon'
import Badge from '@/components/Badge'
import Modal from '@/components/Modal'
import DataTable from '@/components/DataTable'

const ECOSYSTEM_OPTIONS = [
  { value: '*', label: 'All (*)' },
  { value: 'pypi', label: 'PyPI' },
  { value: 'apt', label: 'APT' },
  { value: 'npm', label: 'npm' },
  { value: 'go', label: 'Go' },
  { value: 'cargo', label: 'Cargo' },
  { value: 'maven', label: 'Maven' },
  { value: 'rubygems', label: 'RubyGems' },
  { value: 'composer', label: 'Composer' },
  { value: 'nuget', label: 'NuGet' },
  { value: 'conda', label: 'Conda' },
  { value: 'cran', label: 'CRAN' },
  { value: 'helm', label: 'Helm' },
]

interface RuleForm {
  ecosystem: string
  package_name: string
  version: string
  action: 'allow' | 'deny'
  reason: string
}

const emptyForm: RuleForm = {
  ecosystem: '*',
  package_name: '',
  version: '*',
  action: 'deny',
  reason: '',
}

function getEcosystemBadgeVariant(eco: string): 'pypi' | 'apt' | 'default' {
  if (eco === 'pypi') return 'pypi'
  if (eco === 'apt') return 'apt'
  return 'default'
}

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  if (diffDays === 0) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  if (diffDays < 30) {
    return `${diffDays}d ago`
  }
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export default function Rules() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<RuleForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [testOpen, setTestOpen] = useState(false)
  const [testForm, setTestForm] = useState({ ecosystem: 'pypi', package: '', version: '' })
  const [testResult, setTestResult] = useState<any>(null)
  const [testLoading, setTestLoading] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'rules'],
    queryFn: () => adminApi.listRules(),
    retry: false,
  })

  const items: any[] = data?.data?.items || data?.data || []

  const createMutation = useMutation({
    mutationFn: (d: any) => adminApi.createRule(d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] })
      closeDialog()
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateRule(id, d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'rules'] })
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

  function closeDialog() {
    setDialogOpen(false)
    setEditId(null)
    setForm(emptyForm)
  }

  function openCreate() {
    setEditId(null)
    setForm({ ...emptyForm })
    setDialogOpen(true)
  }

  function openEdit(rule: any) {
    setEditId(rule.id)
    setForm({
      ecosystem: rule.ecosystem || '*',
      package_name: rule.package_name || '',
      version: rule.version || '*',
      action: rule.action || 'deny',
      reason: rule.reason || '',
    })
    setDialogOpen(true)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (editId) {
      updateMutation.mutate({ id: editId, data: form })
    } else {
      createMutation.mutate(form)
    }
  }

  async function handleTest() {
    setTestLoading(true)
    setTestResult(null)
    try {
      const res = await adminApi.testRule(testForm)
      setTestResult(res.data)
    } catch (err: any) {
      setTestResult({ error: err?.response?.data?.message || 'Test failed' })
    } finally {
      setTestLoading(false)
    }
  }

  const isSaving = createMutation.isPending || updateMutation.isPending

  // 402 Pro required gate
  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <div className="space-y-6">
        <Card className="text-center py-12">
          <div className="flex flex-col items-center gap-4">
            <Icon name="shield" size="lg" className="text-on-surface-variant" />
            <h3 className="text-lg font-semibold text-on-surface">{t('rules.proRequired')}</h3>
            <p className="text-sm text-on-surface-variant max-w-md">{t('rules.proDesc')}</p>
            <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer">
              <Button>{t('rules.upgrade')}</Button>
            </a>
          </div>
        </Card>
      </div>
    )
  }

  const columns = [
    {
      key: 'ecosystem',
      label: t('rules.ecosystem'),
      render: (val: unknown) => (
        <Badge variant={getEcosystemBadgeVariant(val as string)}>
          {(val as string) === '*' ? t('rules.allEcosystems') : (val as string)?.toUpperCase()}
        </Badge>
      ),
    },
    {
      key: 'package_name',
      label: t('rules.packageName'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface">{val as string}</span>
      ),
    },
    {
      key: 'version',
      label: t('rules.version'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">
          {(val as string) === '*' ? t('rules.allVersions') : (val as string)}
        </span>
      ),
    },
    {
      key: 'action',
      label: t('rules.action'),
      render: (val: unknown) => {
        const action = val as string
        if (action === 'allow') return <Badge variant="success">{t('rules.allow')}</Badge>
        return <Badge variant="error">{t('rules.deny')}</Badge>
      },
    },
    {
      key: 'reason',
      label: t('rules.reason'),
      render: (val: unknown) => (
        <span
          className="text-xs text-on-surface-variant truncate block max-w-[200px]"
          title={val as string}
        >
          {(val as string) || '\u2014'}
        </span>
      ),
    },
    {
      key: 'created_at',
      label: t('users.createdAt'),
      render: (val: unknown) => (
        <span className="text-xs text-on-surface-variant whitespace-nowrap">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'id',
      label: t('actions'),
      render: (_val: unknown, row: any) => (
        <div className="flex gap-1">
          <button
            onClick={() => openEdit(row)}
            className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1.5"
          >
            <Icon name="edit" size="sm" />
          </button>
          <button
            onClick={() => setDeleteTarget(row.id)}
            className="bg-transparent text-on-surface-variant hover:text-error cursor-pointer transition-colors p-1.5"
          >
            <Icon name="delete" size="sm" />
          </button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div />
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => { setTestOpen(true); setTestResult(null) }}>
            <Icon name="science" size="sm" />
            {t('rules.testRule')}
          </Button>
          <Button onClick={openCreate}>
            <Icon name="add" size="sm" />
            {t('rules.addRule')}
          </Button>
        </div>
      </div>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('rules.noRules')}</div>
        ) : (
          <DataTable columns={columns} data={items} />
        )}
      </Card>

      {/* Create/Edit Modal */}
      <Modal
        open={dialogOpen}
        onClose={closeDialog}
        title={editId ? t('rules.editRule') : t('rules.addRule')}
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.ecosystem')}</label>
            <select
              value={form.ecosystem}
              onChange={(e) => setForm({ ...form, ecosystem: e.target.value })}
              className="w-full bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-base text-on-surface border-b-2 border-transparent focus:border-primary focus:outline-none transition-colors"
            >
              {ECOSYSTEM_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.packageName')}</label>
            <Input
              mono
              value={form.package_name}
              onChange={(e) => setForm({ ...form, package_name: e.target.value })}
              placeholder={t('rules.packagePlaceholder')}
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.version')}</label>
            <Input
              mono
              value={form.version}
              onChange={(e) => setForm({ ...form, version: e.target.value })}
              placeholder={t('rules.versionPlaceholder')}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.action')}</label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setForm({ ...form, action: 'allow' })}
                className={`flex-1 py-2 text-sm font-medium rounded-[0.125rem] transition-colors cursor-pointer ${
                  form.action === 'allow'
                    ? 'bg-success/15 text-success border border-success/30'
                    : 'bg-surface-low text-on-surface-variant border border-transparent hover:bg-surface-container'
                }`}
              >
                {t('rules.allow')}
              </button>
              <button
                type="button"
                onClick={() => setForm({ ...form, action: 'deny' })}
                className={`flex-1 py-2 text-sm font-medium rounded-[0.125rem] transition-colors cursor-pointer ${
                  form.action === 'deny'
                    ? 'bg-error/15 text-error border border-error/30'
                    : 'bg-surface-low text-on-surface-variant border border-transparent hover:bg-surface-container'
                }`}
              >
                {t('rules.deny')}
              </button>
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.reason')}</label>
            <textarea
              value={form.reason}
              onChange={(e) => setForm({ ...form, reason: e.target.value })}
              placeholder={t('rules.reasonPlaceholder')}
              rows={2}
              className="w-full bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-base text-on-surface border-b-2 border-transparent focus:border-primary focus:outline-none transition-colors resize-none"
            />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</Button>
            <Button type="submit" disabled={isSaving}>
              {isSaving ? t('saving') : t('save')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirm Modal */}
      <Modal
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('rules.confirmDelete')}
      >
        <p className="text-sm text-on-surface-variant mb-6">{t('rules.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={deleteMutation.isPending}
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
          >
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </Button>
        </div>
      </Modal>

      {/* Test Rule Modal */}
      <Modal
        open={testOpen}
        onClose={() => setTestOpen(false)}
        title={t('rules.testTitle')}
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.ecosystem')}</label>
            <select
              value={testForm.ecosystem}
              onChange={(e) => setTestForm({ ...testForm, ecosystem: e.target.value })}
              className="w-full bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-base text-on-surface border-b-2 border-transparent focus:border-primary focus:outline-none transition-colors"
            >
              {ECOSYSTEM_OPTIONS.filter((o) => o.value !== '*').map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.packageName')}</label>
            <Input
              mono
              value={testForm.package}
              onChange={(e) => setTestForm({ ...testForm, package: e.target.value })}
              placeholder={t('rules.packagePlaceholder')}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('rules.version')}</label>
            <Input
              mono
              value={testForm.version}
              onChange={(e) => setTestForm({ ...testForm, version: e.target.value })}
              placeholder={t('rules.versionPlaceholder')}
            />
          </div>
          <Button
            onClick={handleTest}
            disabled={testLoading || !testForm.package}
            className="w-full"
          >
            {testLoading ? t('rules.testing') : t('rules.testBtn')}
          </Button>

          {testResult && !testResult.error && (
            <div className={`rounded-[0.25rem] p-4 ${testResult.allowed ? 'bg-success/10 border border-success/20' : 'bg-error/10 border border-error/20'}`}>
              <div className="flex items-center gap-2 mb-2">
                <Icon
                  name={testResult.allowed ? 'check_circle' : 'cancel'}
                  size="sm"
                  className={testResult.allowed ? 'text-success' : 'text-error'}
                />
                <span className={`font-medium text-sm ${testResult.allowed ? 'text-success' : 'text-error'}`}>
                  {testResult.allowed ? t('rules.resultAllowed') : t('rules.resultDenied')}
                </span>
              </div>
              {testResult.matched_rule ? (
                <div className="text-xs text-on-surface-variant space-y-1">
                  <p className="font-medium text-on-surface">{t('rules.matchedRule')}:</p>
                  <p className="font-mono">
                    {testResult.matched_rule.ecosystem}/{testResult.matched_rule.package_name}@{testResult.matched_rule.version}
                    {' \u2192 '}{testResult.matched_rule.action}
                  </p>
                  {testResult.matched_rule.reason && (
                    <p>{testResult.matched_rule.reason}</p>
                  )}
                </div>
              ) : (
                <p className="text-xs text-on-surface-variant">{t('rules.noMatch')}</p>
              )}
            </div>
          )}

          {testResult?.error && (
            <div className="rounded-[0.25rem] p-4 bg-error/10 border border-error/20">
              <p className="text-sm text-error">{testResult.error}</p>
            </div>
          )}
        </div>
      </Modal>
    </div>
  )
}
