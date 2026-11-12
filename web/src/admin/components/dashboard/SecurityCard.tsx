import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { getAdminRouteHref } from '@/admin/routes'
import Icon from '@/components/Icon'
import type { CapabilityFact, SecurityDashboard, SecuritySeverity } from '@/lib/adminApi.types'
import { summarizeProtections } from '@/lib/overviewCockpit'
import { formatTime } from '@/lib/utils'

import { useNumberLocale } from './cockpitFormat'
import { CardNote, CockpitCard, MoreLink, QueryBody } from './cockpitParts'

const SEVERITIES: readonly SecuritySeverity[] = ['critical', 'high', 'medium', 'low', 'unknown']
const MODE_LABEL: Record<string, string> = {
  block: 'security.capabilityBlock',
  warn: 'security.capabilityWarn',
  alert_only: 'security.capabilityAlertOnly',
  mixed: 'overview.protectionMixed',
}

interface SecurityCardProps {
  security: UseQueryResult<SecurityDashboard>
  suggestionsTotal?: number
  capabilities?: CapabilityFact[]
  capabilitiesUnavailable: boolean
}

/** Known vulnerabilities in what is cached, and which protections are switched on. */
export default function SecurityCard({ security, suggestionsTotal, capabilities, capabilitiesUnavailable }: SecurityCardProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const protections = summarizeProtections(capabilities)
  const count = (value: number) => value.toLocaleString(locale)

  return (
    <CockpitCard
      id="security"
      title={t('overview.securityTitle')}
      aside={<MoreLink to={getAdminRouteHref('security')}>{t('overview.linkSecurity')}</MoreLink>}
      busy={security.isPending}
      data-dashboard-security=""
    >
      <QueryBody query={security} unavailable={t('overview.securityUnavailable')} rows={4}>
        {data => {
          const total = SEVERITIES.reduce((sum, key) => sum + (data.by_severity[key] ?? 0), 0)
          return (
            <div className="dash-card-body">
              <p className="dash-lead">{t('overview.securityLead', { vulnerabilities: count(data.total_vulnerabilities), packages: count(data.affected_packages) })}</p>
              {total > 0 && (
                <div className="dash-sev" role="img" aria-label={t('overview.severityDistribution')}>
                  {SEVERITIES.map(key => (data.by_severity[key] ?? 0) > 0 && <i key={key} data-sev={key} style={{ flexGrow: data.by_severity[key] }} />)}
                </div>
              )}
              {total > 0 && (
                <ul className="dash-sev-key">
                  {SEVERITIES.map(key => <li key={key} data-sev={key}><i aria-hidden />{t(`security.${key}`)}<b>{count(data.by_severity[key] ?? 0)}</b></li>)}
                </ul>
              )}
              <dl className="dash-kv is-top">
                <div><dt>{t('overview.pendingSuggestions')}</dt><dd>{suggestionsTotal === undefined ? '—' : count(suggestionsTotal)}</dd></div>
                <div><dt>{t('overview.autoBlocked')}</dt><dd>{count(data.auto_blocked_count)}</dd></div>
                <div className="is-wide">
                  <dt>{t('overview.lastScan')}</dt>
                  <dd>{data.scan_in_progress ? t('overview.scanRunning') : data.last_scan_at ? formatTime(data.last_scan_at, 'auto') : t('overview.neverScanned')}</dd>
                </div>
              </dl>
            </div>
          )
        }}
      </QueryBody>
      {capabilitiesUnavailable && (
        <div className="pt-0" data-dashboard-protections>
          <h3 className="dash-subhead px-3">{t('overview.protectionTitle')}</h3>
          <CardNote tone="warning">{t('overview.protectionUnavailable')}</CardNote>
        </div>
      )}
      {protections.length > 0 && (
        <div className="dash-card-body pt-0" data-dashboard-protections>
          <h3 className="dash-subhead">{t('overview.protectionTitle')}</h3>
          <ul className="dash-prot">
            {protections.map(item => (
              <li key={item.key} data-protection={item.key} data-off={item.armed === 0 || undefined}>
                <span>{t(`security.capability_${item.key}`)}</span>
                <span>
                  {item.armed === 0 || item.mode === null
                    ? t('security.capabilityOff')
                    : t('overview.protectionArmed', { mode: t(MODE_LABEL[item.mode] ?? 'security.capabilityUnknown'), count: item.armed })}
                  {item.dataIssue && (
                    <em data-tone="warning"><Icon name="warning" size="sm" />{t(item.dataIssue === 'error' ? 'security.capabilityError' : 'security.capabilityStale')}</em>
                  )}
                  {!item.dataIssue && item.neverSynced > 0 && <em>{t('overview.protectionNeverSynced', { count: item.neverSynced })}</em>}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </CockpitCard>
  )
}
