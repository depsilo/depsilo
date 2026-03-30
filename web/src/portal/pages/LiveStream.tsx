import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Card from '@/components/Card'
import Badge from '@/components/Badge'
import Button from '@/components/Button'

interface CacheEvent {
  id: string
  timestamp: string
  hit: boolean
  package_name: string
  file_name: string
  upstream?: string
  adapter_type: string
}

interface StatsData {
  today: {
    hit_count: number
    miss_count: number
    hit_rate: number
  }
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

function formatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export default function LiveStream() {
  const { t } = useTranslation()
  const [events, setEvents] = useState<CacheEvent[]>([])
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(false)
  const [extraHits, setExtraHits] = useState(0)
  const [extraMisses, setExtraMisses] = useState(0)

  // Keep ref in sync with state
  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  // Initial stats from API
  const { data: stats } = useQuery<StatsData>({
    queryKey: ['stats-live'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
  })

  const handleEvent = useCallback((e: MessageEvent) => {
    if (pausedRef.current) return
    try {
      const event: CacheEvent = JSON.parse(e.data)
      // Assign a unique id if not present
      if (!event.id) {
        event.id = `${event.timestamp}-${Math.random().toString(36).slice(2, 8)}`
      }
      setEvents((prev) => [event, ...prev].slice(0, 100))
      if (event.hit) {
        setExtraHits((h) => h + 1)
      } else {
        setExtraMisses((m) => m + 1)
      }
    } catch {
      // ignore malformed events
    }
  }, [])

  // SSE connection
  useEffect(() => {
    const es = new EventSource('/api/v1/events/stream')
    es.onmessage = handleEvent
    return () => es.close()
  }, [handleEvent])

  const totalHits = (stats?.today.hit_count ?? 0) + extraHits
  const totalMisses = (stats?.today.miss_count ?? 0) + extraMisses
  const totalRequests = totalHits + totalMisses
  const hitRate = totalRequests > 0 ? ((totalHits / totalRequests) * 100).toFixed(1) : '0.0'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-on-surface">{t('live.title')}</h1>
        <Button
          variant={paused ? 'primary' : 'secondary'}
          className="text-xs"
          onClick={() => setPaused((p) => !p)}
        >
          {paused ? t('live.resume') : t('live.pause')}
        </Button>
      </div>

      {/* Metric cards */}
      <div className="grid grid-cols-3 gap-4">
        <MetricCard label={t('live.todayHits')} value={totalHits.toLocaleString()} />
        <MetricCard label={t('live.todayMisses')} value={totalMisses.toLocaleString()} />
        <MetricCard label={t('live.hitRate')} value={`${hitRate}%`} />
      </div>

      {/* Event list */}
      <Card>
        {events.length === 0 ? (
          <p className="py-12 text-center text-sm text-on-surface-variant">
            {t('live.noEvents')}
          </p>
        ) : (
          <div className="space-y-0">
            {events.map((event, i) => (
              <div
                key={event.id}
                className={`flex items-center gap-4 py-2.5 ${
                  i > 0 ? 'border-t border-outline-variant/10' : ''
                }`}
              >
                {/* Timestamp */}
                <span className="text-xs font-mono text-on-surface-variant shrink-0 w-16">
                  {formatTime(event.timestamp)}
                </span>

                {/* Hit/Miss badge */}
                <Badge variant={event.hit ? 'success' : 'error'} className="shrink-0 w-14 justify-center">
                  {event.hit ? t('live.hit') : t('live.miss')}
                </Badge>

                {/* Package name */}
                <span className="font-mono text-sm text-on-surface truncate min-w-0 flex-1">
                  {event.package_name}
                </span>

                {/* File name */}
                <span className="text-xs text-on-surface-variant truncate max-w-[200px] hidden sm:block">
                  {event.file_name}
                </span>

                {/* Upstream (if miss) */}
                {!event.hit && event.upstream && (
                  <span className="text-xs text-on-surface-variant shrink-0">
                    &rarr; {event.upstream}
                  </span>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
