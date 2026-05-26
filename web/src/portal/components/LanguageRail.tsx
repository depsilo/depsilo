// web/src/portal/components/LanguageRail.tsx
import { useTranslation } from 'react-i18next'
import { LANGUAGES } from '@/lib/ecosystemData'
import EcosystemIcon from '@/components/EcosystemIcon'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

export default function LanguageRail({ selected, onSelect }: Props) {
  const { t } = useTranslation()
  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', alignSelf: 'start' }}
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
            {t('quickstart.allInOne')}
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
            {t('quickstart.allInOneSubtitle')}
          </div>
        </div>
      </button>

      {/* AI Agent row */}
      <button
        type="button"
        onClick={() => onSelect('agent')}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '10px 12px',
          background: selected === 'agent' ? 'var(--brand-soft)' : 'transparent',
          borderBottom: '0.5px solid var(--border)',
          borderLeft: selected === 'agent' ? '2px solid var(--brand)' : '2px solid transparent',
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
            background: selected === 'agent' ? 'var(--brand)' : 'var(--bg-soft)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: selected === 'agent' ? '#fff' : 'var(--brand)',
            flexShrink: 0,
            border: selected === 'agent' ? 'none' : '0.5px solid var(--border)',
          }}
        >
          <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <path
              d="M7 1.5v2M3.5 3l1.4 1.4M10.5 3l-1.4 1.4M2.5 7h2M9.5 7h2M5 7a2 2 0 1 1 4 0v3H5V7zM5.5 11.5h3M6 12.8h2"
              stroke="currentColor"
              strokeWidth="1.2"
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
              color: selected === 'agent' ? 'var(--brand)' : 'var(--text)',
              whiteSpace: 'nowrap',
            }}
          >
            {t('quickstart.aiAgent')}
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
            {t('quickstart.aiAgentSubtitle')}
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
        <span className="eyebrow">{t('quickstart.orByLanguage')}</span>
      </div>

      {/* Language list */}
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {LANGUAGES.map((lang, i) => {
          const active = lang.id === selected
          return (
            <button
              key={lang.id}
              type="button"
              onClick={() => onSelect(lang.id)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '8px 12px',
                textAlign: 'left',
                background: active ? 'var(--brand-soft)' : 'transparent',
                borderLeft: active ? '2px solid var(--brand)' : '2px solid transparent',
                borderBottom:
                  i === LANGUAGES.length - 1 ? 'none' : '0.5px solid var(--border)',
                transition: 'background 100ms ease',
                cursor: 'pointer',
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
