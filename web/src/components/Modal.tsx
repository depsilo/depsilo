import { Dialog } from '@base-ui/react/dialog'
import { type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import IconButton from './IconButton'

interface ModalV2Props {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  width?: number
  initialFocus?: RefObject<HTMLElement | null>
  finalFocus?: RefObject<HTMLElement | null>
}

export default function ModalV2({
  open,
  onClose,
  title,
  children,
  width = 440,
  initialFocus,
  finalFocus,
}: ModalV2Props) {
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  return (
    <Dialog.Root open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()} modal>
      <Dialog.Portal>
        <Dialog.Backdrop className="app-dialog-backdrop" />
        <Dialog.Viewport className="app-dialog-viewport">
          <Dialog.Popup
            className="modal-card app-dialog-popup"
            style={{ maxWidth: width }}
            initialFocus={initialFocus}
            finalFocus={finalFocus ?? true}
          >
            <Dialog.Title className="app-dialog-title">{title}</Dialog.Title>
            {children}
            <Dialog.Close
              render={
                <IconButton
                  icon="close"
                  label={closeLabel}
                  className="app-dialog-close active:scale-[0.96]"
                />
              }
            />
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
