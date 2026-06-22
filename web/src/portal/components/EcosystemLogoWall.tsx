import { LANGUAGES } from '@/lib/ecosystemData'
import EcosystemIcon from '@/components/EcosystemIcon'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

export default function EcosystemLogoWall({ selected, onSelect }: Props) {
  return (
    <div
      role="tablist"
      aria-label="Supported ecosystems"
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: 20,
        paddingTop: 20,
        marginTop: 20,
        borderTop: '0.5px solid var(--border)',
      }}
    >
      {LANGUAGES.map(lang => {
        const active = lang.id === selected
        return (
          <button
            key={lang.id}
            type="button"
            role="tab"
            aria-selected={active}
            title={lang.name}
            onClick={() => onSelect(lang.id)}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 32,
              height: 32,
              padding: 0,
              background: 'transparent',
              border: 'none',
              borderRadius: 6,
              cursor: 'pointer',
              opacity: active ? 1 : 0.55,
              filter: active ? 'none' : 'saturate(0.85)',
              transition: 'opacity 150ms ease, filter 150ms ease, transform 150ms ease',
            }}
            onMouseEnter={e => {
              e.currentTarget.style.opacity = '1'
              e.currentTarget.style.filter = 'none'
              e.currentTarget.style.transform = 'translateY(-1px)'
            }}
            onMouseLeave={e => {
              if (!active) {
                e.currentTarget.style.opacity = '0.55'
                e.currentTarget.style.filter = 'saturate(0.85)'
              }
              e.currentTarget.style.transform = 'translateY(0)'
            }}
          >
            <EcosystemIcon type={lang.iconAdapter as any} size={22} useColor />
          </button>
        )
      })}
    </div>
  )
}
