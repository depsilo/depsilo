import { type ReactNode } from 'react'

interface CardV2Props {
  children?: ReactNode
  className?: string
  elevated?: boolean
  noPad?: boolean
}

export default function CardV2({
  children,
  className = '',
  elevated = false,
  noPad = false,
}: CardV2Props) {
  return (
    <div
      className={`rounded-[5px] ${noPad ? '' : 'p-5'} ${className}`}
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        boxShadow: elevated ? 'var(--shadow-elevated)' : 'var(--shadow-soft)',
      }}
    >
      {children}
    </div>
  )
}
