// HeroAICTA — the QuickStart's hero zone.
//
// Headlines the project-level AI integration workflow ("paste a prompt
// into Claude / Cursor / Copilot Chat, it rewrites Dockerfile / CI /
// build scripts"). Sits above the ecosystem picker because the prompt
// is brand-neutral and language-agnostic — it works for any project
// regardless of which specific ecosystem the operator is wiring next.
//
// Visual: large title + supporting paragraph + primary CTA. Background
// uses the existing brand-aurora gradient over a soft surface so the
// hero reads as elevated without becoming a banner ad.
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
          padding: '32px 36px',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 14,
          isolation: 'isolate',
        }}
      >
        {/* Aurora blob — sits absolutely behind content, very low alpha so
            the eye lands on the text first. The conic-style gradient
            already exists as --grad-aurora; reuse it instead of inventing
            yet another decoration. */}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: -80,
            background: 'var(--grad-aurora)',
            opacity: 0.18,
            filter: 'blur(48px)',
            zIndex: -1,
            pointerEvents: 'none',
          }}
        />
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 18,
            maxWidth: 760,
          }}
        >
          <span
            className="eyebrow"
            style={{ color: 'var(--brand-text)' }}
          >
            <span aria-hidden style={{ marginRight: 6 }}>✦</span>
            {t('quickstart.heroEyebrow')}
          </span>
          <h2
            style={{
              margin: 0,
              fontSize: 'clamp(28px, 4vw, 40px)',
              fontWeight: 700,
              letterSpacing: '-0.03em',
              lineHeight: 1.05,
              color: 'var(--text)',
            }}
          >
            {t('quickstart.heroTitle')}
          </h2>
          <p
            style={{
              margin: 0,
              fontSize: 15,
              lineHeight: 1.55,
              color: 'var(--text-muted)',
              maxWidth: 620,
            }}
          >
            {t('quickstart.heroDesc')}
          </p>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 10, marginTop: 6 }}>
            <button
              type="button"
              onClick={() => setOpen(true)}
              className="active:scale-[0.96]"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 8,
                padding: '10px 18px',
                background: 'var(--brand)',
                color: 'white',
                border: 'none',
                borderRadius: 8,
                fontSize: 13.5,
                fontWeight: 600,
                cursor: 'pointer',
                boxShadow: '0 1px 0 color-mix(in oklab, var(--brand) 60%, black), 0 8px 24px color-mix(in oklab, var(--brand) 32%, transparent)',
                transition: 'transform 120ms cubic-bezier(0.2, 0, 0, 1), filter 120ms ease',
              }}
              onMouseEnter={e => { e.currentTarget.style.filter = 'brightness(1.06)' }}
              onMouseLeave={e => { e.currentTarget.style.filter = 'brightness(1)' }}
            >
              <span aria-hidden>✦</span>
              {t('quickstart.heroCta')}
              <span aria-hidden style={{ opacity: 0.7 }}>→</span>
            </button>
            <span style={{ fontSize: 12, color: 'var(--text-subtle)' }}>
              {t('quickstart.heroCtaHint')}
            </span>
          </div>
        </div>
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
