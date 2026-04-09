import Icon from './Icon'

interface EmptyStateV2Props {
  icon?: string
  title: string
  description?: string
  action?: React.ReactNode
}

export default function EmptyStateV2({ icon = 'inbox', title, description, action }: EmptyStateV2Props) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-8">
      <div
        className="flex items-center justify-center w-12 h-12 rounded-[8px] mb-4"
        style={{ background: 'var(--surface-low)', color: 'var(--body)' }}
      >
        <Icon name={icon} size="lg" />
      </div>
      <h3 className="text-[16px] font-[400] mb-1" style={{ color: 'var(--heading)' }}>
        {title}
      </h3>
      {description && (
        <p className="text-[14px] text-center max-w-sm" style={{ color: 'var(--body)' }}>
          {description}
        </p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}
