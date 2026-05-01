import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { packagesApi } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import BadgeV2 from '@/components/Badge'
import InputV2 from '@/components/Input'
import ButtonV2 from '@/components/Button'
import EcosystemIcon from '@/components/EcosystemIcon'
import EmptyStateV2 from '@/components/EmptyState'
import SelectV2 from '@/components/Select'

interface PackageItem {
  package_name: string
  adapter_type: string
  version_count: number
  total_size: number
  total_hits: number
  last_accessed: string
}

interface PackagesResponse {
  items: PackageItem[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

function timeAgo(dateStr: string): string {
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  const diff = now - then
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export default function PackagesV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [search, setSearch] = useState(searchParams.get('q') || '')
  const [debouncedSearch, setDebouncedSearch] = useState(search)
  const [typeFilter, setTypeFilter] = useState(searchParams.get('type') || '')
  const [sort, setSort] = useState(searchParams.get('sort') || 'hits')
  const [page, setPage] = useState(Number(searchParams.get('page')) || 1)

  useEffect(() => {
    const timer = setTimeout(() => { setDebouncedSearch(search); setPage(1) }, 300)
    return () => clearTimeout(timer)
  }, [search])

  useEffect(() => {
    const params: Record<string, string> = {}
    if (debouncedSearch) params.q = debouncedSearch
    if (typeFilter) params.type = typeFilter
    if (sort && sort !== 'hits') params.sort = sort
    if (page > 1) params.page = String(page)
    setSearchParams(params, { replace: true })
  }, [debouncedSearch, typeFilter, sort, page, setSearchParams])

  const { data, isLoading } = useQuery<PackagesResponse>({
    queryKey: ['packages', debouncedSearch, typeFilter, sort, page],
    queryFn: async () => {
      const res = await packagesApi.list({
        q: debouncedSearch || undefined,
        type: typeFilter || undefined,
        sort,
        page,
        per_page: 20,
      })
      return res.data
    },
  })

  return (
    <div className="space-y-6">
      <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--text)' }}>
        {t('packages.title')}
      </h1>

      {/* Filters */}
      <div className="flex items-center gap-3">
        <div className="flex-1">
          <InputV2
            placeholder={t('packages.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <SelectV2 value={typeFilter} onChange={(e) => { setTypeFilter(e.target.value); setPage(1) }}>
          <option value="">{t('all')}</option>
          <option value="pypi">PyPI</option>
          <option value="apt">APT</option>
          <option value="npm">npm</option>
          <option value="go">Go</option>
          <option value="cargo">Cargo</option>
          <option value="maven">Maven</option>
          <option value="rubygems">RubyGems</option>
          <option value="composer">Composer</option>
          <option value="nuget">NuGet</option>
          <option value="conda">Conda</option>
          <option value="cran">CRAN</option>
          <option value="helm">Helm</option>
        </SelectV2>
        <SelectV2 value={sort} onChange={(e) => { setSort(e.target.value); setPage(1) }}>
          <option value="hits">{t('packages.sortHits')}</option>
          <option value="recent">{t('packages.sortRecent')}</option>
          <option value="size">{t('packages.sortSize')}</option>
          <option value="name">{t('packages.sortName')}</option>
        </SelectV2>
      </div>

      {/* Package list */}
      {isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-16 rounded-[5px] animate-pulse" style={{ background: 'var(--bg-soft)' }} />
          ))}
        </div>
      ) : !data || data.items.length === 0 ? (
        <EmptyStateV2 icon="inventory_2" title={t('packages.noPackages')} />
      ) : (
        <div className="space-y-2">
          {data.items.map((pkg) => (
            <div
              key={`${pkg.adapter_type}-${pkg.package_name}`}
              className="rounded-[5px] px-5 py-4 cursor-pointer transition-all duration-150"
              style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}
              onClick={() => navigate(`/packages/${pkg.adapter_type}/${pkg.package_name}`)}
              onMouseEnter={(e) => { e.currentTarget.style.boxShadow = ''; e.currentTarget.style.transform = 'translateY(-1px)' }}
              onMouseLeave={(e) => { e.currentTarget.style.boxShadow = ''; e.currentTarget.style.transform = '' }}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3 min-w-0">
                  <EcosystemIcon type={pkg.adapter_type as any} size={18} />
                  <span className="text-[16px] font-[400] truncate" style={{ color: 'var(--text)' }}>
                    {pkg.package_name}
                  </span>
                  <BadgeV2 variant="ecosystem">{pkg.adapter_type.toUpperCase()}</BadgeV2>
                </div>
                <span className="text-[12px] shrink-0 ml-4" style={{ color: 'var(--text-soft)' }}>
                  {timeAgo(pkg.last_accessed)}
                </span>
              </div>
              <p className="text-[13px] mt-1 ml-[30px]" style={{ color: 'var(--text-soft)' }}>
                {pkg.version_count} {t('packages.versions')} · {formatBytes(pkg.total_size)} · {pkg.total_hits.toLocaleString()} {t('packages.hits')}
              </p>
            </div>
          ))}
        </div>
      )}

      {/* Pagination */}
      {data && data.total_pages > 1 && (
        <div className="flex items-center justify-between pt-2">
          <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
            {t('totalItems', { total: data.total, page: data.page, totalPages: data.total_pages })}
          </span>
          <div className="flex items-center gap-2">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              {t('prevPage')}
            </ButtonV2>
            <ButtonV2 variant="secondary" size="sm" disabled={page >= data.total_pages} onClick={() => setPage((p) => p + 1)}>
              {t('nextPage')}
            </ButtonV2>
          </div>
        </div>
      )}
    </div>
  )
}
