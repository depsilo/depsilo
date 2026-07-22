import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'

// CompileCacheIntro keeps the public explanation deliberately separate from
// the package-manager console: compiler artifacts have their own storage,
// capacity accounting, and credentials in Depsilo.
export default function CompileCacheIntro() {
  const { t } = useTranslation()

  return (
    <section
      className="compile-cache-intro"
      aria-labelledby="portal-compile-cache-title"
      style={{
        display: 'grid',
        gridTemplateColumns: 'minmax(0, 1fr) minmax(280px, 0.38fr)',
        alignItems: 'center',
        gap: 28,
        padding: '22px 24px',
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border-strong)',
        borderRadius: 16,
        boxShadow: 'var(--shadow-card)',
      }}
    >
      <div className="compile-cache-intro-copy" style={{ display: 'flex', alignItems: 'flex-start', gap: 16, minWidth: 0 }}>
        <span
          aria-hidden="true"
          style={{
            width: 44,
            height: 44,
            flex: '0 0 44px',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--brand-text)',
            background: 'var(--brand-soft)',
            border: '0.5px solid var(--brand-border)',
            borderRadius: 12,
          }}
        >
          <Icon name="memory" />
        </span>

        <div style={{ minWidth: 0 }}>
          <div
            className="eyebrow"
            style={{ color: 'var(--brand-text)', fontSize: 11, marginBottom: 6 }}
          >
            {t('quickstart.compileCacheEyebrow')}
          </div>
          <h2
            id="portal-compile-cache-title"
            style={{
              margin: 0,
              color: 'var(--text)',
              fontFamily: 'var(--font-display)',
              fontSize: 'clamp(21px, 1.8vw, 28px)',
              fontWeight: 650,
              letterSpacing: '-0.025em',
              lineHeight: 1.18,
            }}
          >
            {t('quickstart.compileCacheTitle')}
          </h2>
          <p
            style={{
              maxWidth: 900,
              margin: '8px 0 0',
              color: 'var(--text-muted)',
              fontSize: 13,
              lineHeight: 1.55,
            }}
          >
            {t('quickstart.compileCacheDescription')}
          </p>
          <div className="compile-cache-intro-facts" style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 14px', marginTop: 12 }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--text-soft)', fontSize: 11.5 }}>
              <Icon name="cloud_sync" size="sm" />
              {t('quickstart.compileCacheProtocol')}
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--text-soft)', fontSize: 11.5 }}>
              <Icon name="storage" size="sm" />
              {t('quickstart.compileCacheStorage')}
            </span>
          </div>
        </div>
      </div>

      <div className="compile-cache-intro-action" style={{ minWidth: 0 }}>
        <a
          href="/admin/compile-cache"
          className="stripe-focus-ring bg-[var(--btn)] hover:bg-[var(--btn-press)] active:scale-[0.97]"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 7,
            minHeight: 40,
            padding: '8px 13px',
            color: 'var(--btn-fg)',
            borderRadius: 7,
            boxShadow: 'inset 0 1px 0 color-mix(in oklab, white 16%, transparent), 0 1px 2px rgba(0, 0, 0, 0.18)',
            textDecoration: 'none',
            fontSize: 12.5,
            fontWeight: 600,
            transition: 'background 150ms ease, transform 150ms ease',
          }}
        >
          {t('quickstart.compileCacheAction')}
          <Icon name="arrow_forward" size="sm" />
        </a>
        <p
          style={{
            display: 'flex',
            alignItems: 'flex-start',
            gap: 7,
            margin: '10px 0 0',
            color: 'var(--text-subtle)',
            fontSize: 11,
            lineHeight: 1.45,
          }}
        >
          <Icon name="info" size="sm" style={{ flex: '0 0 auto', marginTop: 1 }} />
          <span>{t('quickstart.compileCacheLimitation')}</span>
        </p>
      </div>
    </section>
  )
}
