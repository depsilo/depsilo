import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { packagesApi } from '@/lib/api'
import Card from '@/components/Card'
import Badge from '@/components/Badge'
import Input from '@/components/Input'
import Button from '@/components/Button'

interface PackageItem {
  name: string
  adapter_type: string
  version_count: number
  total_size: number
  total_hits: number
  last_accessed: string
}

interface PackagesResponse {
  packages: PackageItem[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
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

function SkeletonCard() {
  return (
    <Card>
      <div className="flex items-center justify-between">
        <div className="space-y-2">
          <div className="h-5 w-40 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-56 bg-surface-container rounded-[0.125rem] animate-pulse" />
        </div>
        <div className="h-4 w-16 bg-surface-container rounded-[0.125rem] animate-pulse" />
      </div>
    </Card>
  )
}

export default function Packages() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [search, setSearch] = useState(searchParams.get('q') || '')
  const [debouncedSearch, setDebouncedSearch] = useState(search)
  const [typeFilter, setTypeFilter] = useState(searchParams.get('type') || '')
  const [sort, setSort] = useState(searchParams.get('sort') || 'hits')
  const [page, setPage] = useState(Number(searchParams.get('page')) || 1)

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search)
      setPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [search])

  // Sync params to URL
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
      <h1 className="text-2xl font-bold text-on-surface">{t('packages.title')}</h1>

      {/* Filters */}
      <div className="flex items-center gap-3">
        <div className="flex-1">
          <Input
            placeholder={t('packages.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <select
          className="bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-sm text-on-surface border-b-2 border-transparent focus:border-primary focus:outline-none transition-colors"
          value={typeFilter}
          onChange={(e) => {
            setTypeFilter(e.target.value)
            setPage(1)
          }}
        >
          <option value="">{t('all')}</option>
          <option value="pypi">PyPI</option>
          <option value="apt">APT</option>
        </select>
        <select
          className="bg-surface-low rounded-[0.125rem] px-3 py-2.5 text-sm text-on-surface border-b-2 border-transparent focus:border-primary focus:outline-none transition-colors"
          value={sort}
          onChange={(e) => {
            setSort(e.target.value)
            setPage(1)
          }}
        >
          <option value="hits">{t('packages.sortHits')}</option>
          <option value="recent">{t('packages.sortRecent')}</option>
          <option value="size">{t('packages.sortSize')}</option>
          <option value="name">{t('packages.sortName')}</option>
        </select>
      </div>

      {/* Package list */}
      {isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : !data || data.packages.length === 0 ? (
        <Card>
          <p className="py-8 text-center text-sm text-on-surface-variant">
            {t('packages.noPackages')}
          </p>
        </Card>
      ) : (
        <div className="space-y-3">
          {data.packages.map((pkg) => (
            <Card
              key={`${pkg.adapter_type}-${pkg.name}`}
              className="cursor-pointer hover:bg-surface-container/50 transition-colors"
            >
              <div
                className="flex items-center justify-between"
                onClick={() => navigate(`/packages/${pkg.adapter_type}/${pkg.name}`)}
              >
                <div className="space-y-1 min-w-0">
                  <div className="flex items-center gap-2.5">
                    <span className="text-base font-semibold text-on-surface truncate">
                      {pkg.name}
                    </span>
                    <Badge variant={pkg.adapter_type === 'pypi' ? 'pypi' : 'apt'}>
                      {pkg.adapter_type}
                    </Badge>
                  </div>
                  <p className="text-sm text-on-surface-variant">
                    {pkg.version_count} {t('packages.versions')} &middot;{' '}
                    {formatBytes(pkg.total_size)} &middot;{' '}
                    {pkg.total_hits.toLocaleString()} {t('packages.hits')}
                  </p>
                </div>
                <span className="text-xs text-on-surface-variant shrink-0 ml-4">
                  {timeAgo(pkg.last_accessed)}
                </span>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Pagination */}
      {data && data.total_pages > 1 && (
        <div className="flex items-center justify-between pt-2">
          <span className="text-xs text-on-surface-variant">
            {t('totalItems', {
              total: data.total,
              page: data.page,
              totalPages: data.total_pages,
            })}
          </span>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              className="text-xs px-3 py-1.5"
              disabled={page <= 1}
              onClick={() => setPage((p) => p - 1)}
            >
              {t('prevPage')}
            </Button>
            <Button
              variant="secondary"
              className="text-xs px-3 py-1.5"
              disabled={page >= data.total_pages}
              onClick={() => setPage((p) => p + 1)}
            >
              {t('nextPage')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
