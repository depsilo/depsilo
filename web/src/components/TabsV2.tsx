interface TabItem {
  key: string
  label: string
  icon?: React.ReactNode
}

interface TabsV2Props {
  items: TabItem[]
  active: string
  onChange: (key: string) => void
}

export default function TabsV2({ items, active, onChange }: TabsV2Props) {
  return (
    <div className="flex gap-0" style={{ borderBottom: '1px solid var(--border)' }}>
      {items.map((tab) => {
        const isActive = active === tab.key
        return (
          <button
            key={tab.key}
            onClick={() => onChange(tab.key)}
            className="flex items-center gap-2 px-4 py-2.5 text-[14px] font-[400] bg-transparent cursor-pointer transition-colors duration-150"
            style={{
              color: isActive ? 'var(--heading)' : 'var(--body)',
              borderBottom: isActive ? '2px solid var(--stripe-purple)' : '2px solid transparent',
            }}
          >
            {tab.icon}
            {tab.label}
          </button>
        )
      })}
    </div>
  )
}
