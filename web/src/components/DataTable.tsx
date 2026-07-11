import { type Key, type ReactNode } from 'react'
import TableViewport from './TableViewport'

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
      <table className="w-full text-[12px]">
        <thead>
          <tr style={{ borderBottom: '1px solid var(--border)' }}>
            {columns.map((col) => (
              <th
                key={col.key}
                className="py-2 px-3 first:pl-0 text-left text-[10px] font-mono font-[600] uppercase"
                style={{ color: 'var(--text-subtle)' }}
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
              className="transition-colors duration-100 hover:bg-[var(--bg-soft)]"
              style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
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
