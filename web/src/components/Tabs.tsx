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
  return (
    <Tabs.Root
      value={value}
      onValueChange={onValueChange}
      orientation={orientation}
      className={`min-w-0 data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:gap-6 ${directory ? 'border-t border-[var(--border)] pt-5' : ''}`}
    >
      <Tabs.List
        aria-label={ariaLabel}
        activateOnFocus
        className={`flex min-w-0 overflow-x-auto border-b border-[var(--border)] data-[orientation=vertical]:flex-col data-[orientation=vertical]:overflow-visible data-[orientation=vertical]:border-b-0 data-[orientation=vertical]:border-r ${directory ? 'gap-1 pb-3 data-[orientation=vertical]:self-start data-[orientation=vertical]:pr-4' : ''}`}
      >
        {items.map((item) => (
          <Tabs.Tab
            key={item.key}
            value={item.key}
            disabled={item.disabled}
            className={`stripe-focus-ring relative flex min-h-10 items-center gap-2 whitespace-nowrap font-[500] text-[var(--text-soft)] transition-colors duration-150 data-[active]:font-[600] data-[active]:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50 ${directory
              ? 'justify-start rounded-[4px] px-3 py-2 text-[13px] hover:bg-[var(--bg-hover)] data-[active]:bg-[var(--brand-soft)]'
              : 'px-4 py-2.5 text-[14px] after:absolute after:inset-x-2.5 after:bottom-[-1px] after:h-[2px] after:bg-transparent data-[active]:after:bg-[var(--brand)]'}`}
          >
            {item.icon}
            {item.label}
          </Tabs.Tab>
        ))}
      </Tabs.List>
      <div className={`min-w-0 ${directory && orientation === 'horizontal' ? 'pt-5' : ''}`}>
        {items.map((item) => (
          <Tabs.Panel key={item.key} value={item.key} className="min-w-0">
            {item.content}
          </Tabs.Panel>
        ))}
      </div>
    </Tabs.Root>
  )
}
