import { useState, useCallback } from 'react'
import Icon from '@/components/Icon'

interface CodeBlockProps {
  filename?: string
  code: string
  language?: string
}

export default function CodeBlock({ filename, code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [code])

  return (
    <div
      className="relative rounded-xl overflow-hidden group border border-outline-variant/10"
      style={{ background: 'var(--code-bg)' }}
    >
      {filename && (
        <div
          className="flex items-center justify-between px-4 py-2 border-b border-outline-variant/10"
          style={{ background: 'var(--code-header)' }}
        >
          <span className="text-xs font-mono" style={{ color: 'var(--code-dim)' }}>
            {filename}
          </span>
          <button
            onClick={handleCopy}
            className="p-1 rounded-md bg-transparent hover:bg-white/10 transition-all cursor-pointer"
            style={{ color: 'var(--code-dim)' }}
          >
            <Icon
              name={copied ? 'check' : 'content_copy'}
              size="sm"
              className={copied ? 'text-success' : ''}
            />
          </button>
        </div>
      )}
      <div className="relative">
        <pre className="p-4 overflow-x-auto">
          <code
            className="font-mono text-sm leading-relaxed whitespace-pre-wrap"
            style={{ color: 'var(--code-text)' }}
          >
            {code}
          </code>
        </pre>
        {!filename && (
          <button
            onClick={handleCopy}
            className="absolute top-3 right-3 p-1.5 rounded-md bg-transparent hover:bg-white/10 opacity-0 group-hover:opacity-100 transition-all cursor-pointer"
            style={{ color: 'var(--code-dim)' }}
          >
            <Icon
              name={copied ? 'check' : 'content_copy'}
              size="sm"
              className={copied ? 'text-success' : ''}
            />
          </button>
        )}
      </div>
    </div>
  )
}
