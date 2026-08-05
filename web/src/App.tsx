import { BrowserRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { lazyRoute } from '@/routing/lazyRoute'
import SetupGate from '@/setup/SetupGate'

const PortalApp = lazyRoute(() => import('@/portal/PortalApp'), { surface: 'page' })
const AdminApp = lazyRoute(() => import('@/admin/AdminApp'), { surface: 'page' })

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

function ApplicationRoutes() {
  return (
    <Routes>
      <Route path="/admin/*" element={<AdminApp />} />
      <Route path="/*" element={<PortalApp />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <div className="page-wash" />
        <SetupGate>
          <ApplicationRoutes />
        </SetupGate>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
