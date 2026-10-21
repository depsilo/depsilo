import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'

const queryKey = ['admin', 'docker', 'bootstrap'] as const

export default function DockerBootstrap() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => (await adminApi.getDockerBootstrap({ signal })).data,
    refetchInterval: 5_000,
  })
  const enable = useMutation({
    mutationFn: async () => (await adminApi.enableDockerHub()).data,
    onSuccess: state => queryClient.setQueryData(queryKey, state),
  })

  return (
    <section aria-labelledby="docker-bootstrap-title" className="mt-6 border-t border-[var(--border)] pt-6">
      <h3 id="docker-bootstrap-title" className="text-[14px] font-[650] text-[var(--text)]">{t('onboarding.docker.title')}</h3>
      {query.isLoading && <p role="status" className="mt-2 text-[13px] text-[var(--text-muted)]">{t('onboarding.docker.checking')}</p>}
      {query.isError && <div className="mt-3"><QueryErrorState message={t('onboarding.docker.loadError')} onRetry={() => { void query.refetch() }} /></div>}
      {query.data && (
        <div className="mt-3 space-y-3">
          {query.data.blocked_by_override ? (
            <InlineNotice tone="warning">{t('onboarding.docker.override')}</InlineNotice>
          ) : query.data.pending_restart ? (
            <InlineNotice tone="warning">{t('onboarding.docker.restart')}</InlineNotice>
          ) : query.data.docker_hub ? (
            <InlineNotice tone="success">{t('onboarding.docker.active')}</InlineNotice>
          ) : query.data.configured ? (
            <InlineNotice tone="info">{t('onboarding.docker.custom')}</InlineNotice>
          ) : (
            <>
              <p className="text-[13px] text-[var(--text-muted)]">{t('onboarding.docker.disabled')}</p>
              {canWrite && query.data.config_writable && (
                <Button disabled={enable.isPending} onClick={() => enable.mutate()}>
                  <Icon name="add" size="sm" />{t('onboarding.docker.enable')}
                </Button>
              )}
              {!query.data.config_writable && <InlineNotice tone="warning">{t('onboarding.docker.readOnly')}</InlineNotice>}
              {!canWrite && <p className="text-[13px] text-[var(--text-muted)]">{t('common.permissionDenied')}</p>}
            </>
          )}
          {enable.isError && <p role="alert" className="text-[13px] text-[var(--danger-text)]">{t('onboarding.docker.enableError')}</p>}
          {query.data.effective && query.data.docker_hub && (
            <p className="text-[12px] leading-5 text-[var(--text-muted)]">{t('onboarding.docker.scope')}</p>
          )}
        </div>
      )}
    </section>
  )
}
