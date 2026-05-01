import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import CardV2 from '@/components/Card'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import BadgeV2 from '@/components/Badge'
import MetricCardV2 from '@/components/MetricCard'
import DataTableV2 from '@/components/DataTable'
import TabsV2 from '@/components/Tabs'
import EcosystemIcon from '@/components/EcosystemIcon'

const ECOSYSTEM_OPTIONS = [
  { value: '', label: 'All' },
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

const SEVERITY_OPTIONS = [
  { value: '', label: 'All' },
  { value: 'critical', label: 'Critical' },
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
]

const SEVERITY_BADGE_MAP: Record<string, 'error' | 'warning' | 'default' | 'success'> = {
  critical: 'error',
  high: 'warning',
  medium: 'default',
  low: 'success',
}

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t)
  const now = new Date()
  const diff = Math.floor((now.getTime() - d.getTime()) / 86400000)
  if (diff === 0) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  if (diff < 30) return `${diff}d ago`
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// ─── Overview Tab ────────────────────────────────────────────────────

function OverviewTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'security', 'dashboard'],
    queryFn: () => adminApi.getSecurityDashboard(),
    refetchInterval: 60000,
  })

  const scanMutation = useMutation({
    mutationFn: () => adminApi.triggerSecurityScan(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  const dashboard = data?.data || {}
  const severityDist: { severity: string; count: number }[] = dashboard.severity_distribution || []
  const maxCount = Math.max(...severityDist.map((s) => s.count), 1)

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid gap-4 grid-cols-4">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-24 rounded-[5px] animate-pulse" style={{ background: 'var(--bg-soft)' }} />
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Metrics */}
      <div className="grid gap-4 grid-cols-4">
        <MetricCardV2
          label={t('security.totalVulnerabilities')}
          value={String(dashboard.total_vulnerabilities || 0)}
          icon={<Icon name="bug_report" size="sm" />}
        />
        <MetricCardV2
          label={t('security.affectedPackages')}
          value={String(dashboard.affected_packages || 0)}
          icon={<Icon name="inventory_2" size="sm" />}
        />
        <MetricCardV2
          label={t('security.criticalCount')}
          value={String(dashboard.critical_count || 0)}
          icon={<Icon name="error" size="sm" />}
        />
        <MetricCardV2
          label={t('security.autoBlocked')}
          value={String(dashboard.auto_blocked_count || 0)}
          icon={<Icon name="block" size="sm" />}
        />
      </div>

      {/* Scan action + last scan */}
      <CardV2>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-[14px] font-[400]" style={{ color: 'var(--text)' }}>
              {t('security.scanStatus')}
            </h3>
            <p className="text-[12px] mt-1" style={{ color: 'var(--text-soft)' }}>
              {t('security.lastScan')}: {dashboard.last_scan_at ? formatTime(dashboard.last_scan_at) : t('security.never')}
            </p>
          </div>
          <ButtonV2
            onClick={() => scanMutation.mutate()}
            disabled={scanMutation.isPending}
            size="sm"
          >
            <Icon name="radar" size="sm" />
            {scanMutation.isPending ? t('security.scanning') : t('security.scanNow')}
          </ButtonV2>
        </div>
      </CardV2>

      {/* Severity distribution */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--text-soft)' }}>
          {t('security.severityDistribution')}
        </h3>
        {severityDist.length > 0 ? (
          <div className="space-y-3">
            {severityDist.map((item) => {
              const variant = SEVERITY_BADGE_MAP[item.severity] || 'default'
              const barColors: Record<string, string> = {
                critical: 'var(--danger)',
                high: 'var(--lemon)',
                medium: 'var(--text-soft)',
                low: '#10b981',
              }
              return (
                <div key={item.severity} className="flex items-center gap-3">
                  <div className="w-20 shrink-0">
                    <BadgeV2 variant={variant}>{item.severity.toUpperCase()}</BadgeV2>
                  </div>
                  <div className="flex-1 h-2 rounded-full" style={{ background: 'var(--bg-soft)' }}>
                    <div
                      className="h-full rounded-full transition-all duration-300"
                      style={{
                        width: `${(item.count / maxCount) * 100}%`,
                        background: barColors[item.severity] || 'var(--brand)',
                        minWidth: item.count > 0 ? '4px' : '0',
                      }}
                    />
                  </div>
                  <span className="font-mono text-[12px] tabular-nums w-8 text-right shrink-0" style={{ color: 'var(--text)' }}>
                    {item.count}
                  </span>
                </div>
              )
            })}
          </div>
        ) : (
          <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('security.noVulnerabilities')}</p>
        )}
      </CardV2>
    </div>
  )
}

