import { useEffect, useReducer, type ReactElement } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { usePrincipal } from '@/hooks/usePrincipal'
import QueryErrorState from '@/components/QueryErrorState'
import { useTranslation } from 'react-i18next'
import { AUTH_SESSION_EXPIRED_EVENT } from '@/lib/api'
import { lazyRoute } from '@/routing/lazyRoute'
import RouteNotFound from '@/routing/RouteNotFound'
import { adminRouteManifest, type AdminRouteId } from './routes'

const LoginV2 = lazyRoute(() => import('./pages/Login'), { surface: 'page' })
const AdminShell = lazyRoute(() => import('./AdminShell'), { surface: 'page' })
const DashboardV2 = lazyRoute(() => import('./pages/Dashboard'))
const Attention = lazyRoute(() => import('./pages/Attention'))
const BandwidthReportV2 = lazyRoute(() => import('./pages/BandwidthReport'))
const CacheManageV2 = lazyRoute(() => import('./pages/CacheManage'))
const CacheIndexes = lazyRoute(() => import('./pages/CacheIndexes'))
const CompileCache = lazyRoute(() => import('./pages/CompileCache'))
const UpstreamsV2 = lazyRoute(() => import('./pages/Upstreams'))
const UpstreamUpdates = lazyRoute(() => import('./pages/UpstreamUpdates'))
const AccessLogsV2 = lazyRoute(() => import('./pages/AccessLogs'))
const AuditLogsV2 = lazyRoute(() => import('./pages/AuditLogs'))
const Quarantine = lazyRoute(() => import('./pages/Quarantine'))
const RulesV2 = lazyRoute(() => import('./pages/Rules'))
const Security = lazyRoute(() => import('./pages/Security'))
const Projects = lazyRoute(() => import('./pages/Projects'))
const UsersV2 = lazyRoute(() => import('./pages/Users'))
const License = lazyRoute(() => import('./pages/License'))
const SettingsV2 = lazyRoute(() => import('./pages/Settings'))

const routeElements = {
  dashboard: <DashboardV2 />,
  attention: <Attention />,
  bandwidth: <BandwidthReportV2 />,
  cache: <CacheManageV2 />,
  cacheIndexes: <CacheIndexes />,
  compileCache: <CompileCache />,
  upstreams: <UpstreamsV2 />,
  upstreamUpdates: <UpstreamUpdates />,
  accessLogs: <AccessLogsV2 />,
  auditLogs: <AuditLogsV2 />,
  quarantine: <Quarantine />,
  rules: <RulesV2 />,
  security: <Security />,
  projects: <Projects />,
  users: <UsersV2 />,
  license: <License />,
  settings: <SettingsV2 />,
} satisfies Readonly<Record<AdminRouteId, ReactElement>>

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [, sessionChanged] = useReducer((revision: number) => revision + 1, 0)

  useEffect(() => {
    const handleExpiredSession = () => {
      queryClient.clear()
      sessionChanged()
    }
    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, handleExpiredSession)
    return () => window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, handleExpiredSession)
  }, [queryClient])

  const token = localStorage.getItem('token')
  const { principal, isPending, isError, refetch } = usePrincipal(Boolean(token))

  if (!token) return <Navigate to="/admin/login" state={{ from: location }} replace />
  if (isPending) return <div aria-busy="true" className="min-h-screen" />
  if (isError || !principal) {
    return (
      <main className="grid min-h-screen place-items-center p-4">
        <QueryErrorState message={t('auth.principalLoadError')} onRetry={() => { void refetch() }} />
      </main>
    )
  }
  return <>{children}</>
}

export default function AdminAppV2() {
  return (
    <Routes>
      <Route path="login" element={<LoginV2 />} />
      <Route
        element={
          <RequireAuth>
            <AdminShell />
          </RequireAuth>
        }
      >
        {adminRouteManifest.map(route => route.index
          ? <Route key={route.id} index element={routeElements[route.id]} />
          : <Route key={route.id} path={route.path} element={routeElements[route.id]} />)}
        <Route path="*" element={<RouteNotFound area="admin" />} />
      </Route>
    </Routes>
  )
}
