import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import ConfigurePane from '@/portal/components/ConfigurePane'
import CompileCacheIntro from '@/portal/components/CompileCacheIntro'
import EcosystemCatalog from '@/portal/components/EcosystemCatalog'
import HeroAICTA from '@/portal/components/HeroAICTA'
import { LANGUAGES } from '@/lib/ecosystemData'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'

const RECENT_ECOSYSTEMS_KEY = 'depsilo.portal.recent-ecosystems.v1'
const MAX_RECENT_ECOSYSTEMS = 3

interface Props {
  pytorchIndexPath?: string
}

function readRecentEcosystems(): string[] {
  try {
    const stored = readLocalStorage(RECENT_ECOSYSTEMS_KEY)
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
  writeLocalStorage(RECENT_ECOSYSTEMS_KEY, JSON.stringify(ids))
}

export default function QuickStart({ pytorchIndexPath }: Props) {
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
    <div className="mx-auto flex w-full max-w-[1440px] flex-col gap-7">
      <header className="fade-up max-w-[760px]">
        <h1
          id="quickstart-title"
          className="m-0 font-[var(--font-display)] text-[clamp(30px,3vw,38px)] font-[680] leading-[1.08] text-[var(--text)]"
        >
          {t('quickstart.title')}
        </h1>
        <p className="mt-2 max-w-[68ch] text-[14px] leading-[1.5] text-[var(--text-muted)]">
          {t('quickstart.flowIntro')}
        </p>
      </header>

      <section
        data-quickstart-primary
        aria-labelledby="quickstart-config-title"
        className="fade-up fade-up-d1"
      >
        <h2 id="quickstart-config-title" className="sr-only">
          {t('quickstart.primaryTitle')}
        </h2>
        <div
          data-quickstart-shell
          className="grid grid-cols-1 overflow-hidden rounded-[var(--r-shell)] min-[900px]:grid-cols-[280px_minmax(0,1fr)]"
          style={{
            background: 'var(--bg-card)',
            border: '1px solid var(--border-strong)',
          }}
        >
          <EcosystemCatalog
            selected={language}
            recent={recentEcosystems}
            onSelect={selectLanguage}
          />
          <ConfigurePane
            key={language}
            languageId={language}
            endpoint={endpoint}
            pytorchIndexPath={pytorchIndexPath}
            flush
          />
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
