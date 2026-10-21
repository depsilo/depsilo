import Icon, { type IconName } from '@/components/Icon'

/** The empty trend and request tail share one compact, stretchable body. */
export default function DashboardEmptyState({ icon, title, description }: {
  icon: IconName
  title: string
  description: string
}) {
  return (
    <div className="flex min-h-[112px] min-w-0 flex-1 flex-col items-center justify-center gap-1.5 px-5 pb-5 pt-2 text-center">
      <span aria-hidden className="grid size-8 shrink-0 place-items-center rounded-full" style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}>
        <Icon name={icon} size="sm" />
      </span>
      <h3 className="text-[14px] font-medium leading-snug" style={{ color: 'var(--dash-ink)' }}>{title}</h3>
      <p className="max-w-[48ch] text-[13px] leading-relaxed" style={{ color: 'var(--dash-muted)' }}>{description}</p>
    </div>
  )
}
