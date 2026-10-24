import { useEffect, useReducer, useRef, type ReactElement } from 'react'
import { Routes, Route, Navigate, Link, useLocation } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { usePrincipal } from '@/hooks/usePrincipal'
import QueryErrorState from '@/components/QueryErrorState'
import Icon from '@/components/Icon'
import { Button } from '@/components/ui/button'
import Logo from '@/components/Logo'
import { useTranslation } from 'react-i18next'
import { adminApi, AUTH_SESSION_EXPIRED_EVENT } from '@/lib/api'
import { ONBOARDING_GATE_QUERY_KEY } from '@/lib/onboarding'
import { readLocalStorage } from '@/lib/storage'
import { lazyRoute } from '@/routing/lazyRoute'
import RouteNotFound from '@/routing/RouteNotFound'
import { adminRouteComponents } from './lazyRoutes'
import { ADMIN_LOGIN_PATH, adminAuthRoutes, adminRouteManifest, getAdminRouteHref, resolveAdminRoute, type AdminRouteId } from './routes'

const AuthComplete = lazyRoute(() => import('./pages/AuthComplete'), { surface: 'page' })
const Login = lazyRoute(() => import('./pages/Login'), { surface: 'page' })
const AdminShell = lazyRoute(() => import('./AdminShell'), { surface: 'page' })
const {
  account: Account,
  dashboard: Dashboard,
  connect: ConnectProject,
  attention: Attention,
  cache: CacheManage,
  cacheIndexes: CacheIndexes,
  compileCache: CompileCache,
  upstreams: Upstreams,
  cascade: Cascade,
  upstreamUpdates: UpstreamUpdates,
  accessLogs: AccessLogs,
  auditLogs: AuditLogs,
  quarantine: Quarantine,
  rules: Rules,
  security: Security,
  projects: Projects,
  users: Users,
  license: License,
  authentication: Authentication,
  settings: Settings,
} = adminRouteComponents

const routeElements = {
  account: <Account />,
  dashboard: <Dashboard />,
  connect: <ConnectProject />,
  attention: <Attention />,
  cache: <CacheManage />,
  cacheIndexes: <CacheIndexes />,
  compileCache: <CompileCache />,
  upstreams: <Upstreams />,
  cascade: <Cascade />,
  upstreamUpdates: <UpstreamUpdates />,
  accessLogs: <AccessLogs />,
  auditLogs: <AuditLogs />,
  quarantine: <Quarantine />,
  rules: <Rules />,
  security: <Security />,
  projects: <Projects />,
  users: <Users />,
  license: <License />,
  authentication: <Authentication />,
  settings: <Settings />,
} satisfies Readonly<Record<AdminRouteId, ReactElement>>

function AdminAuthPending() {
  const { t } = useTranslation()

  return (
    <main
      data-admin-auth-state="pending"
      aria-busy="true"
      className="grid min-h-screen place-items-center bg-[var(--bg-page)] p-6"
    >
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="flex min-h-12 items-center gap-3 text-[var(--text)]"
      >
        <span className="grid size-10 shrink-0 place-items-center rounded-md bg-[var(--brand-soft)] text-[var(--brand-text)]">
          <Logo size={26} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="font-display text-[15px] font-[700] leading-5">Depsilo</span>
          <span className="text-[12px] leading-4 text-[var(--text-muted)]">{t('auth.verifyingSession')}</span>
        </span>
        <Icon
          name="progress_activity"
          size="sm"
          className="ml-2 shrink-0 animate-spin text-[var(--text-subtle)] motion-reduce:animate-none"
        />
      </div>
    </main>
  )
}

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

  const token = readLocalStorage('token')
  const { principal, isPending, isError, refetch } = usePrincipal(Boolean(token))
  const onboarding = useQuery({
    queryKey: ONBOARDING_GATE_QUERY_KEY,
    queryFn: async ({ signal }) => (await adminApi.getOnboardingStatus({}, { signal })).data,
    enabled: Boolean(token && principal),
    staleTime: 30_000,
    retry: false,
  })

  if (!token) return <Navigate to={ADMIN_LOGIN_PATH} state={{ from: location }} replace />
  if (isPending) return <AdminAuthPending />
  if (isError || !principal) {
    return (
      <main className="grid min-h-screen place-items-center p-4">
        <QueryErrorState message={t('auth.principalLoadError')} onRetry={() => { void refetch() }} />
      </main>
    )
  }
  if (onboarding.isPending) return <AdminAuthPending />
  if (onboarding.data?.status === 'not_started' && location.pathname !== getAdminRouteHref('connect') && location.pathname !== getAdminRouteHref('account') && !resolveAdminRoute(location.pathname)?.requiresAdmin) {
    return <Navigate to={`${getAdminRouteHref('connect')}?new=1`} replace />
  }
  return <>{children}</>
}

function SystemAccessDenied() {
  const { t } = useTranslation()
  const section = useRef<HTMLElement>(null)
  useEffect(() => { section.current?.focus() }, [])
  return <section ref={section} tabIndex={-1} aria-labelledby="system-access-title" data-admin-access="denied" className="programmatic-focus-target max-w-2xl space-y-4 py-6">
    <h1 id="system-access-title" className="text-2xl font-semibold">{t('nav.systemAccessTitle')}</h1>
    <p className="text-sm leading-6 text-muted-foreground">{t('nav.systemAccessHint')}</p>
    <Button render={<Link to={getAdminRouteHref('dashboard')} />} nativeButton={false} role="link">{t('nav.backToOverview')}</Button>
  </section>
}

function RequireAdministrator({ children }: { children: React.ReactNode }) {
  const { principal } = usePrincipal()
  return principal?.role === 'admin' ? <>{children}</> : <SystemAccessDenied />
}

export default function AdminApp() {
  return (
    <Routes>
      <Route path={adminAuthRoutes.login} element={<Login />} />
      <Route path={adminAuthRoutes.complete} element={<AuthComplete />} />
      <Route
        element={
          <RequireAuth>
            <AdminShell />
          </RequireAuth>
        }
      >
        {adminRouteManifest.map(route => route.index
          ? <Route key={route.id} index element={routeElements[route.id]} />
          : <Route key={route.id} path={route.path} element={route.requiresAdmin ? <RequireAdministrator>{routeElements[route.id]}</RequireAdministrator> : routeElements[route.id]} />)}
        {/* Bandwidth reporting merged into the Dashboard; keep the legacy URL working. */}
        <Route path="bandwidth" element={<Navigate to="/admin" replace />} />
        <Route path="*" element={<RouteNotFound area="admin" />} />
      </Route>
    </Routes>
  )
}
