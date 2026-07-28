import { useState, useCallback, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { copyText } from '@/lib/clipboard'

interface CodeBlockProps {
  filename?: string
  code: string
  language?: string
  copyName?: string
}

// Lightweight syntax highlight: comments + URLs.
// Comments (lines starting with # or ;) render in text-subtle.
// URLs (http://… or https://…) render in brand color, so the user's eye
// lands on the part of the snippet they actually need to verify.
const URL_RE = /(https?:\/\/[^\s'"`<>]+)/g

function highlightLine(line: string, lineIdx: number): ReactNode {
  const trimmed = line.trimStart()
  if (trimmed.startsWith('#') || trimmed.startsWith(';')) {
    return (
      <span key={lineIdx} style={{ color: 'var(--text-subtle)' }}>
        {line}
      </span>
    )
  }
  const parts: ReactNode[] = []
  let last = 0
  let m: RegExpExecArray | null
  URL_RE.lastIndex = 0
  while ((m = URL_RE.exec(line)) !== null) {
    if (m.index > last) parts.push(line.slice(last, m.index))
    parts.push(
      <span key={`u${m.index}`} style={{ color: 'var(--brand-text)', fontWeight: 500 }}>
        {m[0]}
      </span>
    )
    last = m.index + m[0].length
  }
  if (last < line.length) parts.push(line.slice(last))
  return <span key={lineIdx}>{parts}</span>
}

function highlight(code: string): ReactNode {
  const lines = code.split('\n')
  return lines.map((line, i) => (
    <span key={i}>
      {highlightLine(line, i)}
      {i < lines.length - 1 && '\n'}
    </span>
  ))
}

export default function CodeBlock({ filename, code, copyName }: CodeBlockProps) {
  const { t } = useTranslation()
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')

  const handleCopy = useCallback(async () => {
    if (await copyText(code)) {
      setCopyState('copied')
      setTimeout(() => setCopyState('idle'), 2000)
    } else {
      setCopyState('failed')
      setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [code])

  const copied = copyState === 'copied'

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
          minHeight: 40,
          display: 'flex',
          alignItems: 'center',
          padding: '0 4px 0 12px',
          justifyContent: 'space-between',
        }}
      >
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 12,
            color: 'var(--text-subtle)',
          }}
        >
          {filename ?? ''}
        </span>
        <button
          type="button"
          onClick={handleCopy}
          className="active:scale-[0.96] stripe-focus-ring"
          aria-label={
            copyName
              ? t('quickstart.copyNamedCode', { name: copyName })
              : t('quickstart.copyCode')
          }
          style={{
            color: copied
              ? 'var(--ok-text)'
              : copyState === 'failed'
                ? 'var(--danger-text)'
                : 'var(--text-muted)',
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            minHeight: 40,
            padding: '4px 8px',
            borderRadius: 4,
            display: 'flex',
            alignItems: 'center',
            gap: 4,
            fontSize: 12,
            transition: 'color 200ms cubic-bezier(0.2, 0, 0, 1), transform 120ms cubic-bezier(0.2, 0, 0, 1)',
          }}
        >
          {/* Both icons live in the DOM at all times — one absolutely
              positioned so we can cross-fade opacity + scale + blur per
              skill principle #7. Avoids the snap that conditional-render
              swap produces. */}
          <span style={{ position: 'relative', display: 'inline-flex', width: 12, height: 12 }}>
            <svg
              aria-hidden="true"
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              style={{
                position: 'absolute',
                inset: 0,
                opacity: copied ? 1 : 0,
                transform: copied ? 'scale(1)' : 'scale(0.25)',
                filter: copied ? 'blur(0)' : 'blur(4px)',
                transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
              }}
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <svg
              aria-hidden="true"
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              style={{
                position: 'absolute',
                inset: 0,
                opacity: copied ? 0 : 1,
                transform: copied ? 'scale(0.25)' : 'scale(1)',
                filter: copied ? 'blur(4px)' : 'blur(0)',
                transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
              }}
            >
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
            </svg>
          </span>
          {copyState === 'copied'
            ? t('quickstart.copied')
            : copyState === 'failed'
              ? t('quickstart.copyFailed')
              : t('quickstart.copy')}
        </button>
        <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {copyState === 'copied'
            ? t('quickstart.copied')
            : copyState === 'failed'
              ? t('quickstart.copyFailed')
              : ''}
        </span>
      </div>
      <pre
        tabIndex={0}
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
        <code>{highlight(code)}</code>
      </pre>
    </div>
  )
}
