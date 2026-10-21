import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { Button } from '@/components/ui/button'
import { Popover, PopoverClose, PopoverContent, PopoverHeader, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'

interface DashboardMetricHelpProps {
  label: string
  ariaLabel: string
  children: ReactNode
}

/** A metric label and its explanation share one touch-sized, keyboard-accessible
 * trigger. Base UI owns positioning, dismissal, and focus restoration. */
export default function DashboardMetricHelp({ label, ariaLabel, children }: DashboardMetricHelpProps) {
  const { t } = useTranslation()

  return (
    <Popover>
      <PopoverTrigger
        aria-label={`${label} · ${ariaLabel}`}
        render={<Button variant="ghost" className="min-h-[40px] min-w-[40px] max-w-full justify-start gap-1.5 whitespace-normal px-0 text-left text-[length:inherit] font-[inherit] leading-normal text-inherit hover:bg-transparent hover:text-primary data-popup-open:text-primary dark:hover:bg-transparent" />}
      >
        <span>{label}</span>
        <Icon name="info" size="sm" className="shrink-0" />
      </PopoverTrigger>
      <PopoverContent data-dashboard-metric-help align="start" className="w-[360px]">
        <PopoverHeader>
          <PopoverTitle>{label}</PopoverTitle>
          <PopoverClose aria-label={t('common.close')} render={<Button variant="ghost" size="icon" className="-mt-2 -mr-2 -mb-2 text-muted-foreground" />}>
            <Icon name="close" size="sm" />
          </PopoverClose>
        </PopoverHeader>
        <div className="space-y-3">{children}</div>
      </PopoverContent>
    </Popover>
  )
}
