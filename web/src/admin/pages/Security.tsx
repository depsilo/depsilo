import { useState, useRef, type ComponentProps } from 'react'
import type { AxiosResponse } from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatDate, formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import SwitchV2 from '@/components/Switch'
import Icon from '@/components/Icon'
import BadgeV2 from '@/components/Badge'
import Metric from '@/components/Metric'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import DataTableV2 from '@/components/DataTable'
import TabsV2 from '@/components/Tabs'
import EcosystemIcon from '@/components/EcosystemIcon'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import type {
  SecurityPolicy,
  SecurityQuery,
  SecuritySeverity,
  SecurityVulnerability,
  UpdateSecurityPolicyRequest,
} from '@/lib/adminApi.types'

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
  { value: 'alpine', label: 'Alpine' },
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

type EcosystemName = ComponentProps<typeof EcosystemIcon>['type']

// ─── Overview Tab ────────────────────────────────────────────────────

function OverviewTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()

  const query = useQuery({
    queryKey: ['admin', 'security', 'dashboard'],
    queryFn: () => adminApi.getSecurityDashboard(),
    refetchInterval: 60000,
    retry: false,
  })
  const { data } = query

  const scanMutation = useMutation({
    mutationFn: () => adminApi.triggerSecurityScan(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  const dashboard = data?.data
  const severityDist = Object.entries(dashboard?.by_severity ?? {}).map(([severity, count]) => ({ severity, count }))
  const maxCount = Math.max(...severityDist.map((s) => s.count), 1)

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-12">
        <div aria-hidden="true">
        <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-4 lg:gap-8">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="flex flex-col items-center gap-3">
              <div className="h-3 w-20 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
              <div className="h-11 w-32 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
            </div>
          ))}
        </div>
        </div>
      </div>
    )
  }

  if (query.isError && !data) {
    const normalized = getApiError(query.error)
    return <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void query.refetch() }} />
  }

  return (
    <div className="space-y-12">
      {data && query.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
      {/* ── Metrics row ───────────────────────────── */}
      <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-4 lg:gap-8">
        <Metric label={t('security.totalVulnerabilities')} value={String(dashboard?.total_vulnerabilities || 0)} />
        <Metric label={t('security.affectedPackages')} value={String(dashboard?.affected_packages || 0)} />
        <Metric label={t('security.criticalCount')} value={String(dashboard?.by_severity.critical || 0)} />
        <Metric label={t('security.autoBlocked')} value={String(dashboard?.auto_blocked_count || 0)} />
      </div>

      {/* ── Scan status + action ───────────────────── */}
      <section>
        <SectionHeader
          title={t('security.scanStatus')}
          action={canWrite ?
            <ButtonV2
              onClick={() => scanMutation.mutate()}
              disabled={scanMutation.isPending}
              size="sm"
            >
              <Icon name="radar" size="sm" />
              {scanMutation.isPending ? t('security.scanning') : t('security.scanNow')}
            </ButtonV2>
          : undefined}
        />
        {scanMutation.isError && <div className="mb-3"><InlineNotice tone="danger">{getApiError(scanMutation.error).message}</InlineNotice></div>}
        <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
          {t('security.lastScan')}: {dashboard?.last_scan_at ? formatTime(dashboard.last_scan_at, 'relative') : t('security.never')}
        </p>
      </section>

      {/* ── Severity distribution ──────────────────── */}
      <section>
        <SectionHeader title={t('security.severityDistribution')} />
        {severityDist.length > 0 ? (
          <div className="space-y-3">
            {severityDist.map((item) => {
              const variant = SEVERITY_BADGE_MAP[item.severity] || 'default'
              const barColors: Record<string, string> = {
                critical: 'var(--danger)',
                high: 'var(--warn)',
                medium: 'var(--text-soft)',
                low: 'var(--ok)',
              }
              return (
                <div key={item.severity} className="flex items-center gap-3">
                  <div className="w-20 shrink-0">
                    <BadgeV2 variant={variant}>{item.severity.toUpperCase()}</BadgeV2>
                  </div>
                  <div className="flex-1 h-2 rounded-full" style={{ background: 'var(--bg-soft)' }}>
                    <div
                      className="h-full rounded-full transition-[width] duration-300 ease-out"
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
          <EmptyState icon="verified" title={t('security.noVulnerabilities')} minHeight={140} />
        )}
      </section>
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

  const params: SecurityQuery = { page, per_page: perPage }
  if (ecosystem) params.ecosystem = ecosystem
  if (severity) params.severity = severity as SecuritySeverity
  if (search) params.package = search

  const query = useQuery({
    queryKey: ['admin', 'security', 'vulnerabilities', params],
    queryFn: () => adminApi.listVulnerabilities(params),
    retry: false,
  })
  const { data } = query

  const items = data?.data.items ?? []
  const total = data?.data.total ?? items.length

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
          <EcosystemIcon type={v as EcosystemName} size={14} />
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

      {/* Table — bare (no Card wrap) */}
      {query.isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></div>
      ) : query.isError && !data ? (
        <QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : getApiError(query.error).message} onRetry={() => { void query.refetch() }} />
      ) : (
        <div className="space-y-3">
        {data && query.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
        {items.length === 0 ? <EmptyState icon="verified" title={t('security.noVulnerabilities')} minHeight={200} /> : <DataTableV2
          columns={columns}
          data={items.map((item) => ({ ...item }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('security.vulnerabilitiesTable')}
          minWidth={800}
        />}
        </div>
      )}

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('security.showingResults', { from: (page - 1) * perPage + 1, to: Math.min(page * perPage, total), total })}
          </span>
          <div className="flex gap-1">
            <IconButton icon="chevron_left" label={t('prevPage')} disabled={page <= 1} onClick={() => setPage(page - 1)} />
            <span className="flex items-center px-2 text-[12px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>
              {page} / {totalPages}
            </span>
            <IconButton icon="chevron_right" label={t('nextPage')} disabled={page >= totalPages} onClick={() => setPage(page + 1)} />
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
  const { canWrite } = usePrincipal()
  const [page, setPage] = useState(1)

  const query = useQuery({
    queryKey: ['admin', 'security', 'suggestions', { page }],
    queryFn: () => adminApi.listSuggestions({ page, per_page: 20 }),
    retry: false,
  })
  const { data } = query

  const items = data?.data.items ?? []

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

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-4">
        <div aria-hidden="true" className="contents">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="h-20 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
        ))}
        </div>
      </div>
    )
  }

  if (query.isError && !data) {
    const normalized = getApiError(query.error)
    return <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void query.refetch() }} />
  }

  return (
    <div className="space-y-3">
      {data && query.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
      {items.length === 0 ? <EmptyState icon="verified" title={t('security.noSuggestions')} minHeight={240} /> : <>
      <div>
        {items.map((item: SecurityVulnerability, idx: number) => {
          const severityVariant = SEVERITY_BADGE_MAP[item.severity] || 'default'
          const isActing = (approveMutation.isPending && approveMutation.variables === item.id) ||
            (dismissMutation.isPending && dismissMutation.variables === item.id)

          return (
            <div
              key={item.id}
              className="flex items-start justify-between gap-4 py-4"
              style={{ borderBottom: idx < items.length - 1 ? '1px solid var(--border)' : 'none' }}
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  <span className="font-mono text-[13px] font-[500]" style={{ color: 'var(--text)' }}>
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
                  {item.ecosystem && <EcosystemIcon type={item.ecosystem as EcosystemName} size={14} />}
                  <span className="font-mono text-[13px]" style={{ color: 'var(--text)' }}>
                    {item.package_name}
                  </span>
                </div>
              </div>
              {canWrite && <div className="flex gap-2 shrink-0">
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
              </div>}
            </div>
          )
        })}
      </div>

      {/* Simple page nav */}
      <div className="flex justify-center gap-2 mt-6">
        <IconButton icon="chevron_left" label={t('prevPage')} disabled={page <= 1} onClick={() => setPage(page - 1)} />
        <span className="flex items-center text-[12px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>
          {page}
        </span>
        <IconButton icon="chevron_right" label={t('nextPage')} disabled={items.length < 20} onClick={() => setPage(page + 1)} />
      </div>
      </>}
    </div>
  )
}

