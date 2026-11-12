import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { getAdminRouteHref } from '@/admin/routes'
import { useFailureSummary } from '@/admin/upstreams/failureCopy'
import type { AdminUpstream, ReadinessResponse, SecurityDashboard } from '@/lib/adminApi.types'
import type { ServiceProblemCode, ServiceStatusModel } from '@/lib/dashboardOverview'
import { buildAttentionItems, type AttentionItem } from '@/lib/overviewCockpit'

import { ecosystemLabel, useNumberLocale } from './cockpitFormat'
import { CardNote, CockpitCard, MoreLink, ToneIcon, type CockpitTone } from './cockpitParts'

interface AttentionCardProps {
  status: ServiceStatusModel
  pending: boolean
  readiness?: ReadinessResponse
  /** Whether an Upstream health payload has ever loaded. */
  upstreamsAvailable: boolean
  policyStale: boolean
  upstreamList?: AdminUpstream[]
  suggestionsTotal?: number
  /** Whether the open-suggestion count has loaded; until then "nothing to do" cannot be claimed. */
  suggestionsKnown: boolean
  security?: SecurityDashboard
  onOpenStatus: () => void
}

/** Problems an operator resolves on another page, and the copy that says why. */
const PROBLEM_LINKS: Partial<Record<ServiceProblemCode, { title: string; detail: string; route: Parameters<typeof getAdminRouteHref>[0]; action: string }>> = {
  database: { title: 'overview.problemDatabase', detail: 'overview.readinessImpact', route: 'settings', action: 'overview.viewSettings' },
  storage: { title: 'overview.problemStorage', detail: 'overview.readinessImpact', route: 'settings', action: 'overview.viewSettings' },
  policy: { title: 'overview.problemPolicy', detail: 'overview.policyImpact', route: 'rules', action: 'policy.reviewRules' },
  cache: { title: 'overview.problemCache', detail: 'overview.cacheImpact', route: 'cache', action: 'dashboard.manageCache' },
  degraded: { title: 'overview.problemDegraded', detail: 'overview.upstreamImpact', route: 'upstreams', action: 'overview.attentionViewUpstream' },
}

/** Signals that could not be read. Their detail lives in the status dialog. */
const SIGNAL_TITLES: Partial<Record<ServiceProblemCode, string>> = {
  'status-unavailable': 'overview.problemUnavailable',
  'readiness-unavailable': 'overview.problemReadinessUnavailable',
  'readiness-stale': 'overview.problemReadinessStale',
  'upstream-status-unavailable': 'overview.upstreamStatusUnavailable',
  'upstream-status-stale': 'overview.upstreamStatusStale',
  'policy-status-unavailable': 'overview.policyStatusUnavailable',
  'policy-status-stale': 'overview.policyStatusStale',
}

function readinessTone(value: 'ready' | 'unavailable' | undefined): CockpitTone {
  return value === 'ready' ? 'ok' : value === 'unavailable' ? 'danger' : 'unknown'
}

/**
 * What needs an operator, most urgent first, each with what it means for
 * downloads and one place to act. It reports "nothing to do" only when every
 * signal behind the service status is known and clear.
 */
