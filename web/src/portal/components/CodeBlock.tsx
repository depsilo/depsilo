import { useState, useCallback } from 'react'
import Icon from '@/components/Icon'

interface CodeBlockV2Props {
  filename?: string
  code: string
  language?: string
}

export default function CodeBlockV2({ filename, code }: CodeBlockV2Props) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [code])

  return (
    <div className="relative rounded-[6px] overflow-hidden group" style={{ background: 'var(--code-bg)', border: '1px solid var(--border)', borderLeft: '2px solid var(--stripe-purple)' }}>
      {filename && (
        <div
          className="flex items-center justify-between px-4 py-2"
          style={{ background: 'var(--code-header)', borderBottom: '1px solid rgba(255,255,255,0.06)' }}
        >
          <span className="text-[10px] uppercase tracking-wider font-mono font-[500]" style={{ color: 'var(--code-dim)' }}>
            {filename}
          </span>
          <button
            onClick={handleCopy}
            className="p-1 rounded-[4px] bg-transparent hover:bg-white/10 transition-all duration-150 cursor-pointer"
            style={{ color: 'var(--code-dim)' }}
          >
            <Icon name={copied ? 'check' : 'content_copy'} size="sm" className={copied ? 'text-success' : ''} />
          </button>
        </div>
      )}
      <div className="relative">
        <pre className="p-4 overflow-x-auto">
          <code
            className="font-mono text-[12px] font-[500] leading-[2] whitespace-pre-wrap"
            style={{ color: 'var(--code-text)' }}
          >
            {code}
          </code>
        </pre>
        {!filename && (
          <button
            onClick={handleCopy}
            className="absolute top-3 right-3 p-1.5 rounded-[4px] bg-transparent hover:bg-white/10 opacity-0 group-hover:opacity-100 transition-all duration-150 cursor-pointer"
            style={{ color: 'var(--code-dim)' }}
          >
            <Icon name={copied ? 'check' : 'content_copy'} size="sm" className={copied ? 'text-success' : ''} />
          </button>
        )}
      </div>
    </div>
  )
}
