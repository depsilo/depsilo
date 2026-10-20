import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import Icon from '@/components/Icon'

/** Absence of retained statistics cannot prove this is an unused instance. */
export default function ConnectionGuide({ blocked }: { blocked: boolean }) {
  const { t } = useTranslation()
  return (
    <section data-dashboard-onboarding aria-labelledby="dashboard-connect-title" className={`dashboard-connect ${blocked ? 'dashboard-connect-muted' : ''}`}>
      <div className="min-w-0">
        <h2 id="dashboard-connect-title" className="text-[22px] font-semibold tracking-tight">{t('overview.noRecentRequests')}</h2>
        <p className="mt-2 dashboard-connect-copy text-[14px] leading-relaxed text-[var(--dash-muted)]">{t('overview.connectDescription')}</p>
        <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2">
          <Link to={getAdminRouteHref('connect')} className={`dash-focus inline-flex min-h-[40px] items-center justify-center gap-2 rounded-md px-4 text-[14px] font-semibold no-underline ${blocked ? 'border border-[var(--dash-border)] text-[var(--dash-accent)]' : 'dashboard-connect-primary'}`}>{t('overview.connectClient')}<Icon name="arrow_forward" size="sm" /></Link>
          <Link to="/" className="dash-focus inline-flex min-h-[40px] items-center gap-1 rounded-md text-[14px] font-medium text-[var(--dash-accent)] no-underline">{t('overview.connectionDocs')}<Icon name="chevron_right" size="sm" /></Link>
        </div>
        <p className="mt-3 dashboard-connect-note text-[12px] leading-relaxed text-[var(--dash-muted)]">{t('overview.historyLimit')}</p>
      </div>
      <ol className="grid min-w-0 gap-4" aria-label={t('overview.connectionMilestones')}>
        {(['configure', 'request', 'verify'] as const).map((step, index) => <li key={step} className="flex min-w-0 items-start gap-3">
          <span className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-full border border-[var(--dash-border)] font-mono text-[12px] text-[var(--dash-muted)]">{index + 1}</span>
          <div><p className="text-[14px] font-medium">{t(`overview.connectSteps.${step}.title`)}</p><p className="mt-1 text-[12px] leading-relaxed text-[var(--dash-muted)]">{t(`overview.connectSteps.${step}.hint`)}</p></div>
        </li>)}
      </ol>
    </section>
  )
}
