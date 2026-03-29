import { useState, useCallback } from 'react'
import Card from '@/components/Card'
import Icon from '@/components/Icon'

export default function ServiceUrlBar() {
  const [copied, setCopied] = useState(false)
  const url = window.location.origin

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [url])

  return (
    <Card bordered className="flex items-center gap-4 p-4">
      <span className="text-xs uppercase tracking-widest font-medium text-on-surface-variant shrink-0">
        Service Address
      </span>
      <code className="flex-1 font-mono text-primary-dim text-base truncate">
        {url}
      </code>
      <button
        onClick={handleCopy}
        className="shrink-0 p-1.5 rounded-[0.125rem] bg-transparent hover:bg-surface-container text-on-surface-variant hover:text-on-surface transition-colors cursor-pointer"
      >
        <Icon
          name={copied ? 'check' : 'content_copy'}
          size="sm"
          className={copied ? 'text-success' : ''}
        />
      </button>
    </Card>
  )
}