// ─── Vulnerabilities Tab ─────────────────────────────────────────────

function VulnerabilitiesTab() {
  const { t } = useTranslation()
  const [ecosystem, setEcosystem] = useState('')
  const [severity, setSeverity] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const perPage = 20

  const params: Record<string, any> = { page, per_page: perPage }
  if (ecosystem) params.ecosystem = ecosystem
  if (severity) params.severity = severity
  if (search) params.q = search

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'security', 'vulnerabilities', params],
    queryFn: () => adminApi.listVulnerabilities(params),
  })

  const items: any[] = data?.data?.items || data?.data || []
  const total: number = data?.data?.total || items.length

  const columns = [
    {
      key: 'osv_id',
      label: t('security.osvId'),
      render: (v: unknown) => (
        <span className="font-mono text-[12px] font-[400]" style={{ color: 'var(--text)' }}>{v as string}</span>
      ),
    },
    {
      key: 'ecosystem',
      label: t('security.ecosystem'),
      render: (v: unknown) => (
        <div className="flex items-center gap-1.5">
          <EcosystemIcon type={v as any} size={14} />
          <BadgeV2 variant="ecosystem">{(v as string)?.toUpperCase()}</BadgeV2>
        </div>
      ),
    },
    {
      key: 'package_name',
      label: t('security.package'),
      render: (v: unknown) => (
        <span className="font-mono text-[12px]" style={{ color: 'var(--text)' }}>{v as string}</span>
      ),
    },
    {
      key: 'severity',
      label: t('security.severity'),
      render: (v: unknown) => {
        const s = v as string
        const variant = SEVERITY_BADGE_MAP[s] || 'default'
        return <BadgeV2 variant={variant}>{s?.toUpperCase()}</BadgeV2>
      },
    },
    {
      key: 'cvss_score',
      label: t('security.cvssScore'),
      render: (v: unknown) => (
        <span className="font-mono text-[12px] tabular-nums" style={{ color: 'var(--text)' }}>
          {v != null ? Number(v).toFixed(1) : '-'}
        </span>
      ),
    },
    {
      key: 'published_at',
      label: t('security.published'),
      render: (v: unknown) => (
        <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatDate(v as string)}</span>
      ),
    },
  ]

  const totalPages = Math.max(1, Math.ceil(total / perPage))

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="flex items-end gap-3 flex-wrap">
        <div className="w-40">
          <SelectV2 label={t('security.ecosystem')} value={ecosystem} onChange={(e) => { setEcosystem(e.target.value); setPage(1) }}>
            {ECOSYSTEM_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </SelectV2>
        </div>
        <div className="w-36">
          <SelectV2 label={t('security.severity')} value={severity} onChange={(e) => { setSeverity(e.target.value); setPage(1) }}>
            {SEVERITY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </SelectV2>
        </div>
        <div className="flex-1 min-w-[200px]">
          <InputV2
            label={t('security.packageSearch')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('security.searchPlaceholder')}
            mono
            onKeyDown={(e) => { if (e.key === 'Enter') setPage(1) }}
          />
        </div>
        <ButtonV2 size="sm" onClick={() => setPage(1)}>
          <Icon name="search" size="sm" />
          {t('search')}
        </ButtonV2>
      </div>

      {/* Table */}
      <CardV2 noPad>
        {isLoading ? (
          <div className="p-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('security.noVulnerabilities')}</div>
        ) : (
          <DataTableV2 columns={columns} data={items} />
        )}
      </CardV2>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('security.showingResults', { from: (page - 1) * perPage + 1, to: Math.min(page * perPage, total), total })}
          </span>
          <div className="flex gap-1">
            <ButtonV2 variant="ghost" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              <Icon name="chevron_left" size="sm" />
            </ButtonV2>
            <span className="flex items-center px-2 text-[12px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>
              {page} / {totalPages}
            </span>
            <ButtonV2 variant="ghost" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              <Icon name="chevron_right" size="sm" />
            </ButtonV2>
          </div>
        </div>
      )}
    </div>
  )
}

