import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { TIME_PRESETS, type TimePreset, type TimeRangeValue } from '@/admin/listFilters'
import Icon from '@/components/Icon'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverClose, PopoverContent, PopoverHeader, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import { Toggle } from '@/components/ui/toggle'
import { cn } from '@/lib/utils'

interface Props {
  value: TimeRangeValue
  onChange: (value: TimeRangeValue) => void
  className?: string
}

const segmentClass = cn(
  'stripe-focus-ring min-h-10 shrink-0 rounded-sm px-2.5 text-[13px] font-[500] text-[var(--muted-foreground)] sm:min-h-8',
  'hover:bg-[var(--accent)] hover:text-[var(--foreground)]',
  'aria-pressed:bg-[var(--card)] aria-pressed:text-[var(--foreground)] aria-pressed:shadow-[var(--shadow-surface)]',
)

/** `2026-11-16T10:30` in local time, the value a datetime-local input takes. */
function toLocalInput(iso?: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function fromLocalInput(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

/**
 * The time filter of an Admin log list: one tap for the common windows, and
 * a custom start–end range in a popover. The custom button shows the chosen
 * range once applied.
 */
export default function TimeRangeControl({ value, onChange, className }: Props) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draftStart, setDraftStart] = useState('')
  const [draftEnd, setDraftEnd] = useState('')
  const custom = value.preset === 'custom' ? value : undefined
  const invalid = Boolean(draftStart && draftEnd && draftEnd < draftStart)
  const empty = !draftStart && !draftEnd

  const shortFormat = new Intl.DateTimeFormat(i18n.language, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  const customSummary = custom
    ? `${custom.start ? shortFormat.format(new Date(custom.start)) : '…'} – ${custom.end ? shortFormat.format(new Date(custom.end)) : t('filters.now')}`
    : t('filters.timeCustom')

  function openChange(next: boolean) {
    if (next) {
      setDraftStart(toLocalInput(custom?.start))
      setDraftEnd(toLocalInput(custom?.end))
    }
    setOpen(next)
  }

  function apply() {
    if (invalid || empty) return
    onChange({ preset: 'custom', start: fromLocalInput(draftStart), end: fromLocalInput(draftEnd) })
    setOpen(false)
  }

  const labels: Record<TimePreset, string> = {
    '1h': t('filters.time1h'),
    '24h': t('filters.time24h'),
    '7d': t('filters.time7d'),
    '30d': t('filters.time30d'),
    all: t('filters.timeAll'),
  }

  return (
    <div role="group" aria-label={t('filters.timeRange')} data-time-range className={cn('flex min-w-0 items-center gap-1', className)}>
      <div className="flex min-w-0 items-center gap-0.5 overflow-x-auto rounded-md bg-[var(--muted)] p-0.5">
        {TIME_PRESETS.map(preset => (
          <Toggle
            key={preset}
            type="button"
            pressed={value.preset === preset}
            onPressedChange={() => onChange({ preset })}
            className={segmentClass}
            data-time-preset={preset}
          >
            {labels[preset]}
          </Toggle>
        ))}
        <Popover open={open} onOpenChange={openChange}>
          <PopoverTrigger
            aria-pressed={Boolean(custom)}
            data-time-preset="custom"
            render={<Button variant="ghost" className={cn(segmentClass, 'h-auto gap-1 tabular-nums')} />}
          >
            <Icon name="calendar_month" size="sm" />
            <span className="max-w-[18ch] truncate sm:max-w-none">{customSummary}</span>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-[300px]" data-time-custom>
            <PopoverHeader>
              <PopoverTitle>{t('filters.timeCustomTitle')}</PopoverTitle>
              <PopoverClose aria-label={t('common.close')} render={<Button variant="ghost" size="icon" className="-mt-2 -mr-2 -mb-2 text-muted-foreground" />}>
                <Icon name="close" size="sm" />
              </PopoverClose>
            </PopoverHeader>
            <form
              className="flex flex-col gap-3"
              onSubmit={event => { event.preventDefault(); apply() }}
            >
              <label className="flex flex-col gap-1 text-[12px] font-[500] text-[var(--muted-foreground)]">
                {t('filters.timeStart')}
                <Input type="datetime-local" value={draftStart} max={draftEnd || undefined} onChange={event => setDraftStart(event.target.value)} />
              </label>
              <label className="flex flex-col gap-1 text-[12px] font-[500] text-[var(--muted-foreground)]">
                {t('filters.timeEnd')}
                <Input type="datetime-local" value={draftEnd} min={draftStart || undefined} onChange={event => setDraftEnd(event.target.value)} aria-invalid={invalid || undefined} />
              </label>
              <p className="m-0 text-[12px] text-[var(--muted-foreground)]" role={invalid ? 'alert' : undefined}>
                {invalid ? t('filters.timeEndBeforeStart') : t('filters.timeCustomHint')}
              </p>
              <div className="flex justify-end gap-2">
                <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>{t('cancel')}</Button>
                <Button type="submit" size="sm" disabled={invalid || empty}>{t('filters.apply')}</Button>
              </div>
            </form>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  )
}
