import { useTranslation } from 'react-i18next'
import { useTransientFlag } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'

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
    <button
      type="button"
      onClick={copy}
      className="active:scale-[0.96]"
      style={{
        fontSize: 11,
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
        // Visible padding stays compact; ::before in the active rule below
        // expands the hit area without bloating the chrome.
        padding: '4px 10px',
        border: '0.5px solid var(--border)',
        borderRadius: 4,
        cursor: 'pointer',
        flexShrink: 0,
        background: 'transparent',
        position: 'relative',
        transition: 'transform 120ms cubic-bezier(0.2, 0, 0, 1), color 120ms ease',
      }}
    >
      {copied ? t('quickstart.copied') : t('quickstart.copy')}
    </button>
  )
}
