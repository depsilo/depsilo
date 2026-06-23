import { type ReactNode } from 'react'

interface Column<T> {
  key: string
  label: string
  render?: (value: unknown, row: T, index: number) => ReactNode
}

interface DataTableV2Props<T> {
  columns: Column<T>[]
  data: T[]
  onRowClick?: (row: T, index: number) => void
}

// Bare table: no card wrapper, no outer border. Headers are eyebrow-style
// (10px mono caps), rows are separated by a soft 1px border and lift on
// hover. Matches the in-page tables used by AccessLogs / CacheManage etc.
export default function DataTableV2<T extends Record<string, unknown>>({
  columns,
  data,
  onRowClick,
}: DataTableV2Props<T>) {
  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full text-[12px]">
        <thead>
          <tr style={{ borderBottom: '1px solid var(--border)' }}>
            {columns.map((col) => (
              <th
                key={col.key}
                className="text-left text-[10px] font-mono font-[600] uppercase tracking-[0.08em] py-2 px-3 first:pl-0"
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
              key={rowIndex}
              onClick={() => onRowClick?.(row, rowIndex)}
              className={`transition-colors duration-100 hover:bg-[var(--bg-soft)] ${onRowClick ? 'cursor-pointer' : ''}`}
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
    </div>
  )
}
