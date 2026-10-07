import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import Icon, { type IconName } from '@/components/Icon'
import IconButton from '@/components/IconButton'
import { useTransientState } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'
import type { AdminCascadeInfo } from '@/lib/adminApi.types'

interface CascadeTopologyProps {
  info: AdminCascadeInfo
  boundUpstreams: number
  unresolved: number
}

function Node({
  icon,
  eyebrow,
  title,
  subtitle,
  badge,
}: {
  icon: IconName
  eyebrow: string
  title: ReactNode
  subtitle?: ReactNode
  badge?: ReactNode
}) {
  return (
    <div
      data-cascade-node
      className="min-w-0 flex-1 rounded-md border px-4 py-3"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-soft)' }}
    >
      <div className="flex items-center gap-2">
        <span
          className="grid size-7 shrink-0 place-items-center rounded-md"
          style={{ background: 'var(--brand-soft)', color: 'var(--brand-text)' }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <span className="text-[10px] font-[600] uppercase tracking-[0.08em]" style={{ color: 'var(--text-subtle)' }}>
          {eyebrow}
        </span>
        {badge && <span className="ml-auto shrink-0">{badge}</span>}
      </div>
      <div className="mt-2 truncate text-[13px] font-[600]" style={{ color: 'var(--text)' }} title={typeof title === 'string' ? title : undefined}>
        {title}
      </div>
      {subtitle && (
        <div className="mt-0.5 truncate text-[11px]" style={{ color: 'var(--text-soft)' }} title={typeof subtitle === 'string' ? subtitle : undefined}>
          {subtitle}
        </div>
      )}
    </div>
  )
}

function Connector({ label, dashed }: { label: string; dashed?: boolean }) {
  return (
    <div className="flex shrink-0 flex-row items-center gap-1 lg:flex-col lg:justify-center">
      <span className="text-[10px] whitespace-nowrap" style={{ color: 'var(--text-subtle)' }}>{label}</span>
      <Icon
        name="arrow_forward"
        size="sm"
        className="rotate-90 lg:rotate-0"
        style={{ color: dashed ? 'var(--text-subtle)' : 'var(--brand-text)' }}
      />
    </div>
  )
}

export default function CascadeTopology({
  info,
  boundUpstreams,
  unresolved,
}: CascadeTopologyProps) {
  const { t } = useTranslation()
  const [copied, showCopied] = useTransientState(false)
  const peers = info.peers
  const roleLabel = !info.enabled
    ? t('cascade.roleDisabled')
    : (peers.length > 0 ? t('cascade.roleBothShort') : t('cascade.roleParentShort'))
  const peerTitle = peers.length > 0
    ? peers.slice(0, 2).map(peer => peer.name).join(' · ') + (peers.length > 2 ? ` +${peers.length - 2}` : '')
    : t('cascade.topology.direct')
  const peerSubtitle = peers.length > 0
    ? peers[0].url
    : t('cascade.topology.directHint')

  return (
    <div
      data-cascade-topology
      className="rounded-lg border px-5 py-4"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-card)' }}
    >
      <div className="flex flex-col gap-3 lg:flex-row lg:items-stretch">
        <Node
          icon="computer"
          eyebrow={t('cascade.topology.clients')}
          title="npm · pip · go · docker"
          subtitle={t('cascade.topology.clientsHint')}
        />
        <Connector label={t('cascade.topology.serviceRequests')} dashed={!info.enabled} />
        <Node
          icon="hub"
          eyebrow={t('cascade.topology.thisNode')}
          title={roleLabel}
          subtitle={info.instance_id ? t('cascade.topology.instance', { id: info.instance_id.slice(0, 10) }) : undefined}
          badge={(
            <span
              className="inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px] font-[600]"
              style={{
                color: info.enabled ? 'var(--ok-text)' : 'var(--warn-text)',
                background: info.enabled ? 'var(--ok-fill)' : 'var(--warn-fill)',
              }}
            >
              <span className="size-1.5 rounded-full" style={{ background: 'currentColor' }} />
              {t(info.enabled ? 'cascade.enabled' : 'cascade.disabled')}
            </span>
          )}
        />
        <Connector label={t('cascade.topology.originRequests')} dashed={!info.enabled} />
        <Node
          icon={peers.length > 0 ? 'cloud_sync' : 'database'}
          eyebrow={peers.length > 0 ? t('cascade.topology.parents') : t('cascade.topology.upstream')}
          title={peerTitle}
          subtitle={peerSubtitle}
        />
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t pt-3 text-[11px]" style={{ borderColor: 'var(--border)', color: 'var(--text-soft)' }}>
        <span className="inline-flex items-center gap-1">
          <span style={{ color: 'var(--text-subtle)' }}>{t('cascade.instanceId')}</span>
          <span className="font-mono" style={{ color: 'var(--text)' }}>{info.instance_id || '—'}</span>
          {info.instance_id && (
            <IconButton
              icon={copied ? 'check' : 'content_copy'}
              label={t('cascade.copyInstanceId')}
              style={{ width: 30, height: 30, minWidth: 30, minHeight: 30 }}
              onClick={() => {
                void copyText(info.instance_id).then(ok => { if (ok) showCopied(true) })
              }}
            />
          )}
        </span>
        <span className="inline-flex items-center gap-1">
          <span style={{ color: 'var(--text-subtle)' }}>{t('cascade.relayPath')}</span>
          <span className="font-mono" style={{ color: 'var(--text)' }}>{info.relay_path}</span>
        </span>
        <span>{t('cascade.maxHops')} <span className="font-mono" style={{ color: 'var(--text)' }}>{info.max_hops}</span></span>
        <span>{t('cascade.maxTTL')} <span className="font-mono" style={{ color: 'var(--text)' }}>{formatTTL(info.max_ttl_seconds)}</span></span>
        <span>{t('cascade.boundLabel')} <span className="font-mono" style={{ color: 'var(--text)' }}>{boundUpstreams}</span></span>
        {unresolved > 0 && (
          <span style={{ color: 'var(--warn-text)' }}>
            {t('cascade.unresolvedLabel')} <span className="font-mono">{unresolved}</span>
          </span>
        )}
      </div>
    </div>
  )
}

function formatTTL(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '—'
  if (seconds % 86_400 === 0) return `${seconds / 86_400}d`
  if (seconds % 3_600 === 0) return `${seconds / 3_600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}
