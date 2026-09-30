import { type ReactNode } from 'react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

export interface TabItem {
  key: string
  label: string
  icon?: ReactNode
  disabled?: boolean
  content: ReactNode
}

export interface TabsV2Props {
  items: TabItem[]
  value: string
  onValueChange: (value: string) => void
  ariaLabel: string
  orientation?: 'horizontal' | 'vertical'
  appearance?: 'underline' | 'directory'
}

export default function TabsV2({
  items,
  value,
  onValueChange,
  ariaLabel,
  orientation = 'horizontal',
  appearance = 'underline',
}: TabsV2Props) {
  const directory = appearance === 'directory'
  // shadcn's `default` track is the pill rail (Admin's directory index);
  // `line` is the underline rail (page-level sections).
  const variant = directory ? 'default' : 'line'
  return (
    <Tabs
      value={value}
      onValueChange={onValueChange}
      orientation={orientation}
      className={
        directory
          ? 'data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:items-start data-[orientation=vertical]:gap-6'
          : undefined
      }
    >
      <TabsList
        aria-label={ariaLabel}
        activateOnFocus
        variant={variant}
        className={directory ? 'data-[orientation=vertical]:w-full' : undefined}
      >
        {items.map((item) => (
          <TabsTrigger
            key={item.key}
            value={item.key}
            disabled={item.disabled}
          >
            {item.icon}
            {item.label}
          </TabsTrigger>
        ))}
      </TabsList>
      <div className="min-w-0">
        {items.map((item) => (
          <TabsContent key={item.key} value={item.key}>
            {item.content}
          </TabsContent>
        ))}
      </div>
    </Tabs>
  )
}
