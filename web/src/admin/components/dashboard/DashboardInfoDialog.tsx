import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import type { ReactNode } from 'react'



export interface InfoDialogSection {
  title: string
  body: ReactNode
}

interface DashboardInfoDialogProps {
  open: boolean
  onClose: () => void
  title: string
  description?: ReactNode
  sections?: InfoDialogSection[]
  children?: ReactNode
  footer?: ReactNode
  width?: number
}

/**
 * Read-only centered detail dialog shared by the Overview resource, traffic,
 * benefits, and problem explanations. It composes the standard Base UI Dialog,
 * so Esc, focus trapping, focus return, and backdrop dismissal come
 * from the same accessibility-tested primitive as the rest of Admin.
 */
export default function DashboardInfoDialog({
  open,
  onClose,
  title,
  description,
  sections = [],
  children,
  footer,
  width = 560,
}: DashboardInfoDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose() }} modal>
      <DialogContent style={{ maxWidth: width }} finalFocus={true}>
        <DialogTitle className="pr-10">{title}</DialogTitle>
        <div data-dashboard-info-dialog className="flex flex-col gap-4">
          {description && (
            <p className="text-[14px] leading-[1.6]" style={{ color: 'var(--dash-muted, var(--text-soft))' }}>
              {description}
            </p>
          )}
          {sections.map(section => (
            <section key={section.title} className="flex flex-col gap-1.5">
              <h3 className="text-[14px] font-semibold" style={{ color: 'var(--text)' }}>{section.title}</h3>
              <div className="text-[14px] leading-[1.65]" style={{ color: 'var(--text-soft)' }}>{section.body}</div>
            </section>
          ))}
          {children}
          {footer && <div className="mt-1 flex flex-wrap justify-end gap-2">{footer}</div>}
        </div>
      </DialogContent>
    </Dialog>
  )
}
