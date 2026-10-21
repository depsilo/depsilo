import { useTranslation } from 'react-i18next'
import { useTransientFlag } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'
import { Button } from '@/components/ui/button'

interface Props {
  text: string
}

export default function CopyButton({ text }: Props) {
  const { t } = useTranslation()
  const [copied, showCopied] = useTransientFlag()

  async function copy() {
    if (await copyText(text)) {
      showCopied()
    }
  }

  return (
    <Button
      type="button"
      onClick={copy}
      variant="outline"
      className="min-h-[40px] text-xs"
      style={{
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
      }}
    >
      {copied ? t('quickstart.copied') : t('quickstart.copy')}
    </Button>
  )
}
