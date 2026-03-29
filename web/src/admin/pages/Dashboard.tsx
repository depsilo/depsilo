import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Icon from '@/components/Icon'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

export default function Dashboard() {
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'dashboard'],
    queryFn: () => adminApi.getDashboard(),
    refetchInterval: 30000,
  })

  const dashboard = data?.data

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid gap-4 grid-cols-4">
          {[...Array(4)].map((_, i) => (
            <Card key={i} className="h-24 animate-pulse" />
          ))}
        </div>
        <Card className="h-80 animate-pulse" />
      </div>
    )
  }

  const today = dashboard?.today || {}
  const dailyStats = dashboard?.daily_stats || []
  const upstreams = dashboard?.upstreams || []
  const topPackages = dashboard?.top_packages || { pypi: [], apt: [] }

  // Pivot daily_stats [{date, adapter_type, count}] into [{date, pypi, apt}]
  const chartDataMap: Record<string, { date: string; pypi: number; apt: number }> = {}
  for (const entry of dailyStats) {
    if (!chartDataMap[entry.date]) {
      chartDataMap[entry.date] = { date: entry.date, pypi: 0, apt: 0 }
    }
    if (entry.adapter_type === 'pypi') {
      chartDataMap[entry.date].pypi = entry.count
    } else if (entry.adapter_type === 'apt') {
      chartDataMap[entry.date].apt = entry.count
    }
  }
  const chartData = Object.values(chartDataMap).sort((a, b) => a.date.localeCompare(b.date))

  const metrics = [
    { label: '今日请求', value: today.total_requests?.toLocaleString() || '0', icon: 'monitoring' },
    { label: '命中率', value: today.hit_rate != null ? `${(today.hit_rate * 100).toFixed(1)}%` : '0%', icon: 'target' },
    { label: '已服务流量', value: formatBytes(today.bytes_served || 0), icon: 'hard_drive' },
    { label: '平均延迟', value: `${Math.round(today.avg_latency_ms || 0)} ms`, icon: 'timer' },
  ]

  return (
    <div className="space-y-6">
      {/* Metric Cards */}
      <div className="grid gap-4 grid-cols-4">
        {metrics.map((m) => (
          <Card key={m.label}>
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs uppercase tracking-wider text-on-surface-variant font-medium">
                {m.label}
              </span>
              <Icon name={m.icon} size="sm" className="text-on-surface-variant" />
            </div>
            <p className="text-2xl font-mono font-bold text-on-surface">{m.value}</p>
          </Card>
        ))}
      </div>

      {/* Line Chart */}
      <Card>
        <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
          近 7 日请求趋势
        </h3>
        <ResponsiveContainer width="100%" height={300}>
          <LineChart data={chartData}>
            <CartesianGrid stroke="var(--outline-variant)" strokeOpacity={0.15} strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--on-surface-variant)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: 'var(--on-surface-variant)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <Tooltip
              contentStyle={{
                background: 'var(--surface-container)',
                border: '1px solid var(--outline-variant)',
                borderRadius: '0.25rem',
                fontSize: 12,
              }}
            />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Line type="monotone" dataKey="pypi" stroke="var(--primary)" name="PyPI" strokeWidth={2} dot={false} />
            <Line type="monotone" dataKey="apt" stroke="var(--success)" name="APT" strokeWidth={2} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      </Card>

      <div className="grid gap-6 grid-cols-2">
        {/* Upstream Status */}
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            上游源状态
          </h3>
          <div className="space-y-3">
            {upstreams.length === 0 && (
              <p className="text-sm text-on-surface-variant">暂无上游源数据</p>
            )}
            {upstreams.map((u: any) => (
              <div key={u.name} className="flex items-center justify-between bg-surface-container rounded-[0.25rem] px-4 py-3">
                <div className="flex items-center gap-3">
                  <span className={`h-2 w-2 rounded-full ${u.healthy ? 'bg-success' : 'bg-error'}`} />
                  <div>
                    <p className="text-sm font-medium text-on-surface">{u.name}</p>
                    <p className="text-[10px] text-on-surface-variant uppercase tracking-wider">{u.adapter}</p>
                  </div>
                </div>
                <div className="text-right">
                  <p className="text-sm font-mono text-on-surface">{u.avg_latency_ms} ms</p>
                  <p className="text-[10px] text-on-surface-variant">
                    可用率 {((u.success_rate || 0) * 100).toFixed(1)}%
                  </p>
                </div>
              </div>
            ))}
          </div>
        </Card>

        {/* Top Packages */}
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            热门包 TOP 10
          </h3>
          <div className="grid gap-6 grid-cols-2">
            {/* PyPI */}
            <div>
              <p className="text-xs font-medium text-on-surface-variant uppercase tracking-wider mb-3">PyPI</p>
              <div className="space-y-2">
                {(topPackages.pypi || []).slice(0, 10).map((p: any, i: number) => {
                  const max = topPackages.pypi?.[0]?.hit_count || 1
                  return (
                    <div key={p.name} className="space-y-1">
                      <div className="flex items-center justify-between text-xs">
                        <span className="truncate font-mono text-on-surface">
                          {i + 1}. {p.name}
                        </span>
                        <span className="text-on-surface-variant font-mono ml-2">{p.hit_count}</span>
                      </div>
                      <div className="h-1 w-full rounded-full bg-surface-container">
                        <div
                          className="h-1 rounded-full bg-primary transition-all"
                          style={{ width: `${(p.hit_count / max) * 100}%` }}
                        />
                      </div>
                    </div>
                  )
                })}
                {(!topPackages.pypi || topPackages.pypi.length === 0) && (
                  <p className="text-xs text-on-surface-variant">暂无数据</p>
                )}
              </div>
            </div>
            {/* APT */}
            <div>
              <p className="text-xs font-medium text-on-surface-variant uppercase tracking-wider mb-3">APT</p>
              <div className="space-y-2">
                {(topPackages.apt || []).slice(0, 10).map((p: any, i: number) => {
                  const max = topPackages.apt?.[0]?.hit_count || 1
                  return (
                    <div key={p.name} className="space-y-1">
                      <div className="flex items-center justify-between text-xs">
                        <span className="truncate font-mono text-on-surface">
                          {i + 1}. {p.name}
                        </span>
                        <span className="text-on-surface-variant font-mono ml-2">{p.hit_count}</span>
                      </div>
                      <div className="h-1 w-full rounded-full bg-surface-container">
                        <div
                          className="h-1 rounded-full bg-success transition-all"
                          style={{ width: `${(p.hit_count / max) * 100}%` }}
                        />
                      </div>
                    </div>
                  )
                })}
                {(!topPackages.apt || topPackages.apt.length === 0) && (
                  <p className="text-xs text-on-surface-variant">暂无数据</p>
                )}
              </div>
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}
