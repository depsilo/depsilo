import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel } from '@/components/ui/field'
import { useEffect, useState, useRef, type ComponentProps } from 'react'
import VulnerabilityDetails from '@/admin/security/VulnerabilityDetails'
import type { AxiosResponse } from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { adminApi } from '@/lib/api'
import { formatDate, formatTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import Icon from '@/components/Icon'
import { Badge } from '@/components/ui/badge'
import { Meter, MeterIndicator, MeterTrack } from '@/components/ui/meter'
import Metric from '@/components/Metric'
import SectionHeader from '@/components/SectionHeader'

import DataTable from '@/components/DataTable'
import EcosystemIcon from '@/components/EcosystemIcon'
import QueryErrorState from '@/components/QueryErrorState'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { securityEcosystems, supportsPackageRuleRanges, supportsVulnerabilityAutoBlock } from '@/admin/operatorEcosystems'
import { getAdminRouteHref } from '@/admin/routes'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import type {
  SecurityPolicy,
  SecurityQuery,
  SecuritySeverity,
  SecurityVulnerability,
  UpdateSecurityPolicyRequest,
  CapabilityFact,
} from '@/lib/adminApi.types'

const SEVERITY_BADGE_MAP: Record<string, 'destructive' | 'warning' | 'default' | 'success'> = {
  critical: 'destructive',
  high: 'warning',
  medium: 'default',
  low: 'success',
}

const POLICY_SAVE_CONCURRENCY = 4

type EcosystemName = ComponentProps<typeof EcosystemIcon>['type']

function CapabilityOverview() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['admin', 'capabilities', 'summary'],
    refetchInterval: 60_000,
    queryFn: ({ signal }) => adminApi.getCapabilitySummary({ signal }),
    retry: false,
  })
  const facts = query.data?.data.capabilities ?? []
  const grouped = new Map<string, CapabilityFact[]>()
  for (const fact of facts) {
    const list = grouped.get(fact.name) ?? []
    list.push(fact)
    grouped.set(fact.name, list)
  }
  const capabilityLabels: Record<string, string> = {
    proxy_cache: t('security.capability_proxy_cache'),
    manual_rules: t('security.capability_manual_rules'),
    malicious_blocklist: t('security.capability_malicious_blocklist'),
    vulnerability_scanning: t('security.capability_vulnerability_scanning'),
    minimum_release_age: t('security.capability_minimum_release_age'),
    tamper_alerts: t('security.capability_tamper_alerts'),
    policy_snapshot: t('security.capability_policy_snapshot'),
  }
  const label = (value: string) => capabilityLabels[value] ?? value.replaceAll('_', ' ')
  const value = (fact: CapabilityFact, field: 'support' | 'mode' | 'data_status') => {
    const raw = fact[field]
    if (raw === 'safety_disabled') return t('security.capabilitySafetyDisabled')
    if (raw === 'supported') return t('security.capabilitySupported')
    if (raw === 'unsupported') return t('security.capabilityUnsupported')
    if (raw === 'off') return t('security.capabilityOff')
    if (raw === 'warn') return t('security.capabilityWarn')
    if (raw === 'block') return t('security.capabilityBlock')
    if (raw === 'source_bound') return t('security.capabilitySourceBound')
    if (raw === 'approximate') return t('security.capabilityApproximate')
    if (raw === 'observed') return t('security.capabilityObserved')
    if (raw === 'never_synced') return t('security.capabilityNeverSynced')
    if (raw === 'fresh') return t('security.capabilityFresh')
    if (raw === 'stale') return t('security.capabilityStale')
    if (raw === 'error') return t('security.capabilityError')
    if (raw === 'unknown') return t('security.capabilityUnknown')
    if (raw === 'alert_only') return t('security.capabilityAlertOnly')
    return raw
  }
  return (
    <section aria-label={t('security.capabilityTitle')} className="space-y-3">
      <SectionHeader title={t('security.capabilityTitle')} />
      {query.isPending && <p className="text-[12px] text-[var(--text-soft)]" aria-busy="true">{t('security.capabilityLoading')}</p>}
      {query.isError && <Alert variant={"warning"}><AlertDescription>{t('security.capabilityUnavailable')}</AlertDescription></Alert>}
      {!query.isPending && !query.isError && facts.length === 0 && <Alert variant={"warning"}><AlertDescription>{t('security.capabilityNoData')}</AlertDescription></Alert>}
      {facts.length > 0 && (
        <div className="grid gap-3 md:grid-cols-2">
          {[...grouped.entries()].map(([name, rows]) => (
            <details key={name} className="rounded-md border border-[var(--border)] bg-[var(--bg-soft)] px-3 py-2">
              <summary className="cursor-pointer stripe-focus-ring text-[14px] font-[600]">
                {label(name)}
                {rows.some(fact => fact.data_status === 'error' || fact.data_status === 'stale') && <Badge variant="warning" className="ml-2">{t('security.capabilityNeedsReview')}</Badge>}
              </summary>
              <div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                {rows.map((fact) => (
                  <div key={`${fact.ecosystem ?? 'all'}-${fact.name}`} className="min-w-0 rounded-sm bg-[var(--bg-page)] p-2 text-[12px]">
                    <div className="mb-1 font-[600]">{fact.ecosystem?.toUpperCase() ?? t('security.capabilityOverall')}</div>
                    <div><span className="text-[var(--text-soft)]">{t('security.capabilitySupport')}: </span>{value(fact, 'support')}</div>
                    <div><span className="text-[var(--text-soft)]">{t('security.capabilityMode')}: </span>{value(fact, 'mode')}</div>
                    <div><span className="text-[var(--text-soft)]">{t('security.capabilityData')}: </span>{value(fact, 'data_status')}</div>
                    {fact.name === 'minimum_release_age' && fact.ecosystem === 'docker' && fact.support === 'safety_disabled' && (
                      <div className="mt-1 text-[var(--text-soft)]">{t('security.dockerAgeExcluded')}</div>
                    )}
                    {fact.last_success_at && <div className="text-[var(--text-soft)]">{t('security.capabilityLastSuccess')}: {formatTime(fact.last_success_at)}</div>}
                    {fact.recent_failure && <div className="break-words text-[var(--danger)]">{t('security.capabilityRecentFailure')}: {fact.recent_failure}</div>}
                  </div>
                ))}
              </div>
            </details>
          ))}
        </div>
      )}
    </section>
  )
}

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
  const severityDist = Object.entries(dashboard?.by_severity ?? {}).filter(([, count]) => count > 0).map(([severity, count]) => ({ severity, count }))
  const maxCount = Math.max(...severityDist.map((s) => s.count), 1)

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-12">
        <div aria-hidden="true">
        <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-3 lg:gap-8">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="flex flex-col items-center gap-3">
              <Skeleton className="h-3 w-20 rounded" style={{ background: 'var(--bg-soft)' }} />
              <Skeleton className="h-11 w-32 rounded" style={{ background: 'var(--bg-soft)' }} />
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
    <div className="space-y-6">
      {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
      {/* ── Metrics row ───────────────────────────── */}
      <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-3 lg:gap-8">
        <Link className="stripe-focus-ring rounded-md p-3 no-underline hover:bg-muted" to={`${getAdminRouteHref('security')}?tab=vulnerabilities`}><Metric label={t('security.totalVulnerabilities')} value={String(dashboard?.total_vulnerabilities ?? 0)} /></Link>
        <div className="p-3"><Metric label={t('security.affectedPackages')} value={String(dashboard?.affected_packages ?? 0)} /></div>
        <Link className="stripe-focus-ring rounded-md p-3 no-underline hover:bg-muted" to={`${getAdminRouteHref('security')}?tab=vulnerabilities&severity=critical`}><Metric label={t('security.criticalCount')} value={String(dashboard?.by_severity.critical ?? 0)} /></Link>

      </div>

      {/* ── Scan status + action ───────────────────── */}
      <section>
        <SectionHeader
          title={t('security.scanStatus')}
          action={canWrite ?
            <Button
              onClick={() => scanMutation.mutate()}
              aria-busy={scanInProgress || undefined}
              disabled={scanInProgress}
              size="sm"
            >
              <Icon name="radar" size="sm" />
              {scanInProgress ? t('security.scanning') : t('security.scanNow')}
            </Button>
          : undefined}
        />
        {scanMutation.isSuccess && <div className="mb-3"><Alert variant={"success"}><AlertDescription>{t('security.scanStarted')}</AlertDescription></Alert></div>}
        {scanMutation.isError && <div className="mb-3"><Alert variant={"destructive"}><AlertDescription>{getApiError(scanMutation.error).status === 409 ? t('security.scanConflict') : getApiError(scanMutation.error).message}</AlertDescription></Alert></div>}
        <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
          {t('security.lastScan')}: {dashboard?.last_scan_at ? formatTime(dashboard.last_scan_at, 'relative') : t('security.never')}
        </p>
      </section>

      <p className="text-[13px] leading-6 text-muted-foreground">{t('security.advisoryScope')}</p>
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
                    <Badge variant={variant}>{item.severity.toUpperCase()}</Badge>
                  </div>
                  <Meter className="flex-1" value={item.count} max={maxCount} format={{ style: 'decimal' }} aria-label={`${t('security.totalVulnerabilities')} · ${item.severity}`}>
                    <MeterTrack className="h-2">
                      <MeterIndicator style={{ background: barColors[item.severity] || 'var(--brand)', minWidth: item.count > 0 ? '4px' : '0' }} />
                    </MeterTrack>
                  </Meter>
                  <span className="font-mono text-[12px] tabular-nums w-8 text-right shrink-0" style={{ color: 'var(--text)' }}>
                    {item.count}
                  </span>
                </div>
              )
            })}
          </div>
        ) : (
          <Empty style={{ minHeight: 140 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"verified"} size="sm" /></EmptyMedia><EmptyTitle>{t('security.noVulnerabilities')}</EmptyTitle></EmptyHeader></Empty>
        )}
      </section>
      <CapabilityOverview />
    </div>
  )
}

