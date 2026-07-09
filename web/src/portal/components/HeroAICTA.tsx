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
  const { t, i18n } = useTranslation()
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
  const promptLines = prompt ? prompt.split('\n').length : 0

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
          border: '0.5px solid var(--border-strong)',
          borderRadius: 18,
          display: 'grid',
          gridTemplateColumns: 'minmax(340px, 0.44fr) minmax(0, 0.56fr)',
          alignItems: 'stretch',
          gap: 36,
          padding: '54px 56px',
          minHeight: 310,
          boxShadow: 'var(--shadow-pop)',
        }}
      >
        {/* Brand glow rising from the lower-left corner. Theme-aware
            (.quickstart-hero-wash in index.css): light mode halves the
            tint so the white card doesn't read minty. */}
        <div aria-hidden className="quickstart-hero-wash" />
        {/* Left-edge brand bar. */}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            left: 0,
            top: 0,
            bottom: 0,
            width: 5,
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
            className="eyebrow"
            style={{
              color: 'var(--brand-text)',
              marginBottom: 14,
              fontSize: 12,
              alignSelf: 'flex-start',
              padding: '6px 12px',
              borderRadius: 999,
              border: '0.5px solid var(--brand-border)',
              background: 'color-mix(in oklab, var(--brand) 6%, var(--bg-card))',
            }}
          >
            {t('quickstart.heroEyebrow')}
          </div>
          <h2
            style={{
              margin: 0,
              fontSize: 'clamp(34px, 3.2vw, 56px)',
              fontFamily: 'var(--font-display)',
              fontWeight: 680,
              // Inter Tight is pre-tightened; keep added tracking mild
              // for Latin, and milder still for CJK (hanzi carry no
              // sidebearings to absorb negative tracking).
              letterSpacing: i18n.language === 'zh' ? '-0.02em' : '-0.025em',
              lineHeight: 1.02,
              color: 'var(--text)',
              maxWidth: 560,
            }}
          >
            {t('quickstart.heroTitle')}
          </h2>
          <p
            style={{
              margin: '16px 0 0 0',
              fontSize: 16,
              lineHeight: 1.55,
              color: 'var(--text-muted)',
              maxWidth: 560,
            }}
          >
            {t('quickstart.heroDescShort')}
          </p>
          <p
            style={{
              margin: '8px 0 0 0',
              fontSize: 13,
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
            border: '0.5px solid var(--border-strong)',
            borderRadius: 14,
            overflow: 'hidden',
            minWidth: 0,
            boxShadow: 'var(--shadow-card)',
          }}
        >
          {/* Preview header — filename first, the old label demoted to
              a secondary hint beside it. */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '9px 12px 9px 16px',
              borderBottom: '0.5px solid var(--border)',
              background: 'var(--bg-card)',
            }}
          >
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 11,
                fontWeight: 600,
                color: 'var(--text-muted)',
                letterSpacing: '-0.01em',
              }}
            >
              depsilo-integrate.md
            </span>
            <span
              className="eyebrow"
              style={{ color: 'var(--text-subtle)', fontSize: 10, margin: 0 }}
            >
              {t('quickstart.heroPreviewLabel')}
            </span>
            <span
              style={{
                marginLeft: 'auto',
                fontSize: 10,
                fontFamily: 'var(--font-mono)',
                color: 'var(--text-subtle)',
              }}
            >
              {t('quickstart.heroSizeHint')}
            </span>
          </div>
          {/* Preview body — numbered lines, code-editor style. */}
          <pre
            style={{
              margin: 0,
              padding: '16px 18px 14px 14px',
              flex: 1,
              fontFamily: 'var(--font-mono)',
              fontSize: 13,
              lineHeight: 1.58,
              color: 'var(--text)',
              whiteSpace: 'pre',
              overflow: 'hidden',
              maxHeight: 152,
              position: 'relative',
            }}
          >
            <code style={{ display: 'block', opacity: prompt ? 1 : 0.5 }}>
              {error
                ? t('quickstart.aiIntegrationError')
                : previewText.split('\n').map((line, i) => (
                    <span key={i} style={{ display: 'flex', gap: 12 }}>
                      <span
                        aria-hidden
                        style={{
                          userSelect: 'none',
                          minWidth: 16,
                          textAlign: 'right',
                          color: 'var(--text-subtle)',
                          opacity: 0.7,
                          flexShrink: 0,
                        }}
                      >
                        {i + 1}
                      </span>
                      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{line}</span>
                    </span>
                  ))}
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
            className="quickstart-hero-actions"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '12px 14px',
              borderTop: '0.5px solid var(--border)',
              background: 'var(--bg-card)',
            }}
          >
            <button
              type="button"
              onClick={handleCopy}
              disabled={!prompt}
              className={`active:scale-[0.96]${prompt && !copied ? ' cta-shimmer' : ''}`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '9px 15px',
                // Copied — switches to ok-fill tint so the success state
                // reads as state, not just "the button changed color".
                background: copied
                  ? 'var(--ok-fill)'
                  : 'var(--btn-primary-bg, #0A8654)',
                color: copied
                  ? 'var(--ok-text)'
                  : 'var(--btn-primary-fg, #FFFFFF)',
                border: 'none',
                borderRadius: 8,
                fontSize: 13,
                fontWeight: 600,
                whiteSpace: 'nowrap',
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
              {/* Cross-fade the glyph (both stay in the DOM) and stack
                  both labels in one grid cell so the button keeps the
                  width of the longer label — no layout jump on copy. */}
              <span aria-hidden style={{ position: 'relative', display: 'inline-flex', width: 12, height: 15 }}>
                <span
                  style={{
                    position: 'absolute',
                    inset: 0,
                    opacity: copied ? 0 : 1,
                    transform: copied ? 'scale(0.25)' : 'scale(1)',
                    filter: copied ? 'blur(4px)' : 'blur(0)',
                    transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
                  }}
                >
                  ⧉
                </span>
                <span
                  style={{
                    position: 'absolute',
                    inset: 0,
                    opacity: copied ? 1 : 0,
                    transform: copied ? 'scale(1)' : 'scale(0.25)',
                    filter: copied ? 'blur(0)' : 'blur(4px)',
                    transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
                  }}
                >
                  ✓
                </span>
              </span>
              <span style={{ display: 'grid', textAlign: 'center' }}>
                <span style={{ gridArea: '1 / 1', opacity: copied ? 0 : 1, transition: 'opacity 160ms ease' }}>
                  {t('quickstart.heroCopyCta')}
                </span>
                <span style={{ gridArea: '1 / 1', opacity: copied ? 1 : 0, transition: 'opacity 160ms ease' }}>
                  {t('quickstart.copied')}
                </span>
              </span>
            </button>
            <button
              type="button"
              onClick={() => setOpen(true)}
              className="active:scale-[0.96] hit-extend"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '6px 10px',
                background: 'transparent',
                color: 'var(--text-muted)',
                border: '0.5px solid var(--border)',
                borderRadius: 8,
                fontSize: 13,
                whiteSpace: 'nowrap',
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
            {promptLines > 0 && (
              <span
                className="quickstart-hero-compat"
                style={{
                  marginLeft: 'auto',
                  fontSize: 11,
                  fontFamily: 'var(--font-mono)',
                  fontVariantNumeric: 'tabular-nums',
                  color: 'var(--text-subtle)',
                  whiteSpace: 'nowrap',
                }}
              >
                {t('quickstart.heroPromptMeta', { n: promptLines })}
              </span>
            )}
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
