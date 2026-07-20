import { ToastProvider } from '@/components/Toast'
import MainLayout from './components/MainLayout'

// The authenticated admin shell owns cross-page UI dependencies such as
// toasts and the responsive navigation drawer. Keeping them behind this lazy
// route seam avoids loading the management surface for /admin/login or while
// authentication is still unresolved.
export default function AdminShell() {
  return (
    <ToastProvider>
      <MainLayout />
    </ToastProvider>
  )
}
