import type { ReactNode, RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import IconButton from '@/components/app/icon-button'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetTitle,
} from '@/components/ui/sheet'

interface DrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  children: ReactNode
  initialFocus?: RefObject<HTMLElement | null>
}

/**
 * Edge drawer for narrow-viewport navigation. It shares the Base UI dialog
 * primitive with `Modal` but keeps its own layout, so the drawer can own the
 * viewport edge without the centred dialog inheriting that behaviour.
 */
export default function Drawer({
  open,
  onOpenChange,
  title,
  children,
  initialFocus,
}: DrawerProps) {
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal>
      <SheetContent
        side="left"
        showCloseButton={false}
        initialFocus={initialFocus}
        className="w-[min(320px,calc(100vw-40px))] gap-0 p-0 sm:max-w-[320px]"
      >
        <SheetTitle className="sr-only">{title}</SheetTitle>
        {children}
        <SheetClose
          render={
            <IconButton icon="close" label={closeLabel} className="absolute top-3 right-3" />
          }
        />
      </SheetContent>
    </Sheet>
  )
}
