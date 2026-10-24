import { useTranslation } from 'react-i18next'
import QueryErrorState from '@/components/QueryErrorState'
import type { AuthenticationImpact as Impact } from '@/lib/adminApi.types'

/** An observation, not a promise: accounts and sessions can change before restart. */
export default function AuthenticationImpact({ data, pending, retry }: { data?: Impact; pending: boolean; retry: () => void }) {
  const { t, i18n } = useTranslation()
  if (pending) return <p role="status" className="text-sm text-muted-foreground">{t('authentication.impactLoading')}</p>
  if (!data) return <QueryErrorState message={t('authentication.impactError')} onRetry={retry} />
  return <section className="space-y-3 rounded-md border border-border p-4" aria-label={t('authentication.impactTitle')}>
    <h3 className="text-sm font-semibold">{t('authentication.impactTitle')}</h3>
    <dl className="space-y-2 text-sm">
      {(['bound_user_count', 'dependent_managed_user_count', 'active_session_count'] as const).map(key => <div key={key} className="flex justify-between gap-4"><dt className="text-muted-foreground">{t(`authentication.impact.${key}`)}</dt><dd className="font-medium tabular-nums">{data[key]}</dd></div>)}
    </dl>
    {data.users.length > 0 && <details className="text-sm"><summary className="cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-ring">{t('authentication.affectedUsers')}</summary><ul className="mt-2 space-y-1">{data.users.map(user => <li key={user.id} className="flex flex-wrap justify-between gap-2"><span className="break-all">{user.username}</span><span className="text-muted-foreground">{t(user.permission_source !== 'local' ? 'authentication.managedAccount' : 'authentication.localAccount')}{!user.enabled && ` · ${t('authentication.disabled')}`}</span></li>)}</ul>{data.users_truncated && <p className="mt-2 text-muted-foreground">{t('authentication.usersTruncated')}</p>}</details>}
    <p className="text-xs leading-5 text-muted-foreground">{t('authentication.impactObserved', { time: new Date(data.observed_at).toLocaleString(i18n.language) })}</p>
  </section>
}
