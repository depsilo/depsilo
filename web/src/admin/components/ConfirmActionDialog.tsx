import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import type { ReactNode } from 'react'
import { Button } from '@/components/ui/button'

export interface ConfirmActionDetail {
  label: string
  value: ReactNode
  mono?: boolean
}

interface ConfirmActionDialogProps {
  open: boolean
  title: string
  description: ReactNode
  details?: readonly ConfirmActionDetail[]
  cancelLabel: string
  confirmLabel: string
  pendingLabel: string
  confirmVariant?: 'primary' | 'danger'
  pending: boolean
  errorMessage?: string | null
  onClose: () => void
  onConfirm: () => void
}

export default function ConfirmActionDialog({
  open,
  title,
  description,
  details = [],
  cancelLabel,
  confirmLabel,
  pendingLabel,
  confirmVariant = 'danger',
  pending,
  errorMessage,
  onClose,
  onConfirm,
}: ConfirmActionDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen && !pending) onClose() }} modal>
      <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={pending}>
        <DialogTitle className="pr-10">{title}</DialogTitle>
        <div className="space-y-5">
          <p className="text-[14px] leading-6 text-[var(--text-soft)]">{description}</p>

          {details.length > 0 && (
            <dl className="space-y-2 border-y border-[var(--border)] py-3 text-[12px]">
              {details.map((detail) => (
                <div key={detail.label} className="flex min-w-0 items-start justify-between gap-4">
                  <dt className="shrink-0 text-[var(--text-muted)]">{detail.label}</dt>
                  <dd
                    className={`min-w-0 break-words text-right text-[var(--text)] ${detail.mono ? 'font-mono' : ''}`}
                  >
                    {detail.value}
                  </dd>
                </div>
              ))}
            </dl>
          )}

          {errorMessage && <Alert variant={"destructive"}><AlertDescription>{errorMessage}</AlertDescription></Alert>}

          <div className="flex flex-wrap justify-end gap-3">
            <Button
              type="button"
              variant="outline"
              className="min-h-10"
              disabled={pending}
              onClick={onClose}
            >
              {cancelLabel}
            </Button>
            <Button
              type="button"
              variant={confirmVariant === 'danger' ? 'destructive' : 'default'}
              className="min-h-10"
              aria-busy={pending || undefined}
              disabled={pending}
              onClick={onConfirm}
            >
              {pending ? pendingLabel : confirmLabel}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
