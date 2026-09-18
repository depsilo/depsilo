import { createContext, type ReactNode, useContext, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { ToastProvider as UiToastProvider, ToastViewport, useToastManager } from '@/components/ui/toast'

export type { ToastTone } from '@/components/ui/toast'

export interface ToastPayload {
  tone: 'success' | 'destructive' | 'warning'
  message: string
}

export interface AppToastApi {
  show(payload: ToastPayload): string
  close(id?: string): void
}

const AppToastContext = createContext<AppToastApi | null>(null)

function AppToastController({ children }: { children: ReactNode }) {
  const manager = useToastManager()
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'

  const api = useMemo<AppToastApi>(() => ({
    show: ({ tone, message }) => manager.add({
      type: tone,
      description: message,
      // A failure the Operator may act on must not be cleared by an unrelated
      // success toast arriving later.
      priority: tone === 'destructive' ? 'high' : 'low',
    }),
    close: manager.close,
  }), [manager])

  return (
    <AppToastContext.Provider value={api}>
      {children}
      <ToastViewport closeLabel={closeLabel} />
    </AppToastContext.Provider>
  )
}

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <UiToastProvider>
      <AppToastController>{children}</AppToastController>
    </UiToastProvider>
  )
}

// The provider and its consumer hook intentionally share this module API.
// eslint-disable-next-line react-refresh/only-export-components
export function useAppToast(): AppToastApi {
  const value = useContext(AppToastContext)
  if (!value) throw new Error('useAppToast must be used within ToastProvider')
  return value
}
