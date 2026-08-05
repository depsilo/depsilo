import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import Input from '@/components/Input'
import { LANGUAGES, type Language, type LanguageGroup } from '@/lib/ecosystemData'

interface Props {
  selected: string
  recent: string[]
  onSelect: (id: string) => void
}

interface EcosystemButtonProps {
  language: Language
  selected: string
  subtitle: string
  onSelect: (id: string) => void
  compact?: boolean
  chip?: boolean
}

const GROUP_ORDER: LanguageGroup[] = ['os', 'lang', 'data', 'infra']

function groupLabelKey(group: LanguageGroup): string {
  return `quickstart.group${group.charAt(0).toUpperCase()}${group.slice(1)}`
}

function ecoLabelKey(subtitleKey: string): string {
  return `quickstart.eco${subtitleKey.charAt(0).toUpperCase()}${subtitleKey.slice(1)}`
}

function EcosystemButton({
  language,
  selected,
  subtitle,
  onSelect,
  compact = false,
  chip = false,
}: EcosystemButtonProps) {
  const active = language.id === selected

  return (
    <button
      type="button"
      aria-pressed={active}
      data-active={active ? 'true' : undefined}
      title={language.name}
      onClick={() => onSelect(language.id)}
      className="eco-tile stripe-focus-ring active:scale-[0.98]"
      onMouseMove={event => {
        const bounds = event.currentTarget.getBoundingClientRect()
        event.currentTarget.style.setProperty('--spot-x', `${event.clientX - bounds.left}px`)
        event.currentTarget.style.setProperty('--spot-y', `${event.clientY - bounds.top}px`)
      }}
      style={{
        position: 'relative',
        overflow: 'hidden',
        display: 'flex',
        alignItems: 'center',
        gap: compact ? 6 : 11,
        width: '100%',
        minHeight: chip ? 40 : compact ? 40 : 52,
        padding: chip ? '6px 10px' : compact ? '5px 4px' : '8px 10px',
        background: active ? 'var(--brand-soft)' : 'transparent',
        border: `1px solid ${active ? 'var(--brand-border)' : 'transparent'}`,
        borderRadius: chip || compact ? 6 : 8,
        boxShadow: active ? 'inset 1px 0 0 var(--brand)' : 'none',
        textAlign: 'left',
        cursor: 'pointer',
        transition:
          'background 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
      }}
      onMouseEnter={event => {
        if (!active) event.currentTarget.style.background = 'var(--bg-hover)'
      }}
      onMouseLeave={event => {
        if (!active) event.currentTarget.style.background = 'transparent'
      }}
    >
      {!chip && (
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: compact ? 24 : 32,
            height: compact ? 24 : 32,
            borderRadius: 6,
            background: active
              ? 'var(--bg-card)'
              : 'color-mix(in oklab, var(--bg-soft) 78%, transparent)',
            flexShrink: 0,
          }}
        >
          <EcosystemIcon type={language.iconAdapter} size={compact ? 14 : 18} useColor />
        </span>
      )}
      <span style={{ minWidth: 0, flex: 1 }}>
        <span
          style={{
            display: 'block',
            overflow: 'hidden',
            color: active ? 'var(--brand-text)' : 'var(--text)',
            fontSize: chip ? 12 : compact ? 12.5 : 14,
            fontWeight: active ? 640 : 540,
            letterSpacing: compact ? '-0.01em' : undefined,
            lineHeight: 1.25,
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {language.name}
        </span>
        {!compact && !chip && (
          <span
            style={{
              display: 'block',
              overflow: 'hidden',
              marginTop: 2,
              color: 'var(--text-muted)',
              fontSize: 12,
              lineHeight: 1.25,
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {subtitle}
          </span>
        )}
      </span>
    </button>
  )
}

export default function EcosystemCatalog({ selected, recent, onSelect }: Props) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')

  const languagesById = useMemo(
    () => new Map(LANGUAGES.map(language => [language.id, language])),
    [],
  )
  const recentLanguages = recent
    .map(id => languagesById.get(id))
    .filter((language): language is Language => Boolean(language))

  const normalizedQuery = query.trim().toLocaleLowerCase()
  const matchingLanguages = normalizedQuery
    ? LANGUAGES.filter(language => {
        const subtitle = t(ecoLabelKey(language.subtitleKey))
        return [
          language.id,
          language.name,
          subtitle,
          ...language.managers.flatMap(manager => [manager.id, manager.name]),
        ]
          .join(' ')
          .toLocaleLowerCase()
          .includes(normalizedQuery)
      })
    : []

  function grouped(languages: Language[]): Record<LanguageGroup, Language[]> {
    const groups: Record<LanguageGroup, Language[]> = {
      os: [],
      lang: [],
      data: [],
      infra: [],
    }
    for (const language of languages) groups[language.group].push(language)
    return groups
  }

  function renderGroupedLanguages(languages: Language[]) {
    const groups = grouped(languages)
    return GROUP_ORDER.map(group => {
      const items = groups[group]
      if (items.length === 0) return null

      return (
        <section key={group} aria-labelledby={`ecosystem-group-${group}`}>
          <h4
            id={`ecosystem-group-${group}`}
            className="m-0 mb-1 text-[12px] font-[620] text-[var(--text-muted)]"
          >
            {t(groupLabelKey(group))}
          </h4>
          <ul className="m-0 grid list-none grid-cols-1 gap-1 p-0 min-[360px]:grid-cols-2">
            {items.map(language => (
              <li key={language.id}>
                <EcosystemButton
                  language={language}
                  selected={selected}
                  subtitle={t(ecoLabelKey(language.subtitleKey))}
                  onSelect={onSelect}
                  compact
                />
              </li>
            ))}
          </ul>
        </section>
      )
    })
  }

  return (
    <nav
      aria-label={t('quickstart.pickEcosystem')}
      className="eco-catalog border-b border-[var(--border)] min-[900px]:border-r min-[900px]:border-b-0"
      style={{
        minWidth: 0,
        padding: '18px 14px 20px',
        background: 'var(--bg-card)',
      }}
    >
      <h3 className="m-0 text-[16px] font-[650] leading-[1.3] text-[var(--text)]">
        {t('quickstart.pickEcosystem')}
      </h3>

      <div className="relative mt-3" role="search">
        <Icon
          name="search"
          size="sm"
          className="pointer-events-none absolute left-3 top-1/2 z-10 -translate-y-1/2 text-[var(--text-muted)]"
        />
        <Input
          type="search"
          value={query}
          onChange={event => setQuery(event.target.value)}
          onKeyDown={event => {
            if (event.key === 'Escape') {
              setQuery('')
              event.currentTarget.blur()
            }
          }}
          aria-label={t('quickstart.searchEcosystems')}
          placeholder={t('quickstart.searchEcosystemPlaceholder')}
          autoComplete="off"
          className="pl-9"
          style={{ background: 'var(--bg-soft)' }}
        />
      </div>

      {normalizedQuery ? (
        <div className="mt-4 flex flex-col gap-4">
          <span className="sr-only" aria-live="polite">
            {t('quickstart.searchResultCount', {
              count: matchingLanguages.length,
            })}
          </span>
          {matchingLanguages.length > 0 ? (
            renderGroupedLanguages(matchingLanguages)
          ) : (
            <p className="m-0 py-4 text-[13px] leading-[1.5] text-[var(--text-muted)]">
              {t('quickstart.noEcosystemMatch', { query: query.trim() })}
            </p>
          )}
        </div>
      ) : (
        <>
          {recentLanguages.length > 0 && (
            <section className="mt-4" aria-labelledby="recent-ecosystems-title">
              <h4
                id="recent-ecosystems-title"
                className="m-0 mb-1.5 text-[12px] font-[620] text-[var(--text-muted)]"
              >
                {t('quickstart.recentEcosystems')}
              </h4>
              <div className="grid grid-cols-3 gap-1">
                {recentLanguages.map(language => (
                  <EcosystemButton
                    key={language.id}
                    language={language}
                    selected={selected}
                    subtitle={t(ecoLabelKey(language.subtitleKey))}
                    onSelect={onSelect}
                    compact
                    chip
                  />
                ))}
              </div>
            </section>
          )}

          <section
            className="mt-4 border-t border-[var(--border)] pt-3"
            aria-labelledby="all-ecosystems-title"
          >
            <h4
              id="all-ecosystems-title"
              className="m-0 mb-2 text-[12px] font-[620] text-[var(--text-muted)]"
            >
              {t('quickstart.allEcosystems')}
            </h4>
            <div className="flex flex-col gap-3">
              {renderGroupedLanguages(LANGUAGES)}
            </div>
          </section>
        </>
      )}
    </nav>
  )
}
