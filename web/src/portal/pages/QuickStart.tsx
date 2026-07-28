import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import ConfigurePane from '@/portal/components/ConfigurePane'
import CompileCacheIntro from '@/portal/components/CompileCacheIntro'
import EcosystemCatalog from '@/portal/components/EcosystemCatalog'
import HeroAICTA from '@/portal/components/HeroAICTA'
import { LANGUAGES } from '@/lib/ecosystemData'

const RECENT_ECOSYSTEMS_KEY = 'depsilo.portal.recent-ecosystems.v1'
const MAX_RECENT_ECOSYSTEMS = 3

function readRecentEcosystems(): string[] {
  try {
    const stored = window.localStorage.getItem(RECENT_ECOSYSTEMS_KEY)
    if (!stored) return []

    const parsed: unknown = JSON.parse(stored)
    if (!Array.isArray(parsed)) return []

    const supported = new Set(LANGUAGES.map(language => language.id))
    return Array.from(
      new Set(
        parsed.filter(
          (id): id is string => typeof id === 'string' && supported.has(id),
        ),
      ),
    ).slice(0, MAX_RECENT_ECOSYSTEMS)
  } catch {
    return []
  }
}

function writeRecentEcosystems(ids: string[]) {
  try {
    window.localStorage.setItem(RECENT_ECOSYSTEMS_KEY, JSON.stringify(ids))
  } catch {
    // Storage may be unavailable in privacy-restricted browsers. Selection
    // still works for the current visit, so persistence remains optional.
  }
}

export default function QuickStart() {
  const { t } = useTranslation()
  const endpoint = window.location.origin
  const [recentEcosystems, setRecentEcosystems] = useState(readRecentEcosystems)
  const [language, setLanguage] = useState(() => recentEcosystems[0] ?? 'python')

  useEffect(() => {
    writeRecentEcosystems(recentEcosystems)
  }, [recentEcosystems])

  function selectLanguage(id: string) {
    setLanguage(id)
    setRecentEcosystems(previous =>
      [id, ...previous.filter(item => item !== id)].slice(
        0,
        MAX_RECENT_ECOSYSTEMS,
      ),
    )
  }

  return (
    <div className="flex flex-col gap-8 lg:gap-10">
      <header className="fade-up max-w-[780px]">
        <h1
          className="m-0 font-[var(--font-display)] text-[clamp(34px,4vw,48px)] font-[690] leading-[1.04] text-[var(--text)]"
        >
          {t('quickstart.title')}
        </h1>
        <p className="mt-3 max-w-[68ch] text-[15px] leading-[1.6] text-[var(--text-muted)]">
          {t('quickstart.flowIntro')}
        </p>

        <ol
          aria-label={t('quickstart.flowLabel')}
          className="mt-6 grid list-none grid-cols-1 gap-2 p-0 sm:grid-cols-3 sm:gap-5"
        >
          {[
            t('quickstart.flowChoose'),
            t('quickstart.flowCopy'),
            t('quickstart.flowVerify'),
          ].map((label, index) => (
            <li
              key={label}
              className="flex min-h-12 items-center gap-3 border-t border-[var(--border-strong)] pt-3 text-[13px] font-[560] text-[var(--text)]"
            >
              <span
                aria-hidden="true"
                className="num flex size-6 shrink-0 items-center justify-center rounded-[4px] bg-[var(--bg-soft)] text-[12px] font-[650] text-[var(--brand-text)]"
              >
                {index + 1}
              </span>
              {index === 2 ? (
                <a
                  href="/monitor"
                  className="stripe-focus-ring rounded-[4px] text-[var(--text)] underline decoration-[var(--border-strong)] underline-offset-4 hover:text-[var(--brand-text)]"
                >
                  {label}
                </a>
              ) : (
                <span>{label}</span>
              )}
            </li>
          ))}
        </ol>

        <p className="mt-4 border-l-2 border-[var(--brand-border)] pl-3 text-[12px] leading-[1.55] text-[var(--text-muted)]">
          {t('quickstart.heroDescShort')}
        </p>
      </header>

      <section
        data-quickstart-primary
        aria-labelledby="quickstart-config-title"
        className="fade-up fade-up-d1"
      >
        <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
          <div>
            <h2
              id="quickstart-config-title"
              className="m-0 font-[var(--font-display)] text-[20px] font-[650] leading-[1.2] text-[var(--text)]"
            >
              {t('quickstart.primaryTitle')}
            </h2>
            <p className="mt-1 text-[13px] leading-[1.5] text-[var(--text-muted)]">
              {t('quickstart.primaryDescription')}
            </p>
          </div>
        </div>

        <div
          className="grid grid-cols-1 overflow-hidden rounded-[var(--r-shell)] min-[861px]:grid-cols-[minmax(280px,320px)_minmax(0,1fr)]"
          style={{
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border-strong)',
            boxShadow: 'var(--shadow-pop)',
          }}
        >
          <EcosystemCatalog
            selected={language}
            recent={recentEcosystems}
            onSelect={selectLanguage}
          />
          <ConfigurePane key={language} languageId={language} endpoint={endpoint} flush />
        </div>
      </section>

      <section
        data-quickstart-optional
        aria-labelledby="quickstart-optional-title"
        className="fade-up fade-up-d2 border-t border-[var(--border)] pt-8"
      >
        <div className="mb-4 max-w-[68ch]">
          <h2
            id="quickstart-optional-title"
            className="m-0 font-[var(--font-display)] text-[20px] font-[650] leading-[1.2] text-[var(--text)]"
          >
            {t('quickstart.optionalTitle')}
          </h2>
          <p className="mt-1 text-[13px] leading-[1.5] text-[var(--text-muted)]">
            {t('quickstart.optionalDescription')}
          </p>
        </div>

        <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
          <HeroAICTA />
          <CompileCacheIntro />
        </div>
      </section>
    </div>
  )
}
