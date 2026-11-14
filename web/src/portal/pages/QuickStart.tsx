import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import AIIntegrationPane from '@/portal/components/AIIntegration'
import CompileCachePane from '@/portal/components/CompileCachePane'
import ConfigurePane from '@/portal/components/ConfigurePane'
import EcosystemCatalog, { type CatalogEntry } from '@/portal/components/EcosystemCatalog'
import StatusBand from '@/portal/components/StatusBand'
import { LANGUAGES } from '@/lib/ecosystemData'
import { resolveServiceOrigin } from '@/lib/packageManagerConfig'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'

/** The catalog entry a visitor last opened; a first visit opens AI setup. */
const SELECTION_KEY = 'depsilo.portal.quickstart-selection.v1'
const AI_ENTRY = 'ai'
const COMPILE_CACHE_ENTRY = 'compile-cache'

interface Props {
  pytorchIndexPath?: string
}

function readSelection(): string {
  const stored = readLocalStorage(SELECTION_KEY)
  const known = stored === AI_ENTRY || stored === COMPILE_CACHE_ENTRY || LANGUAGES.some(language => language.id === stored)
  return stored && known ? stored : AI_ENTRY
}

export default function QuickStart({ pytorchIndexPath }: Props) {
  const { t } = useTranslation()
  const endpoint = resolveServiceOrigin()
  const [selected, setSelected] = useState(readSelection)

  const leading: CatalogEntry[] = [
    { id: AI_ENTRY, name: t('quickstart.catalog.ai'), subtitle: t('quickstart.catalog.aiSubtitle'), icon: 'lightbulb' },
  ]
  const optional: CatalogEntry[] = [
    { id: COMPILE_CACHE_ENTRY, name: t('quickstart.catalog.compileCache'), subtitle: 'ccache · sccache', icon: 'memory' },
  ]

  function select(id: string) {
    setSelected(id)
    writeLocalStorage(SELECTION_KEY, id)
  }

  return (
    // No page-local max-width: the Portal shell already caps the content
    // width, and a second cap here is what pulled this page's left edge out
    // of line with the sticky header on wide displays.
    <div className="flex w-full flex-col gap-6">
      <StatusBand
        onOpenEcosystem={id => {
          select(id)
          document.getElementById('quickstart-configure')?.scrollIntoView({ block: 'start' })
        }}
      />

      <header className="max-w-[760px]">
        <h1
          id="quickstart-title"
          className="m-0 font-display text-[clamp(30px,3vw,38px)] font-[600] leading-[1.08] text-[var(--text)]"
        >
          {t('quickstart.title')}
        </h1>
        <p className="mt-2 max-w-[68ch] text-[14px] leading-[1.5] text-[var(--text-muted)]">
          {t('quickstart.flowIntro')}
        </p>
      </header>

      <section
        id="quickstart-configure"
        data-quickstart-primary
        aria-labelledby="quickstart-config-title"
        className="scroll-mt-20"
      >
        <h2 id="quickstart-config-title" className="sr-only">
          {t('quickstart.primaryTitle')}
        </h2>
        <div
          data-quickstart-shell
          className="grid grid-cols-1 overflow-hidden rounded-xl min-[900px]:grid-cols-[280px_minmax(0,1fr)]"
          style={{
            background: 'var(--bg-card)',
            border: '1px solid var(--border-strong)',
          }}
        >
          <EcosystemCatalog selected={selected} onSelect={select} leading={leading} optional={optional} />
          {selected === AI_ENTRY ? (
            <AIIntegrationPane />
          ) : selected === COMPILE_CACHE_ENTRY ? (
            <CompileCachePane />
          ) : (
            <ConfigurePane
              key={selected}
              languageId={selected}
              endpoint={endpoint}
              pytorchIndexPath={pytorchIndexPath}
              flush
            />
          )}
        </div>
      </section>
    </div>
  )
}
