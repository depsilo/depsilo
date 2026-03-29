import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Input from '@/components/Input'
import Button from '@/components/Button'
import Logo from '@/components/Logo'
import { authApi } from '@/lib/api'

export default function Login() {
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
    <div className="min-h-screen bg-bg flex items-center justify-center relative overflow-hidden">
      {/* Decorative blurred circles */}
      <div className="absolute top-1/4 -left-20 w-60 h-60 rounded-full bg-primary/5 blur-[120px]" />
      <div className="absolute bottom-1/4 -right-20 w-60 h-60 rounded-full bg-tertiary/5 blur-[120px]" />

      <div className="w-full max-w-sm bg-surface-container/80 backdrop-blur-xl border border-outline-variant/15 rounded-[0.5rem] p-8 relative z-10">
        {/* Logo */}
        <div className="flex flex-col items-center mb-8">
          <div className="flex items-center gap-3">
            <Logo height={36} />
            <h1 className="text-2xl font-bold text-on-surface tracking-tight">Depsilo</h1>
          </div>
          <p className="text-sm text-on-surface-variant mt-2">{t('login.title')}</p>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('login.username')}</label>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder={t('login.usernamePlaceholder')}
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('login.password')}</label>
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t('login.passwordPlaceholder')}
              required
            />
          </div>
          <Button type="submit" className="w-full" disabled={loading}>
            {loading ? t('login.submitting') : t('login.submit')}
          </Button>
        </form>

        {/* Error */}
        {error && (
          <p className="mt-3 text-error text-sm text-center">{error}</p>
        )}

        {/* Status indicators */}
        <div className="mt-6 flex items-center justify-center gap-6">
          <span className="text-[10px] text-on-surface-variant font-mono flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-success inline-block" />
            Nodes: Active
          </span>
          <span className="text-[10px] text-on-surface-variant font-mono flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-success inline-block" />
            Cache: Synced
          </span>
        </div>
      </div>
    </div>
  )
}
