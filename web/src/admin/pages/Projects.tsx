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
import ProRequiredCallout from '@/admin/components/ProRequiredCallout'

const ECOSYSTEM_OPTIONS = [
  { value: '', label: 'All' },
  { value: 'pypi', label: 'PyPI' },
  { value: 'apt', label: 'APT' },
  { value: 'npm', label: 'npm' },
  { value: 'go', label: 'Go' },
  { value: 'cargo', label: 'Cargo' },
  { value: 'maven', label: 'Maven' },
  { value: 'rubygems', label: 'RubyGems' },
  { value: 'composer', label: 'Composer' },
  { value: 'nuget', label: 'NuGet' },
  { value: 'conda', label: 'Conda' },
  { value: 'cran', label: 'CRAN' },
  { value: 'alpine', label: 'Alpine' },
  { value: 'helm', label: 'Helm' },
]

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t)
  const now = new Date()
  const diff = Math.floor((now.getTime() - d.getTime()) / 86400000)
  if (diff === 0) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  if (diff < 30) return `${diff}d ago`
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  async function handleClick() {
    if (await copyText(text)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <button
      onClick={handleClick}
      className="bg-transparent cursor-pointer p-1 rounded-[4px] transition-colors duration-150"
      style={{ color: copied ? 'var(--ok-text)' : 'var(--text-soft)' }}
      title="Copy"
    >
      <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
    </button>
  )
}

