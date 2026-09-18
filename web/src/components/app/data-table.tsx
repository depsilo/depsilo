import { type Key, type ReactNode } from 'react'
import TableViewport from '@/components/app/table-viewport'

interface Column<T> {
  key: string
  label: string
  render?: (value: unknown, row: T, index: number) => ReactNode
  /**
   * Numbers are end-aligned so a column of measurements lines up on the
   * digits; text is start-aligned. The header follows its column, otherwise
   * the label sits on the wrong side of the values it names.
   */
  align?: 'start' | 'end'
}

interface DataTableV2Props<T> {
  columns: Column<T>[]
  data: T[]
  rowKey: (row: T, index: number) => Key
  ariaLabel: string
  minWidth?: number
}

/**
 * Dense tabular data.
 *
 * The table scrolls horizontally inside its own named, focusable region rather
 * than stacking into a list. That is deliberate: at 390px a log row is
 * compared column-by-column with the rows above it, and a stacked list
 * destroys the comparison. A surface whose rows carry an essential action —
 * Quarantine's decisions, the metadata-refresh episodes — opts into a divided
 * list instead, because there the action has to stay reachable.
 *
 * Rows are never focusable; the row's action is. That is asserted across every
 * table in `admin-tables-actions.spec.ts`.
 */
export default function DataTableV2<T extends Record<string, unknown>>({
  columns,
  data,
  rowKey,
  ariaLabel,
  minWidth,
}: DataTableV2Props<T>) {
  return (
    <TableViewport label={ariaLabel} minWidth={minWidth}>
      <table className="w-full text-label">
        <thead>
          <tr className="border-b border-border">
            {columns.map((col) => (
              <th
                key={col.key}
                scope="col"
                className={`py-2 px-3 first:pl-0 text-micro font-mono font-semibold uppercase text-muted-foreground ${
                  col.align === 'end' ? 'text-right' : 'text-left'
                }`}
              >
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row, rowIndex) => (
            <tr
              key={rowKey(row, rowIndex)}
              className="transition-colors duration-100 hover:bg-muted border-b border-border"
            >
              {columns.map((col) => (
                <td
                  key={col.key}
                  className={`py-2 px-3 first:pl-0 ${col.align === 'end' ? 'text-right' : ''}`}
                >
                  {col.render
                    ? col.render(row[col.key], row, rowIndex)
                    : (row[col.key] as ReactNode)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </TableViewport>
  )
}
