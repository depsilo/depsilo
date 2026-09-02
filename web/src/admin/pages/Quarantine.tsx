// Quarantine — supply-chain monitor surface for the minimum-release-age
// subsystem (T1 Task 1). Two tabs:
//   - Events: recent quarantine decisions (block / bypass / approve /
//     revoke). Filterable by ecosystem + action.
//   - Approvals: operator's active permanent approvals; each row has a
//     revoke action.
//
// Wedge feature per docs/DIRECTION.md — open-source, no Pro gating,
// no upgrade callout.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import ModalV2 from '@/components/Modal'
import InputV2 from '@/components/Input'
import TableViewport from '@/components/TableViewport'
import QueryErrorState from '@/components/QueryErrorState'
import TabsV2 from '@/components/Tabs'
import AdminPage from '@/admin/components/AdminPage'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { maliciousBlocklistEcosystems } from '@/admin/operatorEcosystems'

// Backend mirrors db.QuarantineEvent + db.ApprovedVersion. We keep
// the shape narrow to avoid leaking schema details into TS — extra
// fields are fine to ignore at the parsing boundary.
type QuarantineEvent = {
  id: number
  ecosystem: string
  package: string
  version: string
  action: string
  reason: string
  threshold_seconds: number
  age_at_call_seconds: number
  actor_id: number
  client_ip: string
  created_at: string
}

type ApprovedVersion = {
  id: number
  ecosystem: string
  package: string
  version: string
  reason: string
  approved_by: number
  created_at: string
}

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'docker', 'huggingface']
const ACTIONS = ['blocked', 'malware_blocked', 'tamper_detected', 'served_eligible', 'bypassed', 'malware_bypassed', 'warned', 'malware_warned', 'approved', 'approval_revoked', 'override_created', 'override_revoked']

type BlocklistStatus = {
  enabled: boolean
  mode: string
  last_sync_at: string | null
  last_success_at: string | null
  last_error: string
  duration_ms: number
  entry_count: number
  per_ecosystem: Record<string, number>
  ecosystems: string[]
  running?: boolean
  next_sync_at?: string | null
}

type MalwareOverride = {
  id: number
  ecosystem: string
  package: string
  version: string
  reason: string
  actor_id: number
  created_at: string
  expires_at: string
}

function actionBadge(action: string, t: (k: string) => string) {
  switch (action) {
    case 'blocked':
      return <BadgeV2 variant="error">{t('quarantine.action.blocked')}</BadgeV2>
    case 'malware_blocked':
      return <BadgeV2 variant="error">{t('quarantine.action.malware_blocked')}</BadgeV2>
    case 'tamper_detected':
      return <BadgeV2 variant="error">{t('quarantine.action.tamper_detected')}</BadgeV2>
    case 'served_eligible':
      return <BadgeV2 variant="warning">{t('quarantine.action.served_eligible')}</BadgeV2>
    case 'bypassed':
      return <BadgeV2 variant="success">{t('quarantine.action.bypassed')}</BadgeV2>
    case 'malware_bypassed':
      return <BadgeV2 variant="warning">{t('quarantine.action.malware_bypassed')}</BadgeV2>
    case 'warned':
      return <BadgeV2 variant="warning">{t('quarantine.action.warned')}</BadgeV2>
    case 'malware_warned':
      return <BadgeV2 variant="warning">{t('quarantine.action.malware_warned')}</BadgeV2>
    case 'approved':
      return <BadgeV2 variant="success">{t('quarantine.action.approved')}</BadgeV2>
    case 'approval_revoked':
      return <BadgeV2>{t('quarantine.action.approval_revoked')}</BadgeV2>
    case 'override_created':
      return <BadgeV2 variant="warning">{t('quarantine.action.override_created')}</BadgeV2>
    case 'override_revoked':
      return <BadgeV2>{t('quarantine.action.override_revoked')}</BadgeV2>
    default:
      return <BadgeV2>{action}</BadgeV2>
  }
}

