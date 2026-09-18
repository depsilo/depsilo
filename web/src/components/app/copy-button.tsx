import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import Button from '@/components/app/button'
import IconButton from '@/components/app/icon-button'
import { useTransientFlag } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'

interface CopyButtonProps {
  text: string
  /**
   * `text` is the labelled action beside a code block; `icon` is the compact
   * action beside a read-only value in a form. One control with two
   * presentations, so a copy always copies the same way and always reports the
   * same result.
   */
  appearance?: 'text' | 'icon'
  /** Required for `icon`, where the label is the only accessible name. */
  label?: string
  className?: string
}

export default function CopyButton({
  text,
  appearance = 'text',
  label,
  className,
}: CopyButtonProps) {
  const { t } = useTranslation()
  const [copied, showCopied] = useTransientFlag()

  async function copy() {
    if (await copyText(text)) showCopied()
  }

  if (appearance === 'icon') {
    const name = label ?? t('quickstart.copy')
    return (
      <IconButton
        icon={copied ? Check : Copy}
        label={name}
        onClick={() => { void copy() }}
        className={cn(copied && 'text-success', className)}
      />
    )
  }

  return (
    <Button
      type="button"
      variant="secondary"
      size="sm"
      onClick={() => { void copy() }}
      className={cn('shrink-0', copied && 'text-success', className)}
    >
      {copied ? t('quickstart.copied') : t('quickstart.copy')}
    </Button>
  )
}
