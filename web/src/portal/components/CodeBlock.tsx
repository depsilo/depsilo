import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'
import { useCallback, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useTransientState } from '@/hooks/useTransientFlag'
import { copyText } from '@/lib/clipboard'

interface CodeBlockProps {
  filename?: string
  code: string
  language?: string
  copyName?: string
  copyLabel?: string
  copyDisabled?: boolean
  wrap?: boolean
  tone?: 'light' | 'ink'
  /**
   * The Portal's code block: always ink, a hairline edge instead of a drop
   * shadow, and no header bar when there is no filename (the copy control
   * sits in the code's top-right corner instead).
   */
  flat?: boolean
}

// Lightweight syntax highlight: comments + URLs.
// Comments (lines starting with # or ;) render in text-subtle.
// URLs (http://… or https://…) render in brand color, so the user's eye
// lands on the part of the snippet they actually need to verify.
const URL_RE = /(https?:\/\/[^\s'"`<>]+)/g

function highlightLine(line: string, lineIdx: number, ink: boolean, urlWeight: number): ReactNode {
  const trimmed = line.trimStart()
  if (trimmed.startsWith('#') || trimmed.startsWith(';')) {
    return (
      <span
        key={lineIdx}
        style={{ color: ink ? 'var(--code-muted)' : 'var(--text-subtle)' }}
      >
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
      <span
        key={`u${m.index}`}
        style={{ color: ink ? 'var(--code-accent)' : 'var(--brand-text)', fontWeight: urlWeight }}
      >
        {m[0]}
      </span>
    )
    last = m.index + m[0].length
  }
  if (last < line.length) parts.push(line.slice(last))
  return <span key={lineIdx}>{parts}</span>
}

function highlight(code: string, ink: boolean, urlWeight: number): ReactNode {
  const lines = code.split('\n')
  return lines.map((line, i) => (
    <span key={i}>
      {highlightLine(line, i, ink, urlWeight)}
      {i < lines.length - 1 && '\n'}
    </span>
  ))
}

export default function CodeBlock({
  filename,
  code,
  copyName,
  copyLabel,
  copyDisabled = false,
  wrap = false,
  tone = 'light',
  flat = false,
}: CodeBlockProps) {
  const { t } = useTranslation()
  const [copyState, showCopyState] = useTransientState<'idle' | 'copied' | 'failed'>('idle')
  const ink = flat || tone === 'ink'
  const headed = !flat || Boolean(filename)

  const handleCopy = useCallback(async () => {
    if (await copyText(code)) {
      showCopyState('copied', 2_000)
    } else {
      showCopyState('failed', 3_000)
    }
  }, [code, showCopyState])

  const copied = copyState === 'copied'

  return (
    <div
      className="code-block group"
      data-code-tone={ink ? 'ink' : 'light'}
      data-code-flat={flat ? 'true' : undefined}
      style={{
        position: flat ? 'relative' : undefined,
        background: ink ? 'var(--code-bg)' : 'var(--bg-soft)',
        border: flat ? '1px solid var(--code-line)' : ink ? 'none' : '1px solid var(--border)',
        borderRadius: 'var(--r-card)',
        boxShadow: ink && !flat ? 'var(--code-shadow)' : 'none',
        overflow: 'hidden',
      }}
    >
      <div
        style={headed ? {
          background: ink ? 'var(--code-header)' : 'var(--bg-card)',
          borderBottom: `1px solid ${ink ? 'var(--code-line)' : 'var(--border)'}`,
          minHeight: flat ? 40 : 42,
          display: 'flex',
          alignItems: 'center',
          padding: flat ? '0 4px 0 16px' : '0 5px 0 14px',
          justifyContent: 'space-between',
        } : {
          position: 'absolute',
          top: 4,
          right: 4,
          zIndex: 1,
          borderRadius: 6,
          background: 'var(--code-bg)',
        }}
      >
        {headed && (
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 12,
              color: ink ? 'var(--code-muted)' : 'var(--text-subtle)',
            }}
          >
            {filename ?? ''}
          </span>
        )}
        <Button
          variant="ghost"
          type="button"
          disabled={copyDisabled}
          onClick={handleCopy}
          aria-label={
            copyLabel ?? (copyName
              ? t('quickstart.copyNamedCode', { name: copyName })
              : t('quickstart.copyCode'))
          }
          className="code-copy-control min-h-[40px] gap-1 px-2 text-xs"
          style={{ color: copied
              ? ink
                ? 'var(--code-accent)'
                : 'var(--ok-text)'
              : copyState === 'failed'
                ? ink
                  ? 'var(--code-danger)'
                  : 'var(--danger-text)'
                : ink
                  ? 'var(--code-muted)'
                  : 'var(--text-muted)' }}
        >
          <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
          {copyState === 'copied'
            ? t('quickstart.copied')
            : copyState === 'failed'
              ? t('quickstart.copyFailed')
              : copyLabel ?? t('quickstart.copy')}
        </Button>
        <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {copyState === 'copied'
            ? t('quickstart.copied')
            : copyState === 'failed'
              ? t('quickstart.copyFailed')
              : ''}
        </span>
      </div>
      <pre
        className="code-scroll"
        tabIndex={0}
        style={{
          margin: 0,
          padding: flat
            ? headed ? '16px 20px' : '14px 96px 14px 20px'
            : ink ? '18px 20px 20px' : '14px 16px',
          minHeight: headed ? undefined : 48,
          fontFamily: 'var(--font-mono)',
          fontSize: 13,
          lineHeight: ink ? 1.68 : 1.6,
          color: ink ? 'var(--code-fg)' : 'var(--text)',
          overflowX: 'auto',
          whiteSpace: wrap ? 'pre-wrap' : undefined,
          overflowWrap: wrap ? 'anywhere' : undefined,
          // No background here: .code-scroll paints its own, which the edge
          // fade needs to hide the shadow at each end of the scroll.
        }}
      >
        <code>{highlight(code, ink, flat ? 500 : 560)}</code>
      </pre>
    </div>
  )
}
