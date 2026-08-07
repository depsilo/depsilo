import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import Button from '@/components/Button'
import Icon from '@/components/Icon'
import Input from '@/components/Input'
import Logo from '@/components/Logo'
import { authApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { writeLocalStorage } from '@/lib/storage'

function loginDestination(state: unknown) {
  if (!state || typeof state !== 'object' || !('from' in state)) return '/admin'
  const from = (state as { from?: unknown }).from
  if (!from || typeof from !== 'object') return '/admin'
  const candidate = from as { pathname?: unknown; search?: unknown; hash?: unknown }
  if (typeof candidate.pathname !== 'string' ||
      (candidate.pathname !== '/admin' && !candidate.pathname.startsWith('/admin/')) ||
      candidate.pathname === '/admin/login') {
    return '/admin'
  }
  const search = typeof candidate.search === 'string' ? candidate.search : ''
  const hash = typeof candidate.hash === 'string' ? candidate.hash : ''
  return `${candidate.pathname}${search}${hash}`
}

export default function Login() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const loginRequestRef = useRef<AbortController | null>(null)

  useEffect(() => () => {
    loginRequestRef.current?.abort()
    loginRequestRef.current = null
  }, [])

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (loading) return
    setError('')
    setLoading(true)
    const controller = new AbortController()
    loginRequestRef.current = controller
    try {
      const response = await authApi.login(
        { username: username.trim(), password },
        { signal: controller.signal },
      )
      if (controller.signal.aborted) return
      if (!writeLocalStorage('token', response.data.token)) {
        setError(t('login.storageUnavailable'))
        return
      }
      queryClient.clear()
      navigate(loginDestination(location.state), { replace: true })
    } catch (requestError: unknown) {
      if (controller.signal.aborted) return
      const status = getApiError(requestError).status
      setError(status === 429
        ? t('login.rateLimited')
        : status === 401 || status === 403
          ? t('login.failed')
          : t('login.unavailable'))
    } finally {
      if (loginRequestRef.current === controller) {
        loginRequestRef.current = null
        setLoading(false)
      }
    }
  }

  return (
    <main className="grid min-h-screen place-items-center bg-[var(--bg-page)] px-4 py-16 sm:px-6">
      <div className="w-full max-w-[420px]">
        <div className="mb-12 flex items-center justify-between gap-4">
          <Link
            to="/"
            aria-label="Depsilo"
            className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-2 rounded-[6px] text-[var(--text)] no-underline transition-opacity duration-150 hover:opacity-75"
          >
            <Logo size={28} />
            <span className="font-display text-[16px] font-[700]">Depsilo</span>
          </Link>
          <Link
            to="/"
            className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-1.5 rounded-[6px] px-2 text-[12px] font-[550] text-[var(--text-muted)] no-underline transition-colors duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)]"
          >
            <Icon name="arrow_back" size="sm" />
            {t('login.backToPortal')}
          </Link>
        </div>

        <header className="mb-8">
          <h1 className="m-0 font-display text-[30px] font-[650] leading-[1.1] text-[var(--text)]">
            {t('login.title')}
          </h1>
          <p className="mt-2 max-w-[46ch] text-[14px] leading-6 text-[var(--text-muted)]">
            {t('login.subtitle')}
          </p>
        </header>

        <form onSubmit={handleSubmit} className="space-y-5" aria-busy={loading || undefined}>
          <Input
            label={t('login.username')}
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            placeholder={t('login.usernamePlaceholder')}
            autoComplete="username"
            autoFocus
            required
          />
          <Input
            label={t('login.password')}
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder={t('login.passwordPlaceholder')}
            autoComplete="current-password"
            required
          />

          {error && (
            <p
              role="alert"
              className="rounded-[6px] border border-[var(--danger-border)] bg-[var(--danger-fill)] px-3 py-2.5 text-[13px] leading-5 text-[var(--danger-text)]"
            >
              {error}
            </p>
          )}

          <Button
            type="submit"
            className="min-h-10 w-full"
            aria-busy={loading || undefined}
            disabled={loading || !username.trim() || !password}
          >
            {loading ? t('login.submitting') : t('login.submit')}
          </Button>
        </form>
      </div>
    </main>
  )
}
