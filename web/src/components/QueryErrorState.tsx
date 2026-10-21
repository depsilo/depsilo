import { Alert, AlertDescription } from '@/components/ui/alert'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

interface QueryErrorStateProps {
  message: string
  onRetry: () => void
}

export default function QueryErrorState({ message, onRetry }: QueryErrorStateProps) {
  const { t } = useTranslation()
  return (
    <Alert variant={"destructive"}><AlertDescription>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{message}</span>
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          {t('common.retry')}
        </Button>
      </div>
    </AlertDescription></Alert>
  )
}
