import { Dialog, Heading, Modal } from 'react-aria-components'
import { useEffect, useRef, type ReactNode, type RefObject } from 'react'
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
  closeDisabled?: boolean
}

// React Aria's Modal does not auto-focus, so the wrapper supplies the previous
// behaviour: focus the first control the dialog exposes.
function firstFocusable(root: HTMLElement | null): HTMLElement | null {
  if (!root) return null
  return root.querySelector<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )
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

  // React Aria's Modal contains focus and restores the trigger, but it has no
  // auto-focus and exposes neither initialFocus nor finalFocus. Base UI's Modal
  // focused the first control; the wrapper reproduces that, and lets a caller
  // override it.
  const dialogRef = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!open) return
    const target = initialFocus?.current ?? firstFocusable(dialogRef.current)
    target?.focus()
  }, [open, initialFocus])
  useEffect(() => {
    if (!open && finalFocus?.current) finalFocus.current.focus()
  }, [open, finalFocus])

  return (
    <Modal
      isOpen={open}
      onOpenChange={(nextOpen) => { if (!nextOpen && !closeDisabled) onClose() }}
      isDismissable={!closeDisabled}
      isKeyboardDismissDisabled={closeDisabled}
      className="app-dialog-backdrop app-dialog-viewport"
    >
      <Dialog ref={dialogRef} className="modal-card app-dialog-popup" style={{ maxWidth: width }}>
        <Heading slot="title" className="app-dialog-title">{title}</Heading>
        {children}
        <IconButton
          icon="close"
          label={closeLabel}
          disabled={closeDisabled}
          onClick={onClose}
          className="app-dialog-close active:scale-[0.96]"
        />
      </Dialog>
    </Modal>
  )
}
