import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import MetricCardV2 from '@/components/MetricCardV2'
import BadgeV2 from '@/components/BadgeV2'
import ButtonV2 from '@/components/ButtonV2'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'

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
  today: { hit_count: number; miss_count: number; hit_rate: number }
}

function formatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export default function LiveStreamV2() {
  const { t } = useTranslation()
  const [events, setEvents] = useState<CacheEvent[]>([])
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(false)
  const [extraHits, setExtraHits] = useState(0)
  const [extraMisses, setExtraMisses] = useState(0)
  const [connected, setConnected] = useState(false)

  useEffect(() => { pausedRef.current = paused }, [paused])

  const { data: stats } = useQuery<StatsData>({
    queryKey: ['stats-live'],
    queryFn: async () => { const res = await statsApi.getStats(); return res.data },
  })

  const handleEvent = useCallback((e: MessageEvent) => {
    if (pausedRef.current) return
    try {
      const event: CacheEvent = JSON.parse(e.data)
      if (!event.id) event.id = `${event.timestamp}-${Math.random().toString(36).slice(2, 8)}`
      setEvents((prev) => [event, ...prev].slice(0, 100))
      if (event.hit) setExtraHits((h) => h + 1)
      else setExtraMisses((m) => m + 1)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const es = new EventSource('/api/v1/events/stream')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = handleEvent
    return () => es.close()
  }, [handleEvent])

  const totalHits = (stats?.today.hit_count ?? 0) + extraHits
  const totalMisses = (stats?.today.miss_count ?? 0) + extraMisses
  const totalRequests = totalHits + totalMisses
  const hitRate = totalRequests > 0 ? ((totalHits / totalRequests) * 100).toFixed(1) : '0.0'

  return (
    <div className="space-y-6">
      {/* Header with connection status */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--heading)' }}>
            {t('live.title')}
          </h1>
          <div className="flex items-center gap-2 mt-1">
            <span className="relative flex h-2.5 w-2.5">
              {connected && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--success)' }} />}
              <span className={`relative inline-flex rounded-full h-2.5 w-2.5`} style={{ background: connected ? 'var(--success)' : 'var(--error)' }} />
            </span>
            <span className="text-[13px]" style={{ color: connected ? 'var(--success-text)' : 'var(--error)' }}>
              {connected ? t('live.connected') || 'Connected' : t('live.disconnected') || 'Disconnected'}
            </span>
          </div>
        </div>
        <ButtonV2
          variant={paused ? 'primary' : 'secondary'}
          size="sm"
          onClick={() => setPaused((p) => !p)}
        >
          <Icon name={paused ? 'play_arrow' : 'pause'} size="sm" />
          {paused ? t('live.resume') : t('live.pause')}
        </ButtonV2>
      </div>

      {/* Big hit rate display */}
      <div className="text-center py-4">
        <p className="text-[56px] font-[300] tracking-[-1.4px] tabular-nums" style={{ color: 'var(--heading)' }}>
          {hitRate}%
        </p>
        <p className="text-[14px] font-[300]" style={{ color: 'var(--body)' }}>
          {t('live.hitRate')}
        </p>
      </div>

      {/* Metric cards */}
      <div className="grid grid-cols-3 gap-4">
        <MetricCardV2 label={t('live.todayHits')} value={totalHits.toLocaleString()} />
        <MetricCardV2 label={t('live.todayMisses')} value={totalMisses.toLocaleString()} />
        <MetricCardV2 label={t('live.totalRequests') || 'Total'} value={totalRequests.toLocaleString()} />
      </div>

      {/* Event list */}
      <div className="rounded-[5px] overflow-hidden" style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}>
        {events.length === 0 ? (
          <p className="py-16 text-center text-[14px]" style={{ color: 'var(--body)' }}>
            {t('live.noEvents')}
          </p>
        ) : (
          <div>
            {events.map((event, i) => (
              <div
                key={event.id}
                className="flex items-center gap-4 px-4 py-2.5 animate-fade-in"
                style={{
                  borderBottom: i < events.length - 1 ? '1px solid var(--border)' : 'none',
                  borderLeft: `3px solid ${event.hit ? 'var(--success)' : 'var(--lemon, #9b6829)'}`,
                }}
              >
                <span className="text-[12px] font-mono tabular-nums shrink-0 w-16" style={{ color: 'var(--body)' }}>
                  {formatTime(event.timestamp)}
                </span>
                <EcosystemIcon type={event.adapter_type as any} size={14} />
                <span className="font-mono text-[13px] truncate min-w-0 flex-1" style={{ color: 'var(--heading)' }}>
                  {event.package_name}
                </span>
                <BadgeV2 variant={event.hit ? 'success' : 'warning'} className="shrink-0">
                  {event.hit ? 'HIT' : 'MISS'}
                </BadgeV2>
                {!event.hit && event.upstream && (
                  <span className="text-[12px] shrink-0" style={{ color: 'var(--body)' }}>
                    → {event.upstream}
                  </span>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