export default function ProjectsV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [selectedProject, setSelectedProject] = useState<any>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState({ name: '', description: '' })
  const [tokenData, setTokenData] = useState<{ token: string; proxy_url: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [pkgPage, setPkgPage] = useState(1)
  const [pkgEcosystem, setPkgEcosystem] = useState('')
  const [sbomFormat, setSbomFormat] = useState('spdx')
  const [sbomEcosystem, setSbomEcosystem] = useState('')
  const [sbomLoading, setSbomLoading] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'projects'],
    queryFn: () => adminApi.listProjects(),
    retry: false,
  })
  const projects: any[] = data?.data?.items || data?.data || []

  const { data: detailData } = useQuery({
    queryKey: ['admin', 'projects', selectedProject?.id],
    queryFn: () => adminApi.getProject(selectedProject.id),
    enabled: !!selectedProject,
  })
  const projectDetail = detailData?.data || selectedProject

  const { data: pkgData, isLoading: pkgLoading } = useQuery({
    queryKey: ['admin', 'projects', selectedProject?.id, 'packages', pkgPage, pkgEcosystem],
    queryFn: () => adminApi.listProjectPackages(selectedProject.id, { page: pkgPage, per_page: 20, ecosystem: pkgEcosystem || undefined }),
    enabled: !!selectedProject,
  })
  const packages: any[] = pkgData?.data?.items || pkgData?.data || []
  const pkgTotal = pkgData?.data?.total || 0
  const pkgTotalPages = Math.max(1, Math.ceil(pkgTotal / 20))

  const createMutation = useMutation({
    mutationFn: (d: any) => adminApi.createProject(d),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setCreateOpen(false)
      setCreateForm({ name: '', description: '' })
      const result = res.data
      if (result?.token) {
        setTokenData({ token: result.token, proxy_url: result.proxy_url || `${window.location.origin}/projects/${result.slug || result.name}` })
      }
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteProject(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'projects'] })
      setDeleteTarget(null)
      if (selectedProject && deleteTarget === selectedProject.id) {
        setSelectedProject(null)
      }
    },
  })

  function handleCreate(e: React.FormEvent) {
    e.preventDefault()
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

  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <ProRequiredCallout
        icon="folder_managed"
        title={t('projects.proRequired')}
        description={t('projects.proDesc')}
        upgradeLabel={t('projects.upgrade')}
      />
    )
  }

  // ── Detail view ────────────────────────────────────────────────
  if (selectedProject) {
    const proxyUrl = projectDetail?.proxy_url || `${window.location.origin}/projects/${projectDetail?.slug || projectDetail?.name}`
    const ecosystems: Record<string, number> = projectDetail?.ecosystem_breakdown || {}

    const pkgColumns = [
      {
        key: 'ecosystem',
        label: t('type'),
        render: (v: unknown) => (
          <div className="flex items-center gap-1.5">
            <EcosystemIcon type={v as any} size={14} />
            <BadgeV2 variant="ecosystem">{(v as string)?.toUpperCase()}</BadgeV2>
          </div>
        ),
      },
      { key: 'name', label: t('name'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
      { key: 'version', label: t('projects.version'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{(v as string) || '-'}</span> },
      { key: 'first_seen', label: t('projects.firstSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
      { key: 'last_seen', label: t('projects.lastSeen'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
      { key: 'downloads', label: t('projects.downloads'), render: (v: unknown) => <span className="text-[12px] font-mono" style={{ color: 'var(--text-soft)' }}>{(v as number) ?? 0}</span> },
    ]

    return (
      <div className="space-y-12">
        {/* Detail header */}
        <div className="flex items-center gap-3">
          <button
            onClick={() => { setSelectedProject(null); setPkgPage(1); setPkgEcosystem('') }}
            className="bg-transparent cursor-pointer p-1.5 rounded-[4px] transition-colors duration-150"
            style={{ color: 'var(--text-soft)' }}
          >
            <Icon name="arrow_back" size="sm" />
          </button>
          <h2 className="text-[20px] font-[600] tracking-[-0.02em]" style={{ color: 'var(--text)' }}>{projectDetail?.name}</h2>
        </div>

        {/* Project info */}
        <section>
          <SectionHeader title={t('projects.overview')} />
          <div className="space-y-3">
            <div className="flex items-center gap-3">
              <span className="text-[13px] w-32 shrink-0" style={{ color: 'var(--text-soft)' }}>{t('projects.proxyUrl')}</span>
              <span className="font-mono text-[12px] px-2 py-1 rounded-[4px]" style={{ background: 'var(--bg-soft)', color: 'var(--text)' }}>{proxyUrl}</span>
              <CopyButton text={proxyUrl} />
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
                      <EcosystemIcon type={eco as any} size={12} />
                      <span className="text-[12px]" style={{ color: 'var(--text)' }}>{eco.toUpperCase()}</span>
                      <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{count}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </section>

        {/* SBOM export */}
        <section>
          <SectionHeader title={t('sbom.export')} />
          <div className="flex items-end gap-3 flex-wrap">
            <SelectV2 label={t('sbom.format')} value={sbomFormat} onChange={(e) => setSbomFormat(e.target.value)}>
              <option value="spdx">{t('sbom.spdx')}</option>
              <option value="cyclonedx">{t('sbom.cyclonedx')}</option>
            </SelectV2>
            <SelectV2 label={t('sbom.filterEcosystem')} value={sbomEcosystem} onChange={(e) => setSbomEcosystem(e.target.value)}>
              <option value="">{t('sbom.allEcosystems')}</option>
              {ECOSYSTEM_OPTIONS.filter(o => o.value !== '').map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </SelectV2>
            <ButtonV2 onClick={handleSbomDownload} disabled={sbomLoading}>
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
              <SelectV2 value={pkgEcosystem} onChange={(e) => { setPkgEcosystem(e.target.value); setPkgPage(1) }}>
                {ECOSYSTEM_OPTIONS.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.value === '' ? t('all') : opt.label}</option>
                ))}
              </SelectV2>
            }
          />
          {pkgLoading ? (
            <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
          ) : packages.length === 0 ? (
            <EmptyState icon="inventory_2" title={t('projects.noPackages')} minHeight={200} />
          ) : (
            <DataTableV2 columns={pkgColumns} data={packages} />
          )}
          {pkgTotalPages > 1 && (
            <div className="flex items-center justify-between text-[13px] mt-4" style={{ color: 'var(--text-soft)' }}>
              <span>{t('totalItems', { total: pkgTotal, page: pkgPage, totalPages: pkgTotalPages })}</span>
              <div className="flex gap-2">
                <ButtonV2 variant="ghost" size="sm" disabled={pkgPage <= 1} onClick={() => setPkgPage(p => p - 1)}>{t('prevPage')}</ButtonV2>
                <ButtonV2 variant="ghost" size="sm" disabled={pkgPage >= pkgTotalPages} onClick={() => setPkgPage(p => p + 1)}>{t('nextPage')}</ButtonV2>
              </div>
            </div>
          )}
        </section>
      </div>
    )
  }

  // ── List view ──────────────────────────────────────────────────
  const columns = [
    { key: 'name', label: t('projects.name'), render: (v: unknown) => <span className="font-[500] text-[14px]" style={{ color: 'var(--text)' }}>{v as string}</span> },
    { key: 'package_count', label: t('projects.packageCount'), render: (v: unknown) => <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{(v as number) ?? 0}</span> },
    { key: 'last_activity', label: t('projects.lastActivity'), render: (v: unknown) => <span className="text-[12px] whitespace-nowrap" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
    {
      key: 'id',
      label: t('actions'),
      render: (_v: unknown, row: any) => (
        <div className="flex gap-1">
          <button onClick={(e) => { e.stopPropagation(); setSelectedProject(row); setPkgPage(1); setPkgEcosystem('') }} className="bg-transparent cursor-pointer p-1.5 rounded-[4px]" style={{ color: 'var(--text-soft)' }}>
            <Icon name="visibility" size="sm" />
          </button>
          <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row.id) }} className="bg-transparent cursor-pointer p-1.5 rounded-[4px]" style={{ color: 'var(--text-soft)' }}>
            <Icon name="delete" size="sm" />
          </button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-end">
        <ButtonV2 onClick={() => setCreateOpen(true)}>
          <Icon name="add" size="sm" />{t('projects.create')}
        </ButtonV2>
      </div>

      {isLoading ? (
        <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
      ) : projects.length === 0 ? (
        <EmptyState
          icon="folder_managed"
          title={t('projects.noProjects')}
          hint={t('projects.noProjectsHint')}
          action={<ButtonV2 onClick={() => setCreateOpen(true)}><Icon name="add" size="sm" />{t('projects.create')}</ButtonV2>}
          minHeight={240}
        />
      ) : (
        <DataTableV2
          columns={columns}
          data={projects}
          onRowClick={(row) => { setSelectedProject(row); setPkgPage(1); setPkgEcosystem('') }}
        />
      )}

      {/* Create modal */}
      <ModalV2 open={createOpen} onClose={() => setCreateOpen(false)} title={t('projects.create')}>
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
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" onClick={() => setCreateOpen(false)}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="submit" disabled={createMutation.isPending}>
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
                <CopyButton text={tokenData.token} />
              </div>
            </div>
            <div>
              <label className="block text-[13px] font-[500] mb-1" style={{ color: 'var(--text-muted)' }}>{t('projects.proxyUrl')}</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-[12px] font-mono px-3 py-2 rounded-[4px] break-all" style={{ background: 'var(--bg-soft)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                  {tokenData.proxy_url}
                </code>
                <CopyButton text={tokenData.proxy_url} />
              </div>
            </div>
            <div className="flex justify-end pt-2">
              <ButtonV2 onClick={() => setTokenData(null)}>{t('confirm')}</ButtonV2>
            </div>
          </div>
        )}
      </ModalV2>

      {/* Delete confirmation */}
      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('projects.confirmDelete')}>
        <p className="text-[14px] mb-2" style={{ color: 'var(--text-soft)' }}>{t('projects.deleteWarning')}</p>
        <div className="flex justify-end gap-3 pt-4">
          <ButtonV2 variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>
    </div>
  )
}