export default function AttentionCard({
  status, pending, readiness, upstreamsAvailable, policyStale, upstreamList, suggestionsTotal, suggestionsKnown, security, onOpenStatus,
}: AttentionCardProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const failureSummary = useFailureSummary()
  const items = pending ? [] : buildAttentionItems({ status, upstreams: upstreamList, suggestionsTotal, security })

  const render = (item: AttentionItem): { title: string; detail: string; action: ReactNode } => {
    if (item.kind === 'upstream') {
      const ecosystem = ecosystemLabel(item.upstream.adapter)
      const detail = [
        item.failure ? failureSummary(item.failure) : '',
        item.down ? '' : t('overview.attentionSlowDetail', { latency: Math.round(item.upstream.avg_latency_ms).toLocaleString(locale) }),
        // What else the ecosystem can fall back to matters only when this one cannot serve.
        !item.down ? ''
          : item.usableSiblings > 0
            ? t('overview.attentionSiblingsUsable', { ecosystem, count: item.usableSiblings })
            : t('overview.attentionNoSibling', { ecosystem }),
      ].filter(Boolean).join(' ')
      return {
        title: t(item.down ? 'overview.attentionUpstreamDown' : 'overview.attentionUpstreamSlow', { name: item.upstream.name, ecosystem }),
        detail,
        action: <MoreLink to={`${getAdminRouteHref('upstreams')}?status=attention`}>{t('overview.attentionViewUpstream')}</MoreLink>,
      }
    }
    if (item.kind === 'security') {
      return {
        title: t('overview.attentionSecurityTitle', { count: item.suggestions.toLocaleString(locale) }),
        detail: [
          item.affectedPackages === null ? '' : t('overview.attentionSecurityDetail', { count: item.affectedPackages.toLocaleString(locale) }),
          item.critical > 0 ? t('overview.attentionSecurityCritical', { count: item.critical.toLocaleString(locale) }) : '',
        ].filter(Boolean).join(' '),
        action: <MoreLink to={`${getAdminRouteHref('security')}?tab=suggestions`}>{t('overview.attentionViewSuggestions')}</MoreLink>,
      }
    }
    const link = PROBLEM_LINKS[item.code]
    if (link) {
      return {
        title: t(item.code === 'policy' && policyStale ? 'overview.problemPolicyStale' : link.title, { percent: (item.percent ?? 0).toFixed(1) }),
        detail: t(link.detail),
        action: <MoreLink to={getAdminRouteHref(link.route)}>{t(link.action)}</MoreLink>,
      }
    }
    return {
      title: t(SIGNAL_TITLES[item.code] ?? 'overview.problemDegraded'),
      detail: t('overview.healthUnknownImpact'),
      action: <button type="button" className="dash-note-action dash-focus" onClick={onOpenStatus}>{t('overview.viewStatus')}</button>,
    }
  }

  const policyProblem = status.problems.find(problem => problem.code.startsWith('policy'))
  const upstreamTotal = status.upstreams.length
  const upstreamUsable = status.upstreams.filter(upstream => upstream.state !== 'unavailable').length
  const checks: { key: string; label: string; value?: string; tone: CockpitTone; state: string }[] = [
    ...(['database', 'storage'] as const).map(check => ({
      key: check,
      label: t(`overview.healthChecks.${check}`),
      tone: readinessTone(readiness?.checks[check]),
      state: t(readiness?.checks[check] === 'ready' ? 'overview.checkReady'
        : readiness?.checks[check] === 'unavailable' ? 'overview.checkUnavailable' : 'overview.checkUnknown'),
    })),
    {
      key: 'upstreams', label: t('overview.checkUpstreams'),
      value: upstreamsAvailable ? `${upstreamUsable}/${upstreamTotal}` : undefined,
      tone: !upstreamsAvailable ? 'unknown' : status.upstreamIssues.length > 0 ? 'warning' : 'ok',
      state: t(!upstreamsAvailable ? 'overview.checkUnknown' : status.upstreamIssues.length > 0 ? 'overview.checkAttention' : 'overview.checkReady'),
    },
    {
      key: 'policy', label: t('overview.checkPolicy'),
      tone: !policyProblem ? 'ok' : policyProblem.code === 'policy' ? 'warning' : 'unknown',
      state: t(!policyProblem ? 'overview.checkReady' : policyProblem.code === 'policy' ? 'overview.checkAttention' : 'overview.checkUnknown'),
    },
  ]

  return (
    <CockpitCard
      id="attention"
      title={t('overview.attentionTitle')}
      chip={pending ? undefined : items.length.toLocaleString(locale)}
      aside={<MoreLink to={getAdminRouteHref('attention')}>{t('overview.attentionAll')}</MoreLink>}
      busy={pending}
      data-dashboard-attention=""
    >
      {pending ? (
        <CardNote role="status">{t('overview.checkingHealth')}</CardNote>
      ) : items.length === 0 ? (
        status.health === 'healthy' && suggestionsKnown
          ? <p className="dash-allclear" data-attention-clear><ToneIcon tone="ok" />{t('overview.attentionClear')}</p>
          : <CardNote>{t(status.health === 'healthy' ? 'overview.attentionSuggestionsUnknown' : 'overview.attentionUnknown')}</CardNote>
      ) : (
        <ul className="dash-attn">
          {items.map(item => {
            const { title, detail, action } = render(item)
            return (
              <li key={item.key} data-attention-item={item.kind} data-attention-tone={item.tone}>
                <ToneIcon tone={item.tone} />
                <p><strong>{title}</strong>{detail && <span>{detail}</span>}</p>
                {action}
              </li>
            )
          })}
        </ul>
      )}
      {!pending && (
        <ul className="dash-checks" aria-label={t('overview.checksLabel')}>
          {checks.map(check => (
            <li key={check.key} data-check={check.key} data-check-tone={check.tone}>
              <ToneIcon tone={check.tone} />{check.label}{check.value && <b>{check.value}</b>}
              <span className="sr-only">{check.state}</span>
            </li>
          ))}
        </ul>
      )}
    </CockpitCard>
  )
}
