import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import InputV2 from '@/components/Input'
import BadgeV2 from '@/components/Badge'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import TableViewport from '@/components/TableViewport'
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
      {error && <InlineNotice tone="danger">{error}</InlineNotice>}
      {activeId !== 0 && (
        <InlineNotice tone="warning">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span>{t('snapshot.activeNotice', { name: activeName })}</span>
            <ButtonV2 variant="secondary" size="sm" onClick={() => activateMutation.mutate(0)} disabled={activateMutation.isPending}>
              {t('snapshot.deactivate')}
            </ButtonV2>
          </div>
        </InlineNotice>
      )}
      <div className="mt-3 grid min-w-0 gap-3 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto] sm:items-end">
        <InputV2 label={t('snapshot.name')} value={name} onChange={(event) => setName(event.target.value)} placeholder={t('snapshot.namePlaceholder')} />
        <InputV2 label={t('snapshot.note')} value={note} onChange={(event) => setNote(event.target.value)} />
        <ButtonV2
          className="w-full sm:w-auto"
          onClick={() => createMutation.mutate()}
          disabled={createMutation.isPending || name.trim() === ''}
        >
          <Icon name="storage" size="sm" />
          {createMutation.isPending ? t('snapshot.creating') : t('snapshot.create')}
        </ButtonV2>
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
        <ButtonV2 variant="secondary" size="sm" onClick={() => fileInput.current?.click()} disabled={importMutation.isPending}>
          <Icon name="upload_file" size="sm" />
          {importMutation.isPending ? t('snapshot.importing') : t('snapshot.import')}
        </ButtonV2>
      </div>

      <div className="mt-4">
        {snapshotsQuery.isPending ? (
          <div aria-busy="true" className="py-6 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>
            <span aria-hidden="true">{t('loading')}</span>
          </div>
        ) : snapshotsQuery.isError && !data ? (
          <QueryErrorState message={getApiError(snapshotsQuery.error).message} onRetry={() => { void snapshotsQuery.refetch() }} />
        ) : snapshots.length === 0 ? (
          <EmptyState icon="storage" title={t('snapshot.empty')} minHeight={140} />
        ) : (
          <TableViewport label={t('snapshot.table')} minWidth={760}>
            <table className="w-full text-[12px]">
              <thead>
                <tr style={{ color: 'var(--text-soft)' }}>
                  <th className="px-2 py-2 text-left font-[500]">{t('snapshot.name')}</th>
                  <th className="px-2 py-2 text-left font-[500]">{t('snapshot.artifacts')}</th>
                  <th className="px-2 py-2 text-left font-[500]">{t('snapshot.size')}</th>
                  <th className="px-2 py-2 text-left font-[500]">{t('snapshot.createdAt')}</th>
                  <th className="px-2 py-2 text-right font-[500]">{t('snapshot.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {snapshots.map((snapshot) => (
                  <tr key={snapshot.id} style={{ borderTop: '1px solid var(--border)' }}>
                    <td className="px-2 py-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="min-w-0 truncate" style={{ color: 'var(--text)' }}>{snapshot.name}</span>
                        {snapshot.id === activeId && <BadgeV2 variant="success">{t('snapshot.active')}</BadgeV2>}
                      </div>
                      {snapshot.note && <p className="mt-0.5 truncate text-[12px] text-[var(--text-soft)]">{snapshot.note}</p>}
                    </td>
                    <td className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{snapshot.artifact_count}</td>
                    <td className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{formatBytes(snapshot.total_bytes)}</td>
                    <td className="px-2 py-2" style={{ color: 'var(--text-soft)' }}>{dateFormat.format(new Date(snapshot.created_at))}</td>
                    <td className="px-2 py-2">
                      <div className="flex justify-end gap-1.5">
                        {snapshot.id === activeId ? (
                          <ButtonV2 variant="secondary" size="sm" onClick={() => activateMutation.mutate(0)} disabled={activateMutation.isPending}>
                            {t('snapshot.deactivate')}
                          </ButtonV2>
                        ) : (
                          <ButtonV2 variant="secondary" size="sm" onClick={() => activateMutation.mutate(snapshot.id)} disabled={activateMutation.isPending}>
                            {t('snapshot.activate')}
                          </ButtonV2>
                        )}
                        <ButtonV2 variant="secondary" size="sm" onClick={() => { void handleExport(snapshot) }}>
                          <Icon name="download" size="sm" />
                          {t('snapshot.export')}
                        </ButtonV2>
                        <ButtonV2 variant="secondary" size="sm" onClick={() => setDeleteTarget(snapshot)}>
                          <Icon name="delete" size="sm" />
                          {t('delete')}
                        </ButtonV2>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableViewport>
        )}
      </div>

      <ConfirmActionDialog
        open={deleteTarget !== null}
        title={t('snapshot.deleteTitle')}
        description={t('snapshot.deleteDescription', { name: deleteTarget?.name ?? '' })}
        cancelLabel={t('cancel')}
        confirmLabel={t('delete')}
        pendingLabel={t('loading')}
        pending={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (deleteTarget) deleteMutation.mutate(deleteTarget.id)
        }}
      />
    </section>
  )
}
