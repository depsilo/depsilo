import { useTranslation } from 'react-i18next'
import ButtonV2 from './Button'
import InlineNotice from './InlineNotice'

interface QueryErrorStateProps {
  message: string
  onRetry: () => void
}

export default function QueryErrorState({ message, onRetry }: QueryErrorStateProps) {
  const { t } = useTranslation()
  return (
    <InlineNotice tone="danger">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{message}</span>
        <ButtonV2 type="button" variant="secondary" size="sm" onClick={onRetry}>
          {t('common.retry')}
        </ButtonV2>
      </div>
    </InlineNotice>
  )
}
