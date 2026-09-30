import { type Key, type ReactNode } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
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
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((col) => (
              <TableHead key={col.key}>{col.label}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.map((row, rowIndex) => (
            <TableRow key={rowKey(row, rowIndex)}>
              {columns.map((col) => (
                <TableCell key={col.key}>
                  {col.render
                    ? col.render(row[col.key], row, rowIndex)
                    : (row[col.key] as ReactNode)}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableViewport>
  )
}
