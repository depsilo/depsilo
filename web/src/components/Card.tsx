import { type ReactNode } from 'react'

interface CardV2Props {
  children?: ReactNode
  className?: string
  noPad?: boolean
}

export default function CardV2({
  children,
  className = '',
  noPad = false,
}: CardV2Props) {
  return (
    <div
      className={`${noPad ? '' : 'p-5'} ${className}`}
      style={{
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-card)',
      }}
    >
      {children}
    </div>
  )
}
