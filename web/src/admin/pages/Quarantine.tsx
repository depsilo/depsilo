import { TableContainer, Table, TableHeader, TableRow, TableBody, TableHead, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel } from '@/components/ui/field'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'

import { Input } from '@/components/ui/input'

import QueryErrorState from '@/components/QueryErrorState'
import { NativeSelect } from '@/components/ui/native-select'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import { useAppToast } from '@/components/Toast'
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
const PAGE_SIZE = 50

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
      return <Badge variant="destructive">{t('quarantine.action.blocked')}</Badge>
    case 'malware_blocked':
      return <Badge variant="destructive">{t('quarantine.action.malware_blocked')}</Badge>
    case 'tamper_detected':
      return <Badge variant="destructive">{t('quarantine.action.tamper_detected')}</Badge>
    case 'served_eligible':
      return <Badge variant="warning">{t('quarantine.action.served_eligible')}</Badge>
    case 'bypassed':
      return <Badge variant="success">{t('quarantine.action.bypassed')}</Badge>
    case 'malware_bypassed':
      return <Badge variant="warning">{t('quarantine.action.malware_bypassed')}</Badge>
    case 'warned':
      return <Badge variant="warning">{t('quarantine.action.warned')}</Badge>
    case 'malware_warned':
      return <Badge variant="warning">{t('quarantine.action.malware_warned')}</Badge>
    case 'approved':
      return <Badge variant="success">{t('quarantine.action.approved')}</Badge>
    case 'approval_revoked':
      return <Badge>{t('quarantine.action.approval_revoked')}</Badge>
    case 'override_created':
      return <Badge variant="warning">{t('quarantine.action.override_created')}</Badge>
    case 'override_revoked':
      return <Badge>{t('quarantine.action.override_revoked')}</Badge>
    default:
      return <Badge>{action}</Badge>
  }
}

export default function Quarantine() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { canWrite } = usePrincipal()
  const [tab, setTab] = useState<'events' | 'approvals' | 'blocklist'>('events')
  const [eventsPage, setEventsPage] = useState(1)
  const [approvalsPage, setApprovalsPage] = useState(1)

  // Filters (events tab)
  const [ecoFilter, setEcoFilter] = useState('all')
  const [actionFilter, setActionFilter] = useState('all')
  const [pkgSearch, setPkgSearch] = useState('')

  const [revokeOpen, setRevokeOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApprovedVersion | null>(null)
  const [revokeReason, setRevokeReason] = useState('')

  // ── Data ──────────────────────────────────────────────────────
  const eventsParams: Record<string, string | number> = { limit: PAGE_SIZE, offset: (eventsPage - 1) * PAGE_SIZE }
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
    queryKey: ['admin', 'quarantine', 'approvals', approvalsPage],
    queryFn: async ({ signal }) => {
      const res = await adminApi.listQuarantineApprovals({ limit: PAGE_SIZE, offset: (approvalsPage - 1) * PAGE_SIZE }, { signal })
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
      if (approvalsPage > 1 && approvalsQ.data?.items.length === 1) setApprovalsPage(approvalsPage - 1)
      setRevokeOpen(false)
      setRevokeTarget(null)
      setRevokeReason('')
    },
  })

  // ── Render ────────────────────────────────────────────────────
  return (
    <AdminPage description={t('quarantine.subtitle')}>
    <div className="space-y-6">
      <div className="max-w-xs">
          <Field>
            <FieldLabel>{t('quarantine.view')}</FieldLabel>
            <NativeSelect
              value={tab}
              onChange={(event) => {
                const value = event.target.value
                if (value === 'events' || value === 'approvals' || value === 'blocklist') setTab(value)
              }}
            >
              <option value="events">{t('quarantine.tab.events')}</option>
              <option value="approvals">{t('quarantine.tab.approvals')}</option>
              <option value="blocklist">{t('quarantine.tab.blocklist')}</option>
            </NativeSelect>
          </Field>
      </div>
      {tab === 'events' && <EventsTab
        eventsQ={eventsQ}
        page={eventsPage}
        setPage={setEventsPage}
        ecoFilter={ecoFilter}
        setEcoFilter={value => { setEcoFilter(value); setEventsPage(1) }}
        actionFilter={actionFilter}
        setActionFilter={value => { setActionFilter(value); setEventsPage(1) }}
        pkgSearch={pkgSearch}
        setPkgSearch={value => { setPkgSearch(value); setEventsPage(1) }}
      />}
      {tab === 'approvals' && <ApprovalsTab approvalsQ={approvalsQ} page={approvalsPage} setPage={setApprovalsPage} canWrite={canWrite} onRevoke={(row) => {
        revokeM.reset()
        setRevokeTarget(row)
        setRevokeReason('')
        setRevokeOpen(true)
      }} />}
      {tab === 'blocklist' && <BlocklistTab />}

      {/* Revoke dialog */}
        <Dialog open={revokeOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !revokeM.isPending) setRevokeOpen(false) }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={revokeM.isPending}>
            <DialogTitle className="pr-10">{t('quarantine.revoke.title')}</DialogTitle>
            {revokeTarget && (
              <div className="space-y-4">
                <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  {t('quarantine.revoke.body', { eco: revokeTarget.ecosystem, pkg: revokeTarget.package, ver: revokeTarget.version })}
                </p>
                <Field disabled={revokeM.isPending}>
                  <FieldLabel>{t('quarantine.col.reason')}</FieldLabel>
                  <Input
                    placeholder={t('quarantine.reason_placeholder')}
                    value={revokeReason}
                    onChange={(e) => setRevokeReason(e.target.value)}
                    disabled={revokeM.isPending}
                    autoFocus
                  />
                </Field>
                {revokeM.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(revokeM.error).message}</AlertDescription></Alert>}
                <div className="flex justify-end gap-2">
                  <Button variant="outline" onClick={() => setRevokeOpen(false)} disabled={revokeM.isPending}>
                    {t('cancel')}
                  </Button>
                  <Button
                    variant="destructive"
                    onClick={() => revokeM.mutate()}
                    aria-busy={revokeM.isPending || undefined}
                    disabled={revokeReason.trim().length < 3 || revokeM.isPending || !canWrite}
                  >
                    {revokeM.isPending ? t('quarantine.revoke.submitting') : t('quarantine.revoke.submit')}
                  </Button>
                </div>
              </div>
            )}
          </DialogContent>
        </Dialog>
    </div>
    </AdminPage>
  )
}

