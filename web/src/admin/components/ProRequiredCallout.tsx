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
      className="rounded-lg border border-border bg-accent py-12 text-center"
    >
      <div className="flex flex-col items-center gap-4">
        <div
          className="flex size-14 items-center justify-center rounded-md border border-border bg-accent"
        >
          <Icon icon={icon} className="text-primary" size="lg" />
        </div>
        <h3 className="text-subhead font-semibold text-foreground">
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
