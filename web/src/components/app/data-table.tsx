import { type Key, type ReactNode } from 'react'
import TableViewport from '@/components/app/table-viewport'

interface Column<T> {
  key: string
  label: string
  render?: (value: unknown, row: T, index: number) => ReactNode
}

interface DataTableV2Props<T> {
  columns: Column<T>[]
  data: T[]
  rowKey: (row: T, index: number) => Key
  ariaLabel: string
  minWidth?: number
}

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
                className="py-2 px-3 first:pl-0 text-left text-micro font-mono font-semibold uppercase text-muted-foreground"
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
                <td key={col.key} className="py-2 px-3 first:pl-0">
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
