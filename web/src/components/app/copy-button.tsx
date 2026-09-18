import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import Button from '@/components/app/button'
import IconButton from '@/components/app/icon-button'
import { useTransientState } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'

interface CopyButtonProps {
  text: string
  /**
   * `text` is the labelled action beside a block of content, `inline` is the
   * same action inside a code block's header, and `icon` is the compact action
   * beside a read-only value in a form. One control with three presentations,
   * so a copy always copies the same way and always reports the same result.
   */
  appearance?: 'text' | 'icon' | 'inline'
  /**
   * The accessible name. Required for `icon`, and used to disambiguate several
   * code blocks on one page — "copy" repeated four times is not a name.
   */
  label?: string
  /**
   * The surface the `inline` presentation sits on. The code surface has its
   * own text ramp in both themes, where `muted-foreground` is a canvas colour
   * and not a legible one.
   */
  tone?: 'default' | 'ink'
  className?: string
}

const INK_STATE = {
  idle: 'text-code-surface-muted hover:text-code-surface-foreground',
  copied: 'text-code-surface-accent',
  failed: 'text-code-surface-danger',
} as const

const CANVAS_STATE = {
  idle: 'text-muted-foreground hover:text-foreground',
  copied: 'text-success',
  failed: 'text-destructive',
} as const

export default function CopyButton({
  text,
  appearance = 'text',
  label,
  tone = 'default',
  className,
}: CopyButtonProps) {
  const { t } = useTranslation()
  const [state, showState] = useTransientState<'idle' | 'copied' | 'failed'>('idle')

  async function copy() {
    showState((await copyText(text)) ? 'copied' : 'failed', 2_000)
  }

  const announcement = state === 'copied'
    ? t('quickstart.copied')
    : state === 'failed'
      ? t('quickstart.copyFailed')
      : ''

  if (appearance === 'icon') {
    return (
      <IconButton
        icon={state === 'copied' ? Check : Copy}
        label={label ?? t('quickstart.copy')}
        onClick={() => { void copy() }}
        className={cn(state === 'copied' && 'text-success', className)}
      />
    )
  }

  if (appearance === 'inline') {
    const states = tone === 'ink' ? INK_STATE : CANVAS_STATE
    return (
      <button
        type="button"
        onClick={() => { void copy() }}
        aria-label={label ?? t('quickstart.copy')}
        className={cn(
          'inline-flex min-h-10 shrink-0 items-center gap-1 rounded-sm px-2 text-label transition-colors duration-200 active:scale-[0.96]',
          states[state],
          className,
        )}
      >
        <span aria-hidden="true" className="relative inline-flex size-3">
          <Check
            aria-hidden="true"
            className={cn(
              'absolute inset-0 size-3 transition-[opacity,transform,filter] duration-200 ease-out',
              state === 'copied' ? 'scale-100 opacity-100 blur-none' : 'scale-25 opacity-0 blur-xs',
            )}
          />
          <Copy
            aria-hidden="true"
            className={cn(
              'absolute inset-0 size-3 transition-[opacity,transform,filter] duration-200 ease-out',
              state === 'copied' ? 'scale-25 opacity-0 blur-xs' : 'scale-100 opacity-100 blur-none',
            )}
          />
        </span>
        {announcement || t('quickstart.copy')}
        <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {announcement}
        </span>
      </button>
    )
  }

  return (
    <Button
      type="button"
      variant="secondary"
      size="sm"
      onClick={() => { void copy() }}
      className={cn('shrink-0', state === 'copied' && 'text-success', state === 'failed' && 'text-destructive', className)}
    >
      {announcement || t('quickstart.copy')}
      <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {announcement}
      </span>
    </Button>
  )
}
