import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

interface AdminPaginationProps {
  page: number
  pageSize: number
  total: number
  /** Only a completed, successful query can establish the current page bounds. */
  ready: boolean
  onPageChange: (page: number) => void
}

function positiveInteger(value: number, fallback: number) {
  if (!Number.isFinite(value) || value <= 0) return fallback
  return Math.floor(value)
}

/**
 * Pagination for Admin list results. The module owns page bounds, result
 * ranges, localized labels, and the narrow-screen layout.
 */
export default function AdminPagination({
  page,
  pageSize,
  total,
  ready,
  onPageChange,
}: AdminPaginationProps) {
  const { t } = useTranslation()
  const safePageSize = positiveInteger(pageSize, 1)
  const safeTotal = Number.isFinite(total) && total > 0 ? Math.floor(total) : 0
  const totalPages = Math.max(1, Math.ceil(safeTotal / safePageSize))
  const currentPage = Math.min(positiveInteger(page, 1), totalPages)

  useEffect(() => {
    if (ready && page !== currentPage) onPageChange(currentPage)
  }, [ready, page, currentPage, onPageChange])

  if (totalPages <= 1) return null

  const from = ((currentPage - 1) * safePageSize) + 1
  const to = Math.min(currentPage * safePageSize, safeTotal)

  function changePage(nextPage: number) {
    const boundedPage = Math.min(Math.max(Math.floor(nextPage), 1), totalPages)
    if (boundedPage !== currentPage) onPageChange(boundedPage)
  }

  return (
    <nav
      data-admin-pagination
      aria-label={t('pagination.label')}
      className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
        {t('pagination.summary', { from, to, total: safeTotal })}
      </p>
      <div className="flex flex-wrap items-center justify-between gap-2 sm:justify-end">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={currentPage <= 1}
          onClick={() => changePage(currentPage - 1)}
        >
          {t('prevPage')}
        </Button>
        <span
          aria-live="polite"
          className="px-1 text-[12px] font-mono tabular-nums"
          style={{ color: 'var(--text-soft)' }}
        >
          {t('pagination.page', { page: currentPage, totalPages })}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={currentPage >= totalPages}
          onClick={() => changePage(currentPage + 1)}
        >
          {t('nextPage')}
        </Button>
      </div>
    </nav>
  )
}
