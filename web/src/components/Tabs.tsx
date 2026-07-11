import { Tabs } from '@base-ui/react/tabs'
import { type ReactNode } from 'react'

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
}

export default function TabsV2({
  items,
  value,
  onValueChange,
  ariaLabel,
  orientation = 'horizontal',
}: TabsV2Props) {
  return (
    <Tabs.Root
      value={value}
      onValueChange={onValueChange}
      orientation={orientation}
      className="min-w-0 data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:gap-6"
    >
      <Tabs.List
        aria-label={ariaLabel}
        activateOnFocus
        className="flex min-w-0 overflow-x-auto border-b border-[var(--border)] data-[orientation=vertical]:flex-col data-[orientation=vertical]:overflow-visible data-[orientation=vertical]:border-b-0 data-[orientation=vertical]:border-r"
      >
        {items.map((item) => (
          <Tabs.Tab
            key={item.key}
            value={item.key}
            disabled={item.disabled}
            className="stripe-focus-ring relative flex min-h-10 items-center gap-2 whitespace-nowrap px-4 py-2.5 text-[14px] font-[500] text-[var(--text-soft)] transition-colors duration-150 after:absolute after:inset-x-2.5 after:bottom-[-1px] after:h-[2px] after:bg-transparent data-[active]:font-[600] data-[active]:text-[var(--text)] data-[active]:after:bg-[var(--brand)] disabled:cursor-not-allowed disabled:opacity-50"
          >
            {item.icon}
            {item.label}
          </Tabs.Tab>
        ))}
      </Tabs.List>
      <div className="min-w-0">
        {items.map((item) => (
          <Tabs.Panel key={item.key} value={item.key} className="min-w-0">
            {item.content}
          </Tabs.Panel>
        ))}
      </div>
    </Tabs.Root>
  )
}
