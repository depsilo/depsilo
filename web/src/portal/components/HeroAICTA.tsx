import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Modal from '@/components/Modal'
import CopyButton from '@/portal/components/CopyButton'
import { copyText } from '@/lib/clipboard'

export default function HeroAICTA() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)

  const {
    data: prompt,
    error,
    isFetching,
    refetch,
  } = useQuery<string>({
    queryKey: ['integration-prompt'],
    queryFn: async () => {
      const response = await fetch('/api/v1/integration-prompt')
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      return response.text()
    },
    enabled: false,
    staleTime: 5 * 60 * 1000,
  })

  async function loadPrompt(): Promise<string | undefined> {
    if (prompt) return prompt
    const result = await refetch()
    return result.data
  }

  async function handleCopy() {
    setCopyFailed(false)
    const text = await loadPrompt()
    if (!text) return

    if (await copyText(text)) {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } else {
      setCopyFailed(true)
    }
  }

  async function handleReview() {
    const text = await loadPrompt()
    if (text) setOpen(true)
  }

  const hasError = Boolean(error) || copyFailed

  return (
    <>
      <article
        aria-labelledby="quickstart-optional-ai-title"
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
            <Icon name="lightbulb" />
          </span>
          <div className="min-w-0">
            <h3
              id="quickstart-optional-ai-title"
              className="m-0 font-[var(--font-display)] text-[18px] font-[650] leading-[1.25] text-[var(--text)]"
            >
              {t('quickstart.optionalAiTitle')}
            </h3>
            <p className="mt-2 max-w-[62ch] text-[13px] leading-[1.55] text-[var(--text-muted)]">
              {t('quickstart.optionalAiDescription')}
            </p>
          </div>
        </div>

        <div className="mt-auto flex flex-wrap items-center gap-2 pt-5">
          <button
            type="button"
            onClick={() => void handleCopy()}
            disabled={isFetching}
            className="stripe-focus-ring inline-flex min-h-10 min-w-[168px] items-center justify-center gap-2 rounded-[7px] px-3 text-[13px] font-[620] active:scale-[0.97]"
            style={{
              color: copied ? 'var(--ok-text)' : 'var(--btn-fg)',
              background: copied ? 'var(--ok-fill)' : 'var(--btn)',
              border: 0,
              cursor: isFetching ? 'wait' : 'pointer',
              opacity: isFetching ? 0.7 : 1,
              transition:
                'background 150ms ease, color 150ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
            }}
          >
            <Icon
              name={isFetching ? 'progress_activity' : copied ? 'check' : 'content_copy'}
              size="sm"
              className={isFetching ? 'animate-spin' : ''}
            />
            {isFetching
              ? t('quickstart.loading')
              : copied
                ? t('quickstart.copied')
                : t('quickstart.heroCopyCta')}
          </button>
          <button
            type="button"
            onClick={() => void handleReview()}
            disabled={isFetching}
            className="stripe-focus-ring inline-flex min-h-10 items-center gap-2 rounded-[7px] px-3 text-[13px] font-[600] text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.97]"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              cursor: isFetching ? 'wait' : 'pointer',
            }}
          >
            <Icon name="visibility" size="sm" />
            {t('quickstart.heroViewFull')}
          </button>
        </div>

        <span className="sr-only" aria-live="polite">
          {copied ? t('quickstart.copied') : ''}
        </span>
        {hasError && (
          <p
            role="alert"
            className="mb-0 mt-3 text-[13px] leading-[1.5] text-[var(--danger-text)]"
          >
            {copyFailed
              ? t('quickstart.copyFailed')
              : t('quickstart.aiIntegrationError')}
          </p>
        )}
      </article>

      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title={t('quickstart.aiIntegrationTitle')}
        width={720}
      >
        <div className="flex flex-col gap-3">
          <p className="m-0 text-[13px] leading-[1.55] text-[var(--text-muted)]">
            {t('quickstart.aiIntegrationDesc')}
          </p>
          <div
            className="flex flex-wrap items-center justify-between gap-3 rounded-[6px] px-3 py-2"
            style={{
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
            }}
          >
            <span className="min-w-0 flex-1 text-[12px] leading-[1.5] text-[var(--text-muted)]">
              {t('quickstart.aiIntegrationHowto')}
            </span>
            {prompt && <CopyButton text={prompt} />}
          </div>
          <pre
            tabIndex={0}
            className="m-0 min-h-[120px] max-h-[60vh] overflow-auto whitespace-pre rounded-[6px] p-4 font-[var(--font-mono)] text-[12px] leading-[1.55] text-[var(--text)]"
            style={{
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
            }}
          >
            {prompt && <code>{prompt}</code>}
          </pre>
        </div>
      </Modal>
    </>
  )
}
