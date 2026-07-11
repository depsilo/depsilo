import { Dialog } from '@base-ui/react/dialog'
import { type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import IconButton from './IconButton'

interface DrawerV2Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  children: ReactNode
  initialFocus?: RefObject<HTMLElement | null>
}

export default function DrawerV2({ open, onOpenChange, title, children, initialFocus }: DrawerV2Props) {
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange} modal>
      <Dialog.Portal>
        <Dialog.Backdrop className="app-dialog-backdrop app-drawer-backdrop" />
        <Dialog.Viewport className="app-drawer-viewport">
          <Dialog.Popup className="app-drawer-popup" initialFocus={initialFocus} finalFocus>
            <Dialog.Title className="sr-only">{title}</Dialog.Title>
            {children}
            <Dialog.Close
              render={
                <IconButton
                  icon="close"
                  label={closeLabel}
                  className="app-drawer-close active:scale-[0.96]"
                />
              }
            />
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
