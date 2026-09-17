import Icon from '@/components/app/icon'
import type { LucideIcon } from 'lucide-react'
import { proAccessUrl } from '@/lib/buy'

interface ProRequiredCalloutProps {
  /** Registered project icon name. */
  icon: LucideIcon
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
      style={{ background: 'var(--accent)', border: '0.5px solid var(--border)' }}
    >
      <div className="flex flex-col items-center gap-4">
        <div
          className="flex items-center justify-center w-14 h-14 rounded-[8px]"
          style={{ background: 'var(--accent)', border: '0.5px solid var(--border)' }}
        >
          <Icon icon={icon} className="text-primary" size="lg" />
        </div>
        <h3 className="text-subhead font-semibold" style={{ color: 'var(--foreground)', letterSpacing: '-0.02em' }}>
          {title}
        </h3>
        <p className="text-body max-w-md text-muted-foreground">
          {description}
        </p>
        <a
          href={upgradeHref}
          className="inline-flex min-h-9 items-center justify-center rounded-[5px] bg-primary px-3 py-1.5 text-body font-medium text-primary-foreground no-underline transition-colors hover:bg-primary/80 pointer-coarse:min-h-10"
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
