import { Toast } from '@base-ui/react/toast'
import { createContext, type ReactNode, useContext, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import IconButton from './IconButton'

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
  const manager = Toast.useToastManager()
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'
  const api = useMemo<AppToastApi>(() => ({
    show: ({ tone, message }) => manager.add({
      type: tone,
      description: message,
      priority: tone === 'danger' ? 'high' : 'low',
    }),
    close: manager.close,
  }), [manager])

  return (
    <AppToastContext.Provider value={api}>
      {children}
      <Toast.Viewport className="app-toast-viewport">
        {manager.toasts.map((toast) => (
          <Toast.Root
            key={toast.id}
            toast={toast}
            data-toast-tone={toast.type}
            className="app-toast-root"
          >
            <Toast.Content className="app-toast-content">
              <Toast.Description className="app-toast-description" />
              <Toast.Close
                render={
                  <IconButton
                    icon="close"
                    label={closeLabel}
                    className="app-toast-close"
                  />
                }
              />
            </Toast.Content>
          </Toast.Root>
        ))}
      </Toast.Viewport>
    </AppToastContext.Provider>
  )
}

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <Toast.Provider limit={3} timeout={5000}>
      <AppToastController>{children}</AppToastController>
    </Toast.Provider>
  )
}

// The provider and its consumer hook intentionally share this module API.
// eslint-disable-next-line react-refresh/only-export-components
export function useAppToast(): AppToastApi {
  const value = useContext(AppToastContext)
  if (!value) throw new Error('useAppToast must be used within ToastProvider')
  return value
}
