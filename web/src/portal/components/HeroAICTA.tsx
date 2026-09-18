import { Check, Copy, Eye, Lightbulb, LoaderCircle } from 'lucide-react'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/app/icon'
import Modal from '@/components/app/modal'
import CopyButton from '@/components/app/copy-button'
import { useTransientFlag } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'

export default function HeroAICTA() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, showCopied] = useTransientFlag()
  const [copyFailed, setCopyFailed] = useState(false)

  const {
    data: prompt,
    error,
    isFetching,
    refetch,
  } = useQuery<string>({
    queryKey: ['integration-prompt'],
    queryFn: async ({ signal }) => {
      const response = await fetch('/api/v1/integration-prompt', { signal })
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
      showCopied()
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
        className="flex min-h-full flex-col rounded-lg border border-input bg-card p-5 shadow-card sm:p-6"
      >
        <div className="flex items-start gap-3">
          <span
            aria-hidden="true"
            className="flex size-10 shrink-0 items-center justify-center rounded-sm bg-accent text-primary"
          >
            <Lightbulb className="icon" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h3
              id="quickstart-optional-ai-title"
              className="m-0 font-sans text-subhead font-semibold leading-[1.25] text-foreground"
            >
              {t('quickstart.optionalAiTitle')}
            </h3>
            <p className="mt-2 max-w-[62ch] text-body leading-[1.55] text-muted-foreground">
              {t('quickstart.optionalAiDescription')}
            </p>
          </div>
        </div>

        <div className="mt-auto flex flex-wrap items-center gap-2 pt-5">
          <button
            type="button"
            onClick={() => void handleCopy()}
            disabled={isFetching}
            className={cn(
              'inline-flex min-h-10 min-w-[168px] items-center justify-center gap-2 rounded-sm border-0 px-3 text-body font-semibold transition-[background,color,opacity,transform] duration-150 active:scale-[0.97]',
              copied ? 'bg-success-surface text-success' : 'bg-primary text-primary-foreground',
              isFetching ? 'cursor-wait opacity-70' : 'cursor-pointer',
            )}
          >
            <Icon
              icon={isFetching ? LoaderCircle : copied ? Check : Copy}
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
            className={cn(
              'inline-flex min-h-10 items-center gap-2 rounded-sm border border-border bg-transparent px-3 text-body font-semibold text-muted-foreground transition-colors duration-150 hover:bg-accent hover:text-foreground active:scale-[0.97]',
              isFetching ? 'cursor-wait' : 'cursor-pointer',
            )}
          >
            <Eye className="icon icon-sm" aria-hidden="true" />
            {t('quickstart.heroViewFull')}
          </button>
        </div>

        <span className="sr-only" aria-live="polite">
          {copied ? t('quickstart.copied') : ''}
        </span>
        {hasError && (
          <p
            role="alert"
            className="mb-0 mt-3 text-body leading-[1.5] text-destructive"
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
          <p className="m-0 text-body leading-[1.55] text-muted-foreground">
            {t('quickstart.aiIntegrationDesc')}
          </p>
          <div
            className="flex flex-wrap items-center justify-between gap-3 rounded-sm border border-border bg-muted px-3 py-2"
          >
            <span className="min-w-0 flex-1 text-label leading-[1.5] text-muted-foreground">
              {t('quickstart.aiIntegrationHowto')}
            </span>
            {prompt && <CopyButton text={prompt} />}
          </div>
          <pre
            tabIndex={0}
            className="m-0 max-h-[60vh] min-h-30 overflow-auto whitespace-pre rounded-sm border border-border bg-muted p-4 font-mono text-label leading-relaxed text-foreground"
          >
            {prompt && <code>{prompt}</code>}
          </pre>
        </div>
      </Modal>
    </>
  )
}
