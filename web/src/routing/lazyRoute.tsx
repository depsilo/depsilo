import {
  Component,
  lazy,
  Suspense,
  type ComponentType,
  type ReactNode,
  useEffect,
  useId,
  useRef,
} from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Button from '@/components/Button'

type RouteSurface = 'content' | 'page'

interface LazyRouteOptions {
  surface?: RouteSurface
}

interface RouteErrorCatcherProps {
  children: ReactNode
  fallback: ReactNode
  pathname: string
}

interface RouteErrorCatcherState {
  failed: boolean
  pathname: string
}

class RouteErrorCatcher extends Component<RouteErrorCatcherProps, RouteErrorCatcherState> {
  state: RouteErrorCatcherState = {
    failed: false,
    pathname: this.props.pathname,
  }

  static getDerivedStateFromError(): Partial<RouteErrorCatcherState> {
    return { failed: true }
  }

  static getDerivedStateFromProps(
    props: RouteErrorCatcherProps,
    state: RouteErrorCatcherState,
  ): Partial<RouteErrorCatcherState> | null {
    if (props.pathname !== state.pathname) {
      return { failed: false, pathname: props.pathname }
    }
    return null
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children
  }
}

function surfaceClassName(surface: RouteSurface) {
  return surface === 'page' ? 'min-h-screen' : 'min-h-40'
}

function RouteLoading({ surface }: { surface: RouteSurface }) {
  const { t } = useTranslation()
  return (
    <div
      data-route-state="loading"
      role="status"
      aria-live="polite"
      aria-busy="true"
      className={`grid place-items-center px-4 py-10 ${surfaceClassName(surface)}`}
    >
      <span className="text-[13px] text-[var(--text-soft)]">{t('loading')}</span>
    </div>
  )
}

function RouteFailure({ surface }: { surface: RouteSurface }) {
  const { t } = useTranslation()
  const titleId = useId()
  const containerRef = useRef<HTMLElement>(null)

  useEffect(() => {
    containerRef.current?.focus()
  }, [])

  return (
    <section
      ref={containerRef}
      data-route-state="error"
      role="alert"
      aria-labelledby={titleId}
      tabIndex={-1}
      className={`grid place-items-center px-4 py-10 ${surfaceClassName(surface)}`}
    >
      <div
        className="w-full max-w-[520px] rounded-[8px] border border-[var(--danger-border)] bg-[var(--bg-card)] p-5 shadow-[var(--shadow-pop)]"
      >
        <h2 id={titleId} className="text-[17px] font-[600] text-[var(--text)]">
          {t('routeError.title')}
        </h2>
        <p className="mt-2 text-[13px] leading-6 text-[var(--text-soft)]">
          {t('routeError.hint')}
        </p>
        <Button className="mt-4" type="button" onClick={() => window.location.reload()}>
          {t('routeError.reload')}
        </Button>
      </div>
    </section>
  )
}

export function lazyRoute<Props extends object>(
  load: () => Promise<{ default: ComponentType<Props> }>,
  { surface = 'content' }: LazyRouteOptions = {},
): ComponentType<Props> {
  const LazyComponent = lazy(load)

  function LazyRoute(props: Props) {
    const { pathname } = useLocation()
    return (
      <RouteErrorCatcher
        pathname={pathname}
        fallback={<RouteFailure surface={surface} />}
      >
        <Suspense fallback={<RouteLoading surface={surface} />}>
          <LazyComponent {...props} />
        </Suspense>
      </RouteErrorCatcher>
    )
  }

  LazyRoute.displayName = 'LazyRoute'
  return LazyRoute
}