export default function Quarantine() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { canWrite } = usePrincipal()
  const [tab, setTab] = useState<'events' | 'approvals' | 'blocklist'>('events')

  // Filters (events tab)
  const [ecoFilter, setEcoFilter] = useState('all')
  const [actionFilter, setActionFilter] = useState('all')
  const [pkgSearch, setPkgSearch] = useState('')

  const [revokeOpen, setRevokeOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApprovedVersion | null>(null)
  const [revokeReason, setRevokeReason] = useState('')

  // ── Data ──────────────────────────────────────────────────────
  const eventsParams: Record<string, string | number> = { limit: 100 }
  if (ecoFilter !== 'all') eventsParams.ecosystem = ecoFilter
  if (actionFilter !== 'all') eventsParams.action = actionFilter
  if (pkgSearch.trim()) eventsParams.package = pkgSearch.trim()

  const eventsQ = useQuery({
    queryKey: ['admin', 'quarantine', 'events', eventsParams],
    queryFn: async ({ signal }) => {
      const res = await adminApi.listQuarantineEvents(eventsParams, { signal })
      return res.data as { items: QuarantineEvent[]; total: number }
    },
    enabled: tab === 'events',
    refetchInterval: 30_000,
    retry: false,
  })

  const approvalsQ = useQuery({
    queryKey: ['admin', 'quarantine', 'approvals'],
    queryFn: async ({ signal }) => {
      const res = await adminApi.listQuarantineApprovals({ limit: 200 }, { signal })
      return res.data as { items: ApprovedVersion[]; total: number }
    },
    enabled: tab === 'approvals',
    refetchInterval: 30_000,
    retry: false,
  })

  // ── Mutations ─────────────────────────────────────────────────
  const revokeM = useMutation({
    mutationFn: () =>
      adminApi.revokeQuarantineApproval(revokeTarget!.id, { reason: revokeReason.trim() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'quarantine'] })
      setRevokeOpen(false)
      setRevokeTarget(null)
      setRevokeReason('')
    },
  })

  // ── Render ────────────────────────────────────────────────────
  return (
    <AdminPage description={t('quarantine.subtitle')}>
    <div className="space-y-6">
      <InlineNotice tone="warning">{t('quarantine.minimum_age_unavailable')}</InlineNotice>
      <TabsV2
        value={tab}
        onValueChange={(value) => setTab(value as typeof tab)}
        ariaLabel={t('quarantine.title')}
        items={[
          {
            key: 'events',
            label: t('quarantine.tab.events'),
            content: <EventsTab
              eventsQ={eventsQ}
              ecoFilter={ecoFilter}
              setEcoFilter={setEcoFilter}
              actionFilter={actionFilter}
              setActionFilter={setActionFilter}
              pkgSearch={pkgSearch}
              setPkgSearch={setPkgSearch}
            />,
          },
          {
            key: 'approvals',
            label: t('quarantine.tab.approvals'),
            content: <ApprovalsTab approvalsQ={approvalsQ} canWrite={canWrite} onRevoke={(row) => {
              revokeM.reset()
              setRevokeTarget(row)
              setRevokeReason('')
              setRevokeOpen(true)
            }} />,
          },
          { key: 'blocklist', label: t('quarantine.tab.blocklist'), content: <BlocklistTab /> },
        ]}
      />

      {/* Revoke dialog */}
      <ModalV2
        open={revokeOpen}
        onClose={() => setRevokeOpen(false)}
        title={t('quarantine.revoke.title')}
        closeDisabled={revokeM.isPending}
      >
        {revokeTarget && (
          <div className="space-y-4">
            <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
              {t('quarantine.revoke.body', { eco: revokeTarget.ecosystem, pkg: revokeTarget.package, ver: revokeTarget.version })}
            </p>
            <InputV2
              label={t('quarantine.col.reason')}
              placeholder={t('quarantine.reason_placeholder')}
              value={revokeReason}
              onChange={(e) => setRevokeReason(e.target.value)}
              disabled={revokeM.isPending}
              autoFocus
            />
            {revokeM.isError && <InlineNotice tone="danger">{getApiError(revokeM.error).message}</InlineNotice>}
            <div className="flex justify-end gap-2">
              <ButtonV2 variant="secondary" onClick={() => setRevokeOpen(false)} disabled={revokeM.isPending}>
                {t('cancel')}
              </ButtonV2>
              <ButtonV2
                variant="danger"
                onClick={() => revokeM.mutate()}
                aria-busy={revokeM.isPending || undefined}
                disabled={revokeReason.trim().length < 3 || revokeM.isPending || !canWrite}
              >
                {revokeM.isPending ? t('quarantine.revoke.submitting') : t('quarantine.revoke.submit')}
              </ButtonV2>
            </div>
          </div>
        )}
      </ModalV2>
    </div>
    </AdminPage>
  )
}

// ── Events tab ─────────────────────────────────────────────────────

