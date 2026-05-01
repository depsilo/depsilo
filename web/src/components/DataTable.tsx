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

export default function DataTableV2<T extends Record<string, unknown>>({
  columns,
  data,
  onRowClick,
}: DataTableV2Props<T>) {
  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full text-[14px]">
        <thead>
          <tr style={{ borderBottom: '1px solid var(--border)' }}>
            {columns.map((col) => (
              <th
                key={col.key}
                className="text-left text-[12px] font-[400] uppercase tracking-wider py-3 px-4"
                style={{ color: 'var(--text-soft)' }}
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
              className={`transition-colors duration-100 ${onRowClick ? 'cursor-pointer' : ''}`}
              style={{
                borderBottom: '1px solid var(--border)',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = 'var(--bg-soft)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = 'transparent'
              }}
            >
              {columns.map((col) => (
                <td key={col.key} className="py-3 px-4">
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
