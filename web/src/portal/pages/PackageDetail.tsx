import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useParams, Link } from 'react-router-dom'
import { packagesApi } from '@/lib/api'
import Card from '@/components/Card'
import Badge from '@/components/Badge'

interface FileEntry {
  id: number
  file_name: string
  size: number
  hit_count: number
  cached_at: string
  last_accessed: string
}

interface PackageDetailData {
  name: string
  adapter_type: string
  total_hits: number
  total_size: number
  files: FileEntry[]
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr)
  const now = new Date()
  const isToday =
    d.getDate() === now.getDate() &&
    d.getMonth() === now.getMonth() &&
    d.getFullYear() === now.getFullYear()
  if (isToday) {
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  return d.toLocaleDateString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function SkeletonTable() {
  return (
    <div className="space-y-3">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex items-center gap-4">
          <div className="h-4 w-64 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-16 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-12 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-24 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-24 bg-surface-container rounded-[0.125rem] animate-pulse" />
        </div>
      ))}
    </div>
  )
}

export default function PackageDetail() {
  const { t } = useTranslation()
  const { type, name } = useParams<{ type: string; name: string }>()

  const { data, isLoading, isError } = useQuery<PackageDetailData>({
    queryKey: ['package-detail', type, name],
    queryFn: async () => {
      const res = await packagesApi.detail(type!, name!)
      return res.data
    },
    enabled: !!type && !!name,
  })

  if (isLoading) {
    return (
      <div className="space-y-6">
        {/* Breadcrumb skeleton */}
        <div className="h-4 w-48 bg-surface-container rounded-[0.125rem] animate-pulse" />
        {/* Header skeleton */}
        <div className="space-y-2">
          <div className="h-7 w-64 bg-surface-container rounded-[0.125rem] animate-pulse" />
          <div className="h-4 w-40 bg-surface-container rounded-[0.125rem] animate-pulse" />
        </div>
        <Card>
          <SkeletonTable />
        </Card>
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <Link
          to="/packages"
          className="text-sm text-primary-dim hover:text-primary transition-colors"
        >
          &larr; {t('packages.backToList')}
        </Link>
        <Card>
          <p className="py-12 text-center text-sm text-on-surface-variant">
            {t('packages.notFound')}
          </p>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <nav className="text-sm text-on-surface-variant">
        <Link
          to="/packages"
          className="text-primary-dim hover:text-primary transition-colors"
        >
          {t('packages.backToList')}
        </Link>
        <span className="mx-2">/</span>
        <span className="text-on-surface">{data.name}</span>
      </nav>

      {/* Header */}
      <div className="space-y-2">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-bold text-on-surface">{data.name}</h1>
          <Badge variant={data.adapter_type === 'pypi' ? 'pypi' : 'apt'}>
            {data.adapter_type}
          </Badge>
        </div>
        <div className="flex items-center gap-6 text-sm text-on-surface-variant">
          <span>
            {t('packages.totalHits')}:{' '}
            <span className="font-mono text-on-surface">{data.total_hits.toLocaleString()}</span>
          </span>
          <span>
            {t('packages.totalSize')}:{' '}
            <span className="font-mono text-on-surface">{formatBytes(data.total_size)}</span>
          </span>
        </div>
      </div>

      {/* Files table */}
      <Card>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-on-surface-variant">
                <th className="pb-3 pr-4 font-medium">{t('packages.fileName')}</th>
                <th className="pb-3 pr-4 font-medium">{t('packages.size')}</th>
                <th className="pb-3 pr-4 font-medium">{t('packages.hitCount')}</th>
                <th className="pb-3 pr-4 font-medium">{t('packages.cachedAt')}</th>
                <th className="pb-3 font-medium">{t('packages.lastAccessed')}</th>
              </tr>
            </thead>
            <tbody>
              {data.files.length === 0 ? (
                <tr>
                  <td colSpan={5} className="py-8 text-center text-on-surface-variant">
                    {t('noData')}
                  </td>
                </tr>
              ) : (
                data.files.map((file) => (
                  <tr
                    key={file.id}
                    className="border-t border-outline-variant/10"
                  >
                    <td className="py-3 pr-4 font-mono text-on-surface truncate max-w-xs">
                      {file.file_name}
                    </td>
                    <td className="py-3 pr-4 font-mono text-on-surface-variant whitespace-nowrap">
                      {formatBytes(file.size)}
                    </td>
                    <td className="py-3 pr-4 font-mono text-on-surface">
                      {file.hit_count.toLocaleString()}
                    </td>
                    <td className="py-3 pr-4 text-on-surface-variant whitespace-nowrap">
                      {formatDate(file.cached_at)}
                    </td>
                    <td className="py-3 text-on-surface-variant whitespace-nowrap">
                      {formatDate(file.last_accessed)}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  )
}
