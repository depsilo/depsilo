import { cn } from '@/lib/utils'

// A table has one named, keyboard-focusable scroll region. Kept separate from
// Table so empty/error states never create an empty scroll target.
function TableContainer({ label, minWidth = 720, children, className, ...props }: React.ComponentProps<'div'> & { label: string; minWidth?: number }) {
  return (
    <div data-table-viewport role="region" aria-label={label} tabIndex={0} className={cn('stripe-focus-ring w-full overflow-x-auto', className)} {...props}>
      <div style={{ minWidth }}>{children}</div>
    </div>
  )
}

// shadcn/ui Table. The scroll container is deliberately not part of `Table`:
// data tables in this app are wrapped in `TableContainer`, which owns the
// labelled, focusable region the accessibility contract depends on.
function Table({ className, ...props }: React.ComponentProps<'table'>) {
  return (
    <table
      data-slot="table"
      className={cn('w-full caption-bottom text-sm', className)}
      {...props}
    />
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return (
    <thead
      data-slot="table-header"
      className={cn('[&_tr]:border-b', className)}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  return (
    <tbody
      data-slot="table-body"
      className={cn('[&_tr:last-child]:border-0', className)}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<'tfoot'>) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn('border-t border-border bg-muted/50 font-medium', className)}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        'border-b border-border transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted',
        className,
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        'h-10 px-2 text-left align-middle font-medium whitespace-nowrap text-foreground',
        className,
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return (
    <td
      data-slot="table-cell"
      className={cn('p-2 align-middle whitespace-nowrap', className)}
      {...props}
    />
  )
}

function TableCaption({ className, ...props }: React.ComponentProps<'caption'>) {
  return (
    <caption
      data-slot="table-caption"
      className={cn('mt-4 text-sm text-muted-foreground', className)}
      {...props}
    />
  )
}

export {
  TableContainer,
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
}
