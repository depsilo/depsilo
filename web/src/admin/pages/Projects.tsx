import { Checkbox } from '@/components/ui/checkbox'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import { useAppToast } from '@/components/Toast'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import Icon from '@/components/Icon'
import { Badge } from '@/components/ui/badge'
import DataTable from '@/components/DataTable'
import { NativeSelect } from '@/components/ui/native-select'
import EcosystemIcon from '@/components/EcosystemIcon'
import SectionHeader from '@/components/SectionHeader'

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
  const { t } = useTranslation()
  const toast = useAppToast()
  async function handleClick() {
    if (await copyText(text)) {
      showCopied()
      toast.show({ tone: 'success', message: t('copied') })
    } else toast.show({ tone: 'danger', message: t('projects.copyFailed') })
  }
  return (
    <Tooltip>
      <TooltipTrigger render={
        <Button
          onClick={handleClick}
          variant="ghost"
          size="icon"
          data-icon-button
          aria-label={label}
          style={{ color: copied ? 'var(--ok-text)' : 'var(--text-soft)' }}
        />
      }>
        <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

export default function Projects() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()

  const [searchParams, setSearchParams] = useSearchParams()
  const projectParam = Number(searchParams.get('project'))
  const projectId = Number.isSafeInteger(projectParam) && projectParam > 0 ? projectParam : null
  const requestedPage = Number(searchParams.get('page'))
  const pkgPage = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1
  const requestedEcosystem = searchParams.get('ecosystem') ?? ''
  const pkgEcosystem = operatorEcosystems.some(item => item.id === requestedEcosystem) ? requestedEcosystem : ''
  function setSelectedProject(project: ProjectSummary | null) {
    setSearchParams(project ? { project: String(project.id) } : {})
  }
  function setPkgPage(page: number) {
    setSearchParams(current => { const next = new URLSearchParams(current); if (page > 1) next.set('page', String(page)); else next.delete('page'); return next })
  }
  function setPkgEcosystem(ecosystem: string) {
    setSearchParams(current => { const next = new URLSearchParams(current); if (ecosystem) next.set('ecosystem', ecosystem); else next.delete('ecosystem'); next.delete('page'); return next })
  }
  const [exportOpen, setExportOpen] = useState(false)
  const [editForm, setEditForm] = useState<{ name: string; description: string } | null>(null)
  const [rotateOpen, setRotateOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState({ name: '', description: '' })
  const [tokenData, setTokenData] = useState<{ token: string; proxy_url: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ProjectSummary | null>(null)
  const [sbomFormat, setSbomFormat] = useState<ProjectSBOMFormat>('spdx')
  const [sbomEcosystem, setSbomEcosystem] = useState('')
  const [sbomTechnicalFile, setSbomTechnicalFile] = useState(false)
  const [sbomSign, setSbomSign] = useState(false)
  const [sbomError, setSbomError] = useState('')
  const [sbomLoading, setSbomLoading] = useState(false)
  const exportController = useRef<AbortController | null>(null)
  const [viewProjectId, setViewProjectId] = useState(projectId)
  if (viewProjectId !== projectId) {
    setViewProjectId(projectId)
    setExportOpen(false)
    setEditForm(null)
    setRotateOpen(false)
    setTokenData(null)
    setSbomError('')
    setSbomLoading(false)
  }
  useEffect(() => () => { exportController.current?.abort() }, [projectId])


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
  const selectedProject = projects.find(project => project.id === projectId) ?? null

  const detailQuery = useQuery({
    queryKey: ['admin', 'projects', projectId],
    queryFn: ({ signal }) => adminApi.getProject(projectId!, { signal }),
    enabled: projectId !== null,
    retry: false,
  })
  const detailData = detailQuery.data
  const projectDetail: ProjectDetail | ProjectSummary | null = detailData?.data ?? selectedProject

  const packagesQuery = useQuery({
    queryKey: ['admin', 'projects', projectId, 'packages', pkgPage, pkgEcosystem],
    queryFn: ({ signal }) => adminApi.listProjectPackages(projectId!, { page: pkgPage, per_page: 20, ecosystem: pkgEcosystem || undefined }, { signal }),
    enabled: projectId !== null,
    retry: false,
  })
  const pkgData = packagesQuery.data
  const complianceQuery = useQuery({
    queryKey: ['admin', 'compliance', 'profile'],
    queryFn: ({ signal }) => adminApi.complianceProfile({ signal }),
    enabled: projectId !== null && exportOpen,
    retry: false,
    staleTime: 5 * 60 * 1000,
  })
  const compliance = complianceQuery.data?.data
  const packages = pkgData?.data.items ?? []
  const pkgTotal = pkgData?.data.total ?? 0

  const createMutation = useMutation({
    mutationFn: async (input: CreateProjectRequest) => {
      const { data: result } = await adminApi.createProject(input)
      if (result.token) setTokenData({ token: result.token, proxy_url: `${window.location.origin}/p/${result.slug}` })
      // One-time credentials remain in component state, never mutation data.
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setCreateOpen(false)
      setCreateForm({ name: '', description: '' })

    },
  })

  const editMutation = useMutation({
    mutationFn: (form: { name: string; description: string }) => adminApi.updateProject(projectId!, form),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] }); setEditForm(null) },
  })
  const rotateMutation = useMutation({
    mutationFn: async () => {
      const { data: result } = await adminApi.regenerateProjectToken(projectId!)
      setTokenData({ token: result.token, proxy_url: `${window.location.origin}/p/${projectDetail?.slug ?? ''}` })
    },
    onSuccess: () => setRotateOpen(false),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteProject(id),
    onSuccess: (_response, deletedProjectId) => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setDeleteTarget(null)
      if (projectId === deletedProjectId) {
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

  function downloadBlob(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  async function sbomFailureMessage(error: unknown) {
    const response = (error as { response?: { data?: unknown } }).response
    const body = response?.data
    if (body instanceof Blob) {
      try {
        const parsed = JSON.parse(await body.text()) as { message?: string }
        if (parsed.message) return parsed.message
      } catch {
        // Not a JSON error body; fall through to the generic message.
      }
    }
    return t('sbom.downloadFailed')
  }

  async function handleSbomDownload() {
    if (!projectId) return
    const controller = new AbortController()
    exportController.current = controller
    setSbomLoading(true)
    setSbomError('')
    try {
      const res = await adminApi.exportSbom(projectId, {
        format: sbomFormat,
        ecosystem: sbomEcosystem || undefined,
        preset: sbomTechnicalFile ? 'technical-file' : undefined,
        sign: sbomSign || undefined,
      }, { signal: controller.signal })
      if (controller.signal.aborted) return
      const base = `${projectDetail?.name || 'project'}-sbom`
      const filename = sbomTechnicalFile
        ? `${projectDetail?.name || 'project'}-cra-technical-file.json`
        : `${base}.${sbomFormat === 'spdx' ? 'spdx.json' : 'cdx.json'}`
      downloadBlob(new Blob([res.data], { type: 'application/json' }), filename)
      const signature = res.headers?.['x-depsilo-sbom-signature']
      if (typeof signature === 'string' && signature) {
        const sidecar = {
          algorithm: res.headers['x-depsilo-sbom-signature-algorithm'],
          payload_sha256: res.headers['x-depsilo-sbom-signature-payloadsha256'],
          public_key: res.headers['x-depsilo-sbom-signature-publickey'],
          signature,
          document: filename,
        }
        downloadBlob(new Blob([JSON.stringify(sidecar, null, 2)], { type: 'application/json' }), `${filename}.sig.json`)
      }
    } catch (error) {
      if (!controller.signal.aborted) setSbomError(await sbomFailureMessage(error))
    } finally {
      if (exportController.current === controller) setSbomLoading(false)
    }
  }

  const tokenDialog = <>
{/* Token reveal modal */}
        <Dialog open={tokenData !== null} onOpenChange={(nextOpen) => { if (!nextOpen) setTokenData(null) }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true}>
            <DialogTitle className="pr-10">{t('projects.token')}</DialogTitle>
            {tokenData && (
              <div className="space-y-4">
                <div className="rounded-md p-3" style={{ background: 'var(--warn-fill)', border: '1px solid var(--warn-border)' }}>
                  <div className="flex items-center gap-2 mb-1">
                    <Icon name="warning" size="sm" style={{ color: 'var(--warn-text)' }} />
                    <span className="text-[13px] font-[500]" style={{ color: 'var(--warn-text)' }}>{t('projects.tokenWarning')}</span>
                  </div>
                </div>
                <div>
                  <label className="block text-[13px] font-[500] mb-1" style={{ color: 'var(--text-muted)' }}>{t('projects.token')}</label>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-[12px] font-mono px-3 py-2 rounded-sm break-all" style={{ background: 'var(--bg-soft)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                      {tokenData.token}
                    </code>
                    <CopyButton text={tokenData.token} label={t('projects.copyToken')} />
                  </div>
                </div>
                <div>
                  <label className="block text-[13px] font-[500] mb-1" style={{ color: 'var(--text-muted)' }}>{t('projects.proxyUrl')}</label>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-[12px] font-mono px-3 py-2 rounded-sm break-all" style={{ background: 'var(--bg-soft)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                      {tokenData.proxy_url}
                    </code>
                    <CopyButton text={tokenData.proxy_url} label={t('projects.copyProxyUrl')} />
                  </div>
                </div>
                <div className="flex justify-end pt-2">
                  <Button onClick={() => setTokenData(null)}>{t('confirm')}</Button>
                </div>
              </div>
            )}
          </DialogContent>
        </Dialog>
  </>

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
  if (projectId) {
    const proxyUrl = `${window.location.origin}/p/${projectDetail?.slug ?? ''}`
    const ecosystems: Record<string, number> = detailData?.data?.ecosystem_breakdown ?? {}

    const pkgColumns = [
      {
        key: 'ecosystem',
        label: t('type'),
        render: (v: unknown) => (
          <div className="flex items-center gap-1.5">
            {typeof v === 'string' && isAdminEcosystem(v) && <EcosystemIcon type={v} size={14} />}
            <Badge variant="default">{(v as string)?.toUpperCase()}</Badge>
          </div>
        ),
      },
      { key: 'package_name', label: t('name'), render: (v: unknown) => <span className="font-mono text-[13px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
      { key: 'version', label: t('projects.version'), render: (v: unknown) => <span className="font-mono text-[13px]" style={{ color: 'var(--text-soft)' }}>{(v as string) || '-'}</span> },
      { key: 'first_seen_at', label: t('projects.firstSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatProjectTime(v as string)}</span> },
      { key: 'last_seen_at', label: t('projects.lastSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatProjectTime(v as string)}</span> },
      { key: 'download_count', label: t('projects.downloads'), render: (v: unknown) => <span className="text-[12px] font-mono" style={{ color: 'var(--text-soft)' }}>{(v as number) ?? 0}</span> },
    ]

    return (
      <AdminPage description={t('projects.subtitle')}>
      <div className="space-y-6">
        {/* Detail header */}
        <div className="flex min-w-0 flex-wrap items-center gap-3">
            <Tooltip>
              <TooltipTrigger render={
                <Button
                  onClick={() => setSelectedProject(null)}
                  variant="ghost"
                  size="icon"
                  data-icon-button
                  aria-label={t('projects.backToList')}
                  style={{ color: 'var(--text-soft)' }}
                />
              }>
                <Icon name={"arrow_back"} size="sm" />
              </TooltipTrigger>
              <TooltipContent>{t('projects.backToList')}</TooltipContent>
            </Tooltip>
          <h2 className="min-w-0 break-words text-[20px] font-[600] [overflow-wrap:anywhere]" style={{ color: 'var(--text)' }}>{projectDetail?.name}</h2>
          <div className="ml-auto flex flex-wrap gap-2">
            <Button variant="outline" disabled={!detailData} onClick={() => { setSbomError(''); setExportOpen(true) }}><Icon name="download" size="sm" />{t('sbom.export')}</Button>
            {canWrite && <>
              <Button variant="outline" disabled={!detailData} onClick={() => { editMutation.reset(); setEditForm({ name: projectDetail?.name ?? '', description: projectDetail?.description ?? '' }) }}>{t('projects.editTitle')}</Button>
              <Button variant="outline" disabled={!detailData} onClick={() => { rotateMutation.reset(); setRotateOpen(true) }}>{t('projects.regenerateToken')}</Button>
            </>}
          </div>
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
            {projectDetail?.description && <p className="text-sm text-muted-foreground">{projectDetail.description}</p>}
            {detailQuery.isRefetchError && <StaleDataNotice refreshing={detailQuery.isFetching} onRefresh={() => detailQuery.refetch()} />}
            <div
              data-project-proxy-row
              className="grid min-w-0 gap-2 sm:grid-cols-[8rem_minmax(0,1fr)] sm:items-center sm:gap-3"
            >
              <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('projects.proxyUrl')}</span>
              <div className="flex min-w-0 items-center gap-2">
                <span
                  data-project-proxy-value
                  className="min-w-0 flex-1 break-all rounded-sm px-2 py-1 font-mono text-[13px] leading-5"
                  style={{ background: 'var(--bg-soft)', color: 'var(--text)' }}
                >
                  {proxyUrl}
                </span>
                <div data-project-proxy-copy className="shrink-0">
                  <CopyButton text={proxyUrl} label={t('projects.copyProxyUrl')} />
                </div>
              </div>
            </div>
            <p className="max-w-[80ch] text-sm text-muted-foreground">{t('projects.connectionHint')}</p>
            <Button variant="link" className="px-0" render={<Link to={getAdminRouteHref('connect')} />} nativeButton={false} role="link">{t('projects.connectionGuide')}<Icon name="arrow_forward" size="sm" /></Button>
            <div className="flex items-center gap-3">
              <span className="text-[13px] w-32 shrink-0" style={{ color: 'var(--text-soft)' }}>{t('projects.packageCount')}</span>
              <span className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--text)' }}>{projectDetail?.package_count ?? 0}</span>
            </div>
            {Object.keys(ecosystems).length > 0 && (
              <div className="flex items-start gap-3">
                <span className="text-[13px] w-32 shrink-0 pt-1" style={{ color: 'var(--text-soft)' }}>{t('projects.ecosystemBreakdown')}</span>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(ecosystems).map(([eco, count]) => (
                    <div key={eco} className="flex items-center gap-1.5 px-2 py-1 rounded-sm" style={{ background: 'var(--bg-soft)' }}>
                      {isAdminEcosystem(eco) && <EcosystemIcon type={eco} size={12} />}
                      <span className="text-[12px]" style={{ color: 'var(--text)' }}>{eco.toUpperCase()}</span>
                      <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{count}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
          ) : <Empty style={{ minHeight: 180 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"folder_managed"} size="sm" /></EmptyMedia><EmptyTitle>{t('noData')}</EmptyTitle></EmptyHeader></Empty>}
        </section>

        <Dialog open={exportOpen} onOpenChange={open => { if (!sbomLoading) setExportOpen(open) }}>
          <DialogContent className="sm:max-w-[640px]" closeDisabled={sbomLoading}>
          <DialogTitle>{t('sbom.export')}</DialogTitle>
          <p className="min-w-0 break-words text-sm text-muted-foreground [overflow-wrap:anywhere]">{projectDetail?.name}</p>
          {complianceQuery.isPending && <p role="status">{t('loading')}</p>}
          {complianceQuery.isError && <QueryErrorState message={t('sbom.profileUnavailable')} onRetry={() => { void complianceQuery.refetch() }} />}
          {complianceQuery.isRefetchError && compliance && <StaleDataNotice onRefresh={() => { void complianceQuery.refetch() }} />}
          <div data-project-sbom-controls className="grid min-w-0 gap-3 sm:grid-cols-2 sm:items-end">
              <Field>
                <FieldLabel>{t('sbom.format')}</FieldLabel>
                <NativeSelect disabled={sbomLoading} value={sbomFormat} onChange={(event) => setSbomFormat(event.target.value as ProjectSBOMFormat)}>
                  <option value="spdx">{t('sbom.spdx')}</option>
                  <option value="cyclonedx">{t('sbom.cyclonedx')}</option>
                </NativeSelect>
              </Field>
              <Field>
                <FieldLabel>{t('sbom.filterEcosystem')}</FieldLabel>
                <NativeSelect disabled={sbomLoading} value={sbomEcosystem} onChange={(e) => setSbomEcosystem(e.target.value)}>
                  <option value="">{t('sbom.allEcosystems')}</option>
                  {operatorEcosystems.map(ecosystem => (
                    <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
                  ))}
                </NativeSelect>
              </Field>
            <Button className="w-full sm:w-auto" onClick={handleSbomDownload} disabled={sbomLoading || (sbomSign && (!complianceQuery.isSuccess || !compliance?.signing_configured))}>
              <Icon name="download" size="sm" />
              {sbomLoading ? t('sbom.generating') : t('sbom.download')}
            </Button>
          </div>
          <div className="mt-3 grid gap-2">
            <label className="flex cursor-pointer items-start gap-2 text-[14px] text-[var(--text-soft)]">
                <Checkbox
                  className="mt-0.5"
                  disabled={sbomLoading} checked={sbomTechnicalFile}
                  onCheckedChange={setSbomTechnicalFile}
                />
              <span>
                {t('sbom.technicalFile')}
                <span className="block text-[13px] text-[var(--text-soft)]">{t('sbom.technicalFileHint')}</span>
              </span>
            </label>
            <label className="flex cursor-pointer items-start gap-2 text-[14px] text-[var(--text-soft)]">
                <Checkbox
                  className="mt-0.5"
                  checked={sbomSign}
                  disabled={sbomLoading || (!sbomSign && (!complianceQuery.isSuccess || !compliance?.signing_configured))}
                  onCheckedChange={setSbomSign}
                />
              <span>
                {t('sbom.sign')}
                <span className="block text-[13px] text-[var(--text-soft)]">
                  {complianceQuery.isSuccess ? (compliance?.signing_configured ? t('sbom.signHint') : t('sbom.signUnavailable')) : t('sbom.profileUnknown')}
                </span>
              </span>
            </label>
            {sbomTechnicalFile && complianceQuery.isSuccess && !compliance?.organization && (
              <p className="text-[13px] text-[var(--text-soft)]">{t('sbom.organizationMissing')}</p>
            )}
            {sbomError && <Alert variant={"destructive"}><AlertDescription>{sbomError}</AlertDescription></Alert>}
          </div>
          </DialogContent>
        </Dialog>

        {/* Package table */}
        <section>
          <SectionHeader
            title={t('projects.packages')}
            action={
              <NativeSelect aria-label={t('sbom.filterEcosystem')} value={pkgEcosystem} onChange={(e) => setPkgEcosystem(e.target.value)}>
                <option value="">{t('all')}</option>
                {operatorEcosystems.map(ecosystem => (
                  <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
                ))}
              </NativeSelect>
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
                <Empty style={{ minHeight: 200 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"inventory_2"} size="sm" /></EmptyMedia><EmptyTitle>{t('projects.noPackages')}</EmptyTitle><EmptyDescription>{t('projects.noPackagesHelp')}</EmptyDescription></EmptyHeader></Empty>
              ) : (
                <DataTable
                  columns={pkgColumns}
                  data={packages.map((pkg) => ({ ...pkg }))}
                  rowKey={(row) => `${row.ecosystem}:${row.package_name}:${row.version}`}
                  ariaLabel={t('projects.packagesTable')}
                  minWidth={760}
                />
              )}
            </div>
          )}
          <div className="mt-4">
            <AdminPagination page={pkgPage} pageSize={20} total={pkgTotal} ready={packagesQuery.isSuccess && !packagesQuery.isFetching} onPageChange={setPkgPage} />
          </div>
        </section>
      </div>
      {tokenDialog}
      <Dialog open={editForm !== null} onOpenChange={open => { if (!open && !editMutation.isPending) setEditForm(null) }}>
        <DialogContent closeDisabled={editMutation.isPending}>
          <DialogTitle>{t('projects.editTitle')}</DialogTitle>
          {editForm && <form className="space-y-4" onSubmit={event => { event.preventDefault(); if (canWrite) editMutation.mutate(editForm) }}>
            <Field><FieldLabel>{t('projects.name')}</FieldLabel><Input required value={editForm.name} onChange={event => setEditForm({ ...editForm, name: event.target.value })} /></Field>
            <Field><FieldLabel>{t('projects.description')}</FieldLabel><Input value={editForm.description} onChange={event => setEditForm({ ...editForm, description: event.target.value })} /></Field>
            <p className="text-sm text-muted-foreground">{t('projects.slugStable')}</p>
            {editMutation.isError && <Alert variant="destructive"><AlertDescription>{getApiError(editMutation.error).message}</AlertDescription></Alert>}
            <div className="flex justify-end gap-2"><Button type="button" variant="outline" disabled={editMutation.isPending} onClick={() => setEditForm(null)}>{t('cancel')}</Button><Button type="submit" disabled={editMutation.isPending}>{editMutation.isPending ? t('saving') : t('save')}</Button></div>
          </form>}
        </DialogContent>
      </Dialog>
      <ConfirmActionDialog open={rotateOpen} title={t('projects.regenerateToken')} description={t('projects.rotateWarning')}
        details={[{label:t('projects.name'),value:projectDetail?.name ?? ''}]} pending={rotateMutation.isPending}
        cancelLabel={t('cancel')} confirmLabel={t('projects.regenerateToken')} pendingLabel={t('saving')}
        errorMessage={rotateMutation.isError ? getApiError(rotateMutation.error).message : null}
        onClose={() => setRotateOpen(false)} onConfirm={() => { if (canWrite) rotateMutation.mutate() }} />
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
          <Button
            variant="ghost"
            size="sm"
            type="button"
            aria-label={t('projects.viewNamed', { name: row.name })}
            onClick={() => setSelectedProject(row)}
          >
            <Icon name="visibility" size="sm" />
            {t('projects.view')}
          </Button>
          {canWrite && <Tooltip>
            <TooltipTrigger render={
              <Button
                onClick={(e) => { e.stopPropagation(); openDeleteDialog(row) }}
                variant="ghost"
                size="icon"
                data-icon-button
                aria-label={t('projects.deleteNamed', { name: row.name })}
                style={{ color: 'var(--danger-text)' }}
              />
            }>
              <Icon name={"delete"} size="sm" />
            </TooltipTrigger>
            <TooltipContent>{t('projects.deleteNamed', { name: row.name })}</TooltipContent>
          </Tooltip>}
        </div>
      ),
    },
  ]

  return (
    <AdminPage
      description={t('projects.subtitle')}
      actions={canWrite ? (
        <Button onClick={() => { createMutation.reset(); setCreateOpen(true) }}>
          <Icon name="add" size="sm" />{t('projects.create')}
        </Button>
      ) : undefined}
    >
    <div className="space-y-6">
      {data && query.isRefetchError && (
        <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />
      )}

      {projects.length === 0 ? (
        <Empty style={{ minHeight: 240 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"folder_managed"} size="sm" /></EmptyMedia><EmptyTitle>{t('projects.noProjects')}</EmptyTitle>{t('projects.noProjectsHint') && <EmptyDescription>{t('projects.noProjectsHint')}</EmptyDescription>}</EmptyHeader></Empty>
      ) : (
        <DataTable
          columns={columns}
          data={projects.map((project) => ({ ...project }))}
          rowKey={(row) => row.id as number}
          ariaLabel={t('projects.table')}
          minWidth={720}
        />
      )}

      {/* Create modal */}
        <Dialog open={createOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !createMutation.isPending) setCreateOpen(false) }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={createMutation.isPending}>
            <DialogTitle className="pr-10">{t('projects.create')}</DialogTitle>
            <form onSubmit={handleCreate} className="space-y-4">
              <Field>
                <FieldLabel>{t('projects.name')}</FieldLabel>
                <Input
                  value={createForm.name}
                  onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })}
                  placeholder={t('projects.namePlaceholder')}
                  required
                />
              </Field>
              <Field>
                <FieldLabel>{t('projects.description')}</FieldLabel>
                <Input
                  value={createForm.description}
                  onChange={(e) => setCreateForm({ ...createForm, description: e.target.value })}
                  placeholder={t('projects.descPlaceholder')}
                />
              </Field>
              {createMutation.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(createMutation.error).message}</AlertDescription></Alert>}
              <div className="flex justify-end gap-3 pt-2">
                <Button type="button" variant="outline" disabled={createMutation.isPending} onClick={() => setCreateOpen(false)}>{t('cancel')}</Button>
                <Button type="submit" aria-busy={createMutation.isPending || undefined} disabled={createMutation.isPending || !canWrite}>
                  {createMutation.isPending ? t('saving') : t('save')}
                </Button>
              </div>
            </form>
          </DialogContent>
        </Dialog>

      {tokenDialog}

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
