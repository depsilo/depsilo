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
import ModalV2 from '@/components/Modal'
import InputV2 from '@/components/Input'

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
const ACTIONS = ['blocked', 'served_eligible', 'bypassed', 'approved', 'approval_revoked']

function actionBadge(action: string, t: (k: string) => string) {
  switch (action) {
    case 'blocked':
      return <BadgeV2 variant="error">{t('quarantine.action.blocked')}</BadgeV2>
    case 'served_eligible':
      return <BadgeV2 variant="warning">{t('quarantine.action.served_eligible')}</BadgeV2>
    case 'bypassed':
      return <BadgeV2 variant="success">{t('quarantine.action.bypassed')}</BadgeV2>
    case 'approved':
      return <BadgeV2 variant="success">{t('quarantine.action.approved')}</BadgeV2>
    case 'approval_revoked':
      return <BadgeV2>{t('quarantine.action.approval_revoked')}</BadgeV2>
    default:
      return <BadgeV2>{action}</BadgeV2>
  }
}

export default function Quarantine() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'events' | 'approvals'>('events')

  // Filters (events tab)
  const [ecoFilter, setEcoFilter] = useState('all')
  const [actionFilter, setActionFilter] = useState('all')
  const [pkgSearch, setPkgSearch] = useState('')

  // Approve / Revoke dialogs share a tiny per-action state object.
  const [approveOpen, setApproveOpen] = useState(false)
  const [approveTarget, setApproveTarget] = useState<{ ecosystem: string; package: string; version: string } | null>(null)
  const [approveReason, setApproveReason] = useState('')

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
    queryFn: async () => {
      const res = await adminApi.listQuarantineEvents(eventsParams)
      return res.data as { items: QuarantineEvent[]; total: number }
    },
    enabled: tab === 'events',
    refetchInterval: 30_000,
  })

  const approvalsQ = useQuery({
    queryKey: ['admin', 'quarantine', 'approvals'],
    queryFn: async () => {
      const res = await adminApi.listQuarantineApprovals({ limit: 200 })
      return res.data as { items: ApprovedVersion[]; total: number }
    },
    enabled: tab === 'approvals',
    refetchInterval: 30_000,
  })

  // ── Mutations ─────────────────────────────────────────────────
  const approveM = useMutation({
    mutationFn: () =>
      adminApi.approveQuarantine({
        ecosystem: approveTarget!.ecosystem,
        package: approveTarget!.package,
        version: approveTarget!.version,
        reason: approveReason.trim(),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'quarantine'] })
      setApproveOpen(false)
      setApproveTarget(null)
      setApproveReason('')
    },
  })

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
    <div className="space-y-6">
      {/* Header + tabs */}
      <div>
        <h2 className="text-[20px] font-[600] tracking-[-0.02em]" style={{ color: 'var(--text)' }}>
          {t('quarantine.title')}
        </h2>
        <p className="text-[13px] mt-1 max-w-2xl" style={{ color: 'var(--text-soft)' }}>
          {t('quarantine.subtitle')}
        </p>
      </div>

      <div className="flex gap-1 border-b" style={{ borderColor: 'var(--border)' }}>
        {[
          { id: 'events' as const, label: t('quarantine.tab.events') },
          { id: 'approvals' as const, label: t('quarantine.tab.approvals') },
        ].map((x) => {
          const active = tab === x.id
          return (
            <button
              key={x.id}
              type="button"
              onClick={() => setTab(x.id)}
              className="px-3 py-2 text-[13px] cursor-pointer transition-colors duration-150 bg-transparent"
              style={{
                color: active ? 'var(--text)' : 'var(--text-soft)',
                fontWeight: active ? 600 : 500,
                borderBottom: active ? '2px solid var(--brand)' : '2px solid transparent',
                marginBottom: '-1px',
              }}
            >
              {x.label}
            </button>
          )
        })}
      </div>

      {tab === 'events' ? (
        <EventsTab
          eventsQ={eventsQ}
          ecoFilter={ecoFilter}
          setEcoFilter={setEcoFilter}
          actionFilter={actionFilter}
          setActionFilter={setActionFilter}
          pkgSearch={pkgSearch}
          setPkgSearch={setPkgSearch}
          onApprove={(ev) => {
            setApproveTarget({ ecosystem: ev.ecosystem, package: ev.package, version: ev.version })
            setApproveReason('')
            setApproveOpen(true)
          }}
        />
      ) : (
        <ApprovalsTab
          approvalsQ={approvalsQ}
          onRevoke={(row) => {
            setRevokeTarget(row)
            setRevokeReason('')
            setRevokeOpen(true)
          }}
        />
      )}

      {/* Approve dialog */}
      <ModalV2 open={approveOpen} onClose={() => setApproveOpen(false)} title={t('quarantine.approve.title')}>
        {approveTarget && (
          <div className="space-y-4">
            <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
              {t('quarantine.approve.body', { eco: approveTarget.ecosystem, pkg: approveTarget.package, ver: approveTarget.version })}
            </p>
            <InputV2
              placeholder={t('quarantine.reason_placeholder')}
              value={approveReason}
              onChange={(e) => setApproveReason(e.target.value)}
              autoFocus
            />
            {approveM.isError && (
              <p className="text-[12px]" style={{ color: 'var(--danger-text)' }}>
                {(approveM.error as any)?.response?.data?.message || t('quarantine.approve.error')}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <ButtonV2 variant="secondary" onClick={() => setApproveOpen(false)}>
                {t('cancel')}
              </ButtonV2>
              <ButtonV2
                onClick={() => approveM.mutate()}
                disabled={approveReason.trim().length < 3 || approveM.isPending}
              >
                {approveM.isPending ? t('quarantine.approve.submitting') : t('quarantine.approve.submit')}
              </ButtonV2>
            </div>
          </div>
        )}
      </ModalV2>

      {/* Revoke dialog */}
      <ModalV2 open={revokeOpen} onClose={() => setRevokeOpen(false)} title={t('quarantine.revoke.title')}>
        {revokeTarget && (
          <div className="space-y-4">
            <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
              {t('quarantine.revoke.body', { eco: revokeTarget.ecosystem, pkg: revokeTarget.package, ver: revokeTarget.version })}
            </p>
            <InputV2
              placeholder={t('quarantine.reason_placeholder')}
              value={revokeReason}
              onChange={(e) => setRevokeReason(e.target.value)}
              autoFocus
            />
            {revokeM.isError && (
              <p className="text-[12px]" style={{ color: 'var(--danger-text)' }}>
                {(revokeM.error as any)?.response?.data?.message || t('quarantine.revoke.error')}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <ButtonV2 variant="secondary" onClick={() => setRevokeOpen(false)}>
                {t('cancel')}
              </ButtonV2>
              <ButtonV2
                variant="danger"
                onClick={() => revokeM.mutate()}
                disabled={revokeReason.trim().length < 3 || revokeM.isPending}
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

// ── Events tab ─────────────────────────────────────────────────────

function EventsTab(props: {
  eventsQ: ReturnType<typeof useQuery>
  ecoFilter: string; setEcoFilter: (v: string) => void
  actionFilter: string; setActionFilter: (v: string) => void
  pkgSearch: string; setPkgSearch: (v: string) => void
  onApprove: (ev: QuarantineEvent) => void
}) {
  const { t } = useTranslation()
  const data = props.eventsQ.data as { items: QuarantineEvent[]; total: number } | undefined
  const items = data?.items ?? []

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-2">
        <InputV2
          placeholder={t('quarantine.filter.package_placeholder')}
          value={props.pkgSearch}
          onChange={(e) => props.setPkgSearch(e.target.value)}
          style={{ maxWidth: 260 }}
        />
        <FilterSelect
          value={props.ecoFilter}
          onChange={props.setEcoFilter}
          options={[
            { value: 'all', label: t('quarantine.filter.all_ecosystems') },
            ...ECOSYSTEMS.map((eco) => ({ value: eco, label: eco })),
          ]}
        />
        <FilterSelect
          value={props.actionFilter}
          onChange={props.setActionFilter}
          options={[
            { value: 'all', label: t('quarantine.filter.all_actions') },
            ...ACTIONS.map((a) => ({ value: a, label: t(`quarantine.action.${a}`) })),
          ]}
        />
      </div>

      {/* Table */}
      {props.eventsQ.isLoading ? (
        <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</p>
      ) : items.length === 0 ? (
        <EmptyState
          icon="policy"
          title={t('quarantine.events.empty_title')}
          hint={t('quarantine.events.empty_hint')}
        />
      ) : (
        <div className="rounded-[8px] border overflow-hidden" style={{ borderColor: 'var(--border)' }}>
          <table className="w-full" style={{ borderCollapse: 'collapse' }}>
            <thead style={{ background: 'var(--bg-soft)' }}>
              <tr>
                <Th>{t('quarantine.col.time')}</Th>
                <Th>{t('quarantine.col.ecosystem')}</Th>
                <Th>{t('quarantine.col.package')}</Th>
                <Th>{t('quarantine.col.version')}</Th>
                <Th>{t('quarantine.col.action')}</Th>
                <Th>{t('quarantine.col.reason')}</Th>
                <Th>{' '}</Th>
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
                      <EcosystemIcon type={ev.ecosystem as any} size={14} />
                      <span className="text-[12px] font-mono">{ev.ecosystem}</span>
                    </div>
                  </Td>
                  <Td><span className="text-[13px] font-mono">{ev.package}</span></Td>
                  <Td><span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>{ev.version}</span></Td>
                  <Td>{actionBadge(ev.action, t)}</Td>
                  <Td>
                    <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{ev.reason}</span>
                  </Td>
                  <Td>
                    {ev.action === 'blocked' && (
                      <ButtonV2 size="sm" variant="secondary" onClick={() => props.onApprove(ev)}>
                        <Icon name="check" size="sm" /> {t('quarantine.approve.cta')}
                      </ButtonV2>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

// ── Approvals tab ──────────────────────────────────────────────────

function ApprovalsTab(props: {
  approvalsQ: ReturnType<typeof useQuery>
  onRevoke: (row: ApprovedVersion) => void
}) {
  const { t } = useTranslation()
  const data = props.approvalsQ.data as { items: ApprovedVersion[]; total: number } | undefined
  const items = data?.items ?? []

  if (props.approvalsQ.isLoading) {
    return <p className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</p>
  }
  if (items.length === 0) {
    return (
      <EmptyState
        icon="verified"
        title={t('quarantine.approvals.empty_title')}
        hint={t('quarantine.approvals.empty_hint')}
      />
    )
  }
  return (
    <div className="rounded-[8px] border overflow-hidden" style={{ borderColor: 'var(--border)' }}>
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
                  <EcosystemIcon type={row.ecosystem as any} size={14} />
                  <span className="text-[12px] font-mono">{row.ecosystem}</span>
                </div>
              </Td>
              <Td><span className="text-[13px] font-mono">{row.package}</span></Td>
              <Td><span className="text-[13px] font-mono" style={{ color: 'var(--text-soft)' }}>{row.version}</span></Td>
              <Td><span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>{row.reason}</span></Td>
              <Td>
                <ButtonV2 size="sm" variant="danger" onClick={() => props.onRevoke(row)}>
                  <Icon name="undo" size="sm" /> {t('quarantine.revoke.cta')}
                </ButtonV2>
              </Td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ── Tiny shared helpers ────────────────────────────────────────────

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="text-left px-3 py-2.5 text-[11px] font-mono font-[600] uppercase tracking-wider"
        style={{ color: 'var(--text-subtle)' }}>{children}</th>
  )
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-3 py-2 align-middle">{children}</td>
}

function FilterSelect(props: { value: string; onChange: (v: string) => void; options: { value: string; label: string }[] }) {
  return (
    <select
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
      className="h-9 px-3 rounded-[6px] text-[12px] cursor-pointer"
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
