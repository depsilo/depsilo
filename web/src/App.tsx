import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import PortalApp from '@/portal/PortalApp'
import AdminApp from '@/admin/AdminApp'
import { setupApi } from '@/lib/api'
import SetupWizard from '@/setup/SetupWizard'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

function AppRoutes() {
  const { data: setupData, isLoading: setupLoading } = useQuery({
    queryKey: ['setup-status'],
    queryFn: () => setupApi.getStatus(),
    retry: false,
    staleTime: Infinity, // only check once per session
  })

  if (setupLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
        <div style={{ color: 'var(--body)' }}>Loading...</div>
      </div>
    )
  }

  if (setupData?.data?.needs_setup) {
    return <SetupWizard />
  }

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
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  )
}
