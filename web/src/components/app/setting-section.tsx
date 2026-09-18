import type { ReactNode } from 'react'

import SectionHeader from '@/components/app/section-header'
import { cn } from '@/lib/utils'

interface SettingSectionProps {
  title: string
  children: ReactNode
  className?: string
}

/**
 * A titled group of settings, divided by hairlines rather than boxed.
 *
 * The container owns its list rhythm — the separator between rows, and the
 * spacing above and below each one. Each row's *internal* arrangement (label
 * beside or above its control) belongs to `Field`, which owns the label; a
 * surface that positions labels with a descendant selector is reaching into
 * another component's markup.
 */
export default function SettingSection({ title, children, className }: SettingSectionProps) {
  return (
    <section className={cn('min-w-0', className)}>
      <SectionHeader title={title} />
      <div className="flex flex-col divide-y divide-border [&>*]:min-w-0 [&>*]:py-5 [&>*:first-child]:pt-0">
        {children}
      </div>
    </section>
  )
}
