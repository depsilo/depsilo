import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Text,
  UNSTABLE_Toast as Toast,
  UNSTABLE_ToastContent as ToastContent,
  UNSTABLE_ToastQueue as ToastQueue,
  UNSTABLE_ToastRegion as ToastRegion,
} from 'react-aria-components'
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

// React Aria exports its toast primitives with an UNSTABLE_ prefix and drives
// them from an explicit queue rather than a provider plus a manager hook. The
// queue owns the visible limit and the auto-dismiss timeout that the old
// Toast.Provider configured.
const MAX_VISIBLE_TOASTS = 3
const TOAST_TIMEOUT_MS = 5000

function AppToastController({ children }: { children: ReactNode }) {
  const [queue] = useState(() => new ToastQueue<ToastPayload>({ maxVisibleToasts: MAX_VISIBLE_TOASTS }))
  const { i18n } = useTranslation()
  const closeLabel = i18n.language.startsWith('zh') ? '\u5173\u95ed' : 'Close'
  const api = useMemo<AppToastApi>(() => ({
    show: ({ tone, message }) => queue.add({ tone, message }, { timeout: TOAST_TIMEOUT_MS }),
    // No caller passes an empty id today; clearing every visible toast is the
    // honest reading of "close everything".
    close: (id) => { if (id) queue.close(id); else queue.clear() },
  }), [queue])

  return (
    <AppToastContext.Provider value={api}>
      {children}
      <ToastRegion queue={queue} className="app-toast-viewport">
        {({ toast }) => (
          <Toast toast={toast} data-toast-tone={toast.content.tone} className="app-toast-root">
            <ToastContent className="app-toast-content">
              <Text slot="description" className="app-toast-description">{toast.content.message}</Text>
              <IconButton
                icon="close"
                label={closeLabel}
                className="app-toast-close"
                onClick={() => queue.close(toast.key)}
              />
            </ToastContent>
          </Toast>
        )}
      </ToastRegion>
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
