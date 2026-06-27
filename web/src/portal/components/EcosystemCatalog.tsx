// EcosystemCatalog — the QuickStart page's primary picker.
//
// All 14 ecosystems live as tiles arranged into four semantic groups:
// OS, Languages, Data & AI, Infrastructure. No collapsing, no filtering —
// the whole catalog is always visible so users can scan and click without
// hidden state. Group section headers use the global `.eyebrow` utility
// (10px mono caps, brand-aligned tracking).
//
// A tile carries the ecosystem icon, name, and a short native-language
// subtitle ("Python 包" / "Ruby gems") so unfamiliar managers (NuGet,
// Composer, CRAN) read clearly without expanding the design surface.
import { useTranslation } from 'react-i18next'
import { LANGUAGES, type Language, type LanguageGroup } from '@/lib/ecosystemData'
import EcosystemIcon from '@/components/EcosystemIcon'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

const GROUP_ORDER: LanguageGroup[] = ['os', 'lang', 'data', 'infra']

export default function EcosystemCatalog({ selected, onSelect }: Props) {
  const { t } = useTranslation()

  const groups: Record<LanguageGroup, Language[]> = { os: [], lang: [], data: [], infra: [] }
  for (const lang of LANGUAGES) groups[lang.group].push(lang)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
      {GROUP_ORDER.map(g => {
        const items = groups[g]
        if (items.length === 0) return null
        return (
          <section key={g}>
            <div
              className="eyebrow"
              style={{ marginBottom: 10, color: 'var(--text-subtle)' }}
            >
              {t(`quickstart.group${g.charAt(0).toUpperCase()}${g.slice(1)}`)}
            </div>
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
                gap: 10,
              }}
            >
              {items.map(lang => {
                const active = lang.id === selected
                return (
                  <button
                    key={lang.id}
                    type="button"
                    onClick={() => onSelect(lang.id)}
                    className="active:scale-[0.96]"
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      alignItems: 'flex-start',
                      gap: 8,
                      padding: '12px 12px 14px',
                      background: active ? 'var(--brand-soft)' : 'var(--bg-card)',
                      border: `0.5px solid ${active ? 'var(--brand)' : 'var(--border)'}`,
                      borderRadius: 10,
                      cursor: 'pointer',
                      textAlign: 'left',
                      // Tiny lift on hover + a clear brand outline when
                      // active. transition list is explicit so the future
                      // addition of e.g. a gradient bg doesn't auto-animate.
                      transition: 'background 140ms ease, border-color 140ms ease, transform 140ms cubic-bezier(0.2, 0, 0, 1), box-shadow 140ms ease',
                      boxShadow: active
                        ? '0 0 0 2px color-mix(in oklab, var(--brand) 18%, transparent)'
                        : '0 0 0 0 transparent',
                    }}
                    onMouseEnter={e => {
                      if (active) return
                      e.currentTarget.style.borderColor = 'var(--border-strong)'
                      e.currentTarget.style.transform = 'translateY(-1px)'
                    }}
                    onMouseLeave={e => {
                      if (active) return
                      e.currentTarget.style.borderColor = 'var(--border)'
                      e.currentTarget.style.transform = 'translateY(0)'
                    }}
                  >
                    <span
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 32,
                        height: 32,
                        borderRadius: 7,
                        background: active ? 'var(--bg-card)' : 'var(--bg-soft)',
                        flexShrink: 0,
                      }}
                    >
                      <EcosystemIcon type={lang.iconAdapter as any} size={18} useColor />
                    </span>
                    <span
                      style={{
                        fontSize: 13,
                        fontWeight: 600,
                        color: active ? 'var(--brand-text)' : 'var(--text)',
                        letterSpacing: '-0.01em',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {lang.name}
                    </span>
                    <span
                      style={{
                        fontSize: 11,
                        color: active ? 'var(--brand-text)' : 'var(--text-subtle)',
                        opacity: active ? 0.85 : 1,
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        width: '100%',
                      }}
                    >
                      {t(`quickstart.eco${lang.subtitleKey.charAt(0).toUpperCase()}${lang.subtitleKey.slice(1)}`)}
                    </span>
                  </button>
                )
              })}
            </div>
          </section>
        )
      })}
    </div>
  )
}
