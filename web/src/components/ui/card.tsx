import { mergeProps } from '@base-ui/react/merge-props'
import { useRender } from '@base-ui/react/use-render'
import { cn } from '@/lib/utils'

// shadcn Card composition with Base UI rendering so labelled sections keep
// their document semantics. Surface styling has a single owner here.
function Card({ className, render, ...props }: useRender.ComponentProps<'div'>) {
  return useRender({
    defaultTagName: 'div', render, state: { slot: 'card' },
    props: mergeProps<'div'>({ className: cn('flex min-w-0 flex-col gap-4 rounded-lg border border-border bg-card py-4 text-card-foreground', className) }, props),
  })
}

function CardHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="card-header" className={cn('flex flex-wrap items-start justify-between gap-2 px-5', className)} {...props} />
}

function CardTitle({ className, ...props }: React.ComponentProps<'h2'>) {
  return <h2 data-slot="card-title" className={cn('text-[16px] font-semibold leading-snug', className)} {...props} />
}

function CardDescription({ className, ...props }: React.ComponentProps<'p'>) {
  return <p data-slot="card-description" className={cn('text-[13px] leading-relaxed text-muted-foreground', className)} {...props} />
}

function CardContent({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="card-content" className={cn('min-w-0 px-5', className)} {...props} />
}

function CardFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="card-footer" className={cn('flex flex-wrap items-center gap-2 px-5', className)} {...props} />
}

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter }
