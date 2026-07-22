import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import DataTableV2 from '@/components/DataTable'
import EmptyState from '@/components/EmptyState'
import Icon from '@/components/Icon'
import IconButton from '@/components/IconButton'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import Metric from '@/components/Metric'
import ModalV2 from '@/components/Modal'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import SelectV2 from '@/components/Select'
import { useAppToast } from '@/components/Toast'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import type {
  CompileCacheCredential,
  CompileCachePermissions,
  CreateCompileCacheCredentialRequest,
} from '@/lib/adminApi.types'
import { copyText } from '@/lib/clipboard'
import { formatBytes, formatTime } from '@/lib/utils'

const DEFAULT_FORM: CreateCompileCacheCredentialRequest = {
  name: '',
  namespace: '',
  permissions: 'readwrite',
  ttl_days: 30,
}

const NAMESPACE_PATTERN = /^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$/

type CopiedValue = 'ccache-endpoint' | 'sccache-endpoint' | 'ccache-config' | 'sccache-config'

interface OneTimeClientConfigurations {
  ccache: string
  sccache: string
}

export default function CompileCache() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<CreateCompileCacheCredentialRequest>(DEFAULT_FORM)
  const [clientConfigurations, setClientConfigurations] = useState<OneTimeClientConfigurations | null>(null)
  const [copiedValue, setCopiedValue] = useState<CopiedValue | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<CompileCacheCredential | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['admin', 'compile-cache', 'status'],
    queryFn: () => adminApi.getCompileCacheStatus(),
    retry: false,
    refetchInterval: 30000,
  })
  const credentialsQuery = useQuery({
    queryKey: ['admin', 'compile-cache', 'credentials'],
    queryFn: () => adminApi.listCompileCacheCredentials(),
    retry: false,
  })

  const statusResponse = statusQuery.data
  const status = statusResponse?.data
  const credentialsResponse = credentialsQuery.data
  const credentials = credentialsResponse?.data.items ?? []
  const numberFormatter = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language)

  const createMutation = useMutation({
    mutationFn: async (request: CreateCompileCacheCredentialRequest) => {
      const response = await adminApi.createCompileCacheCredential(request)
      // Keep the one-time secret in page memory only. Returning it here would
      // retain it in React Query's mutation cache after the dialog is closed.
      setClientConfigurations({
        ccache: response.data.ccache_remote_storage,
        sccache: response.data.sccache_config,
      })
    },
    onSuccess: () => {
      setCreateOpen(false)
      setForm(DEFAULT_FORM)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'compile-cache', 'credentials'] })
    },
  })

  const revokeMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteCompileCacheCredential(id),
    onSuccess: () => {
      toast.show({ tone: 'success', message: t('compileCache.revokeSuccess') })
      setRevokeTarget(null)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'compile-cache', 'credentials'] })
    },
  })

  const cleanupMutation = useMutation({
    mutationFn: async () => (await adminApi.cleanupCompileCache()).data,
    onSuccess: (result) => {
      toast.show({
        tone: 'success',
        message: t('compileCache.cleanupSuccess', {
          count: result.removed_entries,
          bytes: formatBytes(result.reclaimed_bytes),
        }),
      })
      setCleanupOpen(false)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'compile-cache', 'status'] })
    },
  })

  const normalizedName = form.name.trim()
  const normalizedNamespace = form.namespace.trim().toLowerCase()
  const formValid = normalizedName.length > 0
    && normalizedName.length <= 128
    && NAMESPACE_PATTERN.test(normalizedNamespace)

  function openCreateDialog() {
    createMutation.reset()
    setForm(DEFAULT_FORM)
    setCreateOpen(true)
  }

  function closeCreateDialog() {
    if (createMutation.isPending) return
    setCreateOpen(false)
    setForm(DEFAULT_FORM)
    createMutation.reset()
  }

  function handleCreate(event: FormEvent) {
    event.preventDefault()
    if (!canWrite || !formValid) return
    createMutation.mutate({
      ...form,
      name: normalizedName,
      namespace: normalizedNamespace,
    })
  }

  function closeClientConfigurations() {
    setClientConfigurations(null)
    setCopiedValue(null)
    createMutation.reset()
  }

  async function copyValue(value: string, target: CopiedValue) {
    if (!await copyText(value)) return
    setCopiedValue(target)
    window.setTimeout(() => {
      setCopiedValue(current => current === target ? null : current)
    }, 2000)
  }

  function refreshAll() {
    void statusQuery.refetch()
    void credentialsQuery.refetch()
  }

  const statusError = getApiError(statusQuery.error)
  const credentialsError = getApiError(credentialsQuery.error)
  const isRefreshing = statusQuery.isFetching || credentialsQuery.isFetching
  const usagePercent = status?.enabled && status.stats.max_bytes > 0
    ? Math.min(100, (status.stats.size_bytes / status.stats.max_bytes) * 100)
    : 0

  const credentialColumns = [
    {
      key: 'name',
      label: t('name'),
      render: (value: unknown) => <span className="font-[500] text-[var(--text)]">{value as string}</span>,
    },
    {
      key: 'namespace',
      label: t('compileCache.namespace'),
      render: (value: unknown) => <code className="font-mono text-[12px] text-[var(--text-soft)]">{value as string}</code>,
    },
    {
      key: 'permissions',
      label: t('compileCache.permissions'),
      render: (value: unknown) => {
        const permissions = value as CompileCachePermissions
        return (
          <BadgeV2 variant={permissions === 'readwrite' ? 'success' : 'default'}>
            {t(permissions === 'readwrite' ? 'compileCache.readwrite' : 'compileCache.readonly')}
          </BadgeV2>
        )
      },
    },
    {
      key: 'last_used_at',
      label: t('compileCache.lastUsed'),
      render: (value: unknown) => (
        <span className="font-mono text-[12px] text-[var(--text-soft)]">
          {value ? formatTime(value as string) : t('compileCache.neverUsed')}
        </span>
      ),
    },
    {
      key: 'expires_at',
      label: t('compileCache.expiresAt'),
      render: (value: unknown) => (
        <span className="font-mono text-[12px] text-[var(--text-soft)]">
          {value ? formatTime(value as string) : t('compileCache.neverExpires')}
        </span>
      ),
    },
    {
      key: 'id',
      label: t('actions'),
      render: (_value: unknown, row: CompileCacheCredential & Record<string, unknown>) => canWrite ? (
        <ButtonV2
          type="button"
          variant="danger"
          size="sm"
          disabled={revokeMutation.isPending}
          aria-label={t('compileCache.revokeNamed', { name: row.name })}
          onClick={(event) => {
            event.stopPropagation()
            revokeMutation.reset()
            setRevokeTarget(row)
          }}
        >
          {t('compileCache.revoke')}
        </ButtonV2>
      ) : null,
    },
  ]

  return (
    <AdminPage
      description={t('compileCache.subtitle')}
      actions={(
        <>
          <ButtonV2
            type="button"
            variant="secondary"
            size="sm"
            aria-busy={isRefreshing || undefined}
            disabled={isRefreshing}
            onClick={refreshAll}
          >
            <Icon name={isRefreshing ? 'progress_activity' : 'refresh'} size="sm" />
            {t(isRefreshing ? 'compileCache.refreshing' : 'compileCache.refresh')}
          </ButtonV2>
          {canWrite && (
            <ButtonV2
              type="button"
              variant="danger"
              size="sm"
              disabled={!status?.enabled}
              onClick={() => {
                cleanupMutation.reset()
                setCleanupOpen(true)
              }}
            >
              <Icon name="delete_sweep" size="sm" />
              {t('compileCache.cleanup')}
            </ButtonV2>
          )}
        </>
      )}
    >
      <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {copiedValue ? t('compileCache.copied') : ''}
      </span>
      <div className="space-y-12">
        <section aria-labelledby="compile-cache-status-heading">
          <SectionHeader
            title={t('compileCache.statusTitle')}
            action={status ? (
              <BadgeV2 variant={status.enabled ? 'success' : 'warning'}>
                {t(status.enabled ? 'compileCache.enabled' : 'compileCache.disabled')}
              </BadgeV2>
            ) : undefined}
          />
          <span id="compile-cache-status-heading" className="sr-only">{t('compileCache.statusTitle')}</span>

          {statusQuery.isPending ? (
            <div aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]">
              <span aria-hidden="true">{t('loading')}</span>
            </div>
          ) : statusQuery.isError && !statusResponse ? (
            <QueryErrorState
              message={statusError.status === 403 ? t('common.permissionDenied') : t('compileCache.statusLoadError')}
              onRetry={() => { void statusQuery.refetch() }}
            />
          ) : status ? (
            <div className="space-y-5">
              {statusResponse && statusQuery.isRefetchError && (
                <StaleDataNotice
                  message={t('compileCache.statusStale')}
                  refreshing={statusQuery.isFetching}
                  onRefresh={() => statusQuery.refetch()}
                />
              )}
              {status.enabled ? (
                <>
                  <div className="grid grid-cols-2 gap-x-5 gap-y-8 lg:grid-cols-4">
                    <Metric
                      label={t('compileCache.capacityUsed')}
                      value={formatBytes(status.stats.size_bytes)}
                      size={30}
                    />
                    <Metric
                      label={t('compileCache.entries')}
                      value={`${numberFormatter.format(status.stats.entries)} / ${numberFormatter.format(status.stats.max_entries)}`}
                      size={24}
                    />
                    <Metric
                      label={t('compileCache.hits')}
                      value={numberFormatter.format(status.stats.hits)}
                      size={30}
                    />
                    <Metric
                      label={t('compileCache.namespaces')}
                      value={numberFormatter.format(status.stats.namespace_count)}
                      size={30}
                    />
                  </div>

                  <div>
                    <div className="mb-2 flex items-center justify-between gap-4 text-[11px] text-[var(--text-soft)]">
                      <span>{t('compileCache.capacity')}</span>
                      <span className="font-mono tabular-nums">
                        {formatBytes(status.stats.size_bytes)} / {formatBytes(status.stats.max_bytes)}
                      </span>
                    </div>
                    <div
                      role="progressbar"
                      aria-label={t('compileCache.capacity')}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={Math.round(usagePercent)}
                      className="h-2 overflow-hidden rounded-full bg-[var(--bg-soft)]"
                    >
                      <div
                        className="h-full rounded-full bg-[var(--brand)] transition-[width] duration-300"
                        style={{ width: `${usagePercent}%` }}
                      />
                    </div>
                  </div>

                  <div className="space-y-3 border-t border-[var(--border)] pt-4">
                    <span className="text-[11px] font-[600] uppercase text-[var(--text-subtle)]">
                      {t('compileCache.endpoints')}
                    </span>
                    {(['ccache', 'sccache'] as const).map(client => {
                      const target = `${client}-endpoint` as CopiedValue
                      return (
                        <div key={client} className="flex min-w-0 items-center gap-3">
                          <span className="w-16 shrink-0 font-mono text-[12px] font-[600] text-[var(--text)]">{client}</span>
                          <code className="min-w-0 flex-1 break-all font-mono text-[12px] text-[var(--text-soft)]">
                            {status.endpoints[client]}
                          </code>
                          <IconButton
                            icon={copiedValue === target ? 'check' : 'content_copy'}
                            label={t('compileCache.copyClientEndpoint', { client })}
                            onClick={() => { void copyValue(status.endpoints[client], target) }}
                          />
                        </div>
                      )
                    })}
                  </div>
                </>
              ) : (
                <InlineNotice tone="warning" title={t('compileCache.disabledTitle')}>
                  <p>{t('compileCache.disabledHint')}</p>
                  <pre className="mt-2 overflow-x-auto font-mono text-[12px]">[compile_cache]{'\n'}enabled = true</pre>
                </InlineNotice>
              )}
            </div>
          ) : null}
        </section>

        <section>
          <SectionHeader
            title={t('compileCache.credentialsTitle')}
            hint={t('compileCache.credentialsHint')}
            action={canWrite ? (
              <ButtonV2 type="button" size="sm" disabled={!status?.enabled} onClick={openCreateDialog}>
                <Icon name="add" size="sm" />
                {t('compileCache.createCredential')}
              </ButtonV2>
            ) : undefined}
          />

          {credentialsQuery.isPending ? (
            <div aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]">
              <span aria-hidden="true">{t('loading')}</span>
            </div>
          ) : credentialsQuery.isError && !credentialsResponse ? (
            <QueryErrorState
              message={credentialsError.status === 403 ? t('common.permissionDenied') : t('compileCache.credentialsLoadError')}
              onRetry={() => { void credentialsQuery.refetch() }}
            />
          ) : (
            <div className="space-y-3">
              {credentialsResponse && credentialsQuery.isRefetchError && (
                <StaleDataNotice
                  message={t('compileCache.credentialsStale')}
                  refreshing={credentialsQuery.isFetching}
                  onRefresh={() => credentialsQuery.refetch()}
                />
              )}
              {credentials.length === 0 ? (
                <EmptyState
                  icon="key"
                  title={t('compileCache.noCredentials')}
                  hint={t('compileCache.noCredentialsHint')}
                  minHeight={180}
                />
              ) : (
                <DataTableV2
                  columns={credentialColumns}
                  data={credentials.map(credential => ({ ...credential }))}
                  rowKey={row => row.id as number}
                  ariaLabel={t('compileCache.credentialsTable')}
                  minWidth={860}
                />
              )}
            </div>
          )}
        </section>
      </div>

      <ModalV2 open={createOpen} onClose={closeCreateDialog} title={t('compileCache.createCredential')}>
        <form className="space-y-4" onSubmit={handleCreate}>
          <InputV2
            label={t('compileCache.credentialName')}
            hint={t('compileCache.credentialNameHint')}
            value={form.name}
            maxLength={128}
            autoComplete="off"
            required
            onChange={event => setForm(current => ({ ...current, name: event.target.value }))}
          />
          <InputV2
            label={t('compileCache.namespace')}
            hint={t('compileCache.namespaceHint')}
            value={form.namespace}
            maxLength={64}
            pattern="[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?"
            autoCapitalize="none"
            autoComplete="off"
            spellCheck={false}
            mono
            required
            onChange={event => setForm(current => ({ ...current, namespace: event.target.value.toLowerCase() }))}
          />
          <SelectV2
            label={t('compileCache.permissions')}
            value={form.permissions}
            onChange={event => setForm(current => ({
              ...current,
              permissions: event.target.value as CompileCachePermissions,
            }))}
          >
            <option value="readonly">{t('compileCache.readonly')}</option>
            <option value="readwrite">{t('compileCache.readwrite')}</option>
          </SelectV2>
          <SelectV2
            label={t('compileCache.validity')}
            value={String(form.ttl_days)}
            onChange={event => setForm(current => ({ ...current, ttl_days: Number(event.target.value) }))}
          >
            <option value="7">{t('compileCache.days7')}</option>
            <option value="30">{t('compileCache.days30')}</option>
            <option value="90">{t('compileCache.days90')}</option>
            <option value="0">{t('compileCache.neverExpires')}</option>
          </SelectV2>
          {createMutation.isError && (
            <InlineNotice tone="danger">{getApiError(createMutation.error).message}</InlineNotice>
          )}
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" disabled={createMutation.isPending} onClick={closeCreateDialog}>
              {t('cancel')}
            </ButtonV2>
            <ButtonV2
              type="submit"
              aria-busy={createMutation.isPending || undefined}
              disabled={!formValid || createMutation.isPending || !canWrite}
            >
              {t(createMutation.isPending ? 'compileCache.creating' : 'compileCache.create')}
            </ButtonV2>
          </div>
        </form>
      </ModalV2>

      <ModalV2
        open={clientConfigurations !== null}
        onClose={closeClientConfigurations}
        title={t('compileCache.credentialCreated')}
        width={720}
      >
        <div className="space-y-4">
          <InlineNotice tone="warning" title={t('compileCache.oneTimeTitle')}>
            {t('compileCache.oneTimeWarning')}
          </InlineNotice>
          {clientConfigurations && (
            <div className="grid gap-4 md:grid-cols-2">
              {([
                { client: 'ccache', label: t('compileCache.remoteStorage'), value: clientConfigurations.ccache },
                { client: 'sccache', label: t('compileCache.sccacheConfig'), value: clientConfigurations.sccache },
              ] as const).map(item => {
                const target = `${item.client}-config` as CopiedValue
                return (
                  <section key={item.client} className="flex min-w-0 flex-col rounded-[7px] border border-[var(--border)] p-4">
                    <h3 className="font-mono text-[14px] font-[650] text-[var(--text)]">{item.client}</h3>
                    <p className="mt-1 text-[11px] text-[var(--text-muted)]">{item.label}</p>
                    <div className="mt-3 flex-1 rounded-[5px] bg-[var(--bg-soft)] p-3">
                      <code className="block whitespace-pre-wrap break-all font-mono text-[12px] leading-5 text-[var(--text)]">
                        {item.value}
                      </code>
                    </div>
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      className="mt-3 self-start"
                      aria-label={t(item.client === 'ccache' ? 'compileCache.copyCCacheConfig' : 'compileCache.copySCCacheConfig')}
                      onClick={() => { void copyValue(item.value, target) }}
                    >
                      <Icon name={copiedValue === target ? 'check' : 'content_copy'} size="sm" />
                      {t(copiedValue === target
                        ? 'compileCache.copied'
                        : item.client === 'ccache'
                          ? 'compileCache.copyCCacheConfig'
                          : 'compileCache.copySCCacheConfig')}
                    </ButtonV2>
                  </section>
                )
              })}
            </div>
          )}
          <div className="flex flex-wrap justify-end gap-3">
            <ButtonV2 type="button" onClick={closeClientConfigurations}>{t('common.close')}</ButtonV2>
          </div>
        </div>
      </ModalV2>

      <ModalV2
        open={revokeTarget !== null}
        onClose={() => {
          if (revokeMutation.isPending) return
          setRevokeTarget(null)
          revokeMutation.reset()
        }}
        title={t('compileCache.revokeTitle')}
      >
        <p className="text-[13px] leading-5 text-[var(--text-soft)]">
          {t('compileCache.revokeHint', { name: revokeTarget?.name, namespace: revokeTarget?.namespace })}
        </p>
        {revokeMutation.isError && (
          <div className="mt-4"><InlineNotice tone="danger">{getApiError(revokeMutation.error).message}</InlineNotice></div>
        )}
        <div className="mt-5 flex justify-end gap-3">
          <ButtonV2
            type="button"
            variant="secondary"
            disabled={revokeMutation.isPending}
            onClick={() => setRevokeTarget(null)}
          >
            {t('cancel')}
          </ButtonV2>
          <ButtonV2
            type="button"
            variant="danger"
            aria-busy={revokeMutation.isPending || undefined}
            disabled={!revokeTarget || revokeMutation.isPending || !canWrite}
            onClick={() => revokeTarget && revokeMutation.mutate(revokeTarget.id)}
          >
            {t(revokeMutation.isPending ? 'compileCache.revoking' : 'compileCache.confirmRevoke')}
          </ButtonV2>
        </div>
      </ModalV2>

      <ModalV2
        open={cleanupOpen}
        onClose={() => {
          if (cleanupMutation.isPending) return
          setCleanupOpen(false)
          cleanupMutation.reset()
        }}
        title={t('compileCache.cleanupTitle')}
      >
        <p className="text-[13px] leading-5 text-[var(--text-soft)]">{t('compileCache.cleanupHint')}</p>
        {cleanupMutation.isError && (
          <div className="mt-4"><InlineNotice tone="danger">{getApiError(cleanupMutation.error).message}</InlineNotice></div>
        )}
        <div className="mt-5 flex justify-end gap-3">
          <ButtonV2
            type="button"
            variant="secondary"
            disabled={cleanupMutation.isPending}
            onClick={() => setCleanupOpen(false)}
          >
            {t('cancel')}
          </ButtonV2>
          <ButtonV2
            type="button"
            variant="danger"
            aria-busy={cleanupMutation.isPending || undefined}
            disabled={cleanupMutation.isPending || !status?.enabled || !canWrite}
            onClick={() => cleanupMutation.mutate()}
          >
            {t(cleanupMutation.isPending ? 'compileCache.cleaning' : 'compileCache.confirmCleanup')}
          </ButtonV2>
        </div>
      </ModalV2>
    </AdminPage>
  )
}
