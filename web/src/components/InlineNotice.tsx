import type { ReactNode } from 'react'

export type NoticeTone = 'success' | 'warning' | 'danger' | 'info'

interface InlineNoticeProps {
  tone: NoticeTone
  title?: string
  children: ReactNode
}

const toneStyles: Record<NoticeTone, { background: string; border: string; color: string }> = {
  success: { background: 'var(--ok-fill)', border: 'var(--ok-border)', color: 'var(--ok-text)' },
  warning: { background: 'var(--warn-fill)', border: 'var(--warn-border)', color: 'var(--warn-text)' },
  danger: { background: 'var(--danger-fill)', border: 'var(--danger-border)', color: 'var(--danger-text)' },
  info: { background: 'var(--brand-soft)', border: 'var(--brand-border)', color: 'var(--brand-text)' },
}

export default function InlineNotice({ tone, title, children }: InlineNoticeProps) {
  const colors = toneStyles[tone]
  return (
    <div
      role={tone === 'danger' ? 'alert' : undefined}
      className="rounded-[6px] border px-3 py-2.5 text-[13px] leading-5"
      style={{ background: colors.background, borderColor: colors.border, color: colors.color }}
    >
      {title && <p className="mb-1 font-[600]">{title}</p>}
      <div>{children}</div>
    </div>
  )
}
