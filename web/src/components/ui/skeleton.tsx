import { mergeProps } from '@base-ui/react/merge-props'
import { useRender } from '@base-ui/react/use-render'
import { cn } from '@/lib/utils'

function Skeleton({ className, render, ...props }: useRender.ComponentProps<'div'>) {
  return useRender({
    defaultTagName: 'div', render, state: { slot: 'skeleton' },
    props: mergeProps<'div'>({ 'aria-hidden': true, className: cn('motion-safe:animate-pulse rounded-md bg-muted', className) }, props),
  })
}

export { Skeleton }