// ── Events tab ─────────────────────────────────────────────────────

function EventsTab(props: {
  eventsQ: ReturnType<typeof useQuery>
  page: number; setPage: (page: number) => void
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
        <Input
          aria-label={t('quarantine.filter.package_placeholder')}
          placeholder={t('quarantine.filter.package_placeholder')}
          value={props.pkgSearch}
          onChange={(e) => props.setPkgSearch(e.target.value)}
          className="col-span-2 sm:w-[260px]"
        />
        <NativeSelect className="min-h-10 sm:min-h-9 sm:w-auto" aria-label={t('quarantine.col.ecosystem')} value={props.ecoFilter} onChange={(event) => (props.setEcoFilter)(event.target.value)}>
          {([
            { value: 'all', label: t('quarantine.filter.all_ecosystems') },
            ...ECOSYSTEMS.map((eco) => ({ value: eco, label: eco })),
          ]).map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </NativeSelect>
        <NativeSelect className="min-h-10 sm:min-h-9 sm:w-auto" aria-label={t('quarantine.col.action')} value={props.actionFilter} onChange={(event) => (props.setActionFilter)(event.target.value)}>
          {([
            { value: 'all', label: t('quarantine.filter.all_actions') },
            ...ACTIONS.map((a) => ({ value: a, label: t(`quarantine.action.${a}`) })),
          ]).map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </NativeSelect>
      </div>

      {/* Table */}
      {props.eventsQ.isPending ? (
        <p aria-busy="true" className="text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></p>
      ) : props.eventsQ.isError && !props.eventsQ.data ? (
        <QueryErrorState message={getApiError(props.eventsQ.error).status === 403 ? t('common.permissionDenied') : getApiError(props.eventsQ.error).message} onRetry={() => { void props.eventsQ.refetch() }} />
      ) : (
        <div className="space-y-3">
        {Boolean(data) && props.eventsQ.isRefetchError && <Alert variant={"warning"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><Button type="button" variant="outline" size="sm" onClick={() => { void props.eventsQ.refetch() }}>{t('now.refresh')}</Button></div></AlertDescription></Alert>}
        {items.length === 0 ? <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"policy"} size="sm" /></EmptyMedia><EmptyTitle>{t('quarantine.events.empty_title')}</EmptyTitle>{t('quarantine.events.empty_hint') && <EmptyDescription>{t('quarantine.events.empty_hint')}</EmptyDescription>}</EmptyHeader></Empty> : (
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
        <TableContainer label={t('quarantine.events.table')} minWidth={920}>
          <div className="rounded-md border" style={{ borderColor: 'var(--border)' }}>
            <Table className="w-full" style={{ borderCollapse: 'collapse' }}>
            <TableHeader style={{ background: 'var(--bg-soft)' }}>
              <TableRow>
                <Th>{t('quarantine.col.time')}</Th>
                <Th>{t('quarantine.col.ecosystem')}</Th>
                <Th>{t('quarantine.col.package')}</Th>
                <Th>{t('quarantine.col.version')}</Th>
                <Th>{t('quarantine.col.action')}</Th>
                <Th>{t('quarantine.col.reason')}</Th>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((ev) => (
                <TableRow key={ev.id} style={{ borderTop: '1px solid var(--border)' }}>
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
                </TableRow>
              ))}
            </TableBody>
            </Table>
          </div>
        </TableContainer>
        </div>
        </>
        )}
        <AdminPagination page={props.page} pageSize={PAGE_SIZE} total={data?.total ?? 0} onPageChange={props.setPage} />
        </div>
      )}
    </div>
  )
}

// ── Approvals tab ──────────────────────────────────────────────────

function ApprovalsTab(props: {
  approvalsQ: ReturnType<typeof useQuery>
  page: number; setPage: (page: number) => void
  canWrite: boolean
  onRevoke: (row: ApprovedVersion) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const toast = useAppToast()
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ ecosystem: '', package: '', version: '', reason: '' })
  const capabilityQ = useQuery({
    queryKey: ['admin', 'capabilities', 'summary'],
    queryFn: ({ signal }) => adminApi.getCapabilitySummary({ signal }),
    enabled: props.canWrite,
    staleTime: 30_000,
    retry: false,
  })
  const activeEcosystems = (capabilityQ.data?.data.capabilities ?? [])
    .filter(fact => fact.name === 'minimum_release_age' && fact.support === 'supported'
      && (fact.mode === 'warn' || fact.mode === 'block') && fact.ecosystem)
    .map(fact => fact.ecosystem!)
  const createM = useMutation({
    mutationFn: () => adminApi.approveQuarantine({
      ecosystem: form.ecosystem,
      package: form.package.trim(),
      version: form.version.trim(),
      reason: form.reason.trim(),
    }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'quarantine'] })
      props.setPage(1)
      setCreateOpen(false)
      setForm({ ecosystem: '', package: '', version: '', reason: '' })
      toast.show({ tone: 'success', message: t('quarantine.approvals.createSuccess') })
    },
  })
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
    {props.canWrite && capabilityQ.isError && (
      <Alert variant={"warning"}><AlertDescription>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>{t('quarantine.approvals.capabilityUnavailable')}</span>
          <Button type="button" variant="outline" size="sm" onClick={() => { void capabilityQ.refetch() }}>
            {t('common.retry')}
          </Button>
        </div>
      </AlertDescription></Alert>
    )}
    {props.canWrite && !capabilityQ.isPending && !capabilityQ.isError && activeEcosystems.length === 0 && (
      <Alert variant={"info"}><AlertDescription>{t('quarantine.approvals.noActiveGate')}</AlertDescription></Alert>
    )}
    {props.canWrite && activeEcosystems.length > 0 && !capabilityQ.isError && (
      <div className="flex justify-end">
        <Button type="button" size="sm" onClick={() => {
          createM.reset()
          setForm({ ecosystem: activeEcosystems[0], package: '', version: '', reason: '' })
          setCreateOpen(true)
        }}>
          <Icon name="add" size="sm" />{t('quarantine.approvals.create')}
        </Button>
      </div>
    )}
    {Boolean(data) && props.approvalsQ.isRefetchError && <Alert variant={"warning"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><Button type="button" variant="outline" size="sm" onClick={() => { void props.approvalsQ.refetch() }}>{t('now.refresh')}</Button></div></AlertDescription></Alert>}
    {items.length === 0 ? (
      <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"verified"} size="sm" /></EmptyMedia><EmptyTitle>{t('quarantine.approvals.empty_title')}</EmptyTitle>{t('quarantine.approvals.empty_hint') && <EmptyDescription>{t('quarantine.approvals.empty_hint')}</EmptyDescription>}</EmptyHeader></Empty>
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
            <Button
              className="min-h-[40px] w-full"
              variant="destructive"
              onClick={() => props.onRevoke(row)}
            >
              <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
            </Button>
          )}
        </li>
      ))}
    </ul>
    <div className="hidden sm:block">
    <TableContainer label={t('quarantine.approvals.table')} minWidth={820}>
      <div className="rounded-md border" style={{ borderColor: 'var(--border)' }}>
        <Table className="w-full" style={{ borderCollapse: 'collapse' }}>
        <TableHeader style={{ background: 'var(--bg-soft)' }}>
          <TableRow>
            <Th>{t('quarantine.col.created_at')}</Th>
            <Th>{t('quarantine.col.ecosystem')}</Th>
            <Th>{t('quarantine.col.package')}</Th>
            <Th>{t('quarantine.col.version')}</Th>
            <Th>{t('quarantine.col.reason')}</Th>
            <Th>{' '}</Th>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((row) => (
            <TableRow key={row.id} style={{ borderTop: '1px solid var(--border)' }}>
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
                {props.canWrite && <Button size="sm" variant="destructive" onClick={() => props.onRevoke(row)}>
                  <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                </Button>}
              </Td>
            </TableRow>
          ))}
        </TableBody>
        </Table>
      </div>
    </TableContainer>
    </div>
    </>
    )}
    <AdminPagination page={props.page} pageSize={PAGE_SIZE} total={data?.total ?? 0} onPageChange={props.setPage} />
      <Dialog open={createOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !createM.isPending) (() => { if (!createM.isPending) setCreateOpen(false) })() }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={createM.isPending}>
          <DialogTitle className="pr-10">{t('quarantine.approvals.createTitle')}</DialogTitle>
          <form className="space-y-4" onSubmit={event => {
            event.preventDefault()
            if (!props.canWrite || !activeEcosystems.includes(form.ecosystem) || !form.package.trim()
              || !form.version.trim() || form.reason.trim().length < 3) return
            createM.mutate()
          }}>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.ecosystem')}</FieldLabel>
              <NativeSelect value={form.ecosystem} disabled={createM.isPending} onChange={event => setForm({ ...form, ecosystem: event.target.value })}>
                {activeEcosystems.map(ecosystem => <option key={ecosystem} value={ecosystem}>{ecosystem.toUpperCase()}</option>)}
              </NativeSelect>
            </Field>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.package')}</FieldLabel>
              <Input value={form.package} required disabled={createM.isPending} onChange={event => setForm({ ...form, package: event.target.value })} />
            </Field>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.version')}</FieldLabel>
              <Input value={form.version} required disabled={createM.isPending} onChange={event => setForm({ ...form, version: event.target.value })} />
            </Field>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.reason')}</FieldLabel>
              <Input value={form.reason} required minLength={3} disabled={createM.isPending} onChange={event => setForm({ ...form, reason: event.target.value })} />
            </Field>
            {createM.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(createM.error).message}</AlertDescription></Alert>}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" disabled={createM.isPending} onClick={() => setCreateOpen(false)}>{t('cancel')}</Button>
              <Button type="submit" aria-busy={createM.isPending || undefined} disabled={createM.isPending || !form.package.trim() || !form.version.trim() || form.reason.trim().length < 3}>{t('quarantine.approvals.create')}</Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
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
          {statusQ.data && statusQ.isRefetchError && <Alert variant={"warning"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><Button type="button" variant="outline" size="sm" onClick={() => { void statusQ.refetch() }}>{t('now.refresh')}</Button></div></AlertDescription></Alert>}
          {st && !st.enabled ? (
            <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"gpp_bad"} size="sm" /></EmptyMedia><EmptyTitle>{t('quarantine.blocklist.disabled_title')}</EmptyTitle>{t('quarantine.blocklist.disabled_hint') && <EmptyDescription>{t('quarantine.blocklist.disabled_hint')}</EmptyDescription>}</EmptyHeader></Empty>
          ) : st ? (
      <>
      {st.mode === 'warn' && (
        <Alert variant={"warning"}><AlertDescription>{t('quarantine.blocklist.observe_warning')}</AlertDescription></Alert>
      )}
      <div className="rounded-md border p-4 flex flex-wrap items-center gap-x-8 gap-y-3"
           style={{ borderColor: 'var(--border)', background: 'var(--bg-card)' }}>
        <StatusItem label={t('quarantine.blocklist.mode')}>
          <Badge variant={st.mode === 'warn' ? 'warning' : 'success'}>
            {st.mode === 'warn' ? t('quarantine.blocklist.mode_observe') : t('quarantine.blocklist.mode_enforce')}
          </Badge>
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
          <Button className="min-h-[40px] sm:min-h-9" variant="outline" onClick={() => { createM.reset(); setCreateOpen(true) }}>
            <Icon name="add" size="sm" /> {t('quarantine.blocklist.add_override')}
          </Button>
          <Button className="min-h-[40px] sm:min-h-9" onClick={() => syncM.mutate()} aria-busy={syncM.isPending || undefined} disabled={syncM.isPending || !!st?.running}>
            <Icon name="sync" size="sm" /> {syncM.isPending || st?.running ? t('quarantine.blocklist.syncing') : t('quarantine.blocklist.sync_now')}
          </Button>
        </div>}
        {syncM.isError && <div className="basis-full"><Alert variant={"destructive"}><AlertDescription>{getApiError(syncM.error).message}</AlertDescription></Alert></div>}
      </div>
      </>
          ) : <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"gpp_bad"} size="sm" /></EmptyMedia><EmptyTitle>{t('noData')}</EmptyTitle></EmptyHeader></Empty>}
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
          {overridesQ.data && overridesQ.isRefetchError && <Alert variant={"warning"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><Button type="button" variant="outline" size="sm" onClick={() => { void overridesQ.refetch() }}>{t('now.refresh')}</Button></div></AlertDescription></Alert>}
          {overrides.length === 0 ? (
          <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"verified_user"} size="sm" /></EmptyMedia><EmptyTitle>{t('quarantine.blocklist.no_overrides_title')}</EmptyTitle>{t('quarantine.blocklist.no_overrides_hint') && <EmptyDescription>{t('quarantine.blocklist.no_overrides_hint')}</EmptyDescription>}</EmptyHeader></Empty>
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
                        <Badge>{t('quarantine.blocklist.expired')}</Badge>
                      ) : (
                        <Badge variant="warning">{formatRemaining(msLeft)}</Badge>
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
                    <Button
                      className="min-h-[40px] w-full"
                      variant="destructive"
                      onClick={() => { revokeM.reset(); setRevokeTarget(row); setRevokeReason('') }}
                    >
                      <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                    </Button>
                  )}
                </li>
              )
            })}
          </ul>
          <div className="hidden sm:block">
          <TableContainer label={t('quarantine.blocklist.overrides_table')} minWidth={760}>
            <div className="rounded-md border" style={{ borderColor: 'var(--border)' }}>
              <Table className="w-full" style={{ borderCollapse: 'collapse' }}>
              <TableHeader style={{ background: 'var(--bg-soft)' }}>
                <TableRow>
                  <Th>{t('quarantine.col.ecosystem')}</Th>
                  <Th>{t('quarantine.col.package')}</Th>
                  <Th>{t('quarantine.col.version')}</Th>
                  <Th>{t('quarantine.col.reason')}</Th>
                  <Th>{t('quarantine.blocklist.col_expires')}</Th>
                  <Th>{' '}</Th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {overrides.map((row) => {
                  const msLeft = new Date(row.expires_at).getTime() - now
                  const expired = msLeft <= 0
                  return (
                    <TableRow key={row.id} style={{ borderTop: '1px solid var(--border)', opacity: expired ? 0.55 : 1 }}>
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
                          <Badge>{t('quarantine.blocklist.expired')}</Badge>
                        ) : (
                          <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--warn-text)' }}>
                            {formatRemaining(msLeft)}
                          </span>
                        )}
                      </Td>
                      <Td>
                        {canWrite && !expired && (
                          <Button size="sm" variant="destructive" onClick={() => { revokeM.reset(); setRevokeTarget(row); setRevokeReason('') }}>
                            <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                          </Button>
                        )}
                      </Td>
                    </TableRow>
                  )
                })}
              </TableBody>
              </Table>
            </div>
          </TableContainer>
          </div>
          </>
          )}
          </div>
        )}
      </div>

      {/* Create override dialog */}
      <Dialog open={createOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !createM.isPending) setCreateOpen(false) }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={createM.isPending}>
          <DialogTitle className="pr-10">{t('quarantine.blocklist.create_title')}</DialogTitle>
          <div className="space-y-4">
            <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
              {t('quarantine.blocklist.create_body')}
            </p>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[auto_minmax(0,1fr)]">
              <NativeSelect className="min-h-10 sm:min-h-9 sm:w-auto" aria-label={t('quarantine.col.ecosystem')} value={form.ecosystem} onChange={(event) => ((v) => setForm({ ...form, ecosystem: v }))(event.target.value)} disabled={createM.isPending}>
                {(overrideEcosystems.map((ecosystem) => ({ value: ecosystem.id, label: ecosystem.label }))).map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
              </NativeSelect>
              <Field disabled={createM.isPending}>
                <FieldLabel>{t('quarantine.col.package')}</FieldLabel>
                <Input
                  placeholder={t('quarantine.blocklist.package_placeholder')}
                  value={form.package}
                  onChange={(e) => setForm({ ...form, package: e.target.value })}
                  disabled={createM.isPending}
                />
              </Field>
            </div>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.version')}</FieldLabel>
              <Input className="font-mono"
                placeholder={t('quarantine.blocklist.version_placeholder')}
                value={form.version}
                onChange={(e) => setForm({ ...form, version: e.target.value })}
                disabled={createM.isPending}
              />
            </Field>
            <Field disabled={createM.isPending}>
              <FieldLabel>{t('quarantine.col.reason')}</FieldLabel>
              <Input
                placeholder={t('quarantine.reason_placeholder')}
                value={form.reason}
                onChange={(e) => setForm({ ...form, reason: e.target.value })}
                disabled={createM.isPending}
              />
            </Field>
            {createM.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(createM.error).message}</AlertDescription></Alert>}
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={createM.isPending}>{t('cancel')}</Button>
              <Button
                onClick={() => createM.mutate()}
                aria-busy={createM.isPending || undefined}
                disabled={!form.package.trim() || form.reason.trim().length < 3 || createM.isPending || !canWrite}
              >
                {createM.isPending ? t('quarantine.blocklist.creating') : t('quarantine.blocklist.create_submit')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Revoke override dialog */}
      <Dialog open={!!revokeTarget} onOpenChange={(nextOpen) => { if (!nextOpen && !revokeM.isPending) setRevokeTarget(null) }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={revokeM.isPending}>
          <DialogTitle className="pr-10">{t('quarantine.revoke.title')}</DialogTitle>
          {revokeTarget && (
            <div className="space-y-4">
              <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
                {t('quarantine.revoke.body', { eco: revokeTarget.ecosystem, pkg: revokeTarget.package, ver: revokeTarget.version || t('quarantine.blocklist.all_versions') })}
              </p>
              <Field disabled={revokeM.isPending}>
                <FieldLabel>{t('quarantine.col.reason')}</FieldLabel>
                <Input
                  placeholder={t('quarantine.reason_placeholder')}
                  value={revokeReason}
                  onChange={(e) => setRevokeReason(e.target.value)}
                  disabled={revokeM.isPending}
                  autoFocus
                />
              </Field>
              {revokeM.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(revokeM.error).message}</AlertDescription></Alert>}
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => setRevokeTarget(null)} disabled={revokeM.isPending}>{t('cancel')}</Button>
                <Button
                  variant="destructive"
                  onClick={() => revokeM.mutate()}
                  aria-busy={revokeM.isPending || undefined}
                  disabled={revokeReason.trim().length < 3 || revokeM.isPending || !canWrite}
                >
                  {revokeM.isPending ? t('quarantine.revoke.submitting') : t('quarantine.revoke.submit')}
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
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
    <TableHead className="text-left px-3 py-2.5 text-[11px] font-mono font-[600] uppercase"
        style={{ color: 'var(--text-subtle)' }}>{children}</TableHead>
  )
}

function Td({ children }: { children: React.ReactNode }) {
  return <TableCell className="px-3 py-2 align-middle">{children}</TableCell>
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
