import type { ReactNode } from 'react'

export type AdminPageWidth = 'fluid' | 'readable'

interface AdminPageProps {
  children: ReactNode
  description?: ReactNode
  actions?: ReactNode
  width?: AdminPageWidth
}

const widthClasses: Record<AdminPageWidth, string> = {
  fluid: 'max-w-[1840px]',
  readable: 'max-w-3xl',
}

/**
 * Shared page chrome below the Admin shell title. It owns readable width,
 * introductory copy, responsive actions, and the page-content seam.
 */
export default function AdminPage({
  children,
  description,
  actions,
  width = 'fluid',
}: AdminPageProps) {
  const hasHeader = Boolean(description || actions)

  return (
    <div
      data-admin-page
      data-admin-page-width={width}
      className={`mx-auto min-w-0 w-full ${widthClasses[width]}`}
    >
      {hasHeader && (
        <header
          data-admin-page-header
          className="mb-8 flex min-w-0 flex-col gap-3 border-b border-[var(--border)] pb-4 sm:flex-row sm:items-end sm:justify-between"
        >
          {description && (
            <div data-admin-page-description className="max-w-2xl text-[13px] leading-5" style={{ color: 'var(--text-soft)' }}>
              {description}
            </div>
          )}
          {actions && (
            <div data-admin-page-actions className="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:justify-end">
              {actions}
            </div>
          )}
        </header>
      )}
      <div data-admin-page-content className="min-w-0">
        {children}
      </div>
    </div>
  )
}
