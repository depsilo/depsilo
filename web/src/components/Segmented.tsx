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
      className="flex-wrap"
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
            type="button"
            key={opt.value}
            onClick={() => onChange(opt.value)}
            className="min-h-10 whitespace-nowrap active:scale-[0.96]"
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              fontWeight: active ? 600 : 400,
              letterSpacing: '0.01em',
              color: active ? 'var(--text)' : 'var(--text-soft)',
              background: active ? 'var(--bg-card)' : 'transparent',
              border: active ? '0.5px solid var(--border-strong)' : '0.5px solid transparent',
              // Outer container: borderRadius 8, padding 3.  Inner = 8 - 3 = 5.
              // Concentric — keep these in sync if the container ever changes.
              borderRadius: 5,
              padding: '4px 10px',
              cursor: 'pointer',
              // `transition: all` here used to animate font-weight changes
              // too, producing a half-pixel jitter as glyph metrics shifted.
              transition: 'background 120ms ease, color 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
            }}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
