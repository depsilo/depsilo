import { useEffect, useState, useRef, type ComponentProps } from 'react'
import type { AxiosResponse } from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
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
import QueryErrorState from '@/components/QueryErrorState'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { securityEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import type {
  SecurityPolicy,
  SecurityQuery,
  SecuritySeverity,
  SecurityVulnerability,
  UpdateSecurityPolicyRequest,
} from '@/lib/adminApi.types'

const SEVERITY_BADGE_MAP: Record<string, 'error' | 'warning' | 'default' | 'success'> = {
  critical: 'error',
  high: 'warning',
  medium: 'default',
  low: 'success',
}

const POLICY_SAVE_CONCURRENCY = 4

type EcosystemName = ComponentProps<typeof EcosystemIcon>['type']

// ─── Overview Tab ────────────────────────────────────────────────────

function OverviewTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()

  const query = useQuery({
    queryKey: ['admin', 'security', 'dashboard'],
    queryFn: ({ signal }) => adminApi.getSecurityDashboard({ signal }),
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
  const scanInProgress = scanMutation.isPending || dashboard?.scan_in_progress === true
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
      {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
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
              aria-busy={scanInProgress || undefined}
              disabled={scanInProgress}
              size="sm"
            >
              <Icon name="radar" size="sm" />
              {scanInProgress ? t('security.scanning') : t('security.scanNow')}
            </ButtonV2>
          : undefined}
        />
        {scanMutation.isSuccess && <div className="mb-3"><InlineNotice tone="success">{t('security.scanStarted')}</InlineNotice></div>}
        {scanMutation.isError && <div className="mb-3"><InlineNotice tone="danger">{getApiError(scanMutation.error).status === 409 ? t('security.scanConflict') : getApiError(scanMutation.error).message}</InlineNotice></div>}
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
  const [draftSearch, setDraftSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const [page, setPage] = useState(1)
  const perPage = 20

  const params: SecurityQuery = { page, per_page: perPage }
  if (ecosystem) params.ecosystem = ecosystem
  if (severity) params.severity = severity as SecuritySeverity
  if (appliedSearch) params.package = appliedSearch

  const query = useQuery({
    queryKey: ['admin', 'security', 'vulnerabilities', params],
    queryFn: ({ signal }) => adminApi.listVulnerabilities(params, { signal }),
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

  function applySearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setAppliedSearch(draftSearch.trim())
    setPage(1)
  }

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <form data-security-vulnerability-filters className="flex flex-wrap items-end gap-3" onSubmit={applySearch}>
        <div className="w-40">
          <SelectV2 label={t('security.ecosystem')} value={ecosystem} onChange={(e) => { setEcosystem(e.target.value); setPage(1) }}>
            <option value="">{t('all')}</option>
            {securityEcosystems.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>{candidate.label}</option>
            ))}
          </SelectV2>
        </div>
        <div className="w-36">
          <SelectV2 label={t('security.severity')} value={severity} onChange={(e) => { setSeverity(e.target.value); setPage(1) }}>
            <option value="">{t('all')}</option>
            {(['critical', 'high', 'medium', 'low', 'unknown'] satisfies SecuritySeverity[]).map((value) => (
              <option key={value} value={value}>{t(`security.${value}`)}</option>
            ))}
          </SelectV2>
        </div>
        <div className="flex-1 min-w-[200px]">
          <InputV2
            label={t('security.packageSearch')}
            value={draftSearch}
            onChange={(e) => setDraftSearch(e.target.value)}
            placeholder={t('security.searchPlaceholder')}
            mono
          />
        </div>
        <ButtonV2 type="submit" size="sm">
          <Icon name="search" size="sm" />
          {t('search')}
        </ButtonV2>
      </form>

      {/* Table — bare (no Card wrap) */}
      {query.isPending ? (
        <div role="status" aria-busy="true" className="py-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
      ) : query.isError && !data ? (
        <QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : getApiError(query.error).message} onRetry={() => { void query.refetch() }} />
      ) : (
        <div className="space-y-3">
        {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
        {items.length === 0 ? <EmptyState icon="verified" title={t('security.noVulnerabilities')} minHeight={200} /> : <DataTableV2
          columns={columns}
          data={items.map((item) => ({ ...item }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('security.vulnerabilitiesTable')}
          minWidth={800}
        />}
        </div>
      )}

      <AdminPagination page={page} pageSize={perPage} total={total} onPageChange={setPage} />
    </div>
  )
}

// ─── Suggestions Tab ─────────────────────────────────────────────────

function SuggestionsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const [page, setPage] = useState(1)
  const [actionTarget, setActionTarget] = useState<{
    item: SecurityVulnerability
    action: 'block' | 'dismiss'
  } | null>(null)

  const query = useQuery({
    queryKey: ['admin', 'security', 'suggestions', { page }],
    queryFn: ({ signal }) => adminApi.listSuggestions({ page, per_page: 20 }, { signal }),
    retry: false,
  })
  const { data } = query

  const items = data?.data.items ?? []
  const total = data?.data.total ?? items.length

  const approveMutation = useMutation({
    mutationFn: (vulnId: number) => adminApi.approveSuggestion(vulnId),
    onSuccess: () => {
      setActionTarget(null)
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  const dismissMutation = useMutation({
    mutationFn: (vulnId: number) => adminApi.dismissSuggestion(vulnId),
    onSuccess: () => {
      setActionTarget(null)
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  const activeMutation = actionTarget?.action === 'block' ? approveMutation : dismissMutation

  function openAction(item: SecurityVulnerability, action: 'block' | 'dismiss') {
    approveMutation.reset()
    dismissMutation.reset()
    setActionTarget({ item, action })
  }

  function closeAction() {
    if (approveMutation.isPending || dismissMutation.isPending) return
    setActionTarget(null)
    approveMutation.reset()
    dismissMutation.reset()
  }

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
      {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
      {items.length === 0 ? <EmptyState icon="verified" title={t('security.noSuggestions')} minHeight={240} /> : <>
      <div>
        {items.map((item: SecurityVulnerability, idx: number) => {
          const severityVariant = SEVERITY_BADGE_MAP[item.severity] || 'default'
          const isActing = (approveMutation.isPending && approveMutation.variables === item.id) ||
            (dismissMutation.isPending && dismissMutation.variables === item.id)

          return (
            <div
              key={item.id}
              className="flex min-w-0 flex-col gap-3 py-4 sm:flex-row sm:items-start sm:justify-between sm:gap-4"
              style={{ borderBottom: idx < items.length - 1 ? '1px solid var(--border)' : 'none' }}
            >
              <div className="flex-1 min-w-0">
                <div className="mb-1 flex flex-wrap items-center gap-2">
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
                <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
                  {item.ecosystem && <EcosystemIcon type={item.ecosystem as EcosystemName} size={14} />}
                  <span className="min-w-0 break-all font-mono text-[13px]" style={{ color: 'var(--text)' }}>
                    {item.package_name}
                  </span>
                </div>
              </div>
              {canWrite && <div className="flex shrink-0 flex-wrap gap-2 self-end sm:self-start">
                <ButtonV2
                  variant="danger"
                  size="sm"
                  disabled={isActing}
                  onClick={() => openAction(item, 'block')}
                >
                  <Icon name="block" size="sm" />
                  {t('security.block')}
                </ButtonV2>
                <ButtonV2
                  variant="ghost"
                  size="sm"
                  disabled={isActing}
                  onClick={() => openAction(item, 'dismiss')}
                >
                  <Icon name="close" size="sm" />
                  {t('security.dismiss')}
                </ButtonV2>
              </div>}
            </div>
          )
        })}
      </div>

      <AdminPagination page={page} pageSize={20} total={total} onPageChange={setPage} />
      <ConfirmActionDialog
        open={actionTarget !== null}
        title={actionTarget?.action === 'block' ? t('security.blockTitle') : t('security.dismissTitle')}
        description={actionTarget?.action === 'block'
          ? t('security.blockImpact', { name: actionTarget?.item.package_name ?? '' })
          : t('security.dismissImpact', { name: actionTarget?.item.package_name ?? '' })}
        details={actionTarget ? [
          { label: t('security.package'), value: actionTarget.item.package_name, mono: true },
          { label: t('security.ecosystem'), value: actionTarget.item.ecosystem.toUpperCase() },
          { label: t('security.osvId'), value: actionTarget.item.osv_id, mono: true },
          {
            label: t('security.cvssScore'),
            value: Number(actionTarget.item.cvss_score).toFixed(1),
            mono: true,
          },
        ] : []}
        cancelLabel={t('cancel')}
        confirmLabel={actionTarget?.action === 'block' ? t('security.confirmBlock') : t('security.confirmDismiss')}
        pendingLabel={actionTarget?.action === 'block' ? t('security.blocking') : t('security.dismissing')}
        pending={activeMutation.isPending}
        errorMessage={activeMutation.isError ? getApiError(activeMutation.error).message : null}
        onClose={closeAction}
        onConfirm={() => {
          if (!actionTarget || !canWrite) return
          if (actionTarget.action === 'block') approveMutation.mutate(actionTarget.item.id)
          else dismissMutation.mutate(actionTarget.item.id)
        }}
      />
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
  const importInFlightRef = useRef(false)

  const query = useQuery({
    queryKey: ['admin', 'security', 'policies'],
    queryFn: ({ signal }) => adminApi.listSecurityPolicies({ signal }),
    retry: false,
  })
  const { data } = query

  const policies = data?.data ?? []

  type EditablePolicy = Pick<SecurityPolicy, 'auto_block_enabled' | 'min_cvss_score'>
  type PolicySaveState = { isPending: boolean; error: unknown | null }
  const [localPolicies, setLocalPolicies] = useState<Record<string, EditablePolicy>>({})
  const [policySaveStates, setPolicySaveStates] = useState<Record<string, PolicySaveState>>({})
  const [bulkPolicy, setBulkPolicy] = useState<EditablePolicy>({ auto_block_enabled: false, min_cvss_score: 9 })
  const [showChangedOnly, setShowChangedOnly] = useState(false)
  const [isBatchSaving, setIsBatchSaving] = useState(false)
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false)
  const [batchSaveError, setBatchSaveError] = useState(false)
  const policyWritesInFlightRef = useRef<Set<string>>(new Set())
  const batchSaveInFlightRef = useRef(false)
  const [policyWriteCount, setPolicyWriteCount] = useState(0)

  const importMutation = useMutation({
    mutationFn: (formData: FormData) => adminApi.importVulnerabilities(formData),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
    onSettled: () => { importInFlightRef.current = false },
  })

  function getPolicy(ecosystem: string) {
    if (localPolicies[ecosystem]) return localPolicies[ecosystem]
    const server = policies.find((policy) => policy.ecosystem === ecosystem)
    return {
      auto_block_enabled: server?.auto_block_enabled ?? false,
      min_cvss_score: server?.min_cvss_score ?? 9.0,
    }
  }

  function getServerPolicy(ecosystem: string): EditablePolicy {
    const server = policies.find((policy) => policy.ecosystem === ecosystem)
    return {
      auto_block_enabled: server?.auto_block_enabled ?? false,
      min_cvss_score: server?.min_cvss_score ?? 9.0,
    }
  }

  function isPolicyDirty(ecosystem: string) {
    const local = localPolicies[ecosystem]
    if (!local) return false
    const server = getServerPolicy(ecosystem)
    return local.auto_block_enabled !== server.auto_block_enabled ||
      local.min_cvss_score !== server.min_cvss_score
  }

  function setPolicy(ecosystem: string, patch: Partial<EditablePolicy>) {
    if (batchSaveInFlightRef.current || policyWritesInFlightRef.current.has(ecosystem)) return
    const serverPolicy = getServerPolicy(ecosystem)
    setLocalPolicies((prev) => ({
      ...prev,
      [ecosystem]: { ...(prev[ecosystem] ?? serverPolicy), ...patch },
    }))
  }

  function beginPolicyWrite(ecosystem: string) {
    if (batchSaveInFlightRef.current || policyWritesInFlightRef.current.has(ecosystem)) return false
    policyWritesInFlightRef.current.add(ecosystem)
    setPolicyWriteCount(policyWritesInFlightRef.current.size)
    return true
  }

  function finishPolicyWrite(ecosystem: string) {
    policyWritesInFlightRef.current.delete(ecosystem)
    setPolicyWriteCount(policyWritesInFlightRef.current.size)
  }

  async function persistPolicy(ecosystem: string, policy: UpdateSecurityPolicyRequest) {
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
        const currentDraft = current[ecosystem]
        if (currentDraft &&
          (currentDraft.auto_block_enabled !== policy.auto_block_enabled ||
            currentDraft.min_cvss_score !== policy.min_cvss_score)) {
          return current
        }
        const next = { ...current }
        delete next[ecosystem]
        return next
      })
      setPolicySaveStates((current) => ({ ...current, [ecosystem]: { isPending: false, error: null } }))
      return true
    } catch (error) {
      setPolicySaveStates((current) => ({ ...current, [ecosystem]: { isPending: false, error } }))
      return false
    }
  }

  async function handleSave(ecosystem: string) {
    if (!canWrite || !beginPolicyWrite(ecosystem)) return false
    const policy: UpdateSecurityPolicyRequest = getPolicy(ecosystem)
    try {
      return await persistPolicy(ecosystem, policy)
    } finally {
      finishPolicyWrite(ecosystem)
    }
  }

  function applyBulkPolicy() {
    if (!canWrite || batchSaveInFlightRef.current || policyWritesInFlightRef.current.size > 0) return
    setLocalPolicies(Object.fromEntries(
      securityEcosystems.map(ecosystem => [ecosystem.id, { ...bulkPolicy }]),
    ))
    setShowChangedOnly(true)
  }

  function resetPolicyChanges() {
    if (batchSaveInFlightRef.current || policyWritesInFlightRef.current.size > 0) return
    setLocalPolicies({})
    setPolicySaveStates({})
    setShowChangedOnly(false)
  }

  async function handleSaveAll() {
    const dirty = ecosystems.filter(isPolicyDirty)
    if (!canWrite || batchSaveInFlightRef.current ||
      policyWritesInFlightRef.current.size > 0 || dirty.length === 0) return
    const snapshots = dirty.map(ecosystem => ({
      ecosystem,
      policy: getPolicy(ecosystem) as UpdateSecurityPolicyRequest,
    }))
    batchSaveInFlightRef.current = true
    setIsBatchSaving(true)
    setBatchSaveError(false)
    snapshots.forEach(({ ecosystem }) => policyWritesInFlightRef.current.add(ecosystem))
    setPolicyWriteCount(policyWritesInFlightRef.current.size)
    try {
      const results: boolean[] = []
      for (let index = 0; index < snapshots.length; index += POLICY_SAVE_CONCURRENCY) {
        const batch = snapshots.slice(index, index + POLICY_SAVE_CONCURRENCY)
        const batchResults = await Promise.all(batch.map(async ({ ecosystem, policy }) => {
          try {
            return await persistPolicy(ecosystem, policy)
          } finally {
            finishPolicyWrite(ecosystem)
          }
        }))
        results.push(...batchResults)
      }
      if (results.every(Boolean)) setBatchConfirmOpen(false)
      else setBatchSaveError(true)
    } finally {
      batchSaveInFlightRef.current = false
      policyWritesInFlightRef.current.clear()
      setPolicyWriteCount(0)
      setIsBatchSaving(false)
    }
  }

  function submitImport(file: File) {
    if (!canWrite || importInFlightRef.current) return
    importInFlightRef.current = true
    const formData = new FormData()
    formData.append('file', file)
    importMutation.mutate(formData)
  }

  function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (file) submitImport(file)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const ecosystems = securityEcosystems.map(ecosystem => ecosystem.id)
  const dirtyEcosystems = ecosystems.filter(isPolicyDirty)
  const visibleEcosystems = showChangedOnly ? dirtyEcosystems : ecosystems
  const hasPolicyWrites = policyWriteCount > 0

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
      {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
      {/* ── Per-ecosystem policies ───────────────────── */}
      <section>
        <SectionHeader
          title={t('security.ecosystemPolicies')}
          hint={t('security.policyHint')}
        />
        {canWrite && (
          <div
            data-security-policy-bulk
            className="mb-5 grid min-w-0 gap-4 border-b border-[var(--border)] pb-5 md:grid-cols-[minmax(0,1fr)_minmax(9rem,12rem)_auto] md:items-end"
          >
            <div className="min-w-0">
              <SwitchV2
                label={t('security.bulkAutoBlock')}
                checked={bulkPolicy.auto_block_enabled}
                disabled={isBatchSaving || hasPolicyWrites}
                onCheckedChange={(checked) => setBulkPolicy(current => ({
                  ...current,
                  auto_block_enabled: checked,
                }))}
              />
              <p className="mt-1 text-[12px] leading-5 text-[var(--text-soft)]">
                {t('security.bulkPolicyHint')}
              </p>
            </div>
            <InputV2
              label={t('security.cvssThreshold')}
              type="number"
              min={0}
              max={10}
              step={0.1}
              value={bulkPolicy.min_cvss_score}
              disabled={!bulkPolicy.auto_block_enabled || isBatchSaving || hasPolicyWrites}
              onChange={(event) => setBulkPolicy(current => ({
                ...current,
                min_cvss_score: Number.parseFloat(event.target.value) || 0,
              }))}
              mono
            />
            <ButtonV2
              type="button"
              variant="secondary"
              onClick={applyBulkPolicy}
              disabled={isBatchSaving || hasPolicyWrites}
            >
              {t('security.applyToAll')}
            </ButtonV2>
          </div>
        )}
        <div className="mb-2 flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <span className="text-[12px] text-[var(--text-soft)]" aria-live="polite">
              {t('security.unsavedCount', { count: dirtyEcosystems.length })}
            </span>
            <SwitchV2
              label={t('security.showChangedOnly')}
              checked={showChangedOnly}
              onCheckedChange={setShowChangedOnly}
            />
          </div>
          {canWrite && (
            <div className="flex flex-wrap items-center gap-2">
              <ButtonV2
                type="button"
                variant="ghost"
                size="sm"
                onClick={resetPolicyChanges}
                disabled={dirtyEcosystems.length === 0 || isBatchSaving || hasPolicyWrites}
              >
                {t('security.resetChanges')}
              </ButtonV2>
              <ButtonV2
                type="button"
                size="sm"
                aria-busy={isBatchSaving || undefined}
                onClick={() => {
                  if (batchSaveInFlightRef.current || policyWritesInFlightRef.current.size > 0) return
                  setBatchSaveError(false)
                  setBatchConfirmOpen(true)
                }}
                disabled={dirtyEcosystems.length === 0 || isBatchSaving || hasPolicyWrites}
              >
                {isBatchSaving ? t('security.savingAll') : t('security.saveAll')}
              </ButtonV2>
            </div>
          )}
        </div>
        <div>
          {visibleEcosystems.length === 0 ? (
            <EmptyState
              icon="task_alt"
              title={t('security.noChangedPolicies')}
              hint={t('security.noChangedPoliciesHint')}
              minHeight={160}
            />
          ) : visibleEcosystems.map((eco, idx) => {
            const policy = getPolicy(eco)
            const saveState = policySaveStates[eco]
            const isSaving = saveState?.isPending ?? false
            const isDirty = isPolicyDirty(eco)
            return (
              <div
                key={eco}
                data-policy-ecosystem={eco}
                className="py-3"
                style={{ borderBottom: idx < visibleEcosystems.length - 1 ? '1px solid var(--border)' : 'none' }}
              >
                <div
                  data-security-policy-layout
                  className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 sm:grid-cols-[8rem_auto_7rem_minmax(0,1fr)] sm:gap-4"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <EcosystemIcon type={eco as EcosystemName} size={16} />
                    <span className="min-w-0 truncate text-[13px] font-[500]" title={eco.toUpperCase()} style={{ color: 'var(--text)' }}>
                      {eco.toUpperCase()}
                    </span>
                    {isDirty && <BadgeV2 variant="warning">{t('security.unsaved')}</BadgeV2>}
                  </div>

                  <div className="justify-self-end text-[var(--text-soft)] sm:justify-self-start">
                    <SwitchV2
                      label={t('security.autoBlock')}
                      aria-label={`${eco.toUpperCase()} ${t('security.autoBlock')}`}
                      checked={policy.auto_block_enabled}
                      disabled={!canWrite || isSaving || isBatchSaving}
                      onCheckedChange={(checked) => setPolicy(eco, { auto_block_enabled: checked })}
                    />
                  </div>

                  {/* CVSS threshold */}
                  <div data-security-policy-score className="min-w-0 sm:w-28">
                    <InputV2
                      label={t('security.cvssThreshold')}
                      aria-label={`${eco.toUpperCase()} ${t('security.cvssThreshold')}`}
                      type="number"
                      min={0}
                      max={10}
                      step={0.1}
                      value={policy.min_cvss_score}
                      disabled={!canWrite || isSaving || isBatchSaving || !policy.auto_block_enabled}
                      onChange={(e) => setPolicy(eco, { min_cvss_score: parseFloat(e.target.value) || 0 })}
                      mono
                      className="px-2 py-1 text-center"
                    />
                  </div>

                  {canWrite && <div data-security-policy-save className="justify-self-end">
                    <ButtonV2
                      variant="secondary"
                      size="sm"
                      aria-label={`${eco.toUpperCase()} ${t('save')}`}
                      aria-busy={isSaving || undefined}
                      disabled={isSaving || isBatchSaving || !isDirty}
                      onClick={() => { void handleSave(eco) }}
                    >
                      {isSaving ? t('saving') : isDirty ? t('save') : t('security.saved')}
                    </ButtonV2>
                  </div>}
                </div>
                {saveState?.error ? <div className="mt-2"><InlineNotice tone="danger">{getApiError(saveState.error).message}</InlineNotice></div> : null}
              </div>
            )
          })}
        </div>
      </section>

      <ConfirmActionDialog
        open={batchConfirmOpen}
        title={t('security.reviewChangesTitle')}
        description={t('security.batchImpact', { count: dirtyEcosystems.length })}
        details={[
          {
            label: t('security.changedEcosystems'),
            value: dirtyEcosystems.map(ecosystem => ecosystem.toUpperCase()).join(', '),
            mono: true,
          },
        ]}
        cancelLabel={t('cancel')}
        confirmLabel={t('security.confirmSaveAll')}
        pendingLabel={t('security.savingAll')}
        confirmVariant="primary"
        pending={isBatchSaving}
        errorMessage={batchSaveError ? t('security.batchSaveFailed') : null}
        onClose={() => {
          if (isBatchSaving) return
          setBatchConfirmOpen(false)
          setBatchSaveError(false)
        }}
        onConfirm={() => { void handleSaveAll() }}
      />

      {/* ── Offline import ────────────────────────── */}
      <section>
        <SectionHeader title={t('security.offlineImport')} hint={t('security.offlineImportDesc')} />
        {canWrite && <div
          data-security-import-dropzone
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
            if (file) submitImport(file)
          }}
          aria-busy={importMutation.isPending || undefined}
        >
          <button
            type="button"
            className="inline-flex min-h-10 flex-col items-center justify-center rounded-[4px] bg-transparent px-4 py-2 text-[var(--text-soft)] stripe-focus-ring"
            onClick={() => fileInputRef.current?.click()}
            disabled={importMutation.isPending}
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
            accept=".json,application/json"
            disabled={importMutation.isPending}
            onChange={handleImport}
            className="sr-only"
          />
        </div>}
        {importMutation.isSuccess && (
          <div className="mt-2"><InlineNotice tone="success">
            <p>{t('security.importSuccess', { count: importMutation.data.data.imported })}</p>
            <p className="mt-1 text-[12px]">
              {t('security.importSummary', {
                received: importMutation.data.data.received,
                packages: importMutation.data.data.packages,
                duplicates: importMutation.data.data.duplicates,
                skipped: importMutation.data.data.skipped,
                rulesCreated: importMutation.data.data.rules_created,
              })}
            </p>
          </InlineNotice></div>
        )}
        {importMutation.isError && (
          <div className="mt-2"><InlineNotice tone="danger">{getApiError(importMutation.error).message}</InlineNotice></div>
        )}
      </section>
    </div>
  )
}

// ─── Main Security Page ──────────────────────────────────────────────

export default function Security() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const serializedSearchParams = searchParams.toString()
  const requestedTab = searchParams.get('tab')
  const hasValidTab = ['overview', 'vulnerabilities', 'suggestions', 'policies'].includes(requestedTab ?? '')
  const tab = hasValidTab
    ? requestedTab!
    : 'overview'
  const activeTabRef = useRef(tab)
  useEffect(() => {
    activeTabRef.current = tab
  }, [tab])
  useEffect(() => {
    if (requestedTab === null || hasValidTab) return
    const nextParams = new URLSearchParams(serializedSearchParams)
    nextParams.delete('tab')
    setSearchParams(nextParams, { replace: true })
  }, [hasValidTab, requestedTab, serializedSearchParams, setSearchParams])

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
    <AdminPage description={t('security.subtitle')}>
    <div className="space-y-6">
      <TabsV2
        items={tabs}
        value={tab}
        onValueChange={(nextTab) => {
          if (nextTab === activeTabRef.current) return
          activeTabRef.current = nextTab
          const nextParams = new URLSearchParams(searchParams)
          if (nextTab === 'overview') nextParams.delete('tab')
          else nextParams.set('tab', nextTab)
          setSearchParams(nextParams)
        }}
        ariaLabel={t('security.title')}
      />
    </div>
    </AdminPage>
  )
}
