import { X } from 'lucide-react'
import type { CSSProperties, ReactNode, RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import IconButton from '@/components/app/icon-button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from '@/components/ui/dialog'

interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  /** Maximum width in px. Long-form dialogs opt into a larger value. */
  width?: number
  initialFocus?: RefObject<HTMLElement | null>
  finalFocus?: RefObject<HTMLElement | null>
  /**
   * Keep the dialog open and undismissable. Mutations in flight use this so a
   * half-applied change can never be abandoned by pressing Escape.
   */
  closeDisabled?: boolean
}

/**
 * The product's one dialog. Base UI owns focus trapping, `aria-modal`, and
 * returning focus to the trigger; this component owns the title, the width,
 * and the labelled close action.
 */
export default function Modal({
  open,
  onClose,
  title,
  children,
  width = 440,
  initialFocus,
  finalFocus,
  closeDisabled = false,
}: ModalProps) {
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !closeDisabled) onClose()
      }}
      modal
    >
      <DialogContent
        showCloseButton={false}
        initialFocus={initialFocus}
        finalFocus={finalFocus ?? true}
        className="gap-4 p-5"
        style={{ maxWidth: width } as CSSProperties}
      >
        <DialogTitle className="pr-10 text-title font-semibold">{title}</DialogTitle>
        {children}
        <DialogClose
          render={
            <IconButton
              icon={X}
              label={closeLabel}
              disabled={closeDisabled}
              className="absolute top-2 right-2"
            />
          }
        />
      </DialogContent>
    </Dialog>
  )
}
