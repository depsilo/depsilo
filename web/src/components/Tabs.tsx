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
            className="flex items-center gap-2 px-4 py-2.5 text-[14px] bg-transparent cursor-pointer transition-colors duration-150"
            style={{
              position: 'relative',
              color: isActive ? 'var(--text)' : 'var(--text-soft)',
              fontWeight: isActive ? 600 : 500,
              letterSpacing: isActive ? '-0.005em' : undefined,
            }}
          >
            {tab.icon}
            {tab.label}
            {isActive && (
              <span style={{
                position: 'absolute',
                left: 10,
                right: 10,
                bottom: -1,
                height: 1.5,
                background: 'var(--grad-brand)',
                borderRadius: 1,
              }} />
            )}
          </button>
        )
      })}
    </div>
  )
}
