import { useTranslation } from 'react-i18next'

import Button from '@/components/app/button'
import Notice from '@/components/app/notice'

interface ErrorStateProps {
  message: string
  onRetry: () => void
}

/**
 * The single initial-error presentation for a query boundary: it names the
 * failure and keeps Retry beside the message.
 */
export default function ErrorState({ message, onRetry }: ErrorStateProps) {
  const { t } = useTranslation()
  return (
    <Notice tone="destructive">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{message}</span>
        <Button type="button" variant="secondary" size="sm" onClick={onRetry}>
          {t('common.retry')}
        </Button>
      </div>
    </Notice>
  )
}
