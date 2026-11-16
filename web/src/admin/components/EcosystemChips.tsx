import { useTranslation } from 'react-i18next'

import { ecosystemLabel } from '@/admin/operatorEcosystems'
import { toggleEcosystem } from '@/admin/listFilters'
import EcosystemIcon from '@/components/EcosystemIcon'
import { Toggle } from '@/components/ui/toggle'
import type { EcosystemCount } from '@/lib/adminApi.types'
import { isEcosystemType } from '@/lib/ecosystemTypes'
import { cn } from '@/lib/utils'

interface Props {
  /** Per-ecosystem counts under the page's other filters; undefined while loading. */
  counts?: EcosystemCount[]
  selected: string[]
  onChange: (ids: string[]) => void
  /** A label for a value that is not an ecosystem (the audit log's "admin"). */
  labelFor?: (id: string) => string | undefined
  className?: string
}

const chipClass = cn(
  'stripe-focus-ring inline-flex min-h-10 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-[13px] font-[500] sm:min-h-8',
  'border-[var(--border)] bg-transparent text-[var(--text)] hover:bg-[var(--bg-hover)]',
  'aria-pressed:border-[var(--brand-border)] aria-pressed:bg-[var(--brand-soft)] aria-pressed:text-[var(--brand-text)]',
)

/**
 * The ecosystem filter of an Admin list: one chip per ecosystem that has
 * rows under the other filters, with its count. Chips toggle independently,
 * so several ecosystems can be shown at once; "All" clears the choice.
 */
export default function EcosystemChips({ counts, selected, onChange, labelFor, className }: Props) {
  const { t, i18n } = useTranslation()
  const number = (value: number) => value.toLocaleString(i18n.language)
  // A chosen ecosystem stays visible even when the other filters leave it
  // empty, so it can always be switched off again.
  const known = counts ?? []
  const missing = selected.filter(id => !known.some(item => item.id === id)).map(id => ({ id, count: 0 }))
  const chips = [...known, ...missing]
  const total = known.reduce((sum, item) => sum + item.count, 0)
  // Nothing to choose between: an empty list has no ecosystems to filter.
  if (counts && chips.length === 0) return null

  return (
    <div
      role="group"
      aria-label={t('filters.ecosystems')}
      data-ecosystem-chips
      className={cn('-mx-1 flex gap-1.5 overflow-x-auto px-1 py-0.5 sm:flex-wrap sm:overflow-visible', className)}
    >
      <Toggle
        type="button"
        pressed={selected.length === 0}
        onPressedChange={() => onChange([])}
        className={chipClass}
        data-ecosystem-chip="all"
      >
        {t('filters.allEcosystems')}
        {counts && <span className="tabular-nums text-[12px] text-[var(--text-muted)]">{number(total)}</span>}
      </Toggle>
      {chips.map(item => {
        const pressed = selected.includes(item.id)
        const name = labelFor?.(item.id) ?? ecosystemLabel(item.id)
        return (
          <Toggle
            key={item.id}
            type="button"
            pressed={pressed}
            onPressedChange={() => onChange(toggleEcosystem(selected, item.id))}
            className={chipClass}
            data-ecosystem-chip={item.id}
            aria-label={t('filters.ecosystemChip', { name, count: number(item.count) })}
          >
            {isEcosystemType(item.id) && <EcosystemIcon type={item.id} size={14} useColor decorative />}
            {name}
            <span className="tabular-nums text-[12px] text-[var(--text-muted)]">{number(item.count)}</span>
          </Toggle>
        )
      })}
    </div>
  )
}