function EventsTab(props: {
  eventsQ: ReturnType<typeof useQuery>
  ecoFilter: string; setEcoFilter: (v: string) => void
  actionFilter: string; setActionFilter: (v: string) => void
  pkgSearch: string; setPkgSearch: (v: string) => void
}) {
  const { t } = useTranslation()
  const data = props.eventsQ.data as { items: QuarantineEvent[]; total: number } | undefined
  const items = data?.items ?? []

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
        <InputV2
          aria-label={t('quarantine.filter.package_placeholder')}
          placeholder={t('quarantine.filter.package_placeholder')}
          value={props.pkgSearch}
          onChange={(e) => props.setPkgSearch(e.target.value)}
          className="col-span-2 sm:w-[260px]"
        />
        <FilterSelect
          label={t('quarantine.col.ecosystem')}
          value={props.ecoFilter}
          onChange={props.setEcoFilter}
          options={[
            { value: 'all', label: t('quarantine.filter.all_ecosystems') },
            ...ECOSYSTEMS.map((eco) => ({ value: eco, label: eco })),
          ]}
        />
        <FilterSelect
          label={t('quarantine.col.action')}
          value={props.actionFilter}
          onChange={props.setActionFilter}
          options={[
            { value: 'all', label: t('quarantine.filter.all_actions') },
            ...ACTIONS.map((a) => ({ value: a, label: t(`quarantine.action.${a}`) })),
          ]}
        />
      </div>

      {/* Table */}
      {props.eventsQ.isPending ? (
        <p aria-busy="true" className="text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></p>
      ) : props.eventsQ.isError && !props.eventsQ.data ? (
        <QueryErrorState message={getApiError(props.eventsQ.error).status === 403 ? t('common.permissionDenied') : getApiError(props.eventsQ.error).message} onRetry={() => { void props.eventsQ.refetch() }} />
      ) : (
        <div className="space-y-3">
        {Boolean(data) && props.eventsQ.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void props.eventsQ.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
        {items.length === 0 ? <EmptyState
          icon="policy"
          title={t('quarantine.events.empty_title')}
          hint={t('quarantine.events.empty_hint')}
        /> : (
        <>
        <ul
          aria-label={t('quarantine.events.table')}
          data-quarantine-mobile-list="events"
          className="divide-y divide-[var(--border)] sm:hidden"
        >
          {items.map((ev) => (
            <li key={ev.id} className="space-y-3 py-4 first:pt-0">
              <div className="flex min-w-0 items-start justify-between gap-3">
                <MobilePackageIdentity
                  ecosystem={ev.ecosystem}
                  packageName={ev.package}
                  version={ev.version}
                />
                <div className="shrink-0">{actionBadge(ev.action, t)}</div>
              </div>
              <MobileMetadata
                entries={[
                  { label: t('quarantine.col.reason'), value: ev.reason },
                  { label: t('quarantine.col.time'), value: formatTime(ev.created_at), mono: true },
                ]}
              />
            </li>
          ))}
        </ul>
        <div className="hidden sm:block">
        <TableViewport label={t('quarantine.events.table')} minWidth={920}>
          <div className="rounded-[8px] border" style={{ borderColor: 'var(--border)' }}>
            <table className="w-full" style={{ borderCollapse: 'collapse' }}>
            <thead style={{ background: 'var(--bg-soft)' }}>
              <tr>
                <Th>{t('quarantine.col.time')}</Th>
                <Th>{t('quarantine.col.ecosystem')}</Th>
                <Th>{t('quarantine.col.package')}</Th>
                <Th>{t('quarantine.col.version')}</Th>
                <Th>{t('quarantine.col.action')}</Th>
                <Th>{t('quarantine.col.reason')}</Th>
              </tr>
            </thead>
            <tbody>
              {items.map((ev) => (
                <tr key={ev.id} style={{ borderTop: '0.5px solid var(--border)' }}>
                  <Td>
                    <span className="text-[12px] font-mono whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>
                      {formatTime(ev.created_at)}
                    </span>
                  </Td>
                  <Td>
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(ev.ecosystem) && <EcosystemIcon type={ev.ecosystem} size={14} />}
                      <span className="text-[12px] font-mono">{ev.ecosystem}</span>
                    </div>
                  </Td>
                  <Td><span className="text-[13px] font-mono">{ev.package}</span></Td>
                  <Td><span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>{ev.version}</span></Td>
                  <Td>{actionBadge(ev.action, t)}</Td>
                  <Td>
                    <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{ev.reason}</span>
                  </Td>
                </tr>
              ))}
            </tbody>
            </table>
          </div>
        </TableViewport>
        </div>
        </>
        )}
        </div>
      )}
    </div>
  )
}

