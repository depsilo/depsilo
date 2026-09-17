import type { ReactNode } from 'react'
import ButtonV2 from '@/components/app/button'
import InlineNotice from '@/components/app/notice'
import ModalV2 from '@/components/app/modal'

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
    <ModalV2
      open={open}
      onClose={onClose}
      title={title}
      closeDisabled={pending}
    >
      <div className="space-y-5">
        <p className="text-body leading-6 text-muted-foreground">{description}</p>

        {details.length > 0 && (
          <dl className="space-y-2 border-y border-border py-3 text-label">
            {details.map((detail) => (
              <div key={detail.label} className="flex min-w-0 items-start justify-between gap-4">
                <dt className="shrink-0 text-muted-foreground">{detail.label}</dt>
                <dd
                  className={`min-w-0 break-words text-right text-foreground ${detail.mono ? 'font-mono' : ''}`}
                >
                  {detail.value}
                </dd>
              </div>
            ))}
          </dl>
        )}

        {errorMessage && <InlineNotice tone="danger">{errorMessage}</InlineNotice>}

        <div className="flex flex-wrap justify-end gap-3">
          <ButtonV2
            type="button"
            variant="secondary"
            className="min-h-10"
            disabled={pending}
            onClick={onClose}
          >
            {cancelLabel}
          </ButtonV2>
          <ButtonV2
            type="button"
            variant={confirmVariant}
            className="min-h-10"
            aria-busy={pending || undefined}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? pendingLabel : confirmLabel}
          </ButtonV2>
        </div>
      </div>
    </ModalV2>
  )
}
