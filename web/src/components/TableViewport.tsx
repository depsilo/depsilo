import { type ReactNode } from 'react'

interface TableViewportProps {
  label: string
  minWidth?: number
  children: ReactNode
}

export default function TableViewport({ label, minWidth = 720, children }: TableViewportProps) {
  return (
    <div
      data-table-viewport
      role="region"
      aria-label={label}
      tabIndex={0}
      className="stripe-focus-ring w-full overflow-x-auto"
    >
      <div style={{ minWidth }}>{children}</div>
    </div>
  )
}
