import { useState } from 'react'
import { useTranslation } from 'react-i18next'

interface Props {
  text: string
}

export default function CopyButton({ text }: Props) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }

  return (
    <button
      type="button"
      onClick={copy}
      style={{
        fontSize: 11,
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
        padding: '3px 8px',
        border: '0.5px solid var(--border)',
        borderRadius: 4,
        cursor: 'pointer',
        flexShrink: 0,
        background: 'transparent',
      }}
    >
      {copied ? t('quickstart.copied') : t('quickstart.copy')}
    </button>
  )
}
