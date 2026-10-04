import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import Input from '@/components/Input'
import { useMediaQuery } from '@/hooks/useMediaQuery'
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
  /**
   * Phone rail: intrinsic width so a whole list can scroll sideways in one
   * row. The rail sits on a tinted surface, so its hover and selected states
   * use the card colour instead of the page accent, which would be invisible
   * against a tint of the same value.
   */
  rail?: boolean
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
  rail = false,
}: EcosystemButtonProps) {
  const active = language.id === selected
  // On the tinted phone rail the raised state has to be the card colour;
  // `--accent` is a tint of the same value and would disappear into it.
  const raisedSurface = rail ? 'var(--bg-card)' : 'var(--accent)'

  return (
    <button
      type="button"
      aria-pressed={active}
      data-active={active ? 'true' : undefined}
      title={language.name}
      onClick={() => onSelect(language.id)}
      className="stripe-focus-ring active:scale-[0.98]"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: rail ? 7 : compact ? 6 : 11,
        width: rail ? 'auto' : '100%',
        flexShrink: rail ? 0 : undefined,
        minHeight: chip || rail ? 40 : compact ? 40 : 52,
        padding: rail ? '6px 12px 6px 7px' : chip ? '6px 10px' : compact ? '5px 4px' : '8px 10px',
        // shadcn selection: the accent surface carries state, not a keyline.
        background: active ? raisedSurface : 'transparent',
        border: `1px solid ${active ? 'var(--border)' : 'transparent'}`,
        borderRadius: rail ? 8 : chip || compact ? 6 : 8,
        textAlign: 'left',
        cursor: 'pointer',
        transition:
          'background 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
      }}
      onMouseEnter={event => {
        if (!active) event.currentTarget.style.background = raisedSurface
      }}
      onMouseLeave={event => {
        if (!active) event.currentTarget.style.background = 'transparent'
      }}
    >
      {(rail || !chip) && (
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: rail ? 22 : compact ? 24 : 32,
            height: rail ? 22 : compact ? 24 : 32,
            borderRadius: rail ? 5 : 6,
            background: rail
              ? active ? 'var(--accent)' : 'var(--bg-card)'
              : active
                ? 'var(--bg-card)'
                : 'color-mix(in oklab, var(--bg-soft) 78%, transparent)',
            flexShrink: 0,
          }}
        >
          <EcosystemIcon type={language.iconAdapter} size={rail ? 13 : compact ? 14 : 18} useColor />
        </span>
      )}
      <span style={{ minWidth: 0, flex: rail ? '0 0 auto' : 1 }}>
        <span
          style={{
            display: 'block',
            overflow: 'hidden',
            color: active ? 'var(--brand-text)' : 'var(--text)',
            fontSize: rail ? 13 : chip ? 12 : compact ? 12.5 : 14,
            fontWeight: active ? 640 : 540,
            letterSpacing: compact ? '-0.01em' : undefined,
            lineHeight: 1.25,
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {language.name}
        </span>
        {/* The phone rail is icon + name only: a second line per chip costs
            more height than the extra words are worth there. */}
        {!rail && !compact && !chip && (
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
  // Phones get one scrollable row instead of the full grouped directory:
  // stacked, the 14 ecosystems pushed the actual configuration more than a
  // screen down the page.
  const railLayout = useMediaQuery('(max-width: 899px)')

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

  function renderRail(languages: Language[]) {
    return (
      <div
        data-eco-rail
        className="eco-rail -mx-1.5 flex gap-1.5 overflow-x-auto px-1.5 py-1"
      >
        {languages.map(language => (
          <EcosystemButton
            key={language.id}
            language={language}
            selected={selected}
            subtitle={t(ecoLabelKey(language.subtitleKey))}
            onSelect={onSelect}
            rail
          />
        ))}
      </div>
    )
  }

  return (
    <nav
      aria-label={t('quickstart.pickEcosystem')}
      className="eco-catalog flex flex-col border-b border-[var(--border)] min-[900px]:border-r min-[900px]:border-b-0"
      style={{
        minWidth: 0,
        padding: '18px 14px 20px',
        // A rail, not a card: the tint keeps the column reading as a
        // directory when the configuration pane next to it is taller.
        background: 'var(--bg-soft)',
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
          style={{ background: 'var(--bg-card)' }}
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
              {railLayout
                ? renderRail(recentLanguages)
                : (
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
                )}
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
            {railLayout
              ? renderRail(LANGUAGES)
              : (
                <div className="flex flex-col gap-3">
                  {renderGroupedLanguages(LANGUAGES)}
                </div>
              )}
          </section>
        </>
      )}

      {!railLayout && (
        <div className="mt-auto pt-6">
          <Link
            to="/monitor"
            className="stripe-focus-ring -ml-2 inline-flex min-h-10 items-center gap-1.5 rounded-md px-2 text-[13px] font-[550] no-underline hover:bg-[var(--bg-card)]"
            style={{ color: 'var(--text-muted)' }}
          >
            {t('quickstart.viewUpstreamHealth')}
            <Icon name="arrow_forward" size="sm" />
          </Link>
        </div>
      )}
    </nav>
  )
}
