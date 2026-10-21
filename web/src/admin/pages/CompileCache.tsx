import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Field, FieldLabel, FieldDescription } from '@/components/ui/field'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { Badge } from '@/components/ui/badge'
import { Meter, MeterIndicator, MeterTrack } from '@/components/ui/meter'
import { Button } from '@/components/ui/button'
import DataTable from '@/components/DataTable'

import Icon from '@/components/Icon'

import { Input } from '@/components/ui/input'
import Metric from '@/components/Metric'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import { NativeSelect } from '@/components/ui/native-select'
import { useAppToast } from '@/components/Toast'
import { usePrincipal } from '@/hooks/usePrincipal'
import { useTransientState } from '@/hooks/useTransientFlag'
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
  const [copiedValue, showCopiedValue, resetCopiedValue] = useTransientState<CopiedValue | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<CompileCacheCredential | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['admin', 'compile-cache', 'status'],
    queryFn: ({ signal }) => adminApi.getCompileCacheStatus({ signal }),
    retry: false,
    refetchInterval: 30000,
  })
  const credentialsQuery = useQuery({
    queryKey: ['admin', 'compile-cache', 'credentials'],
    queryFn: ({ signal }) => adminApi.listCompileCacheCredentials({ signal }),
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
    resetCopiedValue()
    createMutation.reset()
  }

  async function copyValue(value: string, target: CopiedValue) {
    if (!await copyText(value)) return
    showCopiedValue(target)
  }

  function refreshAll() {
    void statusQuery.refetch()
    void credentialsQuery.refetch()
  }

  const statusError = getApiError(statusQuery.error)
  const credentialsError = getApiError(credentialsQuery.error)
  const isRefreshing = statusQuery.isFetching || credentialsQuery.isFetching

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
          <Badge variant={permissions === 'readwrite' ? 'success' : 'default'}>
            {t(permissions === 'readwrite' ? 'compileCache.readwrite' : 'compileCache.readonly')}
          </Badge>
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
        <Button
          type="button"
          variant="destructive"
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
        </Button>
      ) : null,
    },
  ]

  return (
    <AdminPage
      description={t('compileCache.subtitle')}
      actions={(
        <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-busy={isRefreshing || undefined}
            disabled={isRefreshing}
            onClick={refreshAll}
          >
            <Icon name={isRefreshing ? 'progress_activity' : 'refresh'} size="sm" />
            {t(isRefreshing ? 'compileCache.refreshing' : 'compileCache.refresh')}
          </Button>
          {canWrite && (
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={!status?.enabled}
              onClick={() => {
                cleanupMutation.reset()
                setCleanupOpen(true)
              }}
            >
              <Icon name="delete_sweep" size="sm" />
              {t('compileCache.cleanup')}
            </Button>
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
              <Badge variant={status.enabled ? 'success' : 'warning'}>
                {t(status.enabled ? 'compileCache.enabled' : 'compileCache.disabled')}
              </Badge>
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
                        {formatBytes(status.stats.size_bytes)}{status.stats.max_bytes > 0 && ` / ${formatBytes(status.stats.max_bytes)}`}
                      </span>
                    </div>
                    {status.stats.max_bytes > 0 && <Meter
                      aria-label={t('compileCache.capacity')}
                      value={status.stats.size_bytes}
                      max={status.stats.max_bytes}
                      aria-valuetext={`${formatBytes(status.stats.size_bytes)} / ${formatBytes(status.stats.max_bytes)}`}
                    >
                      <MeterTrack className="h-2"><MeterIndicator /></MeterTrack>
                    </Meter>}
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
                          <Tooltip>
                            <TooltipTrigger render={
                              <Button
                                onClick={() => { void copyValue(status.endpoints[client], target) }}
                                variant="ghost"
                                size="icon"
                                data-icon-button
                                aria-label={t('compileCache.copyClientEndpoint', { client })}
                                style={{ color: 'var(--text-soft)' }}
                              />
                            }>
                              <Icon name={copiedValue === target ? 'check' : 'content_copy'} size="sm" />
                            </TooltipTrigger>
                            <TooltipContent>{t('compileCache.copyClientEndpoint', { client })}</TooltipContent>
                          </Tooltip>
                        </div>
                      )
                    })}
                  </div>
                </>
              ) : (
                <Alert variant={"warning"}><AlertTitle>{t('compileCache.disabledTitle')}</AlertTitle><AlertDescription>
                  <p>{t('compileCache.disabledHint')}</p>
                  <pre className="mt-2 overflow-x-auto font-mono text-[12px]">[compile_cache]{'\n'}enabled = true</pre>
                </AlertDescription></Alert>
              )}
            </div>
          ) : null}
        </section>

        <section>
          <SectionHeader
            title={t('compileCache.credentialsTitle')}
            hint={t('compileCache.credentialsHint')}
            action={canWrite ? (
              <Button type="button" size="sm" disabled={!status?.enabled} onClick={openCreateDialog}>
                <Icon name="add" size="sm" />
                {t('compileCache.createCredential')}
              </Button>
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
                <Empty style={{ minHeight: 180 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"key"} size="sm" /></EmptyMedia><EmptyTitle>{t('compileCache.noCredentials')}</EmptyTitle>{t('compileCache.noCredentialsHint') && <EmptyDescription>{t('compileCache.noCredentialsHint')}</EmptyDescription>}</EmptyHeader></Empty>
              ) : (
                <DataTable
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

      <Dialog open={createOpen} onOpenChange={(nextOpen) => { if (!nextOpen) closeCreateDialog() }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true}>
          <DialogTitle className="pr-10">{t('compileCache.createCredential')}</DialogTitle>
          <form className="space-y-4" onSubmit={handleCreate}>
            <Field>
              <FieldLabel>{t('compileCache.credentialName')}</FieldLabel>
              <Input
                value={form.name}
                maxLength={128}
                autoComplete="off"
                required
                onChange={event => setForm(current => ({ ...current, name: event.target.value }))}
              />
              {(t('compileCache.credentialNameHint')) && <FieldDescription>{t('compileCache.credentialNameHint')}</FieldDescription>}
            </Field>
            <Field>
              <FieldLabel>{t('compileCache.namespace')}</FieldLabel>
              <Input className="font-mono"
                value={form.namespace}
                maxLength={64}
                pattern="[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?"
                autoCapitalize="none"
                autoComplete="off"
                spellCheck={false}
                required
                onChange={event => setForm(current => ({ ...current, namespace: event.target.value.toLowerCase() }))}
              />
              {(t('compileCache.namespaceHint')) && <FieldDescription>{t('compileCache.namespaceHint')}</FieldDescription>}
            </Field>
            <Field>
              <FieldLabel>{t('compileCache.permissions')}</FieldLabel>
              <NativeSelect
                value={form.permissions}
                onChange={event => setForm(current => ({
                  ...current,
                  permissions: event.target.value as CompileCachePermissions,
                }))}
              >
                <option value="readonly">{t('compileCache.readonly')}</option>
                <option value="readwrite">{t('compileCache.readwrite')}</option>
              </NativeSelect>
            </Field>
            <Field>
              <FieldLabel>{t('compileCache.validity')}</FieldLabel>
              <NativeSelect
                value={String(form.ttl_days)}
                onChange={event => setForm(current => ({ ...current, ttl_days: Number(event.target.value) }))}
              >
                <option value="7">{t('compileCache.days7')}</option>
                <option value="30">{t('compileCache.days30')}</option>
                <option value="90">{t('compileCache.days90')}</option>
                <option value="0">{t('compileCache.neverExpires')}</option>
              </NativeSelect>
            </Field>
            {createMutation.isError && (
              <Alert variant={"destructive"}><AlertDescription>{getApiError(createMutation.error).message}</AlertDescription></Alert>
            )}
            <div className="flex justify-end gap-3 pt-2">
              <Button type="button" variant="outline" disabled={createMutation.isPending} onClick={closeCreateDialog}>
                {t('cancel')}
              </Button>
              <Button
                type="submit"
                aria-busy={createMutation.isPending || undefined}
                disabled={!formValid || createMutation.isPending || !canWrite}
              >
                {t(createMutation.isPending ? 'compileCache.creating' : 'compileCache.create')}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={clientConfigurations !== null} onOpenChange={(nextOpen) => { if (!nextOpen) closeClientConfigurations() }} modal>
        <DialogContent style={{ maxWidth: 720 }} finalFocus={true}>
          <DialogTitle className="pr-10">{t('compileCache.credentialCreated')}</DialogTitle>
          <div className="space-y-4">
            <Alert variant={"warning"}><AlertTitle>{t('compileCache.oneTimeTitle')}</AlertTitle><AlertDescription>
              {t('compileCache.oneTimeWarning')}
            </AlertDescription></Alert>
            {clientConfigurations && (
              <div className="grid gap-4 md:grid-cols-2">
                {([
                  { client: 'ccache', label: t('compileCache.remoteStorage'), value: clientConfigurations.ccache },
                  { client: 'sccache', label: t('compileCache.sccacheConfig'), value: clientConfigurations.sccache },
                ] as const).map(item => {
                  const target = `${item.client}-config` as CopiedValue
                  return (
                    <section key={item.client} className="flex min-w-0 flex-col rounded-md border border-[var(--border)] p-4">
                      <h3 className="font-mono text-[14px] font-[650] text-[var(--text)]">{item.client}</h3>
                      <p className="mt-1 text-[11px] text-[var(--text-muted)]">{item.label}</p>
                      <div className="mt-3 flex-1 rounded-sm bg-[var(--bg-soft)] p-3">
                        <code className="block whitespace-pre-wrap break-all font-mono text-[12px] leading-5 text-[var(--text)]">
                          {item.value}
                        </code>
                      </div>
                      <Button
                        type="button"
                        variant="outline"
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
                      </Button>
                    </section>
                  )
                })}
              </div>
            )}
            <div className="flex flex-wrap justify-end gap-3">
              <Button type="button" onClick={closeClientConfigurations}>{t('common.close')}</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={revokeTarget !== null} onOpenChange={(nextOpen) => {
        if (!nextOpen) (() => {
          if (revokeMutation.isPending) return
          setRevokeTarget(null)
          revokeMutation.reset()
        })()
      }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true}>
          <DialogTitle className="pr-10">{t('compileCache.revokeTitle')}</DialogTitle>
          <p className="text-[13px] leading-5 text-[var(--text-soft)]">
            {t('compileCache.revokeHint', { name: revokeTarget?.name, namespace: revokeTarget?.namespace })}
          </p>
          {revokeMutation.isError && (
            <div className="mt-4"><Alert variant={"destructive"}><AlertDescription>{getApiError(revokeMutation.error).message}</AlertDescription></Alert></div>
          )}
          <div className="mt-5 flex justify-end gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={revokeMutation.isPending}
              onClick={() => setRevokeTarget(null)}
            >
              {t('cancel')}
            </Button>
            <Button
              type="button"
              variant="destructive"
              aria-busy={revokeMutation.isPending || undefined}
              disabled={!revokeTarget || revokeMutation.isPending || !canWrite}
              onClick={() => revokeTarget && revokeMutation.mutate(revokeTarget.id)}
            >
              {t(revokeMutation.isPending ? 'compileCache.revoking' : 'compileCache.confirmRevoke')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={cleanupOpen} onOpenChange={(nextOpen) => {
        if (!nextOpen) (() => {
          if (cleanupMutation.isPending) return
          setCleanupOpen(false)
          cleanupMutation.reset()
        })()
      }} modal>
        <DialogContent style={{ maxWidth: 440 }} finalFocus={true}>
          <DialogTitle className="pr-10">{t('compileCache.cleanupTitle')}</DialogTitle>
          <p className="text-[13px] leading-5 text-[var(--text-soft)]">{t('compileCache.cleanupHint')}</p>
          {cleanupMutation.isError && (
            <div className="mt-4"><Alert variant={"destructive"}><AlertDescription>{getApiError(cleanupMutation.error).message}</AlertDescription></Alert></div>
          )}
          <div className="mt-5 flex justify-end gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={cleanupMutation.isPending}
              onClick={() => setCleanupOpen(false)}
            >
              {t('cancel')}
            </Button>
            <Button
              type="button"
              variant="destructive"
              aria-busy={cleanupMutation.isPending || undefined}
              disabled={cleanupMutation.isPending || !status?.enabled || !canWrite}
              onClick={() => cleanupMutation.mutate()}
            >
              {t(cleanupMutation.isPending ? 'compileCache.cleaning' : 'compileCache.confirmCleanup')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </AdminPage>
  )
}
