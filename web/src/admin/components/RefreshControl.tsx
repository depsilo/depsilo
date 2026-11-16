import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { Button } from '@/components/ui/button'
import { formatTime } from '@/lib/utils'

interface Props {
  /** When the shown data was fetched (React Query's dataUpdatedAt); 0 hides the time. */
  updatedAt: number
  fetching: boolean
  onRefresh: () => void
}

/**
 * The refresh command of an Admin list: when the rows were fetched and a
 * button to fetch them again, in the same place on every list page.
 */
export default function RefreshControl({ updatedAt, fetching, onRefresh }: Props) {
  const { t } = useTranslation()
  return (
    <span data-refresh-control className="inline-flex items-center gap-2">
      {updatedAt > 0 && (
        <span className="text-[12px] tabular-nums text-muted-foreground" data-refresh-updated>
          {t('filters.updatedAt', { time: formatTime(new Date(updatedAt).toISOString(), 'time') })}
        </span>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        aria-busy={fetching || undefined}
        disabled={fetching}
        onClick={onRefresh}
      >
        <Icon name="refresh" size="sm" className={fetching ? 'animate-spin motion-reduce:animate-none' : undefined} />
        {t('filters.refresh')}
      </Button>
    </span>
  )
}
