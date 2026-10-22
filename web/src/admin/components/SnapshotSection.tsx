import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel } from '@/components/ui/field'
import { useRef, useState } from 'react'
import { usePrincipal } from '@/hooks/usePrincipal'
import { useSearchParams } from 'react-router'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import SectionHeader from '@/components/SectionHeader'

import QueryErrorState from '@/components/QueryErrorState'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'

import type { Snapshot } from '@/lib/adminApi.types'

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  const value = bytes / 1024 ** exponent
  return `${value >= 10 || exponent === 0 ? Math.round(value) : value.toFixed(1)} ${units[exponent]}`
}

// SnapshotSection is the freeze / golden-snapshot control on the cache page:
// promote the current cache, import/export manifests, and switch
// snapshot-only mode on or off.
export default function SnapshotSection() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const [searchParams] = useSearchParams()
  const [activationTarget, setActivationTarget] = useState<Snapshot | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const [name, setName] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<Snapshot | null>(null)

  const snapshotsQuery = useQuery({
    queryKey: ['admin', 'snapshots'],
    queryFn: ({ signal }) => adminApi.listSnapshots({ signal }),
    retry: false,
  })
  const detailQuery = useQuery({
    queryKey: ['admin', 'snapshots', activationTarget?.id],
    queryFn: ({ signal }) => adminApi.getSnapshot(activationTarget!.id, { signal }),
    enabled: activationTarget !== null, retry: false,
  })
  const data = snapshotsQuery.data?.data
  const snapshots = data?.items ?? []
  const activeId = data?.active_snapshot_id ?? 0
  const activeName = data?.active_snapshot ?? ''

  const dateFormat = new Intl.DateTimeFormat(i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en-US', {
    dateStyle: 'medium',
    timeStyle: 'short',
  })

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'snapshots'] })
  }
  function fail(err: unknown) {
    setError(getApiError(err).message)
  }

  const createMutation = useMutation({
    mutationFn: () => adminApi.createSnapshot({ name: name.trim(), note: note.trim() }),
    onSuccess: () => {
      setName('')
      setNote('')
      setError('')
      invalidate()
    },
    onError: fail,
  })
  const activateMutation = useMutation({
    mutationFn: (id: number) => adminApi.activateSnapshot(id),
    onSuccess: () => {
      setActivationTarget(null)
      setError('')
      invalidate()
    },
    onError: fail,
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteSnapshot(id),
    onSuccess: () => {
      setDeleteTarget(null)
      setError('')
      invalidate()
    },
    onError: fail,
  })
  const importMutation = useMutation({
    mutationFn: async (file: File) => adminApi.importSnapshot(await file.text()),
    onSuccess: () => {
      setError('')
      invalidate()
    },
    onError: fail,
  })

  async function handleExport(snapshot: Snapshot) {
    try {
      const response = await adminApi.exportSnapshot(snapshot.id)
      const url = URL.createObjectURL(new Blob([response.data], { type: 'application/json' }))
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `snapshot-${snapshot.name}.json`
      document.body.appendChild(anchor)
      anchor.click()
      document.body.removeChild(anchor)
      URL.revokeObjectURL(url)
    } catch (err) {
      fail(err)
    }
  }

  return (
    <section>
      <SectionHeader title={t('snapshot.title')} hint={t('snapshot.description')} />
      {error && <Alert variant={"destructive"}><AlertDescription>{error}</AlertDescription></Alert>}
      {activeId !== 0 && (
        <Alert variant={"warning"}><AlertDescription>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span>{t('snapshot.activeNotice', { name: activeName })}</span>
            {canWrite && <Button variant="outline" size="sm" onClick={() => activateMutation.mutate(0)} disabled={activateMutation.isPending}>
              {t('snapshot.deactivate')}
            </Button>}
          </div>
        </AlertDescription></Alert>
      )}
      <details open={searchParams.get('view') === 'snapshots' || snapshotsQuery.isError} className="mt-3">
      <summary className="cursor-pointer text-sm font-medium">{t('snapshot.manage', { count: data ? snapshots.length : '—' })}</summary>
      {canWrite && <>
      <div className="mt-3 grid min-w-0 gap-3 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto] sm:items-end">
        <Field>
          <FieldLabel>{t('snapshot.name')}</FieldLabel>
          <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('snapshot.namePlaceholder')} />
        </Field>
        <Field>
          <FieldLabel>{t('snapshot.note')}</FieldLabel>
          <Input value={note} onChange={(event) => setNote(event.target.value)} />
        </Field>
        <Button
          className="w-full sm:w-auto"
          onClick={() => createMutation.mutate()}
          disabled={createMutation.isPending || name.trim() === ''}
        >
          <Icon name="storage" size="sm" />
          {createMutation.isPending ? t('snapshot.creating') : t('snapshot.create')}
        </Button>
      </div>
      <p className="mt-2 text-[13px] text-[var(--text-soft)]">{t('snapshot.createHint')}</p>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          className="hidden"
          aria-label={t('snapshot.import')}
          onChange={(event) => {
            const file = event.target.files?.[0]
            if (file) importMutation.mutate(file)
            event.target.value = ''
          }}
        />
        <Button variant="outline" size="sm" onClick={() => fileInput.current?.click()} disabled={importMutation.isPending}>
          <Icon name="upload_file" size="sm" />
          {importMutation.isPending ? t('snapshot.importing') : t('snapshot.import')}
        </Button>
      </div>

      </>}
      {data && snapshotsQuery.isRefetchError && <StaleDataNotice onRefresh={() => snapshotsQuery.refetch()} />}
      <div className="mt-4">
        {snapshotsQuery.isPending ? (
          <div aria-busy="true" className="py-6 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>
            <span aria-hidden="true">{t('loading')}</span>
          </div>
        ) : snapshotsQuery.isError && !data ? (
          <QueryErrorState message={getApiError(snapshotsQuery.error).message} onRetry={() => { void snapshotsQuery.refetch() }} />
        ) : snapshots.length === 0 ? (
          <Empty style={{ minHeight: 140 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"storage"} size="sm" /></EmptyMedia><EmptyTitle>{t('snapshot.empty')}</EmptyTitle></EmptyHeader></Empty>
        ) : (
          <TableContainer label={t('snapshot.table')} minWidth={760}>
            <Table className="w-full text-[12px]">
              <TableHeader>
                <TableRow style={{ color: 'var(--text-soft)' }}>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('snapshot.name')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('snapshot.artifacts')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('snapshot.size')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('snapshot.createdAt')}</TableHead>
                  <TableHead className="px-2 py-2 text-right font-[500]">{t('snapshot.actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {snapshots.map((snapshot) => (
                  <TableRow key={snapshot.id} style={{ borderTop: '1px solid var(--border)' }}>
                    <TableCell className="px-2 py-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="min-w-0 truncate" style={{ color: 'var(--text)' }}>{snapshot.name}</span>
                        {snapshot.id === activeId && <Badge variant="success">{t('snapshot.active')}</Badge>}
                      </div>
                      {snapshot.note && <p className="mt-0.5 truncate text-[12px] text-[var(--text-soft)]">{snapshot.note}</p>}
                    </TableCell>
                    <TableCell className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{snapshot.artifact_count}</TableCell>
                    <TableCell className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{formatBytes(snapshot.total_bytes)}</TableCell>
                    <TableCell className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{dateFormat.format(new Date(snapshot.created_at))}</TableCell>
                    <TableCell className="px-2 py-2">
                      <div className="flex justify-end gap-1.5">
                        {canWrite && (snapshot.id === activeId ? (
                          <Button variant="outline" size="sm" onClick={() => activateMutation.mutate(0)} disabled={activateMutation.isPending}>
                            {t('snapshot.deactivate')}
                          </Button>
                        ) : (
                          <Button variant="outline" size="sm" onClick={() => { activateMutation.reset(); setActivationTarget(snapshot) }} disabled={activateMutation.isPending}>
                            {t('snapshot.activate')}
                          </Button>
                        ))}
                        <Button variant="outline" size="sm" onClick={() => { void handleExport(snapshot) }}>
                          <Icon name="download" size="sm" />
                          {t('snapshot.export')}
                        </Button>
                        {canWrite && <Button variant="outline" size="sm" onClick={() => setDeleteTarget(snapshot)}>
                          <Icon name="delete" size="sm" />
                          {t('delete')}
                        </Button>}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </div>

      </details>
      <Dialog open={activationTarget !== null} onOpenChange={open => { if (!open && !activateMutation.isPending) setActivationTarget(null) }}>
        <DialogContent className="max-w-xl" closeDisabled={activateMutation.isPending}>
          <DialogTitle>{t('snapshot.activationTitle', { name: activationTarget?.name })}</DialogTitle>
          <p className="text-sm leading-6 text-muted-foreground">{t('snapshot.activationImpact')}</p>
          {detailQuery.isPending ? <p role="status">{t('loading')}</p> : detailQuery.isError ? <QueryErrorState message={getApiError(detailQuery.error).message} onRetry={() => detailQuery.refetch()} /> : detailQuery.data && <>
            <p className="text-sm">{t('snapshot.activationScope', { count: detailQuery.data.data.total, shown: detailQuery.data.data.items.length })}</p>
            <ul className="max-h-60 space-y-2 overflow-auto rounded-md border p-3 text-sm">
              {detailQuery.data.data.items.map(item => <li key={item.id} className="break-all"><span className="text-muted-foreground">{item.ecosystem} · </span><code>{item.package}@{item.version}</code></li>)}
            </ul>
          </>}
          {activateMutation.isError && <Alert variant="destructive"><AlertDescription>{getApiError(activateMutation.error).message}</AlertDescription></Alert>}
          <div className="flex flex-wrap justify-end gap-3">
            <Button variant="outline" disabled={activateMutation.isPending} onClick={() => setActivationTarget(null)}>{t('cancel')}</Button>
            <Button disabled={!canWrite || !detailQuery.isSuccess || activateMutation.isPending} onClick={() => activationTarget && activateMutation.mutate(activationTarget.id)}>{t('snapshot.confirmActivation')}</Button>
          </div>
        </DialogContent>
      </Dialog>
      <ConfirmActionDialog
        open={deleteTarget !== null}
        title={t('snapshot.deleteTitle')}
        description={t('snapshot.deleteDescription', { name: deleteTarget?.name ?? '' })}
        cancelLabel={t('cancel')}
        confirmLabel={t('delete')}
        pendingLabel={t('loading')}
        pending={deleteMutation.isPending}
        errorMessage={deleteMutation.isError ? getApiError(deleteMutation.error).message : undefined}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (canWrite && deleteTarget) deleteMutation.mutate(deleteTarget.id)
        }}
      />
    </section>
  )
}
