import { CircleCheck, Info, LoaderCircle, OctagonX, TriangleAlert } from 'lucide-react'
import { Toaster as Sonner, type ToasterProps } from 'sonner'
import { useResolvedTheme } from '@/lib/theme'

// shadcn/ui Toaster (Sonner). Reads Depsilo's resolved theme instead of
// shadcn's own theme hook and points the surface variables at the shadcn
// popover tokens.
function Toaster({ ...props }: ToasterProps) {
  const theme = useResolvedTheme()

  return (
    <Sonner
      theme={theme}
      className="toaster group"
      position="bottom-right"
      offset={16}
      gap={8}
      duration={5000}
      visibleToasts={3}
      closeButton
      icons={{
        success: <CircleCheck className="size-4" aria-hidden="true" />,
        info: <Info className="size-4" aria-hidden="true" />,
        warning: <TriangleAlert className="size-4" aria-hidden="true" />,
        error: <OctagonX className="size-4" aria-hidden="true" />,
        loading: <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />,
      }}
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-border': 'var(--border)',
          '--normal-text': 'var(--popover-foreground)',
          '--border-radius': 'var(--radius)',
          '--width': 'min(380px, calc(100vw - 32px))',
          fontFamily: 'var(--font-sans)',
          fontSize: '0.875rem',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
