import type { ReactNode } from 'react'
import { Popover } from '@base-ui/react/popover'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'

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
    <Popover.Root>
      <Popover.Trigger aria-label={`${label} · ${ariaLabel}`} className="dash-focus dashboard-help-trigger">
        <span>{label}</span>
        <Icon name="info" size="sm" className="shrink-0" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="start" sideOffset={8} collisionPadding={12} className="z-50">
          <Popover.Popup data-dashboard-metric-help className="dashboard-help-popup">
            <div className="dashboard-help-heading">
              <Popover.Title className="dashboard-help-title">{label}</Popover.Title>
              <Popover.Close aria-label={t('common.close')} className="stripe-focus-ring dashboard-help-close">
                <Icon name="close" size="sm" />
              </Popover.Close>
            </div>
            <div className="dashboard-help-body">{children}</div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
