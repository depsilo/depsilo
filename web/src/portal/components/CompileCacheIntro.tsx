import { ArrowRight, ChevronDown, CloudSync, Cpu, HardDrive, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export default function CompileCacheIntro() {
  const { t } = useTranslation()

  return (
    <article
      aria-labelledby="portal-compile-cache-title"
      className="flex min-h-full flex-col rounded-lg border border-input bg-card p-5 shadow-card sm:p-6"
    >
      <div className="flex items-start gap-3">
        <span
          aria-hidden="true"
          className="flex size-10 shrink-0 items-center justify-center rounded-sm bg-accent text-primary"
        >
          <Cpu className="icon" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h3
            id="portal-compile-cache-title"
            className="m-0 font-sans text-subhead font-semibold leading-[1.25] text-foreground"
          >
            {t('quickstart.compileCacheTitle')}
          </h3>
          <p className="mt-2 max-w-[62ch] text-body leading-[1.55] text-muted-foreground">
            {t('quickstart.compileCacheSummary')}
          </p>
        </div>
      </div>

      <div className="mt-auto pt-5">
        <a
          href="/admin/compile-cache"
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-sm border border-border bg-transparent px-3 text-body font-semibold text-primary no-underline transition-[background,transform] duration-150 hover:bg-accent active:scale-[0.97]"
        >
          {t('quickstart.compileCacheAction')}
          <ArrowRight className="icon icon-sm" aria-hidden="true" />
        </a>

        <details className="mt-3 border-t border-border pt-2">
          <summary className="flex min-h-10 cursor-pointer list-none items-center justify-between gap-3 rounded-sm px-1 text-body font-medium text-muted-foreground hover:text-foreground">
            {t('quickstart.compileCacheDetails')}
            <ChevronDown className="icon icon-sm" aria-hidden="true" />
          </summary>
          <ul className="mb-1 mt-2 flex list-none flex-col gap-2 p-0 text-label leading-[1.5] text-muted-foreground">
            <li className="flex items-start gap-2">
              <CloudSync className="mt-0.5 shrink-0 text-primary icon icon-sm" aria-hidden="true" />
              <span>{t('quickstart.compileCacheProtocol')}</span>
            </li>
            <li className="flex items-start gap-2">
              <HardDrive className="mt-0.5 shrink-0 text-primary icon icon-sm" aria-hidden="true" />
              <span>{t('quickstart.compileCacheStorage')}</span>
            </li>
            <li className="flex items-start gap-2">
              <Info className="mt-0.5 shrink-0 text-warning icon icon-sm" aria-hidden="true" />
              <span>{t('quickstart.compileCacheLimitation')}</span>
            </li>
          </ul>
        </details>
      </div>
    </article>
  )
}
