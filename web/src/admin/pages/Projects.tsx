import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import Icon from '@/components/Icon'
import BadgeV2 from '@/components/Badge'
import ModalV2 from '@/components/Modal'
import DataTableV2 from '@/components/DataTable'
import SelectV2 from '@/components/Select'
import EcosystemIcon from '@/components/EcosystemIcon'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import ProRequiredCallout from '@/admin/components/ProRequiredCallout'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { useTransientFlag } from '@/hooks/useTransientFlag'
import type { CreateProjectRequest, ProjectDetail, ProjectSBOMFormat, ProjectSummary } from '@/lib/adminApi.types'

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, showCopied] = useTransientFlag()
  async function handleClick() {
    if (await copyText(text)) {
      showCopied()
    }
  }
  return (
    <IconButton
      icon={copied ? 'check' : 'content_copy'}
      label={label}
      onClick={handleClick}
      style={{ color: copied ? 'var(--ok-text)' : 'var(--text-soft)' }}
    />
  )
}

export default function ProjectsV2() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()

  const [selectedProject, setSelectedProject] = useState<ProjectSummary | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState({ name: '', description: '' })
  const [tokenData, setTokenData] = useState<{ token: string; proxy_url: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ProjectSummary | null>(null)
  const [pkgPage, setPkgPage] = useState(1)
  const [pkgEcosystem, setPkgEcosystem] = useState('')
  const [sbomFormat, setSbomFormat] = useState<ProjectSBOMFormat>('spdx')
  const [sbomEcosystem, setSbomEcosystem] = useState('')
  const [sbomLoading, setSbomLoading] = useState(false)

  const locale = i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en-US'
  const formatProjectTime = (value: string) => {
    if (!value) return '-'
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return '-'
    const now = new Date()
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const startOfDate = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
    const daysAgo = Math.round((startOfToday - startOfDate) / 86_400_000)
    if (daysAgo === 0) return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    if (daysAgo > 0 && daysAgo < 30) return t('now.daysAgo', { count: daysAgo })
    return date.toLocaleDateString(locale, { month: '2-digit', day: '2-digit' })
  }

  const query = useQuery({
    queryKey: ['admin', 'projects'],
    queryFn: ({ signal }) => adminApi.listProjects({ signal }),
    retry: false,
  })
  const { data } = query
  const projects = data?.data.items ?? []

  const detailQuery = useQuery({
    queryKey: ['admin', 'projects', selectedProject?.id],
    queryFn: ({ signal }) => adminApi.getProject(selectedProject!.id, { signal }),
    enabled: !!selectedProject,
    retry: false,
  })
  const detailData = detailQuery.data
  const projectDetail: ProjectDetail | ProjectSummary | null = detailData?.data ?? selectedProject

  const packagesQuery = useQuery({
    queryKey: ['admin', 'projects', selectedProject?.id, 'packages', pkgPage, pkgEcosystem],
    queryFn: ({ signal }) => adminApi.listProjectPackages(selectedProject!.id, { page: pkgPage, per_page: 20, ecosystem: pkgEcosystem || undefined }, { signal }),
    enabled: !!selectedProject,
    retry: false,
  })
  const pkgData = packagesQuery.data
  const packages = pkgData?.data.items ?? []
  const pkgTotal = pkgData?.data.total ?? 0

  const createMutation = useMutation({
    mutationFn: (input: CreateProjectRequest) => adminApi.createProject(input),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setCreateOpen(false)
      setCreateForm({ name: '', description: '' })
      const result = res.data
      if (result?.token) {
        setTokenData({ token: result.token, proxy_url: result.proxy_url || `${window.location.origin}/p/${result.slug}` })
      }
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteProject(id),
    onSuccess: (_response, deletedProjectId) => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setDeleteTarget(null)
      if (selectedProject?.id === deletedProjectId) {
        setSelectedProject(null)
      }
    },
  })

  function openDeleteDialog(project: ProjectSummary) {
    deleteMutation.reset()
    setDeleteTarget(project)
  }

  function closeDeleteDialog() {
    if (deleteMutation.isPending) return
    deleteMutation.reset()
    setDeleteTarget(null)
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!canWrite) return
    createMutation.mutate(createForm)
  }

  async function handleSbomDownload() {
    if (!selectedProject) return
    setSbomLoading(true)
    try {
      const res = await adminApi.exportSbom(selectedProject.id, {
        format: sbomFormat,
        ecosystem: sbomEcosystem || undefined,
      })
      const blob = new Blob([res.data])
      const ext = sbomFormat === 'spdx' ? 'spdx.json' : 'cdx.json'
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${projectDetail?.name || 'project'}-sbom.${ext}`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch {
      // ignore
    } finally {
      setSbomLoading(false)
    }
  }

  const apiError = getApiError(query.error)
  if (query.isError && !data && apiError.status === 402) {
    return (
      <AdminPage description={t('projects.subtitle')}>
        <ProRequiredCallout
          icon="folder_managed"
          title={t('projects.proRequired')}
          description={t('projects.proDesc')}
          upgradeLabel={t('projects.upgrade')}
        />
      </AdminPage>
    )
  }

  if (query.isPending) {
    return (
      <AdminPage description={t('projects.subtitle')}>
        <div role="status" aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]">{t('loading')}</div>
      </AdminPage>
    )
  }

  if (query.isError && !data) {
    return (
      <AdminPage description={t('projects.subtitle')}>
        <QueryErrorState message={apiError.status === 403 ? t('common.permissionDenied') : apiError.message} onRetry={() => { void query.refetch() }} />
      </AdminPage>
    )
  }

  // ── Detail view ────────────────────────────────────────────────
  if (selectedProject) {
    const proxyUrl = detailData?.data?.proxy_url ?? `${window.location.origin}/p/${projectDetail?.slug ?? ''}`
    const ecosystems: Record<string, number> = detailData?.data?.ecosystem_breakdown ?? {}

    const pkgColumns = [
      {
        key: 'ecosystem',
        label: t('type'),
        render: (v: unknown) => (
          <div className="flex items-center gap-1.5">
            {typeof v === 'string' && isAdminEcosystem(v) && <EcosystemIcon type={v} size={14} />}
            <BadgeV2 variant="ecosystem">{(v as string)?.toUpperCase()}</BadgeV2>
          </div>
        ),
      },
      { key: 'package_name', label: t('name'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
      { key: 'version', label: t('projects.version'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{(v as string) || '-'}</span> },
      { key: 'first_seen_at', label: t('projects.firstSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatProjectTime(v as string)}</span> },
      { key: 'last_seen_at', label: t('projects.lastSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatProjectTime(v as string)}</span> },
      { key: 'download_count', label: t('projects.downloads'), render: (v: unknown) => <span className="text-[12px] font-mono" style={{ color: 'var(--text-soft)' }}>{(v as number) ?? 0}</span> },
    ]

    return (
      <AdminPage description={t('projects.subtitle')}>
      <div className="space-y-12">
        {/* Detail header */}
        <div className="flex min-w-0 items-center gap-3">
          <IconButton
            icon="arrow_back"
            label={t('projects.backToList')}
            onClick={() => { setSelectedProject(null); setPkgPage(1); setPkgEcosystem('') }}
          />
          <h2 className="min-w-0 break-words text-[20px] font-[600] [overflow-wrap:anywhere]" style={{ color: 'var(--text)' }}>{projectDetail?.name}</h2>
        </div>

        {/* Project info */}
        <section>
          <SectionHeader title={t('projects.overview')} />
          {detailQuery.isPending ? (
            <div aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]"><span aria-hidden="true">{t('loading')}</span></div>
          ) : detailQuery.isError && !detailData ? (
            <QueryErrorState
              message={getApiError(detailQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(detailQuery.error).message}
              onRetry={() => { void detailQuery.refetch() }}
            />
          ) : detailData?.data ? (
          <div className="space-y-3">
            {detailQuery.isRefetchError && <StaleDataNotice refreshing={detailQuery.isFetching} onRefresh={() => detailQuery.refetch()} />}
            <div
              data-project-proxy-row
              className="grid min-w-0 gap-2 sm:grid-cols-[8rem_minmax(0,1fr)] sm:items-center sm:gap-3"
            >
              <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('projects.proxyUrl')}</span>
              <div className="flex min-w-0 items-center gap-2">
                <span
                  data-project-proxy-value
                  className="min-w-0 flex-1 break-all rounded-[4px] px-2 py-1 font-mono text-[12px] leading-5"
                  style={{ background: 'var(--bg-soft)', color: 'var(--text)' }}
                >
                  {proxyUrl}
                </span>
                <div data-project-proxy-copy className="shrink-0">
                  <CopyButton text={proxyUrl} label={t('projects.copyProxyUrl')} />
                </div>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-[13px] w-32 shrink-0" style={{ color: 'var(--text-soft)' }}>{t('projects.packageCount')}</span>
              <span className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>{projectDetail?.package_count ?? 0}</span>
            </div>
            {Object.keys(ecosystems).length > 0 && (
              <div className="flex items-start gap-3">
                <span className="text-[13px] w-32 shrink-0 pt-1" style={{ color: 'var(--text-soft)' }}>{t('projects.ecosystemBreakdown')}</span>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(ecosystems).map(([eco, count]) => (
                    <div key={eco} className="flex items-center gap-1.5 px-2 py-1 rounded-[4px]" style={{ background: 'var(--bg-soft)' }}>
                      {isAdminEcosystem(eco) && <EcosystemIcon type={eco} size={12} />}
                      <span className="text-[12px]" style={{ color: 'var(--text)' }}>{eco.toUpperCase()}</span>
                      <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{count}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
          ) : <EmptyState icon="folder_managed" title={t('noData')} minHeight={180} />}
        </section>

        {/* SBOM export */}
        <section>
          <SectionHeader title={t('sbom.export')} />
          <div data-project-sbom-controls className="grid min-w-0 gap-3 sm:grid-cols-[minmax(0,12rem)_minmax(0,14rem)_auto] sm:items-end">
            <SelectV2 label={t('sbom.format')} value={sbomFormat} onChange={(event) => setSbomFormat(event.target.value as ProjectSBOMFormat)}>
              <option value="spdx">{t('sbom.spdx')}</option>
              <option value="cyclonedx">{t('sbom.cyclonedx')}</option>
            </SelectV2>
            <SelectV2 label={t('sbom.filterEcosystem')} value={sbomEcosystem} onChange={(e) => setSbomEcosystem(e.target.value)}>
              <option value="">{t('sbom.allEcosystems')}</option>
              {operatorEcosystems.map(ecosystem => (
                <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
              ))}
            </SelectV2>
            <ButtonV2 className="w-full sm:w-auto" onClick={handleSbomDownload} disabled={sbomLoading}>
              <Icon name="download" size="sm" />
              {sbomLoading ? t('sbom.generating') : t('sbom.download')}
            </ButtonV2>
          </div>
        </section>

        {/* Package table */}
        <section>
          <SectionHeader
            title={t('projects.packages')}
            action={
              <SelectV2 aria-label={t('sbom.filterEcosystem')} value={pkgEcosystem} onChange={(e) => { setPkgEcosystem(e.target.value); setPkgPage(1) }}>
                <option value="">{t('all')}</option>
                {operatorEcosystems.map(ecosystem => (
                  <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
                ))}
              </SelectV2>
            }
          />
          {packagesQuery.isPending ? (
            <div aria-busy="true" className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></div>
          ) : packagesQuery.isError && !pkgData ? (
            <QueryErrorState
              message={getApiError(packagesQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(packagesQuery.error).message}
              onRetry={() => { void packagesQuery.refetch() }}
            />
          ) : (
            <div className="space-y-3">
              {pkgData && packagesQuery.isRefetchError && <StaleDataNotice refreshing={packagesQuery.isFetching} onRefresh={() => packagesQuery.refetch()} />}
              {packages.length === 0 ? (
                <EmptyState icon="inventory_2" title={t('projects.noPackages')} minHeight={200} />
              ) : (
                <DataTableV2
                  columns={pkgColumns}
                  data={packages.map((pkg) => ({ ...pkg }))}
                  rowKey={(row) => `${row.ecosystem}:${row.package_name}:${row.version}`}
                  ariaLabel={t('projects.packagesTable')}
                  minWidth={900}
                />
              )}
            </div>
          )}
          <div className="mt-4">
            <AdminPagination page={pkgPage} pageSize={20} total={pkgTotal} onPageChange={setPkgPage} />
          </div>
        </section>
      </div>
      </AdminPage>
    )
  }

  // ── List view ──────────────────────────────────────────────────
  const columns = [
    { key: 'name', label: t('projects.name'), render: (v: unknown) => <span className="font-[500] text-[14px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
    { key: 'package_count', label: t('projects.packageCount'), render: (v: unknown) => <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{(v as number) ?? 0}</span> },
    { key: 'last_activity_at', label: t('projects.lastActivity'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatProjectTime(v as string)}</span> },
    {
      key: 'id',
      label: t('actions'),
      render: (_v: unknown, row: ProjectSummary & Record<string, unknown>) => (
        <div className="flex gap-1">
          <ButtonV2
            variant="ghost"
            size="sm"
            type="button"
            aria-label={t('projects.viewNamed', { name: row.name })}
            onClick={() => { setSelectedProject(row); setPkgPage(1); setPkgEcosystem('') }}
          >
            <Icon name="visibility" size="sm" />
            {t('projects.view')}
          </ButtonV2>
          {canWrite && <IconButton icon="delete" label={t('projects.deleteNamed', { name: row.name })} tone="danger" onClick={(e) => { e.stopPropagation(); openDeleteDialog(row) }} />}
        </div>
      ),
    },
  ]

  return (
    <AdminPage
      description={t('projects.subtitle')}
      actions={canWrite ? (
        <ButtonV2 onClick={() => { createMutation.reset(); setCreateOpen(true) }}>
          <Icon name="add" size="sm" />{t('projects.create')}
        </ButtonV2>
      ) : undefined}
    >
    <div className="space-y-6">
      {data && query.isRefetchError && (
        <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />
      )}

      {projects.length === 0 ? (
        <EmptyState
          icon="folder_managed"
          title={t('projects.noProjects')}
          hint={t('projects.noProjectsHint')}
          minHeight={240}
        />
      ) : (
        <DataTableV2
          columns={columns}
          data={projects.map((project) => ({ ...project }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('projects.table')}
          minWidth={720}
        />
      )}

      {/* Create modal */}
      <ModalV2 open={createOpen} onClose={() => setCreateOpen(false)} title={t('projects.create')} closeDisabled={createMutation.isPending}>
        <form onSubmit={handleCreate} className="space-y-4">
          <InputV2
            label={t('projects.name')}
            value={createForm.name}
            onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })}
            placeholder={t('projects.namePlaceholder')}
            required
          />
          <InputV2
            label={t('projects.description')}
            value={createForm.description}
            onChange={(e) => setCreateForm({ ...createForm, description: e.target.value })}
            placeholder={t('projects.descPlaceholder')}
          />
          {createMutation.isError && <InlineNotice tone="danger">{getApiError(createMutation.error).message}</InlineNotice>}
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" disabled={createMutation.isPending} onClick={() => setCreateOpen(false)}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="submit" aria-busy={createMutation.isPending || undefined} disabled={createMutation.isPending || !canWrite}>
              {createMutation.isPending ? t('saving') : t('save')}
            </ButtonV2>
          </div>
        </form>
      </ModalV2>

      {/* Token reveal modal */}
      <ModalV2 open={tokenData !== null} onClose={() => setTokenData(null)} title={t('projects.token')}>
        {tokenData && (
          <div className="space-y-4">
            <div className="rounded-[6px] p-3" style={{ background: 'var(--warn-fill)', border: '0.5px solid var(--warn-border)' }}>
              <div className="flex items-center gap-2 mb-1">
                <Icon name="warning" size="sm" style={{ color: 'var(--warn-text)' }} />
                <span className="text-[13px] font-[500]" style={{ color: 'var(--warn-text)' }}>{t('projects.tokenWarning')}</span>
              </div>
            </div>
            <div>
              <label className="block text-[13px] font-[500] mb-1" style={{ color: 'var(--text-muted)' }}>{t('projects.token')}</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-[12px] font-mono px-3 py-2 rounded-[4px] break-all" style={{ background: 'var(--bg-soft)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                  {tokenData.token}
                </code>
                <CopyButton text={tokenData.token} label={t('projects.copyToken')} />
              </div>
            </div>
            <div>
              <label className="block text-[13px] font-[500] mb-1" style={{ color: 'var(--text-muted)' }}>{t('projects.proxyUrl')}</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-[12px] font-mono px-3 py-2 rounded-[4px] break-all" style={{ background: 'var(--bg-soft)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                  {tokenData.proxy_url}
                </code>
                <CopyButton text={tokenData.proxy_url} label={t('projects.copyProxyUrl')} />
              </div>
            </div>
            <div className="flex justify-end pt-2">
              <ButtonV2 onClick={() => setTokenData(null)}>{t('confirm')}</ButtonV2>
            </div>
          </div>
        )}
      </ModalV2>

      <ConfirmActionDialog
        open={deleteTarget !== null}
        title={t('projects.confirmDelete')}
        description={t('projects.deleteWarning')}
        details={deleteTarget ? [
          { label: t('projects.name'), value: deleteTarget.name },
          { label: t('projects.slug'), value: deleteTarget.slug, mono: true },
        ] : []}
        cancelLabel={t('cancel')}
        confirmLabel={t('delete')}
        pendingLabel={t('deleting')}
        pending={deleteMutation.isPending}
        errorMessage={deleteTarget && deleteMutation.isError ? getApiError(deleteMutation.error).message : null}
        onClose={closeDeleteDialog}
        onConfirm={() => {
          if (deleteTarget && canWrite) deleteMutation.mutate(deleteTarget.id)
        }}
      />
    </div>
    </AdminPage>
  )
}
