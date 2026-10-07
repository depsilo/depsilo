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

function FlowNode({
  icon,
  title,
  subtitle,
  badge,
  highlight = false,
}: {
  icon: IconName
  title: ReactNode
  subtitle?: ReactNode
  badge?: ReactNode
  highlight?: boolean
}) {
  return (
    <div
      data-cascade-node
      className="inline-flex min-w-0 max-w-full items-center gap-2 rounded-md border px-3 py-2"
      style={{
        borderColor: highlight ? 'color-mix(in oklab, var(--brand-text) 35%, var(--border))' : 'var(--border)',
        background: highlight ? 'var(--brand-soft)' : 'var(--bg-soft)',
      }}
    >
      <Icon name={icon} size="sm" className="shrink-0" style={{ color: 'var(--brand-text)' }} />
      <span className="min-w-0">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[12px] font-[600]" style={{ color: 'var(--text)' }}>{title}</span>
          {badge}
        </span>
        {subtitle && (
          <span className="block truncate text-[10px]" style={{ color: 'var(--text-soft)' }}>{subtitle}</span>
        )}
      </span>
    </div>
  )
}

function FlowArrow({ label, dashed }: { label: string; dashed?: boolean }) {
  return (
    <span className="hidden shrink-0 flex-col items-center px-1 sm:inline-flex">
      <span className="text-[9px] whitespace-nowrap" style={{ color: 'var(--text-subtle)' }}>{label}</span>
      <Icon
        name="arrow_forward"
        size="sm"
        style={{ color: dashed ? 'var(--text-subtle)' : 'var(--brand-text)' }}
      />
    </span>
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
  const peerSubtitle = peers.length > 0 ? peers[0].url : t('cascade.topology.directHint')

  const chips: ReactNode[] = []
  if (info.enabled && info.instance_id) {
    chips.push(
      <span key="instance" className="inline-flex items-center gap-0.5">
        <span style={{ color: 'var(--text-subtle)' }}>{t('cascade.instanceId')}</span>
        <span className="font-mono" style={{ color: 'var(--text)' }}>{info.instance_id}</span>
        <IconButton
          icon={copied ? 'check' : 'content_copy'}
          label={t('cascade.copyInstanceId')}
          style={{ width: 26, height: 26, minWidth: 26, minHeight: 26 }}
          onClick={() => {
            void copyText(info.instance_id).then(ok => { if (ok) showCopied(true) })
          }}
        />
      </span>,
    )
    chips.push(
      <span key="relay">
        {t('cascade.relayPath')} <span className="font-mono" style={{ color: 'var(--text)' }}>{info.relay_path}</span>
      </span>,
    )
  }
  if (boundUpstreams > 0 || info.enabled) {
    chips.push(
      <span key="bound">
        {t('cascade.boundLabel')} <span className="font-mono" style={{ color: 'var(--text)' }}>{boundUpstreams}</span>
      </span>,
    )
  }
  if (unresolved > 0) {
    chips.push(
      <span key="unresolved" style={{ color: 'var(--warn-text)' }}>
        {t('cascade.unresolvedLabel')} <span className="font-mono">{unresolved}</span>
      </span>,
    )
  }

  return (
    <div
      data-cascade-topology
      className="rounded-lg border px-4 py-3"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-card)' }}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <FlowNode
          icon="computer"
          title={t('cascade.topology.clients')}
          subtitle="npm · pip · go · docker"
        />
        <FlowArrow label={t('cascade.topology.serviceRequests')} dashed={!info.enabled} />
        <FlowNode
          icon="hub"
          title={roleLabel}
          subtitle={info.enabled && info.instance_id ? t('cascade.topology.instance', { id: info.instance_id.slice(0, 10) }) : undefined}
          highlight
          badge={(
            <span
              className="size-1.5 shrink-0 rounded-full"
              style={{ background: info.enabled ? 'var(--ok-text)' : 'var(--warn-text)' }}
              title={t(info.enabled ? 'cascade.enabled' : 'cascade.disabled')}
            />
          )}
        />
        <FlowArrow
          label={peers.length > 0 ? t('cascade.topology.originRequests') : t('cascade.topology.direct')}
          dashed={!info.enabled || peers.length === 0}
        />
        <FlowNode
          icon={peers.length > 0 ? 'cloud_sync' : 'database'}
          title={peerTitle}
          subtitle={peerSubtitle}
        />
      </div>
      {chips.length > 0 && (
        <div
          className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 border-t pt-2 text-[11px]"
          style={{ borderColor: 'var(--border)', color: 'var(--text-soft)' }}
        >
          {chips}
        </div>
      )}
    </div>
  )
}
