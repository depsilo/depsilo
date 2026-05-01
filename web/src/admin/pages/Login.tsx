import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import InputV2 from '@/components/Input'
import ButtonV2 from '@/components/Button'
import Logo from '@/components/Logo'
import { authApi } from '@/lib/api'

export default function LoginV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await authApi.login({ username, password })
      const { token, user } = res.data
      localStorage.setItem('token', token)
      localStorage.setItem('user', JSON.stringify(user))
      navigate('/admin', { replace: true })
    } catch (err: any) {
      setError(err.response?.data?.message || t('login.failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center relative overflow-hidden" style={{ background: 'var(--bg)' }}>
      {/* Decorative */}
      <div className="absolute top-1/4 -left-20 w-60 h-60 rounded-full blur-[120px]" style={{ background: 'rgba(83,58,253,0.06)' }} />
      <div className="absolute bottom-1/4 -right-20 w-60 h-60 rounded-full blur-[120px]" style={{ background: 'rgba(233,34,97,0.04)' }} />

      <div
        className="w-full max-w-sm rounded-[8px] p-8 relative z-10"
        style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-elevated)' }}
      >
        {/* Logo */}
        <div className="flex flex-col items-center mb-8">
          <div className="flex items-center gap-3">
            <Logo size={32} />
            <h1 className="text-[22px] font-[300] tracking-[-0.22px]" style={{ color: 'var(--heading)' }}>Depsilo</h1>
          </div>
          <p className="text-[14px] font-[300] mt-2" style={{ color: 'var(--body)' }}>{t('login.title')}</p>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <InputV2
            label={t('login.username')}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder={t('login.usernamePlaceholder')}
            required
          />
          <InputV2
            label={t('login.password')}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t('login.passwordPlaceholder')}
            required
          />
          <ButtonV2 type="submit" className="w-full" disabled={loading}>
            {loading ? t('login.submitting') : t('login.submit')}
          </ButtonV2>
        </form>

        {error && <p className="mt-3 text-[14px] text-center" style={{ color: 'var(--error)' }}>{error}</p>}

        {/* Status */}
        <div className="mt-6 flex items-center justify-center gap-6">
          <span className="text-[10px] font-mono flex items-center gap-1.5" style={{ color: 'var(--body)' }}>
            <span className="w-1.5 h-1.5 rounded-full inline-block" style={{ background: 'var(--success)' }} />
            Nodes: Active
          </span>
          <span className="text-[10px] font-mono flex items-center gap-1.5" style={{ color: 'var(--body)' }}>
            <span className="w-1.5 h-1.5 rounded-full inline-block" style={{ background: 'var(--success)' }} />
            Cache: Synced
          </span>
        </div>

        {/* Back link */}
        <div className="mt-4 text-center">
          <Link to="/" className="text-[13px] no-underline transition-colors duration-150" style={{ color: 'var(--stripe-purple)' }}>
            ← {t('login.backToPortal') || 'Back to Portal'}
          </Link>
        </div>
      </div>
    </div>
  )
}
