// EcosystemCatalog — the left-rail picker inside QuickStart's console card.
//
// Vertical list, ≈260px wide, 4 grouped sections (OS / Languages / Data & AI /
// Infra). All 14 ecosystems always visible without scrolling on any
// reasonable height. Selection drives the right-pane configuration view.
//
// Each row is a compact ≈36px line — icon + name + subtitle — so the
// whole catalog fits beside the active config pane in a single
// viewport. Replaces the earlier large-tile grid that pushed the
// configuration pane below the fold.
import { useTranslation } from 'react-i18next'
import { LANGUAGES, type Language, type LanguageGroup } from '@/lib/ecosystemData'
import EcosystemIcon from '@/components/EcosystemIcon'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

const GROUP_ORDER: LanguageGroup[] = ['os', 'lang', 'data', 'infra']

function groupLabelKey(g: LanguageGroup): string {
  return `quickstart.group${g.charAt(0).toUpperCase()}${g.slice(1)}`
}

function ecoLabelKey(subtitleKey: string): string {
  return `quickstart.eco${subtitleKey.charAt(0).toUpperCase()}${subtitleKey.slice(1)}`
}

export default function EcosystemCatalog({ selected, onSelect }: Props) {
  const { t } = useTranslation()

  const groups: Record<LanguageGroup, Language[]> = { os: [], lang: [], data: [], infra: [] }
  for (const lang of LANGUAGES) groups[lang.group].push(lang)

  return (
    <nav
      aria-label={t('quickstart.pickEcosystem')}
      style={{
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--bg-soft)',
        borderRight: '0.5px solid var(--border)',
        padding: '12px 0 16px',
        minWidth: 0,
        overflowY: 'auto',
      }}
    >
      {/* Rail title */}
      <div
        style={{
          padding: '0 16px 10px',
          marginBottom: 4,
          fontSize: 11,
          fontWeight: 600,
          color: 'var(--text-subtle)',
          letterSpacing: '0.04em',
          textTransform: 'uppercase',
          fontFamily: 'var(--font-mono)',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        {t('quickstart.pickEcosystem')}
      </div>

      {GROUP_ORDER.map(g => {
        const items = groups[g]
        if (items.length === 0) return null
        return (
          <section key={g} className="sv-reveal" style={{ marginTop: 12 }}>
            <div
              className="eyebrow"
              style={{
                padding: '0 16px',
                marginBottom: 4,
                color: 'var(--text-subtle)',
                fontSize: 9,
              }}
            >
              {t(groupLabelKey(g))}
            </div>
            <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {items.map(lang => {
                const active = lang.id === selected
                return (
                  <li key={lang.id}>
                    <button
                      type="button"
                      onClick={() => onSelect(lang.id)}
                      className="active:scale-[0.98] eco-tile"
                      onMouseMove={e => {
                        // Track the pointer inside the tile via CSS vars
                        // so a radial-gradient pseudo-element can follow it.
                        // Zero per-frame React state — we mutate raw CSS
                        // custom properties on the element itself.
                        const r = e.currentTarget.getBoundingClientRect()
                        e.currentTarget.style.setProperty('--spot-x', `${e.clientX - r.left}px`)
                        e.currentTarget.style.setProperty('--spot-y', `${e.clientY - r.top}px`)
                      }}
                      style={{
                        position: 'relative',
                        overflow: 'hidden',
                        display: 'flex',
                        alignItems: 'center',
                        gap: 10,
                        width: '100%',
                        padding: '7px 16px 7px 13px',
                        background: active
                          ? 'color-mix(in oklab, var(--brand) 10%, transparent)'
                          : 'transparent',
                        border: 'none',
                        borderLeft: active
                          ? '3px solid var(--brand)'
                          : '3px solid transparent',
                        textAlign: 'left',
                        cursor: 'pointer',
                        transition:
                          'background 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
                      }}
                      onMouseEnter={e => {
                        if (active) return
                        e.currentTarget.style.background = 'var(--bg-hover)'
                      }}
                      onMouseLeave={e => {
                        if (active) return
                        e.currentTarget.style.background = 'transparent'
                      }}
                    >
                      <span
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          width: 22,
                          height: 22,
                          borderRadius: 5,
                          background: active ? 'var(--bg-card)' : 'transparent',
                          flexShrink: 0,
                          opacity: active ? 1 : 0.85,
                        }}
                      >
                        <EcosystemIcon
                          type={lang.iconAdapter as any}
                          size={14}
                          useColor
                        />
                      </span>
                      <span style={{ flex: 1, minWidth: 0 }}>
                        <span
                          style={{
                            display: 'block',
                            fontSize: 13,
                            fontWeight: active ? 600 : 500,
                            color: active ? 'var(--brand-text)' : 'var(--text)',
                            letterSpacing: '-0.005em',
                            whiteSpace: 'nowrap',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                          }}
                        >
                          {lang.name}
                        </span>
                        <span
                          style={{
                            display: 'block',
                            fontSize: 10.5,
                            color: 'var(--text-subtle)',
                            letterSpacing: '0',
                            marginTop: 1,
                            whiteSpace: 'nowrap',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                          }}
                        >
                          {t(ecoLabelKey(lang.subtitleKey))}
                        </span>
                      </span>
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        )
      })}
    </nav>
  )
}
