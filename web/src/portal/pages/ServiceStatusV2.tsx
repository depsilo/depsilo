import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import MetricCardV2 from '@/components/MetricCard'
import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'

interface StatsData {
  service: { version: string; uptime_seconds: number; status: string }
  today: {
    total_requests: number; hit_count: number; miss_count: number
    hit_rate: number; bytes_served: number; bytes_saved: number
  }
  cache: { total_files: number; total_size_bytes: number; pypi_files: number; apt_files: number }
  upstreams: Array<{ name: string; adapter: string; healthy: boolean; avg_latency_ms: number; success_rate: number }>
  top_packages: {
    pypi: Array<{ name: string; hit_count: number }>
    apt: Array<{ name: string; hit_count: number }>
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

export default function ServiceStatusV2() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery<StatsData>({
    queryKey: ['stats'],
    queryFn: async () => { const res = await statsApi.getStats(); return res.data },
    refetchInterval: 30000,
  })

  if (isLoading || !data) {
    return (
      <div className="space-y-8">
        <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--heading)' }}>
          {t('serviceStatus.title')}
        </h1>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="h-24 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />
          ))}
        </div>
      </div>
    )
  }

  const maxPypi = Math.max(...(data.top_packages.pypi?.map((p) => p.hit_count) ?? [1]), 1)
  const maxApt = Math.max(...(data.top_packages.apt?.map((p) => p.hit_count) ?? [1]), 1)

  return (
    <div className="space-y-8">
      <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--heading)' }}>
        {t('serviceStatus.title')}
      </h1>

      {/* Metrics */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCardV2 label={t('serviceStatus.todayRequests')} value={data.today.total_requests.toLocaleString()} />
        <MetricCardV2 label={t('serviceStatus.cacheHitRate')} value={`${(data.today.hit_rate * 100).toFixed(1)}%`} />
        <MetricCardV2 label={t('serviceStatus.savedTraffic')} value={formatBytes(data.today.bytes_saved)} />
        <MetricCardV2 label={t('serviceStatus.cacheFiles')} value={data.cache.total_files.toLocaleString()} />
      </div>

      {/* Upstream health */}
      <section className="space-y-3">
        <h2 className="text-[13px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>
          {t('serviceStatus.upstreamStatus')}
        </h2>
        <CardV2>
          {data.upstreams.length === 0 ? (
            <p className="py-4 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('serviceStatus.noUpstreams')}</p>
          ) : (
            <div>
              {data.upstreams.map((u, i) => (
                <div
                  key={u.name}
                  className="flex items-center gap-4 py-3"
                  style={{ borderTop: i > 0 ? '1px solid var(--border)' : 'none' }}
                >
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <span className="relative flex h-2.5 w-2.5 shrink-0">
                      {u.healthy && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--success)' }} />}
                      <span className="relative inline-flex rounded-full h-2.5 w-2.5" style={{ background: u.healthy ? 'var(--success)' : 'var(--error)' }} />
                    </span>
                    <EcosystemIcon type={u.adapter as any} size={16} />
                    <span className="text-[14px] font-[400] truncate" style={{ color: 'var(--heading)' }}>{u.name}</span>
                    <BadgeV2 variant="ecosystem">{u.adapter.toUpperCase()}</BadgeV2>
                  </div>
                  <div className="flex items-center gap-6 shrink-0">
                    <span className="font-mono text-[13px] tabular-nums" style={{ color: u.avg_latency_ms < 100 ? 'var(--success-text)' : u.avg_latency_ms < 500 ? 'var(--body)' : 'var(--error)' }}>
                      {u.avg_latency_ms} ms
                    </span>
                    <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--body)' }}>
                      {(u.success_rate * 100).toFixed(1)}%
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardV2>
      </section>

      {/* Top packages */}
      <section className="space-y-3">
        <h2 className="text-[13px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>
          {t('serviceStatus.topPackages')}
        </h2>
        <div className="grid gap-4 md:grid-cols-2">
          {/* PyPI */}
          <CardV2>
            <div className="flex items-center gap-2 mb-4">
              <EcosystemIcon type="pypi" size={16} />
              <p className="text-[12px] uppercase tracking-widest font-[400]" style={{ color: 'var(--body)' }}>PyPI</p>
            </div>
            {!data.top_packages.pypi || data.top_packages.pypi.length === 0 ? (
              <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
            ) : (
              <div className="space-y-3">
                {data.top_packages.pypi.slice(0, 10).map((pkg) => (
                  <div key={pkg.name} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-[13px] truncate" style={{ color: 'var(--heading)' }}>{pkg.name}</span>
                      <span className="text-[12px] font-mono tabular-nums shrink-0 ml-2" style={{ color: 'var(--body)' }}>{pkg.hit_count.toLocaleString()}</span>
                    </div>
                    <div className="h-1 rounded-full overflow-hidden" style={{ background: 'var(--surface-container)' }}>
                      <div className="h-full rounded-full transition-all" style={{ width: `${(pkg.hit_count / maxPypi) * 100}%`, background: 'var(--stripe-purple)' }} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardV2>

          {/* APT */}
          <CardV2>
            <div className="flex items-center gap-2 mb-4">
              <EcosystemIcon type="apt" size={16} />
              <p className="text-[12px] uppercase tracking-widest font-[400]" style={{ color: 'var(--body)' }}>APT</p>
            </div>
            {!data.top_packages.apt || data.top_packages.apt.length === 0 ? (
              <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
            ) : (
              <div className="space-y-3">
                {data.top_packages.apt.slice(0, 10).map((pkg) => (
                  <div key={pkg.name} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-[13px] truncate" style={{ color: 'var(--heading)' }}>{pkg.name}</span>
                      <span className="text-[12px] font-mono tabular-nums shrink-0 ml-2" style={{ color: 'var(--body)' }}>{pkg.hit_count.toLocaleString()}</span>
                    </div>
                    <div className="h-1 rounded-full overflow-hidden" style={{ background: 'var(--surface-container)' }}>
                      <div className="h-full rounded-full transition-all" style={{ width: `${(pkg.hit_count / maxApt) * 100}%`, background: 'var(--success)' }} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardV2>
        </div>
      </section>
    </div>
  )
}
