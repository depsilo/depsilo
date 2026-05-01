import { useState, useCallback } from 'react'
import Icon from '@/components/Icon'

export default function ServiceUrlBarV2() {
  const [copied, setCopied] = useState(false)
  const url = window.location.origin

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [url])

  return (
    <div
      className="flex items-center gap-4 rounded-[5px] px-4 py-3"
      style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}
    >
      <span className="text-[12px] uppercase tracking-widest font-[400] shrink-0" style={{ color: 'var(--text-soft)' }}>
        Service Address
      </span>
      <code className="flex-1 font-mono text-[14px] truncate" style={{ color: 'var(--brand)' }}>
        {url}
      </code>
      <button
        onClick={handleCopy}
        className="shrink-0 p-1.5 rounded-[4px] bg-transparent cursor-pointer transition-colors duration-150"
        style={{ color: 'var(--text-soft)' }}
        onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--text)' }}
        onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-soft)' }}
      >
        <Icon name={copied ? 'check' : 'content_copy'} size="sm" className={copied ? 'text-success' : ''} />
      </button>
    </div>
  )
}
