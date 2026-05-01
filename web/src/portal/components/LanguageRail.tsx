// web/src/portal/components/LanguageRail.tsx
import { LANGUAGES } from '@/lib/ecosystemData'
import EcosystemIcon from '@/components/EcosystemIcon'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

export default function LanguageRail({ selected, onSelect }: Props) {
  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      {/* All-in-one row */}
      <button
        type="button"
        onClick={() => onSelect('all')}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '10px 12px',
          background: selected === 'all' ? 'var(--brand-soft)' : 'transparent',
          borderBottom: '0.5px solid var(--border)',
          borderLeft: selected === 'all' ? '2px solid var(--brand)' : '2px solid transparent',
          textAlign: 'left',
          cursor: 'pointer',
          width: '100%',
        }}
      >
        <div
          style={{
            width: 26,
            height: 26,
            borderRadius: 6,
            background: 'var(--brand)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            flexShrink: 0,
          }}
        >
          <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <path
              d="M2.5 4l2 2 4-4M2.5 9l2 2 4-4M11 6h.5M11 11h.5"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 500,
              color: selected === 'all' ? 'var(--brand)' : 'var(--text)',
              whiteSpace: 'nowrap',
            }}
          >
            All-in-one
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
            Configure everything
          </div>
        </div>
      </button>

      {/* Eyebrow */}
      <div
        style={{
          padding: '8px 12px 4px',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <span className="eyebrow">Or by language</span>
      </div>

      {/* Language list */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
        {LANGUAGES.map((lang, i) => {
          const active = lang.id === selected
          return (
            <button
              key={lang.id}
              type="button"
              onClick={() => onSelect(lang.id)}
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '0 12px',
                textAlign: 'left',
                background: active ? 'var(--brand-soft)' : 'transparent',
                borderLeft: active ? '2px solid var(--brand)' : '2px solid transparent',
                borderBottom:
                  i === LANGUAGES.length - 1 ? 'none' : '0.5px solid var(--border)',
                transition: 'background 100ms ease',
                cursor: 'pointer',
                minHeight: 0,
                width: '100%',
              }}
            >
              <span
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: 4,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0,
                  opacity: active ? 1 : 0.65,
                }}
              >
                <EcosystemIcon
                  type={lang.iconAdapter as any}
                  size={15}
                  useColor={true}
                />
              </span>
              <span
                style={{
                  fontSize: 12.5,
                  fontWeight: active ? 500 : 400,
                  color: active ? 'var(--brand)' : 'var(--text)',
                  flex: 1,
                  minWidth: 0,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {lang.name}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
