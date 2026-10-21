import { Alert, AlertDescription } from '@/components/ui/alert'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

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
    <Alert variant={"warning"}><AlertDescription>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{message ?? t('now.staleData')}</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-busy={refreshing || undefined}
          disabled={refreshing}
          onClick={() => { void onRefresh() }}
        >
          {t('now.refresh')}
        </Button>
      </div>
    </AlertDescription></Alert>
  )
}
