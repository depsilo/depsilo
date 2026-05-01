interface SegmentedOption {
  value: string
  label: string
}

interface Props {
  options: SegmentedOption[]
  value: string
  onChange: (value: string) => void
}

export default function Segmented({ options, value, onChange }: Props) {
  return (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        padding: 3,
        gap: 2,
        flexShrink: 0,
      }}
    >
      {options.map(opt => {
        const active = opt.value === value
        return (
          <button
            key={opt.value}
            onClick={() => onChange(opt.value)}
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              fontWeight: active ? 600 : 400,
              letterSpacing: '0.01em',
              color: active ? 'var(--text)' : 'var(--text-soft)',
              background: active ? 'var(--bg-card)' : 'transparent',
              border: active ? '0.5px solid var(--border-strong)' : '0.5px solid transparent',
              borderRadius: 5,
              padding: '4px 10px',
              cursor: 'pointer',
              transition: 'all 120ms ease',
            }}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
