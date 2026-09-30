import { type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog, DialogClose, DialogContent, DialogTitle } from '@/components/ui/dialog'
import IconButton from './IconButton'

interface ModalV2Props {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  width?: number
  initialFocus?: RefObject<HTMLElement | null>
  finalFocus?: RefObject<HTMLElement | null>
  closeDisabled?: boolean
}

export default function ModalV2({
  open,
  onClose,
  title,
  children,
  width = 440,
  initialFocus,
  finalFocus,
  closeDisabled = false,
}: ModalV2Props) {
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => !nextOpen && !closeDisabled && onClose()}
      modal
    >
      <DialogContent
        style={{ maxWidth: width }}
        initialFocus={initialFocus}
        finalFocus={finalFocus ?? true}
        showCloseButton={false}
      >
        <DialogTitle className="pr-10">{title}</DialogTitle>
        {children}
        {/* Rendered last on purpose: the close action is the final tabbable
            element, so Shift+Tab from the first field lands on it. */}
        <DialogClose
          className="absolute top-3 right-3 active:scale-[0.96]"
          render={
            <IconButton
              icon="close"
              label={closeLabel}
              disabled={closeDisabled}
            />
          }
        />
      </DialogContent>
    </Dialog>
  )
}
