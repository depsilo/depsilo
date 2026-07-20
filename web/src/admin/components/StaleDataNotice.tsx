import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import ButtonV2 from '@/components/Button'
import InlineNotice from '@/components/InlineNotice'

interface StaleDataNoticeProps {
  onRefresh: () => unknown
  message?: ReactNode
  refreshing?: boolean
}

/** A consistent recovery action for queries that are showing stale data. */
export default function StaleDataNotice({
  onRefresh,
  message,
  refreshing = false,
}: StaleDataNoticeProps) {
  const { t } = useTranslation()

  return (
    <InlineNotice tone="warning">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{message ?? t('now.staleData')}</span>
        <ButtonV2
          type="button"
          variant="secondary"
          size="sm"
          aria-busy={refreshing || undefined}
          disabled={refreshing}
          onClick={() => { void onRefresh() }}
        >
          {t('now.refresh')}
        </ButtonV2>
      </div>
    </InlineNotice>
  )
}
