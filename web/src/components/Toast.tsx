import { createContext, type ReactNode, useContext, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Toaster } from '@/components/ui/sonner'

export type ToastTone = 'success' | 'danger' | 'warning'

export interface ToastPayload {
  tone: ToastTone
  message: string
}

export interface AppToastApi {
  show(payload: ToastPayload): string
  close(id?: string): void
}

const AppToastContext = createContext<AppToastApi | null>(null)

function AppToastController({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const closeLabel = t('common.close')
  const api = useMemo<AppToastApi>(() => ({
    show: ({ tone, message }) => {
      const notify = tone === 'danger' ? toast.error : tone === 'warning' ? toast.warning : toast.success
      return String(notify(message))
    },
    close: (id) => {
      toast.dismiss(id)
    },
  }), [])

  return (
    <AppToastContext.Provider value={api}>
      {children}
      <Toaster
        toastOptions={{
          // Sonner styles the toast surface with an attribute selector, so the
          // Instrument tone borders need `!` to win the cascade.
          classNames: {
            success: 'border-[var(--ok-border)]!',
            error: 'border-[var(--danger-border)]!',
            warning: 'border-[var(--warn-border)]!',
          },
          closeButtonAriaLabel: closeLabel,
        }}
        containerAriaLabel={t('common.notifications')}
      />
    </AppToastContext.Provider>
  )
}

export function ToastProvider({ children }: { children: ReactNode }) {
  return <AppToastController>{children}</AppToastController>
}

// The provider and its consumer hook intentionally share this module API.
// eslint-disable-next-line react-refresh/only-export-components
export function useAppToast(): AppToastApi {
  const value = useContext(AppToastContext)
  if (!value) throw new Error('useAppToast must be used within ToastProvider')
  return value
}
