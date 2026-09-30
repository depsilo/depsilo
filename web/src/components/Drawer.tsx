import { type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { Sheet, SheetClose, SheetContent, SheetTitle } from '@/components/ui/sheet'
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
    <Sheet open={open} onOpenChange={onOpenChange} modal>
      <SheetContent
        side="left"
        showCloseButton={false}
        initialFocus={initialFocus}
        finalFocus
      >
        <SheetTitle className="sr-only">{title}</SheetTitle>
        {children}
        <SheetClose
          className="absolute top-2 right-2 active:scale-[0.96]"
          render={<IconButton icon="close" label={closeLabel} />}
        />
      </SheetContent>
    </Sheet>
  )
}
