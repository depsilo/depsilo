import { useState, useCallback } from 'react'

interface CodeBlockProps {
  filename?: string
  code: string
  language?: string
}

export default function CodeBlock({ filename, code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code)
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
      .catch(() => {
        // clipboard write failed (e.g., no Secure Context or focus)
      })
  }, [code])

  return (
    <div
      className="group"
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-card)',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          background: 'var(--bg-card)',
          borderBottom: '0.5px solid var(--border)',
          height: 28,
          display: 'flex',
          alignItems: 'center',
          padding: '0 12px',
          justifyContent: 'space-between',
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--text-subtle)',
          }}
        >
          {filename ?? ''}
        </span>
        <button
          onClick={handleCopy}
          style={{
            color: copied ? 'var(--ok)' : 'var(--text-subtle)',
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            padding: '2px 4px',
            borderRadius: 4,
            display: 'flex',
            alignItems: 'center',
            gap: 4,
            fontSize: 11,
          }}
        >
          {copied ? (
            <svg aria-hidden="true" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="20 6 9 17 4 12" />
            </svg>
          ) : (
            <svg aria-hidden="true" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
            </svg>
          )}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre
        style={{
          margin: 0,
          padding: '12px 16px',
          fontFamily: 'var(--font-mono)',
          fontSize: 13,
          lineHeight: 1.6,
          color: 'var(--text)',
          overflowX: 'auto',
          background: 'transparent',
        }}
      >
        <code>{code}</code>
      </pre>
    </div>
  )
}
