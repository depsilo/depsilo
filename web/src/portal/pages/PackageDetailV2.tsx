import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useParams, Link } from 'react-router-dom'
import { packagesApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'

interface FileEntry {
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
  versions: FileEntry[]
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
  const isToday = d.getDate() === now.getDate() && d.getMonth() === now.getMonth() && d.getFullYear() === now.getFullYear()
  if (isToday) return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return d.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export default function PackageDetailV2() {
  const { t } = useTranslation()
  const { type, name } = useParams<{ type: string; name: string }>()

  const { data, isLoading, isError } = useQuery<PackageDetailData>({
    queryKey: ['package-detail', type, name],
    queryFn: async () => { const res = await packagesApi.detail(type!, name!); return res.data },
    enabled: !!type && !!name,
  })

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="h-4 w-48 rounded-[4px] animate-pulse" style={{ background: 'var(--surface-container)' }} />
        <div className="h-8 w-64 rounded-[4px] animate-pulse" style={{ background: 'var(--surface-container)' }} />
        <div className="h-64 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="space-y-6">
        <Link to="/packages" className="text-[14px] transition-colors duration-150" style={{ color: 'var(--stripe-purple)' }}>
          ← {t('packages.backToList')}
        </Link>
        <CardV2>
          <p className="py-12 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('packages.notFound')}</p>
        </CardV2>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <nav className="text-[14px]" style={{ color: 'var(--body)' }}>
        <Link to="/packages" className="transition-colors duration-150" style={{ color: 'var(--stripe-purple)' }}>
          {t('packages.backToList')}
        </Link>
        <span className="mx-2">/</span>
        <span style={{ color: 'var(--heading)' }}>{data.name}</span>
      </nav>

      <div className="space-y-2">
        <div className="flex items-center gap-3">
          <EcosystemIcon type={data.adapter_type as any} size={22} />
          <h1 className="text-[26px] font-[300] tracking-[-0.26px]" style={{ color: 'var(--heading)' }}>{data.name}</h1>
          <BadgeV2 variant="ecosystem">{data.adapter_type.toUpperCase()}</BadgeV2>
        </div>
        <div className="flex items-center gap-6 text-[14px]" style={{ color: 'var(--body)' }}>
          <span>{t('packages.totalHits')}: <span className="font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{data.total_hits.toLocaleString()}</span></span>
          <span>{t('packages.totalSize')}: <span className="font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{formatBytes(data.total_size)}</span></span>
        </div>
      </div>

      <CardV2 noPad>
        <div className="overflow-x-auto">
          <table className="w-full text-[14px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                <th className="text-left text-[12px] font-[400] uppercase tracking-wider pb-3 pt-4 px-5" style={{ color: 'var(--body)' }}>{t('packages.fileName')}</th>
                <th className="text-left text-[12px] font-[400] uppercase tracking-wider pb-3 pt-4 px-4" style={{ color: 'var(--body)' }}>{t('packages.size')}</th>
                <th className="text-left text-[12px] font-[400] uppercase tracking-wider pb-3 pt-4 px-4" style={{ color: 'var(--body)' }}>{t('packages.hitCount')}</th>
                <th className="text-left text-[12px] font-[400] uppercase tracking-wider pb-3 pt-4 px-4" style={{ color: 'var(--body)' }}>{t('packages.cachedAt')}</th>
                <th className="text-left text-[12px] font-[400] uppercase tracking-wider pb-3 pt-4 px-4" style={{ color: 'var(--body)' }}>{t('packages.lastAccessed')}</th>
              </tr>
            </thead>
            <tbody>
              {data.versions.length === 0 ? (
                <tr><td colSpan={5} className="py-8 text-center" style={{ color: 'var(--body)' }}>{t('noData')}</td></tr>
              ) : (
                data.versions.map((file) => (
                  <tr key={file.file_name} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td className="py-3 px-5 font-mono text-[13px] truncate max-w-xs" style={{ color: 'var(--heading)' }}>{file.file_name}</td>
                    <td className="py-3 px-4 font-mono text-[13px] whitespace-nowrap" style={{ color: 'var(--body)' }}>{formatBytes(file.size)}</td>
                    <td className="py-3 px-4 font-mono text-[13px] tabular-nums" style={{ color: 'var(--heading)' }}>{file.hit_count.toLocaleString()}</td>
                    <td className="py-3 px-4 text-[13px] whitespace-nowrap" style={{ color: 'var(--body)' }}>{formatDate(file.cached_at)}</td>
                    <td className="py-3 px-4 text-[13px] whitespace-nowrap" style={{ color: 'var(--body)' }}>{formatDate(file.last_accessed)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </CardV2>
    </div>
  )
}
