import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import InputV2 from '@/components/Input'
import ButtonV2 from '@/components/Button'
import Logo from '@/components/Logo'
import EcosystemIcon from '@/components/EcosystemIcon'
import { authApi, statsApi } from '@/lib/api'
import { formatVersion } from '@/lib/utils'
import { LANGUAGES } from '@/lib/ecosystemData'
import { isAdminEcosystem } from '@/lib/adminApi.types'

interface LoginStats {
  service?: { version?: string }
  today?: { total_requests?: number; hit_rate?: number }
  upstreams?: { healthy: boolean }[]
}

function formatRequests(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`
  return String(n)
}

export default function LoginV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const { data: stats } = useQuery<LoginStats>({
    queryKey: ['stats-login'],
    queryFn: async () => (await statsApi.getStats()).data,
    staleTime: 60 * 1000,
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    localStorage.removeItem('token')
    queryClient.clear()
    try {
      const response = await authApi.login({ username, password })
      localStorage.setItem('token', response.data.token)
      navigate('/admin', { replace: true })
    } catch (err: unknown) {
      const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message
      setError(message || t('login.failed'))
    } finally {
      setLoading(false)
    }
  }

  const hitRatePct = stats?.today?.hit_rate != null ? (stats.today.hit_rate * 100).toFixed(1) : '-'
  const requests = stats?.today?.total_requests != null ? formatRequests(stats.today.total_requests) : '-'
  const healthyUpstreams = stats?.upstreams?.filter(u => u.healthy).length ?? null
  const totalUpstreams = stats?.upstreams?.length ?? null
  const mirrorsLabel = healthyUpstreams != null && totalUpstreams != null
    ? `${healthyUpstreams}/${totalUpstreams}`
    : '-'

  return (
    <div
      className="min-h-screen"
      style={{
        background: 'var(--bg-page)',
        position: 'relative',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '24px',
      }}
    >
      <div className="page-wash" />

      {/* Top-left: back to Portal */}
      <Link
        to="/"
        style={{
          position: 'absolute',
          top: 20,
          left: 24,
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          fontSize: 12,
          color: 'var(--text-muted)',
          textDecoration: 'none',
          padding: '4px 8px',
          borderRadius: 4,
          zIndex: 10,
        }}
      >
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" aria-hidden="true">
          <path d="M6.5 2L2.5 5L6.5 8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {t('login.backToPortal')}
      </Link>

      {/* Split card */}
      <div
        className="aurora-rim fade-up"
        style={{
          position: 'relative',
          zIndex: 1,
          width: '100%',
          maxWidth: 820,
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)',
          background: 'var(--bg-card)',
          border: '0.5px solid var(--border)',
          borderRadius: 14,
          boxShadow: 'var(--shadow-card)',
          overflow: 'hidden',
        }}
      >
        {/* Right: form */}
        <div
          style={{
            padding: '40px 36px 32px',
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'center',
            gap: 24,
            minHeight: 460,
            order: 2,
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
              <Logo size={28} />
              <span
                style={{
                  fontSize: 17,
                  fontWeight: 600,
                  color: 'var(--text)',
                }}
              >
                depsilo
              </span>
            </div>
            <h1
              style={{
                margin: 0,
                fontSize: 24,
                fontWeight: 600,
                color: 'var(--text)',
                lineHeight: 1.15,
              }}
            >
              {t('login.title')}
            </h1>
            <p
              style={{
                margin: 0,
                fontSize: 13,
                lineHeight: 1.5,
                color: 'var(--text-muted)',
              }}
            >
              {t('login.subtitle')}
            </p>
          </div>

          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <InputV2
              label={t('login.username')}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder={t('login.usernamePlaceholder')}
              autoComplete="username"
              autoFocus
              required
            />
            <InputV2
              label={t('login.password')}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t('login.passwordPlaceholder')}
              autoComplete="current-password"
              required
            />

            {error && (
              <div
                role="alert"
                style={{
                  background: 'var(--danger-fill)',
                  border: '0.5px solid var(--danger-border)',
                  borderRadius: 6,
                  padding: '8px 12px',
                  fontSize: 13,
                  color: 'var(--danger-text)',
                  lineHeight: 1.45,
                }}
              >
                {error}
              </div>
            )}

            <ButtonV2 type="submit" className="w-full" disabled={loading}>
              {loading ? t('login.submitting') : t('login.submit')}
            </ButtonV2>
          </form>
        </div>

        {/* Left: brand panel */}
        <div
          className="login-brand-panel"
          style={{
            position: 'relative',
            padding: '40px 36px 32px',
            display: 'flex',
            flexDirection: 'column',
            gap: 28,
            background: `
              radial-gradient(120% 80% at 0% 0%, color-mix(in oklab, var(--brand) 15%, transparent), transparent 55%),
              radial-gradient(100% 80% at 100% 100%, color-mix(in oklab, var(--brand) 10%, transparent), transparent 55%),
              var(--bg-soft)
            `,
            borderRight: '0.5px solid var(--border)',
            overflow: 'hidden',
            minHeight: 460,
            order: 1,
          }}
        >
          {/* Top: tagline */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            <span className="eyebrow">depsilo</span>
            <h2
              style={{
                margin: 0,
                fontSize: 22,
                fontWeight: 600,
                lineHeight: 1.15,
                color: 'var(--text)',
              }}
            >
              {t('login.tagline')}
            </h2>
            <p style={{ margin: 0, fontSize: 13, lineHeight: 1.5, color: 'var(--text-muted)' }}>
              {t('login.taglineSub')}
            </p>
          </div>

          {/* Ecosystem mini wall */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <span className="eyebrow">{t('login.ecosystemEyebrow')}</span>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14 }}>
              {LANGUAGES.map(lang => (
                <span
                  key={lang.id}
                  title={lang.name}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: 22,
                    height: 22,
                    opacity: 0.72,
                  }}
                >
                  {isAdminEcosystem(lang.iconAdapter) && <EcosystemIcon type={lang.iconAdapter} size={18} useColor />}
                </span>
              ))}
            </div>
          </div>

          {/* Live stats */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(3, 1fr)',
              gap: 0,
              marginTop: 'auto',
              paddingTop: 16,
              borderTop: '0.5px solid var(--border)',
            }}
          >
            {[
              { label: t('login.statHitRate'), value: hitRatePct, unit: '%' },
              { label: t('login.statRequests'), value: requests, unit: '' },
              { label: t('login.statMirrors'), value: mirrorsLabel, unit: '' },
            ].map((s, i) => (
              <div
                key={s.label}
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 4,
                  paddingLeft: i === 0 ? 0 : 14,
                  borderLeft: i === 0 ? 'none' : '0.5px solid var(--border)',
                }}
              >
                <span style={{ fontSize: 10.5, color: 'var(--text-subtle)', textTransform: 'uppercase' }}>
                  {s.label}
                </span>
                <span
                  className="mono"
                  style={{
                    fontSize: 22,
                    fontWeight: 600,
                    color: 'var(--text)',
                    lineHeight: 1,
                  }}
                >
                  {s.value}
                  {s.unit && (
                    <span style={{ fontSize: 13, color: 'var(--text-soft)', fontWeight: 400, marginLeft: 2 }}>
                      {s.unit}
                    </span>
                  )}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Bottom-right: version */}
      {stats?.service?.version && (
        <span
          className="mono"
          title={stats.service.version}
          style={{
            position: 'absolute',
            bottom: 16,
            right: 20,
            fontSize: 11,
            color: 'var(--text-subtle)',
            padding: '2px 6px',
            border: '0.5px solid var(--border)',
            borderRadius: 4,
            background: 'var(--bg-card)',
            zIndex: 1,
          }}
        >
          {formatVersion(stats.service.version)}
        </span>
      )}

      {/* Responsive: collapse brand panel on narrow screens */}
      <style>{`
        @media (max-width: 720px) {
          .login-brand-panel {
            display: none !important;
          }
          .aurora-rim {
            grid-template-columns: 1fr !important;
            max-width: 420px !important;
          }
        }
      `}</style>
    </div>
  )
}
