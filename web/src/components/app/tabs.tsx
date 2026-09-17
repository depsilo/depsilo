import type { ReactNode } from 'react'

import {
  Tabs as UiTabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

export interface TabItem {
  key: string
  label: string
  icon?: ReactNode
  disabled?: boolean
  content: ReactNode
}

export interface TabsProps {
  items: TabItem[]
  value: string
  onValueChange: (value: string) => void
  ariaLabel: string
  orientation?: 'horizontal' | 'vertical'
  /**
   * `underline` is the page-local destination rail; `directory` is the indexed
   * list used by Settings, where selection reads as a filled row.
   */
  appearance?: 'underline' | 'directory'
}

export default function Tabs({
  items,
  value,
  onValueChange,
  ariaLabel,
  orientation = 'horizontal',
  appearance = 'underline',
}: TabsProps) {
  const directory = appearance === 'directory'
  const vertical = orientation === 'vertical'

  return (
    <UiTabs
      value={value}
      onValueChange={onValueChange}
      orientation={orientation}
      className={cn(
        'min-w-0',
        vertical && 'grid grid-cols-[180px_minmax(0,1fr)] gap-6',
        directory && !vertical && 'border-t border-border pt-5',
      )}
    >
      <TabsList
        aria-label={ariaLabel}
        activateOnFocus
        variant={directory ? 'default' : 'line'}
        className={cn(
          'h-auto w-full min-w-0 justify-start gap-1 overflow-x-auto rounded-none bg-transparent p-0',
          vertical && 'flex-col items-stretch self-start overflow-visible',
          !directory && 'border-b border-border',
        )}
      >
        {items.map((item) => (
          <TabsTrigger
            key={item.key}
            value={item.key}
            disabled={item.disabled}
            className={cn(
              'min-h-10 shrink-0 gap-2 px-4 py-2.5 text-[13px]',
              directory
                ? cn(
                    'justify-start rounded-md text-muted-foreground data-active:bg-accent data-active:text-accent-foreground',
                    vertical && 'w-full',
                  )
                : 'rounded-none data-active:bg-transparent',
            )}
          >
            {item.icon}
            {item.label}
          </TabsTrigger>
        ))}
      </TabsList>
      <div className={cn('min-w-0', directory && !vertical && 'pt-5')}>
        {items.map((item) => (
          <TabsContent key={item.key} value={item.key} className="min-w-0">
            {item.content}
          </TabsContent>
        ))}
      </div>
    </UiTabs>
  )
}
