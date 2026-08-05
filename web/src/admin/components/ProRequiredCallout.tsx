import Icon from '@/components/Icon'
import { proAccessUrl } from '@/lib/buy'

interface ProRequiredCalloutProps {
  /** Material symbol name (e.g. "lock", "shield", "security", "folder_managed") */
  icon: string
  /** Already-translated title text */
  title: string
  /** Already-translated description text */
  description: string
  /** Already-translated CTA button label */
  upgradeLabel: string
  /**
   * Where the CTA links to. Defaults to the neutral Pro access enquiry
   * resolved from `lib/buy.ts`.
   */
  upgradeHref?: string
}

/**
 * Shown on Pro-only admin pages when the backend returns 402.
 * Strings are passed pre-translated so the component stays
 * i18n-framework-agnostic.
 */
export default function ProRequiredCallout({
  icon,
  title,
  description,
  upgradeLabel,
  upgradeHref = proAccessUrl(),
}: ProRequiredCalloutProps) {
  return (
    <div
      className="text-center py-12 rounded-[10px]"
      style={{ background: 'var(--brand-soft)', border: '0.5px solid var(--brand-border)' }}
    >
      <div className="flex flex-col items-center gap-4">
        <div
          className="flex items-center justify-center w-14 h-14 rounded-[8px]"
          style={{ background: 'var(--brand-soft)', border: '0.5px solid var(--brand-border)' }}
        >
          <Icon name={icon} size="lg" style={{ color: 'var(--brand)' }} />
        </div>
        <h3 className="text-[18px] font-[600]" style={{ color: 'var(--text)', letterSpacing: '-0.02em' }}>
          {title}
        </h3>
        <p className="text-[14px] max-w-md" style={{ color: 'var(--text-soft)' }}>
          {description}
        </p>
        <a
          href={upgradeHref}
          className="app-button stripe-focus-ring inline-flex min-h-9 items-center justify-center rounded-[5px] bg-[var(--btn)] px-3 py-1.5 text-[13px] font-[500] no-underline text-[var(--btn-fg)] transition-[background,color,transform] duration-150 hover:bg-[var(--btn-press)] active:scale-[0.96]"
          style={{
            boxShadow: 'inset 0 1px 0 color-mix(in oklab, white 16%, transparent), 0 1px 2px rgba(0, 0, 0, 0.18)',
          }}
        >
          {upgradeLabel}
        </a>
      </div>
    </div>
  )
}