// ── Approvals tab ──────────────────────────────────────────────────

function ApprovalsTab(props: {
  approvalsQ: ReturnType<typeof useQuery>
  canWrite: boolean
  onRevoke: (row: ApprovedVersion) => void
}) {
  const { t } = useTranslation()
  const data = props.approvalsQ.data as { items: ApprovedVersion[]; total: number } | undefined
  const items = data?.items ?? []

  if (props.approvalsQ.isPending) {
    return <p aria-busy="true" className="text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></p>
  }
  if (props.approvalsQ.isError && !props.approvalsQ.data) {
    const normalized = getApiError(props.approvalsQ.error)
    return <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void props.approvalsQ.refetch() }} />
  }
  return (
    <div className="space-y-3">
    {Boolean(data) && props.approvalsQ.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void props.approvalsQ.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
    {items.length === 0 ? (
      <EmptyState
        icon="verified"
        title={t('quarantine.approvals.empty_title')}
        hint={t('quarantine.approvals.empty_hint')}
      />
    ) : (
    <>
    <ul
      aria-label={t('quarantine.approvals.table')}
      data-quarantine-mobile-list="approvals"
      className="divide-y divide-[var(--border)] sm:hidden"
    >
      {items.map((row) => (
        <li key={row.id} className="space-y-3 py-4 first:pt-0">
          <div className="flex min-w-0 items-start justify-between gap-3">
            <MobilePackageIdentity
              ecosystem={row.ecosystem}
              packageName={row.package}
              version={row.version}
            />
            <div className="shrink-0">{actionBadge('approved', t)}</div>
          </div>
          <MobileMetadata
            entries={[
              { label: t('quarantine.col.reason'), value: row.reason },
              { label: t('quarantine.col.created_at'), value: formatTime(row.created_at), mono: true },
            ]}
          />
          {props.canWrite && (
            <ButtonV2
              className="min-h-[40px] w-full"
              variant="danger"
              onClick={() => props.onRevoke(row)}
            >
              <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
            </ButtonV2>
          )}
        </li>
      ))}
    </ul>
    <div className="hidden sm:block">
    <TableViewport label={t('quarantine.approvals.table')} minWidth={820}>
      <div className="rounded-[8px] border" style={{ borderColor: 'var(--border)' }}>
        <table className="w-full" style={{ borderCollapse: 'collapse' }}>
        <thead style={{ background: 'var(--bg-soft)' }}>
          <tr>
            <Th>{t('quarantine.col.created_at')}</Th>
            <Th>{t('quarantine.col.ecosystem')}</Th>
            <Th>{t('quarantine.col.package')}</Th>
            <Th>{t('quarantine.col.version')}</Th>
            <Th>{t('quarantine.col.reason')}</Th>
            <Th>{' '}</Th>
          </tr>
        </thead>
        <tbody>
          {items.map((row) => (
            <tr key={row.id} style={{ borderTop: '0.5px solid var(--border)' }}>
              <Td>
                <span className="text-[12px] font-mono whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>
                  {formatTime(row.created_at)}
                </span>
              </Td>
              <Td>
                <div className="flex items-center gap-1.5">
                  {isAdminEcosystem(row.ecosystem) && <EcosystemIcon type={row.ecosystem} size={14} />}
                  <span className="text-[12px] font-mono">{row.ecosystem}</span>
                </div>
              </Td>
              <Td><span className="text-[13px] font-mono">{row.package}</span></Td>
              <Td><span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>{row.version}</span></Td>
              <Td><span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{row.reason}</span></Td>
              <Td>
                {props.canWrite && <ButtonV2 size="sm" variant="danger" onClick={() => props.onRevoke(row)}>
                  <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                </ButtonV2>}
              </Td>
            </tr>
          ))}
        </tbody>
        </table>
      </div>
    </TableViewport>
    </div>
    </>
    )}
    </div>
  )
}

// ── Blocklist tab ──────────────────────────────────────────────────
//
// Sync status + entry counts + manual refresh, and the 24h-expiring
// false-positive overrides. Blocked-request events show up in the
// Events tab (action = malware_blocked) — this tab is about the
// dataset and the exemptions, not the traffic.

