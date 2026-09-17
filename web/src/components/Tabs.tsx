import { Tab, TabList, TabPanel, Tabs } from 'react-aria-components'
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
    // React Aria selects on focus by default (its `keyboardActivation`
    // "automatic"), which is what Base UI's activateOnFocus did. `Tab` renders a
    // div carrying role="tab" and the selected state lands on data-selected, not
    // data-active. Only the selected panel mounts.
    <Tabs
      selectedKey={value}
      onSelectionChange={(key) => onValueChange(String(key))}
      orientation={orientation}
      className={`min-w-0 data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:gap-6 ${directory ? 'border-t border-[var(--border)] pt-5' : ''}`}
    >
      <TabList
        aria-label={ariaLabel}
        className={`flex min-w-0 overflow-x-auto border-b border-[var(--border)] data-[orientation=vertical]:flex-col data-[orientation=vertical]:overflow-visible data-[orientation=vertical]:border-b-0 data-[orientation=vertical]:border-r ${directory ? 'gap-1 pb-3 data-[orientation=vertical]:self-start data-[orientation=vertical]:pr-4' : ''}`}
      >
        {items.map((item) => (
          <Tab
            key={item.key}
            id={item.key}
            isDisabled={item.disabled}
            className={`stripe-focus-ring relative flex min-h-10 items-center gap-2 whitespace-nowrap font-[500] text-[var(--text-soft)] transition-colors duration-150 data-[selected]:font-[600] data-[selected]:text-[var(--text)] data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50 ${directory
              ? 'justify-start rounded-[4px] px-3 py-2 text-[13px] hover:bg-[var(--bg-hover)] data-[selected]:bg-[var(--brand-soft)]'
              : 'px-4 py-2.5 text-[14px] after:absolute after:inset-x-2.5 after:bottom-[-1px] after:h-[2px] after:bg-transparent data-[selected]:after:bg-[var(--brand)]'}`}
          >
            {item.icon}
            {item.label}
          </Tab>
        ))}
      </TabList>
      <div className={`min-w-0 ${directory && orientation === 'horizontal' ? 'pt-5' : ''}`}>
        {items.map((item) => (
          <TabPanel key={item.key} id={item.key} className="min-w-0">
            {item.content}
          </TabPanel>
        ))}
      </div>
    </Tabs>
  )
}
