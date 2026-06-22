import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'

interface ProRequiredCalloutProps {
  /** Material symbol name (e.g. "lock", "shield", "security", "folder_managed") */
  icon: string
  /** Already-translated title text */
  title: string
  /** Already-translated description text */
  description: string
  /** Already-translated upgrade button label */
  upgradeLabel: string
  /** Where the upgrade button links to. Defaults to depsilo.com/#pricing. */
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
  upgradeHref = 'https://depsilo.com/#pricing',
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
        <a href={upgradeHref} target="_blank" rel="noopener noreferrer">
          <ButtonV2>{upgradeLabel}</ButtonV2>
        </a>
      </div>
    </div>
  )
}
