// HeroAICTA — compact horizontal strip at the top of QuickStart.
//
// Two columns side by side: a punchy text block on the left (eyebrow +
// title + one-line desc) and a real prompt preview on the right
// (5-line monospace excerpt + copy + view-full). The preview is the
// load-bearing element — it proves the prompt exists and lets the user
// copy directly without ever opening the modal.
//
// The modal still exists, but it's now a read-only viewer for users who
// want to inspect the full prompt before pasting. Copying is the
// primary action; the modal is secondary.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import ModalV2 from '@/components/Modal'
import CopyButton from '@/portal/components/CopyButton'
import { copyText } from '@/lib/clipboard'

const PREVIEW_LINES = 5

export default function HeroAICTA() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  // Eagerly fetch — the prompt is ~5 KB and we want to render the
  // preview block immediately on page load. Cached for 5 min so the
  // 24-hour-uptime portal doesn't re-fetch on every navigation.
  const { data: prompt, isLoading, error } = useQuery<string>({
    queryKey: ['integration-prompt'],
    queryFn: async () => {
      const res = await fetch('/api/v1/integration-prompt')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.text()
    },
    staleTime: 5 * 60 * 1000,
  })

  const previewText =
    prompt?.split('\n').slice(0, PREVIEW_LINES).join('\n') ??
    (isLoading ? t('quickstart.loading') : '')

  async function handleCopy() {
    if (!prompt) return
    if (await copyText(prompt)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  return (
    <>
      <section
        className="quickstart-hero"
        style={{
          position: 'relative',
          overflow: 'hidden',
          isolation: 'isolate',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 12,
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 0.45fr) minmax(0, 0.55fr)',
          alignItems: 'stretch',
          gap: 24,
          padding: '20px 22px 20px 26px',
        }}
      >
        {/* Brand-aurora wash — pulled back from "salmon cream wash" to
            a single soft warm glow rising from the lower-left corner.
            7% peak alpha drops to transparent before crossing the
            midline so the right half (where the preview card lives)
            stays neutral. The previous 14%-from-left-to-42% covered
            the entire strip and made the eye see "pinkish hero"; this
            reads as "warm light leaking up from one corner". */}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            background:
              'radial-gradient(540px 320px at 0% 110%, color-mix(in oklab, var(--brand) 9%, transparent) 0%, color-mix(in oklab, var(--brand) 3%, transparent) 50%, transparent 75%)',
            pointerEvents: 'none',
            zIndex: -1,
          }}
        />
        {/* Left-edge brand bar. */}
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

        {/* Column 1 — eyebrow + title + concise desc. */}
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'center',
            minWidth: 0,
          }}
        >
          <div
            className="eyebrow ai-chip"
            style={{
              color: 'var(--brand-text)',
              marginBottom: 8,
              fontSize: 10,
              alignSelf: 'flex-start',
              padding: '4px 10px',
              borderRadius: 999,
              background: 'color-mix(in oklab, var(--brand) 6%, var(--bg-card))',
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
              lineHeight: 1.18,
              color: 'var(--text)',
            }}
          >
            {t('quickstart.heroTitle')}
          </h2>
          <p
            style={{
              margin: '8px 0 0 0',
              fontSize: 12.5,
              lineHeight: 1.55,
              color: 'var(--text-muted)',
            }}
          >
            {t('quickstart.heroDescShort')}
          </p>
          <p
            style={{
              margin: '6px 0 0 0',
              fontSize: 11,
              lineHeight: 1.5,
              color: 'var(--text-subtle)',
            }}
          >
            {t('quickstart.heroSub')}
          </p>
        </div>

        {/* Column 2 — preview card with inline copy + view-full. */}
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            background: 'var(--bg-soft)',
            border: '0.5px solid var(--border)',
            borderRadius: 10,
            overflow: 'hidden',
            minWidth: 0,
          }}
        >
          {/* Preview header */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 8,
              padding: '6px 8px 6px 12px',
              borderBottom: '0.5px solid var(--border)',
              background: 'var(--bg-card)',
            }}
          >
            <span
              className="eyebrow"
              style={{ color: 'var(--text-subtle)', fontSize: 9, margin: 0 }}
            >
              {t('quickstart.heroPreviewLabel')}
            </span>
            <span
              style={{
                fontSize: 10,
                fontFamily: 'var(--font-mono)',
                color: 'var(--text-subtle)',
              }}
            >
              {t('quickstart.heroSizeHint')}
            </span>
          </div>
          {/* Preview body */}
          <pre
            style={{
              margin: 0,
              padding: 12,
              flex: 1,
              fontFamily: 'var(--font-mono)',
              fontSize: 11.5,
              lineHeight: 1.55,
              color: 'var(--text)',
              whiteSpace: 'pre',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              maxHeight: 110,
              position: 'relative',
            }}
          >
            <code style={{ display: 'block', opacity: prompt ? 1 : 0.5 }}>
              {error ? t('quickstart.aiIntegrationError') : previewText}
            </code>
            {/* Fade-out gradient at the bottom edge so the truncation
                reads as "more below" rather than "cut off". */}
            <span
              aria-hidden
              style={{
                position: 'absolute',
                left: 0,
                right: 0,
                bottom: 0,
                height: 28,
                background:
                  'linear-gradient(to bottom, color-mix(in oklab, var(--bg-soft) 0%, transparent), var(--bg-soft) 100%)',
                pointerEvents: 'none',
              }}
            />
          </pre>
          {/* Preview actions */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '8px 8px 8px 12px',
              borderTop: '0.5px solid var(--border)',
              background: 'var(--bg-card)',
            }}
          >
            <button
              type="button"
              onClick={handleCopy}
              disabled={!prompt}
              className="active:scale-[0.96]"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '6px 12px',
                // Copied — switches to ok-fill tint so the success state
                // reads as state, not just "the button changed color".
                background: copied
                  ? 'var(--ok-fill)'
                  : 'var(--btn-primary-bg, #0A8654)',
                color: copied
                  ? 'var(--ok-text)'
                  : 'var(--btn-primary-fg, #FFFFFF)',
                border: 'none',
                borderRadius: 6,
                fontSize: 12,
                fontWeight: 600,
                cursor: prompt ? 'pointer' : 'not-allowed',
                opacity: prompt ? 1 : 0.55,
                // Instrument primary — solid deep green with a 1px inset
                // top highlight + soft drop shadow. Matches ButtonV2's
                // primary so every CTA across the product looks like
                // the same component.
                boxShadow: copied
                  ? 'none'
                  : 'inset 0 1px 0 color-mix(in oklab, white 16%, transparent), 0 1px 2px rgba(0, 0, 0, 0.18)',
                transition:
                  'background 160ms ease, color 160ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
              }}
            >
              <span aria-hidden>{copied ? '✓' : '⧉'}</span>
              {copied ? t('quickstart.copied') : t('quickstart.heroCopyCta')}
            </button>
            <button
              type="button"
              onClick={() => setOpen(true)}
              className="active:scale-[0.96]"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '6px 10px',
                background: 'transparent',
                color: 'var(--text-muted)',
                border: '0.5px solid var(--border)',
                borderRadius: 6,
                fontSize: 12,
                cursor: 'pointer',
                transition:
                  'color 120ms ease, background 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
              }}
              onMouseEnter={e => {
                e.currentTarget.style.background = 'var(--bg-hover)'
                e.currentTarget.style.color = 'var(--text)'
              }}
              onMouseLeave={e => {
                e.currentTarget.style.background = 'transparent'
                e.currentTarget.style.color = 'var(--text-muted)'
              }}
            >
              {t('quickstart.heroViewFull')}
              <span aria-hidden style={{ opacity: 0.7 }}>→</span>
            </button>
            <span
              style={{
                marginLeft: 'auto',
                fontSize: 11,
                color: 'var(--text-subtle)',
                whiteSpace: 'nowrap',
              }}
            >
              {t('quickstart.heroCompat')}
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
