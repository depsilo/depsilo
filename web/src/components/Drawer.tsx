import { Dialog, Heading, Modal } from 'react-aria-components'
import { useEffect, type ReactNode, type RefObject } from 'react'
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

  // See Modal: React Aria has no initialFocus prop, so the wrapper focuses it
  // after React Aria's own FocusScope has run.
  useEffect(() => {
    if (open && initialFocus?.current) initialFocus.current.focus()
  }, [open, initialFocus])

  return (
    <Modal
      isOpen={open}
      onOpenChange={onOpenChange}
      className="app-dialog-backdrop app-drawer-backdrop"
    >
      <Dialog className="app-drawer-popup">
        <Heading slot="title" className="sr-only">{title}</Heading>
        {children}
        <IconButton
          icon="close"
          label={closeLabel}
          onClick={() => onOpenChange(false)}
          className="app-drawer-close active:scale-[0.96]"
        />
      </Dialog>
    </Modal>
  )
}
