import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Card from '@/components/Card'
import Badge from '@/components/Badge'

interface StatsData {
  service: { version: string; uptime_seconds: number; status: string }
  today: {
    total_requests: number
    hit_count: number
    miss_count: number
    hit_rate: number
    bytes_served: number
    bytes_saved: number
  }
  cache: {
    total_files: number
    total_size_bytes: number
    pypi_files: number
    apt_files: number
  }
  upstreams: Array<{
    name: string
    adapter: string
    healthy: boolean
    avg_latency_ms: number
    success_rate: number
  }>
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

function latencyColor(ms: number): string {
  if (ms < 100) return 'text-success'
  if (ms < 500) return 'text-on-surface-variant'
  return 'text-error'
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <p className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-2">
        {label}
      </p>
      <p className="text-2xl font-mono font-bold text-on-surface">{value}</p>
    </Card>
  )
}

function SkeletonCard() {
  return (
    <Card>
      <div className="h-4 w-20 bg-surface-container rounded-[0.125rem] mb-3 animate-pulse" />
      <div className="h-8 w-24 bg-surface-container rounded-[0.125rem] animate-pulse" />
    </Card>
  )
}

export default function ServiceStatus() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery<StatsData>({
    queryKey: ['stats'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  if (isLoading || !data) {
    return (
      <div className="space-y-8">
        <h1 className="text-2xl font-bold text-on-surface">{t('serviceStatus.title')}</h1>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      </div>
    )
  }

  const maxPypi = Math.max(
    ...(data.top_packages.pypi?.map((p) => p.hit_count) ?? [1]),
    1
  )
  const maxApt = Math.max(
    ...(data.top_packages.apt?.map((p) => p.hit_count) ?? [1]),
    1
  )

  return (
    <div className="space-y-8">
      <h1 className="text-2xl font-bold text-on-surface">{t('serviceStatus.title')}</h1>

      {/* Metric cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          label={t('serviceStatus.todayRequests')}
          value={data.today.total_requests.toLocaleString()}
        />
        <MetricCard
          label={t('serviceStatus.cacheHitRate')}
          value={`${(data.today.hit_rate * 100).toFixed(1)}%`}
        />
        <MetricCard
          label={t('serviceStatus.savedTraffic')}
          value={formatBytes(data.today.bytes_saved)}
        />
        <MetricCard
          label={t('serviceStatus.cacheFiles')}
          value={data.cache.total_files.toLocaleString()}
        />
      </div>

      {/* Upstream health */}
      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wider text-on-surface-variant">
          {t('serviceStatus.upstreamStatus')}
        </h2>
        <Card>
          {data.upstreams.length === 0 ? (
            <p className="py-4 text-center text-sm text-on-surface-variant">
              {t('serviceStatus.noUpstreams')}
            </p>
          ) : (
            <div className="space-y-0">
              {data.upstreams.map((u, i) => (
                <div
                  key={u.name}
                  className={`flex items-center gap-4 py-3 ${
                    i > 0 ? 'border-t border-outline-variant/10' : ''
                  }`}
                >
                  {/* Status dot + name + badge */}
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <span
                      className={`w-2 h-2 rounded-full shrink-0 ${
                        u.healthy ? 'bg-success' : 'bg-error'
                      }`}
                    />
                    <span className="text-sm font-medium text-on-surface truncate">
                      {u.name}
                    </span>
                    <Badge variant={u.adapter === 'pypi' ? 'pypi' : 'apt'}>
                      {u.adapter}
                    </Badge>
                  </div>

                  {/* Latency + success rate */}
                  <div className="flex items-center gap-6 shrink-0">
                    <span
                      className={`font-mono text-sm ${latencyColor(u.avg_latency_ms)}`}
                    >
                      {u.avg_latency_ms} ms
                    </span>
                    <span className="text-xs text-on-surface-variant font-mono">
                      {(u.success_rate * 100).toFixed(1)}%
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </section>

      {/* Top packages */}
      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wider text-on-surface-variant">
          {t('serviceStatus.topPackages')}
        </h2>
        <div className="grid gap-4 md:grid-cols-2">
          {/* PyPI */}
          <Card>
            <p className="text-xs uppercase tracking-widest font-medium text-on-surface-variant mb-4">
              PyPI
            </p>
            {!data.top_packages.pypi || data.top_packages.pypi.length === 0 ? (
              <p className="text-sm text-on-surface-variant">{t('noData')}</p>
            ) : (
              <div className="space-y-3">
                {data.top_packages.pypi.slice(0, 10).map((pkg) => (
                  <div key={pkg.name} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-sm text-on-surface truncate">
                        {pkg.name}
                      </span>
                      <span className="text-xs font-mono text-on-surface-variant shrink-0 ml-2">
                        {pkg.hit_count.toLocaleString()}
                      </span>
                    </div>
                    <div className="h-1 rounded-full bg-surface-container overflow-hidden">
                      <div
                        className="h-full rounded-full bg-primary transition-all"
                        style={{
                          width: `${(pkg.hit_count / maxPypi) * 100}%`,
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>

          {/* APT */}
          <Card>
            <p className="text-xs uppercase tracking-widest font-medium text-on-surface-variant mb-4">
              APT
            </p>
            {!data.top_packages.apt || data.top_packages.apt.length === 0 ? (
              <p className="text-sm text-on-surface-variant">{t('noData')}</p>
            ) : (
              <div className="space-y-3">
                {data.top_packages.apt.slice(0, 10).map((pkg) => (
                  <div key={pkg.name} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-sm text-on-surface truncate">
                        {pkg.name}
                      </span>
                      <span className="text-xs font-mono text-on-surface-variant shrink-0 ml-2">
                        {pkg.hit_count.toLocaleString()}
                      </span>
                    </div>
                    <div className="h-1 rounded-full bg-surface-container overflow-hidden">
                      <div
                        className="h-full rounded-full bg-success transition-all"
                        style={{
                          width: `${(pkg.hit_count / maxApt) * 100}%`,
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
      </section>
    </div>
  )
}
