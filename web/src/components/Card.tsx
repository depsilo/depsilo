import { type ReactNode } from 'react'

interface CardProps {
  children?: ReactNode
  className?: string
  bordered?: boolean
}

export default function Card({
  children,
  className = '',
  bordered = false,
}: CardProps) {
  return (
    <div
      className={`bg-surface-low rounded-[0.25rem] p-5 ${bordered ? 'border border-outline-variant/15' : ''} ${className}`}
    >
      {children}
    </div>
  )
}
