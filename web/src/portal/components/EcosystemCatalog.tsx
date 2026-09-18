import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import EcosystemIcon from '@/components/app/ecosystem-icon'
import Input from '@/components/app/input'
import { LANGUAGES, type Language, type LanguageGroup } from '@/lib/ecosystemData'
import { cn } from '@/lib/utils'

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
      className={cn(
        'relative flex w-full cursor-pointer items-center overflow-hidden rounded-sm border text-left transition-[background,border-color,transform] duration-150 active:scale-[0.98]',
        chip
          ? 'min-h-10 gap-1.5 px-2.5 py-1.5'
          : compact
            ? 'min-h-10 gap-1.5 px-1 py-1'
            : 'min-h-13 gap-3 px-2.5 py-2',
        active
          ? 'border-border bg-accent shadow-[inset_1px_0_0_var(--primary)]'
          : 'border-transparent bg-transparent hover:bg-accent',
      )}
    >
      {!chip && (
        <span
          aria-hidden="true"
          className={cn(
            'inline-flex shrink-0 items-center justify-center rounded-sm',
            compact ? 'size-6' : 'size-8',
            active ? 'bg-card' : 'bg-muted/80',
          )}
        >
          <EcosystemIcon type={language.iconAdapter} size={compact ? 14 : 18} useColor />
        </span>
      )}
      <span className="min-w-0 flex-1">
        <span
          className={cn(
            'block truncate leading-tight',
            chip || compact ? 'text-label' : 'text-body',
            active ? 'font-semibold text-primary' : 'font-medium text-foreground',
          )}
        >
          {language.name}
        </span>
        {!compact && !chip && (
          <span className="mt-0.5 block truncate text-label leading-tight text-muted-foreground">
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
            className="m-0 mb-1 text-label font-semibold text-muted-foreground"
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
      className="min-w-0 border-b border-border bg-card px-3.5 pb-5 pt-4.5 min-[900px]:border-b-0 min-[900px]:border-r"
    >
      <h3 className="m-0 text-field font-semibold leading-[1.3] text-foreground">
        {t('quickstart.pickEcosystem')}
      </h3>

      <div className="relative mt-3" role="search">
        <Search className="pointer-events-none absolute left-3 top-1/2 z-10 -translate-y-1/2 text-muted-foreground icon icon-sm" aria-hidden="true" />
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
          className="pl-9 bg-muted"
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
            <p className="m-0 py-4 text-body leading-[1.5] text-muted-foreground">
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
                className="m-0 mb-1.5 text-label font-semibold text-muted-foreground"
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
            className="mt-4 border-t border-border pt-3"
            aria-labelledby="all-ecosystems-title"
          >
            <h4
              id="all-ecosystems-title"
              className="m-0 mb-2 text-label font-semibold text-muted-foreground"
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
