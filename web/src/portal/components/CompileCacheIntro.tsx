import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'

export default function CompileCacheIntro() {
  const { t } = useTranslation()

  return (
    <article
      aria-labelledby="portal-compile-cache-title"
      className="flex min-h-full flex-col rounded-[var(--r-card)] p-5 sm:p-6"
      style={{
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border-strong)',
        boxShadow: 'var(--shadow-card)',
      }}
    >
      <div className="flex items-start gap-3">
        <span
          aria-hidden="true"
          className="flex size-10 shrink-0 items-center justify-center rounded-[9px] bg-[var(--brand-soft)] text-[var(--brand-text)]"
        >
          <Icon name="memory" />
        </span>
        <div className="min-w-0">
          <h3
            id="portal-compile-cache-title"
            className="m-0 font-[var(--font-display)] text-[18px] font-[650] leading-[1.25] text-[var(--text)]"
          >
            {t('quickstart.compileCacheTitle')}
          </h3>
          <p className="mt-2 max-w-[62ch] text-[13px] leading-[1.55] text-[var(--text-muted)]">
            {t('quickstart.compileCacheSummary')}
          </p>
        </div>
      </div>

      <div className="mt-auto pt-5">
        <a
          href="/admin/compile-cache"
          className="stripe-focus-ring inline-flex min-h-10 items-center justify-center gap-2 rounded-[7px] px-3 text-[13px] font-[620] no-underline hover:bg-[var(--bg-hover)] active:scale-[0.97]"
          style={{
            color: 'var(--brand-text)',
            background: 'transparent',
            border: '1px solid var(--brand-border)',
            transition:
              'background 150ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
          }}
        >
          {t('quickstart.compileCacheAction')}
          <Icon name="arrow_forward" size="sm" />
        </a>

        <details className="mt-3 border-t border-[var(--border)] pt-2">
          <summary className="stripe-focus-ring flex min-h-10 cursor-pointer list-none items-center justify-between gap-3 rounded-[6px] px-1 text-[13px] font-[560] text-[var(--text-muted)] hover:text-[var(--text)]">
            {t('quickstart.compileCacheDetails')}
            <Icon name="expand_more" size="sm" />
          </summary>
          <ul className="mb-1 mt-2 flex list-none flex-col gap-2 p-0 text-[12px] leading-[1.5] text-[var(--text-muted)]">
            <li className="flex items-start gap-2">
              <Icon
                name="cloud_sync"
                size="sm"
                className="mt-0.5 shrink-0 text-[var(--brand-text)]"
              />
              <span>{t('quickstart.compileCacheProtocol')}</span>
            </li>
            <li className="flex items-start gap-2">
              <Icon
                name="storage"
                size="sm"
                className="mt-0.5 shrink-0 text-[var(--brand-text)]"
              />
              <span>{t('quickstart.compileCacheStorage')}</span>
            </li>
            <li className="flex items-start gap-2">
              <Icon
                name="info"
                size="sm"
                className="mt-0.5 shrink-0 text-[var(--warn-text)]"
              />
              <span>{t('quickstart.compileCacheLimitation')}</span>
            </li>
          </ul>
        </details>
      </div>
    </article>
  )
}