// ─── Suggestions Tab ─────────────────────────────────────────────────

function SuggestionsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'security', 'suggestions', { page }],
    queryFn: () => adminApi.listSuggestions({ page, per_page: 20 }),
  })

  const items: any[] = data?.data?.items || data?.data || []

  const approveMutation = useMutation({
    mutationFn: (vulnId: number) => adminApi.approveSuggestion(vulnId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  const dismissMutation = useMutation({
    mutationFn: (vulnId: number) => adminApi.dismissSuggestion(vulnId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  if (isLoading) {
    return (
      <div className="space-y-4">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="h-28 rounded-[5px] animate-pulse" style={{ background: 'var(--bg-soft)' }} />
        ))}
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="text-center py-12">
        <Icon name="verified" size="lg" style={{ color: 'var(--ok)' }} />
        <p className="text-[14px] mt-3" style={{ color: 'var(--text-soft)' }}>{t('security.noSuggestions')}</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {items.map((item: any) => {
        const severityVariant = SEVERITY_BADGE_MAP[item.severity] || 'default'
        const isActing = (approveMutation.isPending && approveMutation.variables === item.id) ||
          (dismissMutation.isPending && dismissMutation.variables === item.id)

        return (
          <CardV2 key={item.id}>
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  <span className="font-mono text-[13px] font-[400]" style={{ color: 'var(--text)' }}>
                    {item.osv_id}
                  </span>
                  <BadgeV2 variant={severityVariant}>{item.severity?.toUpperCase()}</BadgeV2>
                  {item.cvss_score != null && (
                    <span className="font-mono text-[11px] tabular-nums" style={{ color: 'var(--text-soft)' }}>
                      CVSS {Number(item.cvss_score).toFixed(1)}
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-2 mb-1">
                  {item.ecosystem && <EcosystemIcon type={item.ecosystem} size={14} />}
                  <span className="font-mono text-[13px]" style={{ color: 'var(--text)' }}>
                    {item.package_name}
                  </span>
                </div>
                {item.proposed_version && (
                  <p className="text-[12px] mt-1" style={{ color: 'var(--text-soft)' }}>
                    {t('security.proposedVersion')}: <span className="font-mono">{item.proposed_version}</span>
                  </p>
                )}
              </div>
              <div className="flex gap-2 shrink-0">
                <ButtonV2
                  variant="danger"
                  size="sm"
                  disabled={isActing}
                  onClick={() => approveMutation.mutate(item.id)}
                >
                  <Icon name="block" size="sm" />
                  {t('security.block')}
                </ButtonV2>
                <ButtonV2
                  variant="ghost"
                  size="sm"
                  disabled={isActing}
                  onClick={() => dismissMutation.mutate(item.id)}
                >
                  <Icon name="close" size="sm" />
                  {t('security.dismiss')}
                </ButtonV2>
              </div>
            </div>
          </CardV2>
        )
      })}

      {/* Simple page nav */}
      <div className="flex justify-center gap-2">
        <ButtonV2 variant="ghost" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
          <Icon name="chevron_left" size="sm" />
        </ButtonV2>
        <span className="flex items-center text-[12px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>
          {page}
        </span>
        <ButtonV2 variant="ghost" size="sm" disabled={items.length < 20} onClick={() => setPage(page + 1)}>
          <Icon name="chevron_right" size="sm" />
        </ButtonV2>
      </div>
    </div>
  )
}

// ─── Policies Tab ────────────────────────────────────────────────────

function PoliciesTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'security', 'policies'],
    queryFn: () => adminApi.listSecurityPolicies(),
  })

  const policies: any[] = data?.data?.items || data?.data || []

  const [localPolicies, setLocalPolicies] = useState<Record<string, { auto_block: boolean; cvss_threshold: number }>>({})
  const [savingEco, setSavingEco] = useState<string | null>(null)

  const updateMutation = useMutation({
    mutationFn: ({ ecosystem, data: d }: { ecosystem: string; data: any }) =>
      adminApi.updateSecurityPolicy(ecosystem, d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security', 'policies'] })
      setSavingEco(null)
    },
    onError: () => {
      setSavingEco(null)
    },
  })

  const importMutation = useMutation({
    mutationFn: (formData: FormData) => adminApi.importVulnerabilities(formData),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  function getPolicy(ecosystem: string) {
    if (localPolicies[ecosystem]) return localPolicies[ecosystem]
    const server = policies.find((p: any) => p.ecosystem === ecosystem)
    return {
      auto_block: server?.auto_block ?? false,
      cvss_threshold: server?.cvss_threshold ?? 7.0,
    }
  }

  function setPolicy(ecosystem: string, patch: Partial<{ auto_block: boolean; cvss_threshold: number }>) {
    setLocalPolicies((prev) => ({
      ...prev,
      [ecosystem]: { ...getPolicy(ecosystem), ...patch },
    }))
  }

  function handleSave(ecosystem: string) {
    const policy = getPolicy(ecosystem)
    setSavingEco(ecosystem)
    updateMutation.mutate({ ecosystem, data: policy })
  }

  function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const formData = new FormData()
    formData.append('file', file)
    importMutation.mutate(formData)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const ecosystems = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm']

  if (isLoading) {
    return (
      <div className="space-y-4">
        {[...Array(4)].map((_, i) => (
          <div key={i} className="h-16 rounded-[5px] animate-pulse" style={{ background: 'var(--bg-soft)' }} />
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Per-ecosystem policies */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--text-soft)' }}>
          {t('security.ecosystemPolicies')}
        </h3>
        <div className="space-y-3">
          {ecosystems.map((eco) => {
            const policy = getPolicy(eco)
            const isSaving = savingEco === eco && updateMutation.isPending
            return (
              <div
                key={eco}
                className="flex items-center gap-4 py-3 px-4 rounded-[4px]"
                style={{ background: 'var(--bg-soft)', border: '1px solid var(--border)' }}
              >
                <div className="flex items-center gap-2 w-32 shrink-0">
                  <EcosystemIcon type={eco as any} size={16} />
                  <span className="text-[13px] font-[400]" style={{ color: 'var(--text)' }}>
                    {eco.toUpperCase()}
                  </span>
                </div>

                {/* Auto-block toggle */}
                <label className="flex items-center gap-2 cursor-pointer shrink-0">
                  <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{t('security.autoBlock')}</span>
                  <button
                    type="button"
                    onClick={() => setPolicy(eco, { auto_block: !policy.auto_block })}
                    className="relative w-9 h-5 rounded-full cursor-pointer transition-colors duration-200"
                    style={{
                      background: policy.auto_block ? 'var(--brand)' : 'var(--bg-soft)',
                      border: 'none',
                    }}
                  >
                    <span
                      className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform duration-200"
                      style={{
                        background: 'white',
                        transform: policy.auto_block ? 'translateX(16px)' : 'translateX(0)',
                      }}
                    />
                  </button>
                </label>

                {/* CVSS threshold */}
                <div className="flex items-center gap-2 shrink-0">
                  <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{t('security.cvssThreshold')}</span>
                  <input
                    type="number"
                    min={0}
                    max={10}
                    step={0.1}
                    value={policy.cvss_threshold}
                    onChange={(e) => setPolicy(eco, { cvss_threshold: parseFloat(e.target.value) || 0 })}
                    className="w-16 rounded-[4px] px-2 py-1 text-[13px] font-mono text-center"
                    style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)', outline: 'none' }}
                  />
                </div>

                <div className="ml-auto">
                  <ButtonV2 variant="secondary" size="sm" disabled={isSaving} onClick={() => handleSave(eco)}>
                    {isSaving ? t('saving') : t('save')}
                  </ButtonV2>
                </div>
              </div>
            )
          })}
        </div>
      </CardV2>

      {/* Offline import */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--text-soft)' }}>
          {t('security.offlineImport')}
        </h3>
        <p className="text-[13px] mb-4" style={{ color: 'var(--text-soft)' }}>
          {t('security.offlineImportDesc')}
        </p>
        <div
          className="rounded-[4px] p-6 text-center cursor-pointer transition-colors duration-150"
          style={{
            border: '2px dashed var(--border)',
            background: 'var(--bg-soft)',
          }}
          onClick={() => fileInputRef.current?.click()}
          onDragOver={(e) => { e.preventDefault(); e.stopPropagation() }}
          onDrop={(e) => {
            e.preventDefault()
            e.stopPropagation()
            const file = e.dataTransfer.files[0]
            if (file) {
              const formData = new FormData()
              formData.append('file', file)
              importMutation.mutate(formData)
            }
          }}
        >
          <Icon name="upload_file" size="lg" style={{ color: 'var(--text-soft)' }} />
          <p className="text-[13px] mt-2" style={{ color: 'var(--text-soft)' }}>
            {importMutation.isPending ? t('security.importing') : t('security.dropOrClick')}
          </p>
          <input
            ref={fileInputRef}
            type="file"
            accept=".json,.zip"
            onChange={handleImport}
            className="hidden"
          />
        </div>
        {importMutation.isSuccess && (
          <p className="text-[12px] mt-2" style={{ color: 'var(--ok)' }}>{t('security.importSuccess')}</p>
        )}
        {importMutation.isError && (
          <p className="text-[12px] mt-2" style={{ color: 'var(--danger)' }}>{t('security.importError')}</p>
        )}
      </CardV2>
    </div>
  )
}

// ─── Main Security Page ──────────────────────────────────────────────

export default function Security() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('overview')

  // Check Pro gating via the dashboard query
  const { error } = useQuery({
    queryKey: ['admin', 'security', 'dashboard'],
    queryFn: () => adminApi.getSecurityDashboard(),
    retry: false,
  })

  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <div className="space-y-6">
        <div className="text-center py-12 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.04)', border: '1px solid var(--border-purple)' }}>
          <div className="flex flex-col items-center gap-4">
            <div className="flex items-center justify-center w-14 h-14 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.08)' }}>
              <Icon name="shield" size="lg" style={{ color: 'var(--brand)' }} />
            </div>
            <h3 className="text-[18px] font-[300]" style={{ color: 'var(--text)' }}>{t('security.proRequired')}</h3>
            <p className="text-[14px] max-w-md" style={{ color: 'var(--text-soft)' }}>{t('security.proDesc')}</p>
            <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer">
              <ButtonV2>{t('security.upgrade')}</ButtonV2>
            </a>
          </div>
        </div>
      </div>
    )
  }

  const tabs = [
    { key: 'overview', label: t('security.overview'), icon: <Icon name="dashboard" size="sm" /> },
    { key: 'vulnerabilities', label: t('security.vulnerabilities'), icon: <Icon name="bug_report" size="sm" /> },
    { key: 'suggestions', label: t('security.suggestions'), icon: <Icon name="lightbulb" size="sm" /> },
    { key: 'policies', label: t('security.policies'), icon: <Icon name="policy" size="sm" /> },
  ]

  return (
    <div className="space-y-6">
      <TabsV2 items={tabs} active={tab} onChange={setTab} />

      {tab === 'overview' && <OverviewTab />}
      {tab === 'vulnerabilities' && <VulnerabilitiesTab />}
      {tab === 'suggestions' && <SuggestionsTab />}
      {tab === 'policies' && <PoliciesTab />}
    </div>
  )
}