// ─── Vulnerabilities Tab ─────────────────────────────────────────────

function VulnerabilitiesTab() {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<SecurityVulnerability | null>(null)
  const [filterParams, setFilterParams] = useSearchParams()
  const ecosystem = filterParams.get('ecosystem') || ''
  const severity = filterParams.get('severity') || ''
  const appliedSearch = filterParams.get('package') || ''
  const page = Math.max(1, Number.isSafeInteger(Number(filterParams.get('vpage'))) ? Number(filterParams.get('vpage')) : 1)
  function updateFilters(patch: Record<string, string>) {
    setFilterParams(current => { const next = new URLSearchParams(current); for (const [key, value] of Object.entries(patch)) { if (value && !(key === 'vpage' && value === '1')) next.set(key, value); else next.delete(key) } return next })
  }
  const setPage = (page: number) => updateFilters({ vpage: String(page) })
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
      render: (v: unknown, row: SecurityVulnerability) => (
        <Button variant="link" className="h-auto min-h-10 p-0 text-left whitespace-normal" onClick={() => setSelected(row)}>{v as string}</Button>
      ),
    },
    {
      key: 'ecosystem',
      label: t('security.ecosystem'),
      render: (v: unknown) => (
        <div className="flex items-center gap-1.5">
          <EcosystemIcon type={v as EcosystemName} size={14} />
          <Badge variant="default">{(v as string)?.toUpperCase()}</Badge>
        </div>
      ),
    },
    {
      key: 'package_name',
      label: t('security.package'),
      render: (v: unknown, row: SecurityVulnerability) => (
        <div className="max-w-sm"><span className="font-mono text-[14px]">{v as string}</span><p className="mt-1 line-clamp-2 text-[13px] text-muted-foreground">{row.summary}</p></div>
      ),
    },
    {
      key: 'severity',
      label: t('security.severity'),
      render: (v: unknown) => {
        const s = v as string
        const variant = SEVERITY_BADGE_MAP[s] || 'default'
        return <Badge variant={variant}>{s?.toUpperCase()}</Badge>
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
    updateFilters({ package: String(new FormData(event.currentTarget).get('package') ?? '').trim(), vpage: '1' })
  }

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <form data-security-vulnerability-filters className="flex flex-wrap items-end gap-3" onSubmit={applySearch}>
        <div className="w-40">
          <Field>
            <FieldLabel>{t('security.ecosystem')}</FieldLabel>
            <NativeSelect value={ecosystem} onChange={(e) => updateFilters({ ecosystem: e.target.value, vpage: '1' })}>
              <option value="">{t('all')}</option>
              {securityEcosystems.map((candidate) => (
                <option key={candidate.id} value={candidate.id}>{candidate.label}</option>
              ))}
            </NativeSelect>
          </Field>
        </div>
        <div className="w-36">
          <Field>
            <FieldLabel>{t('security.severity')}</FieldLabel>
            <NativeSelect value={severity} onChange={(e) => updateFilters({ severity: e.target.value, vpage: '1' })}>
              <option value="">{t('all')}</option>
              {(['critical', 'high', 'medium', 'low', 'unknown'] satisfies SecuritySeverity[]).map((value) => (
                <option key={value} value={value}>{t(`security.${value}`)}</option>
              ))}
            </NativeSelect>
          </Field>
        </div>
        <div className="flex-1 min-w-[200px]">
          <Field>
            <FieldLabel>{t('security.packageSearch')}</FieldLabel>
            <Input className="font-mono"
              key={appliedSearch}
              name="package"
              defaultValue={appliedSearch}
              placeholder={t('security.searchPlaceholder')}
            />
          </Field>
        </div>
        <Button type="submit" size="sm">
          <Icon name="search" size="sm" />
          {t('search')}
        </Button>
      </form>

      {/* Table — bare (no Card wrap) */}
      {query.isPending ? (
        <div role="status" aria-busy="true" className="py-8 text-center text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
      ) : query.isError && !data ? (
        <QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : getApiError(query.error).message} onRetry={() => { void query.refetch() }} />
      ) : (
        <div className="space-y-3">
        {data && query.isRefetchError && <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />}
        {items.length === 0 ? <Empty style={{ minHeight: 200 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"verified"} size="sm" /></EmptyMedia><EmptyTitle>{t(appliedSearch || ecosystem || severity ? 'security.noMatchingAdvisories' : 'security.noVulnerabilities')}</EmptyTitle><EmptyDescription>{t('security.advisoryScope')}</EmptyDescription></EmptyHeader></Empty> : <><ul className="divide-y sm:hidden" aria-label={t('security.vulnerabilitiesTable')}>
          {items.map(item => <li key={item.id} className="py-4 space-y-2"><Button variant="link" className="h-auto min-h-10 p-0 whitespace-normal text-left break-all" onClick={() => setSelected(item)}>{item.package_name} · {item.osv_id}</Button><p className="text-[14px] text-muted-foreground break-words">{item.summary}</p><Badge variant={SEVERITY_BADGE_MAP[item.severity] ?? 'default'}>{t(`security.${item.severity}`)}</Badge></li>)}
        </ul><div className="hidden sm:block"><DataTable
          columns={columns}
          data={items.map((item) => ({ ...item }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('security.vulnerabilitiesTable')}
          minWidth={800}
        /></div></>}
        </div>
      )}

      <AdminPagination ready={query.isSuccess && !query.isFetching} page={page} pageSize={perPage} total={total} onPageChange={setPage} />
      <VulnerabilityDetails item={selected} onClose={() => setSelected(null)} />
    </div>
  )
}

// ─── Suggestions Tab ─────────────────────────────────────────────────

function SuggestionsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const [pageParams, setPageParams] = useSearchParams()
  const page = Math.max(1, Number.isSafeInteger(Number(pageParams.get('spage'))) ? Number(pageParams.get('spage')) : 1)
  const setPage = (page: number) => setPageParams(current => { const next = new URLSearchParams(current); if (page > 1) next.set('spage', String(page)); else next.delete('spage'); return next })
  const [dismissTarget, setDismissTarget] = useState<SecurityVulnerability | null>(null)
  const [selected, setSelected] = useState<SecurityVulnerability | null>(null)

  const query = useQuery({
    queryKey: ['admin', 'security', 'suggestions', { page }],
    queryFn: ({ signal }) => adminApi.listSuggestions({ page, per_page: 20 }, { signal }),
    retry: false,
  })
  const { data } = query

  const items = data?.data.items ?? []
  const total = data?.data.total ?? items.length

  const dismissMutation = useMutation({
    mutationFn: (vulnId: number) => adminApi.dismissSuggestion(vulnId),
    onSuccess: () => {
      setDismissTarget(null)
      queryClient.invalidateQueries({ queryKey: ['admin', 'security'] })
    },
  })

  function openDismiss(item: SecurityVulnerability) {
    dismissMutation.reset()
    setDismissTarget(item)
  }

  function closeDismiss() {
    if (dismissMutation.isPending) return
    setDismissTarget(null)
    dismissMutation.reset()
  }

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-4">
        <div aria-hidden="true" className="contents">
        {[...Array(3)].map((_, i) => (
          <Skeleton key={i} className="h-20 rounded" style={{ background: 'var(--bg-soft)' }} />
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
      {items.length > 0 && <Alert variant={"warning"}><AlertDescription>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>{t('security.manualRuleRequired')}</span>
          <Link
            to={getAdminRouteHref('rules')}
            className="stripe-focus-ring inline-flex min-h-10 shrink-0 items-center rounded-sm px-2 text-[12px] font-[650] text-[var(--brand-text)] no-underline hover:bg-[var(--bg-hover)]"
          >
            {t('security.openPackageRules')}
          </Link>
        </div>
      </AlertDescription></Alert>}
      {items.length === 0 ? <Empty style={{ minHeight: 240 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"verified"} size="sm" /></EmptyMedia><EmptyTitle>{t('security.noSuggestions')}</EmptyTitle></EmptyHeader></Empty> : <>
      <div>
        {items.map((item: SecurityVulnerability, idx: number) => {
          const severityVariant = SEVERITY_BADGE_MAP[item.severity] || 'default'
          const isActing = dismissMutation.isPending && dismissMutation.variables === item.id

          return (
            <div
              key={item.id}
              className="flex min-w-0 flex-col gap-3 py-4 sm:flex-row sm:items-start sm:justify-between sm:gap-4"
              style={{ borderBottom: idx < items.length - 1 ? '1px solid var(--border)' : 'none' }}
            >
              <div className="flex-1 min-w-0">
                <div className="mb-1 flex flex-wrap items-center gap-2">
                  <Button variant="link" className="h-auto min-h-10 p-0 text-left whitespace-normal" onClick={() => setSelected(item)}>{item.osv_id}</Button>
                  <Badge variant={severityVariant}>{item.severity?.toUpperCase()}</Badge>
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
                <p className="text-[14px] leading-6 text-muted-foreground break-words">{item.summary}</p>
              </div>
              {canWrite && <div className="flex shrink-0 flex-wrap gap-2 self-end sm:self-start">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={isActing}
                  onClick={() => openDismiss(item)}
                >
                  <Icon name="close" size="sm" />
                  {t('security.dismiss')}
                </Button>
              </div>}
            </div>
          )
        })}
      </div>

      </>}
      <AdminPagination ready={query.isSuccess && !query.isFetching} page={page} pageSize={20} total={total} onPageChange={setPage} />
      <VulnerabilityDetails item={selected} onClose={() => setSelected(null)} />
      <ConfirmActionDialog
        open={dismissTarget !== null}
        title={t('security.dismissTitle')}
        description={t('security.dismissImpact', { name: dismissTarget?.package_name ?? '' })}
        details={dismissTarget ? [
          { label: t('security.package'), value: dismissTarget.package_name, mono: true },
          { label: t('security.ecosystem'), value: dismissTarget.ecosystem.toUpperCase() },
          { label: t('security.osvId'), value: dismissTarget.osv_id, mono: true },
          {
            label: t('security.cvssScore'),
            value: Number(dismissTarget.cvss_score).toFixed(1),
            mono: true,
          },
        ] : []}
        cancelLabel={t('cancel')}
        confirmLabel={t('security.confirmDismiss')}
        pendingLabel={t('security.dismissing')}
        pending={dismissMutation.isPending}
        errorMessage={dismissMutation.isError ? getApiError(dismissMutation.error).message : null}
        onClose={closeDismiss}
        onConfirm={() => {
          if (!dismissTarget || !canWrite) return
          dismissMutation.mutate(dismissTarget.id)
        }}
      />
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
      ecosystems.map(ecosystem => {
        const autoBlockSupported = supportsVulnerabilityAutoBlock(ecosystem)
        return [ecosystem, {
          auto_block_enabled: autoBlockSupported && bulkPolicy.auto_block_enabled,
          min_cvss_score: autoBlockSupported
            ? bulkPolicy.min_cvss_score
            : getPolicy(ecosystem).min_cvss_score,
        }]
      }),
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

  const ecosystems = securityEcosystems.map(ecosystem => ecosystem.id).filter(ecosystem =>
    policies.some(policy => policy.ecosystem === ecosystem && policy.auto_block_enabled) || localPolicies[ecosystem],
  )
  const dirtyEcosystems = ecosystems.filter(isPolicyDirty)
  const visibleEcosystems = showChangedOnly ? dirtyEcosystems : ecosystems
  const hasPolicyWrites = policyWriteCount > 0
  const hasAutoBlockSupport = ecosystems.some(supportsVulnerabilityAutoBlock)

  if (query.isPending) {
    return (
      <div aria-busy="true" className="space-y-2">
        <div aria-hidden="true" className="contents">
        {[...Array(4)].map((_, i) => (
          <Skeleton key={i} className="h-12 rounded" style={{ background: 'var(--bg-soft)' }} />
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
      {/* ── Offline import ────────────────────────── */}
      <section>
        <SectionHeader title={t('security.offlineImport')} hint={t('security.offlineImportDesc')} />
        {canWrite && <div
          data-security-import-dropzone
          className="rounded-sm p-6 text-center transition-colors duration-150"
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
          <Button variant="ghost"
            type="button"
            className="h-auto min-h-[40px] flex-col whitespace-normal px-4 py-2 text-muted-foreground"
            onClick={() => fileInputRef.current?.click()}
            disabled={importMutation.isPending}
          >
            <Icon name="upload_file" size="lg" />
            <span className="mt-2 text-[13px]">
              {importMutation.isPending ? t('security.importing') : t('security.dropOrClick')}
            </span>
          </Button>
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
          <div className="mt-2"><Alert variant={"success"}><AlertDescription>
            <p>{t('security.importSuccess', { count: importMutation.data.data.imported })}</p>
            <p className="mt-1 text-[12px]">
              {t('security.importSummary', {
                received: importMutation.data.data.received,
                packages: importMutation.data.data.packages,
                duplicates: importMutation.data.data.duplicates,
                skipped: importMutation.data.data.skipped,
              })}
            </p>
          </AlertDescription></Alert></div>
        )}
        {importMutation.isError && (
          <div className="mt-2"><Alert variant={"destructive"}><AlertDescription>{getApiError(importMutation.error).message}</AlertDescription></Alert></div>
        )}
      </section>
      {ecosystems.length === 0 && <Alert><AlertDescription>{t('security.noLegacyPolicies')}</AlertDescription></Alert>}
      {/* Only actionable legacy policies belong in this migration section. */}
      {ecosystems.length > 0 && <section>
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
              <Field orientation="horizontal" disabled={!hasAutoBlockSupport || isBatchSaving || hasPolicyWrites}>
                <Switch
                  checked={bulkPolicy.auto_block_enabled}
                  disabled={!hasAutoBlockSupport || isBatchSaving || hasPolicyWrites}
                  onCheckedChange={(checked) => setBulkPolicy(current => ({
                    ...current,
                    auto_block_enabled: checked,
                  }))}
                />
                <FieldLabel>{t('security.bulkAutoBlock')}</FieldLabel>
              </Field>
              <p className="mt-1 text-[12px] leading-5 text-[var(--text-soft)]">
                {t('security.bulkPolicyHint')}
              </p>
            </div>
            <Field disabled={!hasAutoBlockSupport || !bulkPolicy.auto_block_enabled || isBatchSaving || hasPolicyWrites}>
              <FieldLabel>{t('security.cvssThreshold')}</FieldLabel>
              <Input className="font-mono"
                type="number"
                min={0}
                max={10}
                step={0.1}
                value={bulkPolicy.min_cvss_score}
                disabled={!hasAutoBlockSupport || !bulkPolicy.auto_block_enabled || isBatchSaving || hasPolicyWrites}
                onChange={(event) => setBulkPolicy(current => ({
                  ...current,
                  min_cvss_score: Number.parseFloat(event.target.value) || 0,
                }))}
              />
            </Field>
            <Button
              type="button"
              variant="outline"
              onClick={applyBulkPolicy}
              disabled={isBatchSaving || hasPolicyWrites}
            >
              {t('security.applyToAll')}
            </Button>
          </div>
        )}
        <div className="mb-2 flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <span className="text-[12px] text-[var(--text-soft)]" aria-live="polite">
              {t('security.unsavedCount', { count: dirtyEcosystems.length })}
            </span>
            <Field orientation="horizontal">
              <Switch
                checked={showChangedOnly}
                onCheckedChange={setShowChangedOnly}
              />
              <FieldLabel>{t('security.showChangedOnly')}</FieldLabel>
            </Field>
          </div>
          {canWrite && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={resetPolicyChanges}
                disabled={dirtyEcosystems.length === 0 || isBatchSaving || hasPolicyWrites}
              >
                {t('security.resetChanges')}
              </Button>
              <Button
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
              </Button>
            </div>
          )}
        </div>
        <div>
          {visibleEcosystems.length === 0 ? (
            <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"task_alt"} size="sm" /></EmptyMedia><EmptyTitle>{t('security.noChangedPolicies')}</EmptyTitle>{t('security.noChangedPoliciesHint') && <EmptyDescription>{t('security.noChangedPoliciesHint')}</EmptyDescription>}</EmptyHeader></Empty>
          ) : visibleEcosystems.map((eco, idx) => {
            const policy = getPolicy(eco)
            const saveState = policySaveStates[eco]
            const isSaving = saveState?.isPending ?? false
            const isDirty = isPolicyDirty(eco)
            const autoBlockSupported = supportsVulnerabilityAutoBlock(eco)
            const autoBlockUnavailableHint = supportsPackageRuleRanges(eco)
              ? 'security.autoBlockUnavailableOSVProjection'
              : eco === 'apt'
                ? 'security.autoBlockUnavailableApt'
                : eco === 'composer'
                  ? 'security.autoBlockUnavailableComposer'
                  : eco === 'rubygems'
                    ? 'security.autoBlockUnavailableNoPackageRules'
                    : 'security.autoBlockUnavailableExactOnly'
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
                    {isDirty && <Badge variant="warning">{t('security.unsaved')}</Badge>}
                  </div>

                  <div className="justify-self-end text-[var(--text-soft)] sm:justify-self-start">
                    <Field orientation="horizontal" disabled={!canWrite || isSaving || isBatchSaving || (!autoBlockSupported && !policy.auto_block_enabled)}>
                      <Switch
                        aria-label={`${eco.toUpperCase()} ${t('security.autoBlock')}`}
                        checked={policy.auto_block_enabled}
                        disabled={!canWrite || isSaving || isBatchSaving || (!autoBlockSupported && !policy.auto_block_enabled)}
                        onCheckedChange={(checked) => setPolicy(eco, { auto_block_enabled: checked })}
                      />
                      <FieldLabel>{t('security.autoBlock')}</FieldLabel>
                    </Field>
                  </div>

                  {/* CVSS threshold */}
                  <div data-security-policy-score className="min-w-0 sm:w-28">
                    <Field disabled={!canWrite || isSaving || isBatchSaving || !autoBlockSupported || !policy.auto_block_enabled}>
                      <FieldLabel>{t('security.cvssThreshold')}</FieldLabel>
                      <Input
                        aria-label={`${eco.toUpperCase()} ${t('security.cvssThreshold')}`}
                        type="number"
                        min={0}
                        max={10}
                        step={0.1}
                        value={policy.min_cvss_score}
                        disabled={!canWrite || isSaving || isBatchSaving || !autoBlockSupported || !policy.auto_block_enabled}
                        onChange={(e) => setPolicy(eco, { min_cvss_score: parseFloat(e.target.value) || 0 })}
                        className={['font-mono', "px-2 py-1 text-center"].join(' ')}
                      />
                    </Field>
                  </div>

                  {canWrite && <div data-security-policy-save className="justify-self-end">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`${eco.toUpperCase()} ${t('save')}`}
                      aria-busy={isSaving || undefined}
                      disabled={isSaving || isBatchSaving || !isDirty}
                      onClick={() => { void handleSave(eco) }}
                    >
                      {isSaving ? t('saving') : isDirty ? t('save') : t('security.saved')}
                    </Button>
                  </div>}
                </div>
                {!autoBlockSupported && (
                  <p className="mt-1 text-[12px] leading-5 text-[var(--text-soft)]">
                    {t(autoBlockUnavailableHint)}
                  </p>
                )}
                {saveState?.error ? <div className="mt-2"><Alert variant={"destructive"}><AlertDescription>{getApiError(saveState.error).message}</AlertDescription></Alert></div> : null}
              </div>
            )
          })}
        </div>
      </section>}

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

  const views = [
    { key: 'overview', label: t('security.overview'), content: <OverviewTab /> },
    { key: 'vulnerabilities', label: t('security.vulnerabilities'), content: <VulnerabilitiesTab /> },
    { key: 'suggestions', label: t('security.suggestions'), content: <SuggestionsTab /> },
    { key: 'policies', label: t('security.policies'), content: <PoliciesTab /> },
  ]

  return (
    <AdminPage description={t('security.subtitle')}>
    <div className="space-y-6">
      <div className="max-w-xs">
          <Field>
            <FieldLabel>{t('security.view')}</FieldLabel>
            <NativeSelect
              value={tab}
              onChange={(event) => {
                const nextTab = event.target.value
                if (nextTab === activeTabRef.current) return
                activeTabRef.current = nextTab
                const nextParams = new URLSearchParams(searchParams)
                if (nextTab === 'overview') nextParams.delete('tab')
                else nextParams.set('tab', nextTab)
                setSearchParams(nextParams)
              }}
            >
              {views.map(view => <option key={view.key} value={view.key}>{view.label}</option>)}
            </NativeSelect>
          </Field>
      </div>
      {views.find(view => view.key === tab)?.content}
    </div>
    </AdminPage>
  )
}
