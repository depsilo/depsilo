// HeroAICTA — compact horizontal strip at the top of QuickStart.
//
// Replaces the original 32-padding 5-line marketing card. The page below
// (catalog + config pane) is the real surface; this strip's job is just
// to keep the project-level AI integration workflow visible and one
// click away, not to dominate the fold.
//
// Layout: ~110px tall. Vertical brand bar on the left edge. Eyebrow +
// title + supporting copy in the middle. Primary CTA on the right.
// Subtle aurora wash underneath so it reads as elevated without becoming
// a banner ad.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import ModalV2 from '@/components/Modal'
import CopyButton from '@/portal/components/CopyButton'

export default function HeroAICTA() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const { data: prompt, isLoading, error } = useQuery<string>({
    queryKey: ['integration-prompt'],
    queryFn: async () => {
      const res = await fetch('/api/v1/integration-prompt')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.text()
    },
    enabled: open,
    staleTime: 5 * 60 * 1000,
  })

  return (
    <>
      <section
        style={{
          position: 'relative',
          overflow: 'hidden',
          isolation: 'isolate',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 12,
          display: 'flex',
          alignItems: 'center',
          gap: 24,
          padding: '18px 22px 18px 26px',
          minHeight: 96,
        }}
      >
        {/* Brand-aurora wash — saturated enough to feel premium but
            confined to the strip; no longer a giant blur. */}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            background:
              'linear-gradient(90deg, color-mix(in oklab, var(--brand) 14%, transparent) 0%, color-mix(in oklab, var(--brand) 4%, transparent) 38%, transparent 70%)',
            pointerEvents: 'none',
            zIndex: -1,
          }}
        />
        {/* Left-edge brand bar — load-bearing visual anchor. */}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            left: 0,
            top: 0,
            bottom: 0,
            width: 4,
            background: 'var(--grad-brand)',
          }}
        />

        {/* Text block */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            className="eyebrow"
            style={{
              color: 'var(--brand-text)',
              marginBottom: 6,
              fontSize: 10,
            }}
          >
            <span aria-hidden style={{ marginRight: 5 }}>✦</span>
            {t('quickstart.heroEyebrow')}
          </div>
          <h2
            style={{
              margin: 0,
              fontSize: 22,
              fontWeight: 700,
              letterSpacing: '-0.02em',
              lineHeight: 1.15,
              color: 'var(--text)',
            }}
          >
            {t('quickstart.heroTitle')}
          </h2>
          <p
            style={{
              margin: '4px 0 0 0',
              fontSize: 12.5,
              lineHeight: 1.45,
              color: 'var(--text-muted)',
              maxWidth: 700,
            }}
          >
            {t('quickstart.heroDescShort')}
          </p>
        </div>

        {/* Primary CTA */}
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="active:scale-[0.96]"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            padding: '10px 16px',
            background: 'var(--brand)',
            color: 'white',
            border: 'none',
            borderRadius: 8,
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
            flexShrink: 0,
            boxShadow:
              'inset 0 1px 0 color-mix(in oklab, white 22%, transparent), 0 1px 0 color-mix(in oklab, var(--brand) 80%, black), 0 6px 18px color-mix(in oklab, var(--brand) 28%, transparent)',
            transition: 'transform 120ms cubic-bezier(0.2, 0, 0, 1), filter 120ms ease',
          }}
          onMouseEnter={e => { e.currentTarget.style.filter = 'brightness(1.07)' }}
          onMouseLeave={e => { e.currentTarget.style.filter = 'brightness(1)' }}
        >
          <span aria-hidden>✦</span>
          {t('quickstart.heroCta')}
          <span aria-hidden style={{ opacity: 0.7 }}>→</span>
        </button>
      </section>

      <ModalV2
        open={open}
        onClose={() => setOpen(false)}
        title={t('quickstart.aiIntegrationTitle')}
        width={720}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.55 }}>
            {t('quickstart.aiIntegrationDesc')}
          </p>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '6px 10px',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 6,
            }}
          >
            <span style={{ fontSize: 11, color: 'var(--text-muted)', flex: 1, minWidth: 0 }}>
              {t('quickstart.aiIntegrationHowto')}
            </span>
            {prompt && <CopyButton text={prompt} />}
          </div>
          <pre
            style={{
              margin: 0,
              padding: 14,
              fontFamily: 'var(--font-mono)',
              fontSize: 12,
              lineHeight: 1.55,
              color: 'var(--text)',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 6,
              whiteSpace: 'pre',
              maxHeight: '60vh',
              overflow: 'auto',
              minHeight: 120,
            }}
          >
            {isLoading && <span style={{ color: 'var(--text-muted)' }}>{t('quickstart.loading')}</span>}
            {error && <span style={{ color: 'var(--danger-text)' }}>{t('quickstart.aiIntegrationError')}</span>}
            {prompt && <code style={{ display: 'block' }}>{prompt}</code>}
          </pre>
        </div>
      </ModalV2>
    </>
  )
}