function BlocklistTab() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { canWrite } = usePrincipal()

  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ ecosystem: 'npm', package: '', version: '', reason: '' })
  const [revokeTarget, setRevokeTarget] = useState<MalwareOverride | null>(null)
  const [revokeReason, setRevokeReason] = useState('')
  const [mountedAt] = useState(() => Date.now())

  const statusQ = useQuery({
    queryKey: ['admin', 'blocklist', 'status'],
    queryFn: async ({ signal }) => (await adminApi.getBlocklistStatus({ signal })).data as BlocklistStatus,
    refetchInterval: 15_000,
    retry: false,
  })
  const overridesQ = useQuery({
    queryKey: ['admin', 'blocklist', 'overrides'],
    queryFn: async ({ signal }) => (await adminApi.listBlocklistOverrides({ signal })).data as { items: MalwareOverride[]; now: string },
    refetchInterval: 30_000,
    retry: false,
  })

  const syncM = useMutation({
    mutationFn: () => adminApi.triggerBlocklistSync(),
    onSuccess: () => {
      // The sync runs async server-side; refresh status shortly after
      // so LastSyncAt flips while the operator is still looking.
      setTimeout(() => qc.invalidateQueries({ queryKey: ['admin', 'blocklist'] }), 1500)
    },
  })
  const createM = useMutation({
    mutationFn: async () => (await adminApi.createBlocklistOverride(form)).data as MalwareOverride,
    onSuccess: (created) => {
      qc.setQueryData<{ items: MalwareOverride[]; now: string }>(['admin', 'blocklist', 'overrides'], (current) => current ? {
        ...current,
        items: [...current.items.filter((item) => item.id !== created.id), created],
      } : { items: [created], now: created.created_at })
      qc.invalidateQueries({ queryKey: ['admin', 'quarantine'] })
      setCreateOpen(false)
      setForm({ ecosystem: 'npm', package: '', version: '', reason: '' })
    },
  })
  const revokeM = useMutation({
    mutationFn: () => adminApi.revokeBlocklistOverride(revokeTarget!.id, { reason: revokeReason.trim() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'blocklist'] })
      qc.invalidateQueries({ queryKey: ['admin', 'quarantine'] })
      setRevokeTarget(null)
      setRevokeReason('')
    },
  })

  const st = statusQ.data
  const overrides = overridesQ.data?.items ?? []
  const now = overridesQ.data?.now ? new Date(overridesQ.data.now).getTime() : mountedAt
  const reportedEcosystems = new Set(st?.ecosystems ?? [])
  const overrideEcosystems = maliciousBlocklistEcosystems.filter(ecosystem => reportedEcosystems.has(ecosystem.id))

  return (
    <div className="space-y-5">
      {/* Status card */}
      {statusQ.isPending ? (
        <p aria-busy="true" className="text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></p>
      ) : statusQ.isError && !statusQ.data ? (
        <QueryErrorState message={getApiError(statusQ.error).status === 403 ? t('common.permissionDenied') : getApiError(statusQ.error).message} onRetry={() => { void statusQ.refetch() }} />
      ) : (
        <div className="space-y-3">
          {statusQ.data && statusQ.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void statusQ.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
          {st && !st.enabled ? (
            <EmptyState
              icon="gpp_bad"
              title={t('quarantine.blocklist.disabled_title')}
              hint={t('quarantine.blocklist.disabled_hint')}
            />
          ) : st ? (
      <>
      {st.mode === 'warn' && (
        <InlineNotice tone="warning">{t('quarantine.blocklist.observe_warning')}</InlineNotice>
      )}
      <div className="rounded-[8px] border p-4 flex flex-wrap items-center gap-x-8 gap-y-3"
           style={{ borderColor: 'var(--border)', background: 'var(--bg-card)' }}>
        <StatusItem label={t('quarantine.blocklist.mode')}>
          <BadgeV2 variant={st.mode === 'warn' ? 'warning' : 'success'}>
            {st.mode === 'warn' ? t('quarantine.blocklist.mode_observe') : t('quarantine.blocklist.mode_enforce')}
          </BadgeV2>
        </StatusItem>
        <StatusItem label={t('quarantine.blocklist.entries')}>
          <span className="text-[22px] font-mono font-[600] tabular-nums">{st?.entry_count ?? 0}</span>
        </StatusItem>
        <StatusItem label={t('quarantine.blocklist.last_success')}>
          <span className="text-[13px] font-mono" style={{ color: st?.last_success_at ? 'var(--text)' : 'var(--warn-text)' }}>
            {st?.last_success_at ? formatTime(st.last_success_at) : t('quarantine.blocklist.never')}
          </span>
        </StatusItem>
        <StatusItem label={t('quarantine.blocklist.next_sync')}>
          <span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>
            {st?.running
              ? t('quarantine.blocklist.syncing')
              : st?.next_sync_at
                ? formatTime(st.next_sync_at)
                : '—'}
          </span>
        </StatusItem>
        <StatusItem label={t('quarantine.blocklist.coverage')}>
          <span className="text-[12px] font-mono" style={{ color: 'var(--text-soft)' }}>
            {(st?.ecosystems ?? []).join(' · ')}
          </span>
        </StatusItem>
        {st?.last_error && (
          <StatusItem label={t('quarantine.blocklist.last_error')}>
            <span className="text-[12px]" style={{ color: 'var(--danger-text)' }}>{st.last_error}</span>
          </StatusItem>
        )}
        {canWrite && <div className="grid w-full grid-cols-1 gap-2 min-[400px]:grid-cols-2 sm:ml-auto sm:flex sm:w-auto">
          <ButtonV2 className="min-h-[40px] sm:min-h-9" variant="secondary" onClick={() => { createM.reset(); setCreateOpen(true) }}>
            <Icon name="add" size="sm" /> {t('quarantine.blocklist.add_override')}
          </ButtonV2>
          <ButtonV2 className="min-h-[40px] sm:min-h-9" onClick={() => syncM.mutate()} aria-busy={syncM.isPending || undefined} disabled={syncM.isPending || !!st?.running}>
            <Icon name="sync" size="sm" /> {syncM.isPending || st?.running ? t('quarantine.blocklist.syncing') : t('quarantine.blocklist.sync_now')}
          </ButtonV2>
        </div>}
        {syncM.isError && <div className="basis-full"><InlineNotice tone="danger">{getApiError(syncM.error).message}</InlineNotice></div>}
      </div>
      </>
          ) : <EmptyState icon="gpp_bad" title={t('noData')} />}
        </div>
      )}

      {/* Overrides */}
      <div>
        <h3 className="text-[14px] font-[600] mb-2">{t('quarantine.blocklist.overrides_title')}</h3>
        <p className="text-[12px] mb-3" style={{ color: 'var(--text-soft)' }}>
          {t('quarantine.blocklist.overrides_hint')}
        </p>
        {overridesQ.isPending ? (
          <p aria-busy="true" className="text-[13px] text-[var(--text-soft)]"><span aria-hidden="true">{t('loading')}</span></p>
        ) : overridesQ.isError && !overridesQ.data ? (
          <QueryErrorState message={getApiError(overridesQ.error).status === 403 ? t('common.permissionDenied') : getApiError(overridesQ.error).message} onRetry={() => { void overridesQ.refetch() }} />
        ) : (
          <div className="space-y-3">
          {overridesQ.data && overridesQ.isRefetchError && <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void overridesQ.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>}
          {overrides.length === 0 ? (
          <EmptyState
            icon="verified_user"
            title={t('quarantine.blocklist.no_overrides_title')}
            hint={t('quarantine.blocklist.no_overrides_hint')}
          />
        ) : (
          <>
          <ul
            aria-label={t('quarantine.blocklist.overrides_table')}
            data-quarantine-mobile-list="overrides"
            className="divide-y divide-[var(--border)] sm:hidden"
          >
            {overrides.map((row) => {
              const msLeft = new Date(row.expires_at).getTime() - now
              const expired = msLeft <= 0
              return (
                <li key={row.id} className="space-y-3 py-4 first:pt-0" style={{ opacity: expired ? 0.55 : 1 }}>
                  <div className="flex min-w-0 items-start justify-between gap-3">
                    <MobilePackageIdentity
                      ecosystem={row.ecosystem}
                      packageName={row.package}
                      version={row.version || t('quarantine.blocklist.all_versions')}
                    />
                    <div className="shrink-0">
                      {expired ? (
                        <BadgeV2>{t('quarantine.blocklist.expired')}</BadgeV2>
                      ) : (
                        <BadgeV2 variant="warning">{formatRemaining(msLeft)}</BadgeV2>
                      )}
                    </div>
                  </div>
                  <MobileMetadata
                    entries={[
                      { label: t('quarantine.col.reason'), value: row.reason },
                      {
                        label: t('quarantine.blocklist.col_expires'),
                        value: expired ? t('quarantine.blocklist.expired') : formatRemaining(msLeft),
                        mono: !expired,
                      },
                    ]}
                  />
                  {canWrite && !expired && (
                    <ButtonV2
                      className="min-h-[40px] w-full"
                      variant="danger"
                      onClick={() => { revokeM.reset(); setRevokeTarget(row); setRevokeReason('') }}
                    >
                      <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                    </ButtonV2>
                  )}
                </li>
              )
            })}
          </ul>
          <div className="hidden sm:block">
          <TableViewport label={t('quarantine.blocklist.overrides_table')} minWidth={760}>
            <div className="rounded-[8px] border" style={{ borderColor: 'var(--border)' }}>
              <table className="w-full" style={{ borderCollapse: 'collapse' }}>
              <thead style={{ background: 'var(--bg-soft)' }}>
                <tr>
                  <Th>{t('quarantine.col.ecosystem')}</Th>
                  <Th>{t('quarantine.col.package')}</Th>
                  <Th>{t('quarantine.col.version')}</Th>
                  <Th>{t('quarantine.col.reason')}</Th>
                  <Th>{t('quarantine.blocklist.col_expires')}</Th>
                  <Th>{' '}</Th>
                </tr>
              </thead>
              <tbody>
                {overrides.map((row) => {
                  const msLeft = new Date(row.expires_at).getTime() - now
                  const expired = msLeft <= 0
                  return (
                    <tr key={row.id} style={{ borderTop: '0.5px solid var(--border)', opacity: expired ? 0.55 : 1 }}>
                      <Td>
                        <div className="flex items-center gap-1.5">
                          {isAdminEcosystem(row.ecosystem) && <EcosystemIcon type={row.ecosystem} size={14} />}
                          <span className="text-[12px] font-mono">{row.ecosystem}</span>
                        </div>
                      </Td>
                      <Td><span className="text-[13px] font-mono">{row.package}</span></Td>
                      <Td>
                        <span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>
                          {row.version || t('quarantine.blocklist.all_versions')}
                        </span>
                      </Td>
                      <Td><span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{row.reason}</span></Td>
                      <Td>
                        {expired ? (
                          <BadgeV2>{t('quarantine.blocklist.expired')}</BadgeV2>
                        ) : (
                          <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--warn-text)' }}>
                            {formatRemaining(msLeft)}
                          </span>
                        )}
                      </Td>
                      <Td>
                        {canWrite && !expired && (
                          <ButtonV2 size="sm" variant="danger" onClick={() => { revokeM.reset(); setRevokeTarget(row); setRevokeReason('') }}>
                            <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                          </ButtonV2>
                        )}
                      </Td>
                    </tr>
                  )
                })}
              </tbody>
              </table>
            </div>
          </TableViewport>
          </div>
          </>
          )}
          </div>
        )}
      </div>

      {/* Create override dialog */}
      <ModalV2
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title={t('quarantine.blocklist.create_title')}
        closeDisabled={createM.isPending}
      >
        <div className="space-y-4">
          <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
            {t('quarantine.blocklist.create_body')}
          </p>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[auto_minmax(0,1fr)]">
            <FilterSelect
              label={t('quarantine.col.ecosystem')}
              value={form.ecosystem}
              onChange={(v) => setForm({ ...form, ecosystem: v })}
              options={overrideEcosystems.map((ecosystem) => ({ value: ecosystem.id, label: ecosystem.label }))}
              disabled={createM.isPending}
            />
            <InputV2
              label={t('quarantine.col.package')}
              placeholder={t('quarantine.blocklist.package_placeholder')}
              value={form.package}
              onChange={(e) => setForm({ ...form, package: e.target.value })}
              disabled={createM.isPending}
            />
          </div>
          <InputV2
            label={t('quarantine.col.version')}
            placeholder={t('quarantine.blocklist.version_placeholder')}
            value={form.version}
            onChange={(e) => setForm({ ...form, version: e.target.value })}
            disabled={createM.isPending}
            mono
          />
          <InputV2
            label={t('quarantine.col.reason')}
            placeholder={t('quarantine.reason_placeholder')}
            value={form.reason}
            onChange={(e) => setForm({ ...form, reason: e.target.value })}
            disabled={createM.isPending}
          />
          {createM.isError && <InlineNotice tone="danger">{getApiError(createM.error).message}</InlineNotice>}
          <div className="flex justify-end gap-2">
            <ButtonV2 variant="secondary" onClick={() => setCreateOpen(false)} disabled={createM.isPending}>{t('cancel')}</ButtonV2>
            <ButtonV2
              onClick={() => createM.mutate()}
              aria-busy={createM.isPending || undefined}
              disabled={!form.package.trim() || form.reason.trim().length < 3 || createM.isPending || !canWrite}
            >
              {createM.isPending ? t('quarantine.blocklist.creating') : t('quarantine.blocklist.create_submit')}
            </ButtonV2>
          </div>
        </div>
      </ModalV2>

      {/* Revoke override dialog */}
      <ModalV2
        open={!!revokeTarget}
        onClose={() => setRevokeTarget(null)}
        title={t('quarantine.revoke.title')}
        closeDisabled={revokeM.isPending}
      >
        {revokeTarget && (
          <div className="space-y-4">
            <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
              {t('quarantine.revoke.body', { eco: revokeTarget.ecosystem, pkg: revokeTarget.package, ver: revokeTarget.version || t('quarantine.blocklist.all_versions') })}
            </p>
            <InputV2
              label={t('quarantine.col.reason')}
              placeholder={t('quarantine.reason_placeholder')}
              value={revokeReason}
              onChange={(e) => setRevokeReason(e.target.value)}
              disabled={revokeM.isPending}
              autoFocus
            />
            {revokeM.isError && <InlineNotice tone="danger">{getApiError(revokeM.error).message}</InlineNotice>}
            <div className="flex justify-end gap-2">
              <ButtonV2 variant="secondary" onClick={() => setRevokeTarget(null)} disabled={revokeM.isPending}>{t('cancel')}</ButtonV2>
              <ButtonV2
                variant="danger"
                onClick={() => revokeM.mutate()}
                aria-busy={revokeM.isPending || undefined}
                disabled={revokeReason.trim().length < 3 || revokeM.isPending || !canWrite}
              >
                {revokeM.isPending ? t('quarantine.revoke.submitting') : t('quarantine.revoke.submit')}
              </ButtonV2>
            </div>
          </div>
        )}
      </ModalV2>
    </div>
  )
}

function StatusItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] font-mono font-[600] uppercase" style={{ color: 'var(--text-subtle)' }}>
        {label}
      </span>
      {children}
    </div>
  )
}

// formatRemaining renders "23h 59m" style countdowns for override TTL.
function formatRemaining(ms: number): string {
  const totalMin = Math.max(0, Math.floor(ms / 60_000))
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

// ── Tiny shared helpers ────────────────────────────────────────────

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="text-left px-3 py-2.5 text-[11px] font-mono font-[600] uppercase"
        style={{ color: 'var(--text-subtle)' }}>{children}</th>
  )
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-3 py-2 align-middle">{children}</td>
}

function MobilePackageIdentity(props: { ecosystem: string; packageName: string; version: string }) {
  return (
    <div className="flex min-w-0 items-start gap-2">
      {isAdminEcosystem(props.ecosystem) && (
        <span className="mt-0.5 shrink-0">
          <EcosystemIcon type={props.ecosystem} size={16} />
        </span>
      )}
      <div className="min-w-0">
        <p className="break-words font-mono text-[14px] font-[550]" style={{ color: 'var(--text)' }}>
          {props.packageName}
        </p>
        <p className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
          <span>{props.ecosystem}</span>
          <span aria-hidden="true">·</span>
          <span className="break-all">{props.version}</span>
        </p>
      </div>
    </div>
  )
}

function MobileMetadata({ entries }: {
  entries: { label: string; value: React.ReactNode; mono?: boolean }[]
}) {
  return (
    <dl className="grid grid-cols-1 gap-2.5">
      {entries.map((entry) => (
        <div key={entry.label} className="min-w-0">
          <dt className="text-[11px] font-[600]" style={{ color: 'var(--text-subtle)' }}>
            {entry.label}
          </dt>
          <dd
            className={`mt-0.5 break-words text-[13px] leading-[1.5] ${entry.mono ? 'font-mono tabular-nums' : ''}`}
            style={{ color: 'var(--text-soft)' }}
          >
            {entry.value || '—'}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function FilterSelect(props: {
  label: string
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
  disabled?: boolean
}) {
  return (
    <select
      aria-label={props.label}
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
      disabled={props.disabled}
      className="min-h-[40px] w-full cursor-pointer rounded-[6px] px-3 text-[16px] disabled:cursor-not-allowed disabled:opacity-50 sm:h-9 sm:min-h-9 sm:w-auto sm:text-[12px]"
      style={{
        background: 'var(--bg-soft)',
        color: 'var(--text)',
        border: '0.5px solid var(--border)',
      }}
    >
      {props.options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  )
}
