import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import CopyButton from '@/components/app/copy-button'
import { cn } from '@/lib/utils'

interface CodeBlockProps {
  filename?: string
  code: string
  /** Informational: the snippet's language, exposed as `data-code-language`. */
  language?: string
  /** Names the block in the copy action: "Copy code for <name>". */
  copyName?: string
  /**
   * `ink` is the dark surface for a command the reader is meant to paste;
   * `light` is the same block on the canvas, for output shown for reference.
   */
  tone?: 'light' | 'ink'
}

/*
 * Lightweight syntax highlight: comments and URLs.
 *
 * Comments render muted and URLs render in the accent colour, because those
 * are the two things a reader checks: whether the line is a note, and whether
 * the endpoint is theirs. A full tokeniser would be a dependency, a theme, and
 * the promise that every language we proxy is covered.
 */
const URL_RE = /(https?:\/\/[^\s'"`<>]+)/g

function highlightLine(line: string, lineIdx: number, ink: boolean): ReactNode {
  const trimmed = line.trimStart()
  if (trimmed.startsWith('#') || trimmed.startsWith(';')) {
    return (
      <span key={lineIdx} className={ink ? 'text-code-surface-muted' : 'text-muted-foreground'}>
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
        className={cn('font-medium', ink ? 'text-code-surface-accent' : 'text-primary')}
      >
        {m[0]}
      </span>,
    )
    last = m.index + m[0].length
  }
  if (last < line.length) parts.push(line.slice(last))
  return <span key={lineIdx}>{parts}</span>
}

function highlight(code: string, ink: boolean): ReactNode {
  const lines = code.split('\n')
  return lines.map((line, i) => (
    <span key={i}>
      {highlightLine(line, i, ink)}
      {i < lines.length - 1 && '\n'}
    </span>
  ))
}

/**
 * A command, a configuration file, or a prompt.
 *
 * The block holds the copy action rather than a caller assembling one beside
 * it: the label has to name *this* block, and the header is where the reader
 * looks for it. Admin onboarding and the anonymous Portal share the component,
 * so a copied command cannot behave two ways.
 */
export default function CodeBlock({
  filename,
  code,
  language,
  copyName,
  tone = 'light',
}: CodeBlockProps) {
  const { t } = useTranslation()
  const ink = tone === 'ink'
  const copyLabel = copyName
    ? t('quickstart.copyNamedCode', { name: copyName })
    : t('quickstart.copyCode')

  return (
    <div
      data-code-tone={tone}
      data-code-language={language}
      className={cn(
        'group overflow-hidden rounded-sm',
        ink ? 'bg-code-surface shadow-pop' : 'border border-border bg-muted',
      )}
    >
      <div
        className={cn(
          'flex min-h-10 items-center justify-between gap-2 pr-1.5 pl-3.5',
          ink
            ? 'border-b border-code-surface-border bg-code-surface-header'
            : 'border-b border-border bg-card',
        )}
      >
        <span
          className={cn(
            'min-w-0 truncate font-mono text-label',
            ink ? 'text-code-surface-muted' : 'text-muted-foreground',
          )}
        >
          {filename ?? ''}
        </span>
        <CopyButton text={code} appearance="inline" label={copyLabel} tone={ink ? 'ink' : 'default'} />
      </div>
      <pre
        tabIndex={0}
        className={cn(
          'm-0 overflow-x-auto bg-transparent font-mono text-body leading-relaxed',
          ink ? 'px-5 py-4 text-code-surface-foreground' : 'p-4 text-foreground',
        )}
      >
        <code>{highlight(code, ink)}</code>
      </pre>
    </div>
  )
}
