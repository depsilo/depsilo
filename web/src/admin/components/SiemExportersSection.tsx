import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel } from '@/components/ui/field'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { NativeSelect } from '@/components/ui/native-select'
import SectionHeader from '@/components/SectionHeader'

import QueryErrorState from '@/components/QueryErrorState'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'

import { usePrincipal } from '@/hooks/usePrincipal'
import { formatTime } from '@/lib/utils'
import type { AuditExporter, AuditExporterKind } from '@/lib/adminApi.types'

// SiemExportersSection routes the durable audit stream to external collectors.
// It is deliberately plain: name, format, endpoint, optional token, and an
// event filter; delivery state (lag, last error) stays visible next to each
// collector so a broken feed cannot fail silently.
export default function SiemExportersSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const [form, setForm] = useState<{ name: string; kind: AuditExporterKind; url: string; token: string; events: string }>({
    name: '',
    kind: 'ndjson',
    url: '',
    token: '',
    events: '*',
  })
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<AuditExporter | null>(null)

  const query = useQuery({
    queryKey: ['admin', 'audit', 'exporters'],
    queryFn: ({ signal }) => adminApi.listAuditExporters({ signal }),
    retry: false,
  })
  const data = query.data?.data
  const exporters = data?.items ?? []

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'audit', 'exporters'] })
  }
  function fail(err: unknown) {
    setNotice('')
    setError(getApiError(err).message)
  }

  const createMutation = useMutation({
    mutationFn: () => adminApi.createAuditExporter({
      name: form.name.trim(),
      kind: form.kind,
      url: form.url.trim(),
      token: form.token,
      events: form.events.trim() || '*',
    }),
    onSuccess: () => {
      setForm({ name: '', kind: 'ndjson', url: '', token: '', events: '*' })
      setError('')
      setNotice(t('siem.created'))
      invalidate()
    },
    onError: fail,
  })
  const toggleMutation = useMutation({
    mutationFn: (exporter: AuditExporter) => adminApi.updateAuditExporter(exporter.id, { enabled: !exporter.enabled }),
    onSuccess: () => {
      setError('')
      invalidate()
    },
    onError: fail,
  })
  const testMutation = useMutation({
    mutationFn: (id: number) => adminApi.testAuditExporter(id),
    onSuccess: (_result, id) => {
      setError('')
      const exporter = exporters.find(item => item.id === id)
      setNotice(t('siem.testDelivered', { name: exporter?.name ?? '' }))
      invalidate()
    },
    onError: fail,
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteAuditExporter(id),
    onSuccess: () => {
      setDeleteTarget(null)
      setError('')
      setNotice('')
      invalidate()
    },
    onError: fail,
  })

  return (
    <section data-siem-exporters>
      <SectionHeader title={t('siem.title')} hint={t('siem.description')} />
      {error && <Alert variant={"destructive"}><AlertDescription>{error}</AlertDescription></Alert>}
      {notice && <Alert variant={"success"}><AlertDescription>{notice}</AlertDescription></Alert>}

      {canWrite && (
        <div className="mt-3 grid min-w-0 items-end gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <Field>
            <FieldLabel>{t('siem.name')}</FieldLabel>
            <Input value={form.name} onChange={event => setForm(value => ({ ...value, name: event.target.value }))} placeholder={t('siem.namePlaceholder')} />
          </Field>
          <Field>
            <FieldLabel>{t('siem.kind')}</FieldLabel>
            <NativeSelect value={form.kind} onChange={event => setForm(value => ({ ...value, kind: event.target.value as AuditExporterKind }))}>
              <option value="ndjson">{t('siem.kindNdjson')}</option>
              <option value="splunk_hec">{t('siem.kindSplunk')}</option>
            </NativeSelect>
          </Field>
          <Field>
            <FieldLabel>{t('siem.url')}</FieldLabel>
            <Input
              value={form.url}
              onChange={event => setForm(value => ({ ...value, url: event.target.value }))}
              placeholder={form.kind === 'splunk_hec' ? t('siem.urlSplunkPlaceholder') : t('siem.urlPlaceholder')}
            />
          </Field>
          <Field>
            <FieldLabel>{t('siem.token')}</FieldLabel>
            <Input type="password" value={form.token} onChange={event => setForm(value => ({ ...value, token: event.target.value }))} placeholder={t('siem.tokenOptional')} />
          </Field>
          <Field>
            <FieldLabel>{t('siem.events')}</FieldLabel>
            <Input value={form.events} onChange={event => setForm(value => ({ ...value, events: event.target.value }))} placeholder="*" />
          </Field>
          <Button
            className="w-full"
            onClick={() => createMutation.mutate()}
            disabled={createMutation.isPending || form.name.trim() === '' || form.url.trim() === ''}
          >
            <Icon name="add" size="sm" />
            {createMutation.isPending ? t('siem.creating') : t('siem.create')}
          </Button>
        </div>
      )}
      <p className="mt-2 text-[13px] text-[var(--text-soft)]">{t('siem.filterHint')}</p>

      <div className="mt-4">
        {query.isPending ? (
          <div aria-busy="true" className="py-6 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>
            <span aria-hidden="true">{t('loading')}</span>
          </div>
        ) : query.isError && !data ? (
          <QueryErrorState message={getApiError(query.error).message} onRetry={() => { void query.refetch() }} />
        ) : exporters.length === 0 ? (
          <Empty style={{ minHeight: 140 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"receipt_long"} size="sm" /></EmptyMedia><EmptyTitle>{t('siem.empty')}</EmptyTitle></EmptyHeader></Empty>
        ) : (
          <TableContainer label={t('siem.table')} minWidth={860}>
            <Table className="w-full text-[12px]">
              <TableHeader>
                <TableRow style={{ color: 'var(--text-soft)' }}>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('siem.name')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('siem.kind')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('siem.status')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('siem.events')}</TableHead>
                  <TableHead className="px-2 py-2 text-right font-[500]">{t('siem.delivered')}</TableHead>
                  <TableHead className="px-2 py-2 text-right font-[500]">{t('siem.lag')}</TableHead>
                  <TableHead className="px-2 py-2 text-left font-[500]">{t('siem.lastDelivery')}</TableHead>
                  <TableHead className="px-2 py-2 text-right font-[500]">{t('siem.actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {exporters.map(exporter => (
                  <TableRow key={exporter.id} style={{ borderTop: '1px solid var(--border)' }}>
                    <TableCell className="px-2 py-3">
                      <div className="flex min-w-0 flex-col">
                        <span className="min-w-0 truncate" style={{ color: 'var(--text)' }}>{exporter.name}</span>
                        <span className="min-w-0 truncate font-mono text-[12px] text-[var(--text-soft)]" title={exporter.url}>{exporter.url}</span>
                      </div>
                    </TableCell>
                    <TableCell className="px-2 py-3" style={{ color: 'var(--text-soft)' }}>
                      {exporter.kind === 'splunk_hec' ? 'Splunk HEC' : 'NDJSON'}
                      {exporter.token_set && <span className="ml-1"><Icon name="key" size="sm" /></span>}
                    </TableCell>
                    <TableCell className="px-2 py-3">
                      <Badge variant={exporter.enabled ? 'success' : 'neutral'}>
                        {exporter.enabled ? t('siem.enabled') : t('siem.disabled')}
                      </Badge>
                    </TableCell>
                    <TableCell className="px-2 py-3 font-mono" style={{ color: 'var(--text-soft)' }}>{exporter.events}</TableCell>
                    <TableCell className="px-2 py-3 text-right font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                      {exporter.delivered_count.toLocaleString()}
                    </TableCell>
                    <TableCell className="px-2 py-3 text-right font-mono tabular-nums" style={{ color: exporter.lag > 0 ? 'var(--dash-warn)' : 'var(--text-soft)' }}>
                      {exporter.lag.toLocaleString()}
                    </TableCell>
                    <TableCell className="max-w-[220px] px-2 py-3">
                      {exporter.last_error ? (
                        <span className="block truncate text-[12px]" style={{ color: 'var(--danger)' }} title={exporter.last_error}>{exporter.last_error}</span>
                      ) : (
                        <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
                          {exporter.last_success_at ? formatTime(exporter.last_success_at, 'auto') : t('siem.never')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="px-2 py-3">
                      <div className="flex justify-end gap-1.5">
                        <Button variant="outline" size="sm" onClick={() => testMutation.mutate(exporter.id)} disabled={testMutation.isPending}>
                          {t('siem.test')}
                        </Button>
                        {canWrite && (
                          <Button variant="outline" size="sm" onClick={() => toggleMutation.mutate(exporter)} disabled={toggleMutation.isPending}>
                            {exporter.enabled ? t('siem.disable') : t('siem.enable')}
                          </Button>
                        )}
                        {canWrite && (
                          <Button variant="outline" size="sm" onClick={() => setDeleteTarget(exporter)}>
                            <Icon name="delete" size="sm" />
                            {t('delete')}
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </div>

      <ConfirmActionDialog
        open={deleteTarget !== null}
        title={t('siem.deleteTitle')}
        description={t('siem.deleteDescription', { name: deleteTarget?.name ?? '' })}
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