// ─── Policies Tab ────────────────────────────────────────────────────

function PoliciesTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const query = useQuery({
    queryKey: ['admin', 'security', 'policies'],
    queryFn: () => adminApi.listSecurityPolicies(),
    retry: false,
  })
  const { data } = query

  const policies = data?.data ?? []

  type EditablePolicy = Pick<SecurityPolicy, 'auto_block_enabled' | 'min_cvss_score'>
  type PolicySaveState = { isPending: boolean; error: unknown | null }
  const [localPolicies, setLocalPolicies] = useState<Record<string, EditablePolicy>>({})
  const [policySaveStates, setPolicySaveStates] = useState<Record<string, PolicySaveState>>({})

  const importMutation = useMutation({
    mutationFn: (formData: FormData) => adminApi.importVulnerabilities(formData),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  function getPolicy(ecosystem: string) {
    if (localPolicies[ecosystem]) return localPolicies[ecosystem]
    const server = policies.find((policy) => policy.ecosystem === ecosystem)
    return {
      auto_block_enabled: server?.auto_block_enabled ?? false,
      min_cvss_score: server?.min_cvss_score ?? 9.0,
    }
  }

  function setPolicy(ecosystem: string, patch: Partial<EditablePolicy>) {
    setLocalPolicies((prev) => ({
      ...prev,
      [ecosystem]: { ...getPolicy(ecosystem), ...patch },
    }))
  }

  async function handleSave(ecosystem: string) {
    if (!canWrite || policySaveStates[ecosystem]?.isPending) return
    const policy: UpdateSecurityPolicyRequest = getPolicy(ecosystem)
    setPolicySaveStates((current) => ({ ...current, [ecosystem]: { isPending: true, error: null } }))
    try {
      const { data: updated } = await adminApi.updateSecurityPolicy(ecosystem, policy)
      queryClient.setQueryData<AxiosResponse<SecurityPolicy[]>>(
        ['admin', 'security', 'policies'],
        (current) => current ? { ...current, data: current.data.some((item) => item.ecosystem === updated.ecosystem)
          ? current.data.map((item) => item.ecosystem === updated.ecosystem ? updated : item)
          : [...current.data, updated] } : current,
      )
      setLocalPolicies((current) => {
        const next = { ...current }
        delete next[ecosystem]
        return next
      })
      setPolicySaveStates((current) => ({ ...current, [ecosystem]: { isPending: false, error: null } }))
    } catch (error) {
      setPolicySaveStates((current) => ({ ...current, [ecosystem]: { isPending: false, error } }))
    }
  }

  function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const formData = new FormData()
    formData.append('file', file)
    importMutation.mutate(formData)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const ecosystems = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm']

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-2">
        <div aria-hidden="true" className="contents">
        {[...Array(4)].map((_, i) => (
          <div key={i} className="h-12 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
        ))}
        </div>
      </div>
    )
  }

  if (query.isError && !data) {
    const normalized = getApiError(query.error)
    return <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void query.refetch() }} />
  }

  return (
    <div className="space-y-12">
      {data && query.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
      {/* ── Per-ecosystem policies ───────────────────── */}
      <section>
        <SectionHeader title={t('security.ecosystemPolicies')} />
        <div>
          {ecosystems.map((eco, idx) => {
            const policy = getPolicy(eco)
            const saveState = policySaveStates[eco]
            const isSaving = saveState?.isPending ?? false
            return (
              <div
                key={eco}
                data-policy-ecosystem={eco}
                className="py-3"
                style={{ borderBottom: idx < ecosystems.length - 1 ? '1px solid var(--border)' : 'none' }}
              >
                <div className="flex items-center gap-4">
                <div className="flex items-center gap-2 w-32 shrink-0">
                  <EcosystemIcon type={eco as EcosystemName} size={16} />
                  <span className="text-[13px] font-[500]" style={{ color: 'var(--text)' }}>
                    {eco.toUpperCase()}
                  </span>
                </div>

                <div className="shrink-0 text-[var(--text-soft)]">
                  <SwitchV2
                    label={t('security.autoBlock')}
                    aria-label={`${eco.toUpperCase()} ${t('security.autoBlock')}`}
                    checked={policy.auto_block_enabled}
                    disabled={!canWrite || isSaving}
                    onCheckedChange={(checked) => setPolicy(eco, { auto_block_enabled: checked })}
                  />
                </div>

                {/* CVSS threshold */}
                <div className="w-28 shrink-0">
                  <InputV2
                    label={t('security.cvssThreshold')}
                    aria-label={`${eco.toUpperCase()} ${t('security.cvssThreshold')}`}
                    type="number"
                    min={0}
                    max={10}
                    step={0.1}
                    value={policy.min_cvss_score}
                    disabled={!canWrite || isSaving}
                    onChange={(e) => setPolicy(eco, { min_cvss_score: parseFloat(e.target.value) || 0 })}
                    mono
                    className="px-2 py-1 text-center"
                  />
                </div>

                {canWrite && <div className="ml-auto">
                  <ButtonV2
                    variant="secondary"
                    size="sm"
                    aria-label={`${eco.toUpperCase()} ${t('save')}`}
                    aria-busy={isSaving || undefined}
                    disabled={isSaving}
                    onClick={() => { void handleSave(eco) }}
                  >
                    {isSaving ? t('saving') : t('save')}
                  </ButtonV2>
                </div>}
                </div>
                {saveState?.error ? <div className="mt-2"><InlineNotice tone="danger">{getApiError(saveState.error).message}</InlineNotice></div> : null}
              </div>
            )
          })}
        </div>
      </section>

      {/* ── Offline import ────────────────────────── */}
      <section>
        <SectionHeader title={t('security.offlineImport')} hint={t('security.offlineImportDesc')} />
        {canWrite && <div
          className="rounded-[4px] p-6 text-center transition-colors duration-150"
          style={{
            border: '2px dashed var(--border)',
            background: 'var(--bg-soft)',
          }}
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
          <button
            type="button"
            className="inline-flex min-h-10 flex-col items-center justify-center rounded-[4px] bg-transparent px-4 py-2 text-[var(--text-soft)] stripe-focus-ring"
            onClick={() => fileInputRef.current?.click()}
          >
            <Icon name="upload_file" size="lg" />
            <span className="mt-2 text-[13px]">
              {importMutation.isPending ? t('security.importing') : t('security.dropOrClick')}
            </span>
          </button>
          <label htmlFor="security-vulnerability-import" className="sr-only">
            {t('security.dropOrClick')}
          </label>
          <input
            id="security-vulnerability-import"
            ref={fileInputRef}
            type="file"
            accept=".json,.zip"
            onChange={handleImport}
            className="sr-only"
          />
        </div>}
        {importMutation.isSuccess && (
          <p className="text-[12px] mt-2" style={{ color: 'var(--ok-text)' }}>{t('security.importSuccess')}</p>
        )}
        {importMutation.isError && (
          <p className="text-[12px] mt-2" style={{ color: 'var(--danger-text)' }}>{t('security.importError')}</p>
        )}
      </section>
    </div>
  )
}

// ─── Main Security Page ──────────────────────────────────────────────

export default function Security() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('overview')

  // Pro gate removed 2026-06-28 — the dashboard query no longer needs
  // to be intercepted for 402, and the page's own data queries cover
  // the loading / error UI in their tabs.

  // Security intelligence dashboard moved to open-source on 2026-06-28 —
  // the page no longer 402s, so there is no Pro paywall branch.

  const tabs = [
    { key: 'overview', label: t('security.overview'), icon: <Icon name="dashboard" size="sm" />, content: <OverviewTab /> },
    { key: 'vulnerabilities', label: t('security.vulnerabilities'), icon: <Icon name="bug_report" size="sm" />, content: <VulnerabilitiesTab /> },
    { key: 'suggestions', label: t('security.suggestions'), icon: <Icon name="lightbulb" size="sm" />, content: <SuggestionsTab /> },
    { key: 'policies', label: t('security.policies'), icon: <Icon name="policy" size="sm" />, content: <PoliciesTab /> },
  ]

  return (
    <div className="space-y-6">
      <TabsV2
        items={tabs}
        value={tab}
        onValueChange={setTab}
        ariaLabel={t('security.title')}
      />
    </div>
  )
}
