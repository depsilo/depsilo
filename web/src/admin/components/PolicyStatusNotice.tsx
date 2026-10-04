import { useTranslation } from 'react-i18next'

import ButtonV2 from '@/components/Button'
import InlineNotice from '@/components/InlineNotice'
import { usePolicyStatus } from '../usePolicyStatus'

/**
 * In-page policy notice. It renders inside the page content, below the page
 * heading, so it never displaces the page's own identity at the top.
 */
export default function PolicyStatusNotice() {
  const { t } = useTranslation()
  const policy = usePolicyStatus()
  if (!policy.needsAttention) return null

  return (
    <div
      data-admin-policy-status-banner
      role="status"
      aria-live="polite"
      aria-atomic="true"
      className="mb-4"
    >
      <InlineNotice tone="warning">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="font-[600]">
              {policy.isUnverified ? t('policy.statusUnavailable') : t('policy.staleSnapshot')}
            </p>
            {!policy.isUnverified && (
              <p className="mt-0.5 text-[12px]" style={{ color: 'var(--text-soft)' }}>
                {policy.refreshTime
                  ? t('policy.lastSuccessfulRefresh', { time: policy.refreshTime })
                  : t('policy.neverRefreshed')}
              </p>
            )}
          </div>
          <ButtonV2
            type="button"
            variant="secondary"
            size="sm"
            aria-busy={policy.refreshing || undefined}
            disabled={policy.refreshing}
            onClick={policy.refresh}
          >
            {policy.refreshing ? t('policy.refreshing') : t('policy.refresh')}
          </ButtonV2>
        </div>
      </InlineNotice>
    </div>
  )
}
