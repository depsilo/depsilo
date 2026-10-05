// Monitor — the portal's health page.
//
// Audience: an End User whose install is slow or failing, coming here
// to answer "is it the proxy or the upstream?". Upstream health panels
// are therefore the page's main content, right below the header.
//
// Value metrics (hit rate / bandwidth saved) are demoted to two compact
// numbers in the header summary row and use a 7-day rolling window from
// /api/v1/stats `week` — a day-scoped rate resets at midnight and
// swings wildly at low sample counts. Request counts live in the admin
// dashboard, not here.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { statsApi } from '@/lib/api'
import ButtonV2 from '@/components/Button'
import EmptyState from '@/components/EmptyState'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import StatusDot from '@/components/StatusDot'
import { UpstreamGroupedPanel, type UpstreamItem } from '@/components/UpstreamCard'
import { LANGUAGES } from '@/lib/ecosystemData'
import { upstreamStatus, type MirrorStatus } from '@/lib/upstreamStatus'

interface LatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
  requests: number
}

interface UpstreamInfo {
  id: number
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  latency_series?: LatencyPoint[]
}

interface StatsData {
  service: { status: string }
  week?: {
    total_requests: number
    hit_count: number
    hit_rate: number
    bytes_saved: number
  }
  upstreams: UpstreamInfo[]
}

function formatBytes(bytes: number): { value: string; unit: string } {
  if (bytes >= 1e12) return { value: (bytes / 1e12).toFixed(1), unit: 'TB' }
  if (bytes >= 1e9)  return { value: (bytes / 1e9).toFixed(0),  unit: 'GB' }
  if (bytes >= 1e6)  return { value: (bytes / 1e6).toFixed(0),  unit: 'MB' }
  if (bytes >= 1e3)  return { value: (bytes / 1e3).toFixed(0),  unit: 'KB' }
  return { value: String(bytes), unit: 'B' }
}

function latencySeriesToBeats(series?: LatencyPoint[]): (number | null)[] {
  if (!series || series.length === 0) return []
  return series.filter(pt => pt.requests > 0).map(pt => {
    if (!pt.healthy) return -1
    return pt.latency_ms
  })
}

function latencySeriesToLabels(series: LatencyPoint[] | undefined, locale: string): string[] {
  return series?.filter(pt => pt.requests > 0).map(pt =>
    new Date(pt.time).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  ) ?? []
}

function toUpstreamItem(upstream: UpstreamInfo, locale: string): UpstreamItem {
  const beats = latencySeriesToBeats(upstream.latency_series)
  return {
    name: upstream.name,
    adapter: upstream.adapter,
    url: upstream.url,
    healthy: upstream.healthy,
    avg_latency_ms: upstream.avg_latency_ms,
    success_rate: upstream.success_rate,
    beats: beats.length > 0 ? beats : undefined,
    beatLabels: latencySeriesToLabels(upstream.latency_series, locale),
  }
}

// adapter → extra searchable aliases (ecosystem id + display name), so
// "python" finds the pypi group and "node" finds npm. Built once from
// the same catalog QuickStart uses.
const ADAPTER_ALIASES: Record<string, string> = {}
for (const lang of LANGUAGES) {
  ADAPTER_ALIASES[lang.iconAdapter] =
    `${ADAPTER_ALIASES[lang.iconAdapter] ?? ''} ${lang.id} ${lang.name}`.toLowerCase()
}

function matchesQuery(u: UpstreamItem, q: string): boolean {
  return (
    u.adapter.toLowerCase().includes(q) ||
    (ADAPTER_ALIASES[u.adapter] ?? '').includes(q) ||
    u.name.toLowerCase().includes(q) ||
    (u.url ?? '').toLowerCase().includes(q)
  )
}

function summarizeQuery(value: string, maxLength = 48): string {
  const characters = Array.from(value)
  if (characters.length <= maxLength) return value
  return `${characters.slice(0, maxLength - 1).join('')}…`
}

// ── SearchPill ────────────────────────────────────────────────────
// Compact header search. "/" focuses it from anywhere on the page,
// Escape clears and blurs. Pure client-side filter — the stats payload
// already carries every upstream.
function SearchPill({
  value,
  onChange,
  placeholder,
  clearLabel,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  clearLabel: string
}) {
  const ref = useRef<HTMLInputElement>(null)
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== '/') return
      const t = e.target as HTMLElement
      if (t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement || t.isContentEditable) return
      e.preventDefault()
      ref.current?.focus()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  return (
    <div
      role="search"
      className="portal-monitor-search"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 7,
        width: 'min(100%, 320px)',
        minHeight: 40,
        padding: '0 0 0 10px',
        background: 'var(--bg-soft)',
        border: `1px solid ${focused ? 'var(--brand-border)' : 'var(--border)'}`,
        borderRadius: 8,
        transition: 'border-color 120ms ease, background 120ms ease',
      }}
    >
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true" style={{ color: 'var(--text-subtle)', flexShrink: 0 }}>
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.3-4.3" />
      </svg>
      <input
        ref={ref}
        type="text"
        value={value}
        onChange={e => onChange(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onKeyDown={e => {
          if (e.key === 'Escape') {
            onChange('')
            e.currentTarget.blur()
          }
        }}
        placeholder={placeholder}
        aria-label={placeholder}
        style={{
          flex: 1,
          minWidth: 0,
          background: 'transparent',
          border: 'none',
          outline: 'none',
          fontSize: 13,
          fontFamily: 'var(--font-mono)',
          color: 'var(--text)',
        }}
      />
      {value ? (
        <button
          type="button"
          onClick={() => {
            onChange('')
            ref.current?.focus()
          }}
          aria-label={clearLabel}
          className="portal-monitor-search-clear stripe-focus-ring"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 40,
            height: 40,
            flexShrink: 0,
            padding: 0,
            background: 'transparent',
            border: 'none',
            borderRadius: 4,
            color: 'var(--text-subtle)',
            cursor: 'pointer',
            transition: 'color 120ms ease',
          }}
          onMouseEnter={e => { e.currentTarget.style.color = 'var(--text)' }}
          onMouseLeave={e => { e.currentTarget.style.color = 'var(--text-subtle)' }}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
            <path d="M1.5 1.5l7 7M8.5 1.5l-7 7" />
          </svg>
        </button>
      ) : (
        <kbd
          aria-hidden
          style={{
            fontSize: 11,
            fontFamily: 'var(--font-mono)',
            color: 'var(--text-subtle)',
            padding: '0 5px',
            lineHeight: '15px',
            border: '1px solid var(--border)',
            borderRadius: 4,
            opacity: focused ? 0 : 1,
            transition: 'opacity 120ms ease',
          }}
        >
          /
        </kbd>
      )}
    </div>
  )
}

export default function MonitorPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language === 'zh' ? 'zh-CN' : 'en-US'
  const [query, setQuery] = useState('')
  const [problemsOnly, setProblemsOnly] = useState(false)
  const statsQuery = useQuery<StatsData>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => {
      const res = await statsApi.getStats({ signal })
      return res.data
    },
    staleTime: 30_000,
    retry: false,
  })

  // Latency series loaded separately (heavy query, ~160KB)
  const latencyQuery = useQuery<Record<string, LatencyPoint[]>>({
    queryKey: ['latency-series'],
    queryFn: async ({ signal }) => {
      const res = await statsApi.getLatencySeries({ signal })
      return res.data
    },
    enabled: statsQuery.isSuccess && (statsQuery.data?.upstreams.length ?? 0) > 0,
    refetchInterval: 60000,
    retry: false,
  })

  // Merge latency_series into upstream objects
  const rawUpstreams = statsQuery.data?.upstreams ?? []
  const upstreams = rawUpstreams.map(u => ({
    ...u,
    latency_series: latencyQuery.data?.[String(u.id)],
  }))
  const week = statsQuery.data?.week

  const healthyCounts = upstreams.reduce(
    (acc, u) => {
      const s = upstreamStatus(u)
      acc[s] = (acc[s] ?? 0) + 1
      return acc
    },
    {} as Record<MirrorStatus, number>
  )
  const upstreamItems = upstreams.map(u => toUpstreamItem(u, locale))

  const problemCount = (healthyCounts.degraded ?? 0) + (healthyCounts.failed ?? 0)
  // The count can drop to zero between polls; when it does the filter is
  // inert rather than leaving the page showing an empty list.
  const filteringProblems = problemsOnly && problemCount > 0

  // Client-side quick filters. Header counts stay GLOBAL — they describe
  // the service, not the current search or filter.
  const q = query.trim().toLowerCase()
  const matchedItems = q ? upstreamItems.filter(u => matchesQuery(u, q)) : upstreamItems
  const visibleItems = filteringProblems
    ? matchedItems.filter(u => upstreamStatus(u) !== 'healthy')
    : matchedItems
  const summarizedQuery = summarizeQuery(query.trim())

  const savedFmt = formatBytes(week?.bytes_saved ?? 0)

  const hasSummary = Boolean(statsQuery.data)
  const summaryRef = useRef<HTMLParagraphElement | null>(null)
  const [summaryStuck, setSummaryStuck] = useState(false)

  useEffect(() => {
    const node = summaryRef.current
    if (!node || typeof IntersectionObserver === 'undefined') return
    // Shrink the observation box by the sticky header, so the pinned copy only
    // appears once the inline counts are actually behind it rather than merely
    // near the top of the viewport.
    const observer = new IntersectionObserver(
      entries => setSummaryStuck(!entries[0].isIntersecting),
      { rootMargin: '-64px 0px 0px 0px' },
    )
    observer.observe(node)
    return () => observer.disconnect()
  }, [hasSummary])

  const summaryCounts = (
    <>
      <span>
        <span className="num">{upstreams.length}</span> {t('monitor.upstreams')}
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
        <StatusDot status="healthy" />
        <span className="num">{healthyCounts.healthy ?? 0}</span> {t('monitor.healthy')}
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
        <StatusDot status="degraded" />
        <span className="num">{healthyCounts.degraded ?? 0}</span> {t('monitor.degraded')}
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
        <StatusDot status="failed" />
        <span className="num">{healthyCounts.failed ?? 0}</span> {t('monitor.failed')}
      </span>
      {/* Value metrics — compact, 7-day rolling window. Hidden on fresh
          installs (no traffic yet) so the row never shows a meaningless 0%. */}
      {week && week.total_requests > 0 && (
        <span className="inline-flex flex-wrap items-center gap-x-3 gap-y-1 border-l border-[var(--border-strong)] pl-3">
          <span>
            {t('monitor.hitRate7d')}{' '}
            <span className="num" style={{ color: 'var(--brand-text)', fontWeight: 600 }}>
              {(week.hit_rate * 100).toFixed(1)}%
            </span>
          </span>
          <span>
            {t('monitor.saved7d')}{' '}
            <span className="num" style={{ color: 'var(--text)', fontWeight: 600 }}>
              {savedFmt.value} {savedFmt.unit}
            </span>
          </span>
        </span>
      )}
    </>
  )

  return (
    // Stagger: header first, the upstream panel ~70ms later.
    <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      {/* The counts are the one thing worth keeping while scrolling; the header
          rail already carries search and the problem filter. Hidden from
          assistive technology because the inline row below is the semantic
          copy and repeating the numbers would read them twice. */}
      {hasSummary && (
        <div
          aria-hidden="true"
          data-monitor-summary-bar
          data-pinned={summaryStuck ? 'true' : undefined}
          style={{
            position: 'fixed',
            top: 'var(--portal-header-height)',
            left: 0,
            right: 0,
            zIndex: 20,
            pointerEvents: 'none',
            opacity: summaryStuck ? 1 : 0,
            transform: summaryStuck ? 'translateY(0)' : 'translateY(-4px)',
            transition: 'opacity 150ms ease, transform 150ms ease',
          }}
        >
          <div
            style={{
              maxWidth: 'var(--portal-width)',
              margin: '0 auto',
              padding: '7px var(--portal-gutter)',
              display: 'flex',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: '6px 12px',
              fontSize: 13,
              lineHeight: 1.3,
              color: 'var(--text-muted)',
              background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
              backdropFilter: 'saturate(180%) blur(8px)',
              WebkitBackdropFilter: 'saturate(180%) blur(8px)',
              borderBottom: '1px solid var(--border)',
            }}
          >
            {summaryCounts}
          </div>
        </div>
      )}

      {/* Page summary */}
      <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div>
          <div
            style={{
              display: 'flex',
              alignItems: 'flex-end',
              justifyContent: 'space-between',
              gap: '12px 16px',
              flexWrap: 'wrap',
            }}
          >
            <h1
              style={{
                margin: 0,
                fontSize: 'clamp(32px, 5vw, 44px)',
                fontWeight: 700,
                // Inter Tight is pre-tightened — mild tracking only,
                // and milder still for CJK.
                letterSpacing: i18n.language === 'zh' ? '-0.02em' : '-0.025em',
                lineHeight: 1.02,
                color: 'var(--text)',
              }}
            >
              {t('monitor.title')}
            </h1>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'flex-end',
                gap: 8,
                flex: '1 1 auto',
                minWidth: 0,
                flexWrap: 'wrap',
              }}
            >
              <SearchPill
                value={query}
                onChange={setQuery}
                placeholder={t('monitor.searchPlaceholder')}
                clearLabel={t('monitor.clearSearch')}
              />
              {/* A wall of upstream cards hides the few that need action, so
                  the page offers the reduction instead of re-sorting a
                  directory operators already learned the order of. */}
              {problemCount > 0 && (
                <button
                  type="button"
                  aria-pressed={problemsOnly}
                  data-active={problemsOnly ? 'true' : undefined}
                  title={t('monitor.onlyProblemsHint')}
                  onClick={() => setProblemsOnly(current => !current)}
                  className="portal-monitor-filter stripe-focus-ring"
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    minHeight: 40,
                    padding: '0 12px',
                    background: problemsOnly ? 'var(--brand-soft)' : 'var(--bg-soft)',
                    border: `1px solid ${problemsOnly ? 'var(--brand-border)' : 'var(--border)'}`,
                    borderRadius: 8,
                    color: problemsOnly ? 'var(--brand-text)' : 'var(--text-muted)',
                    fontSize: 12.5,
                    fontWeight: problemsOnly ? 600 : 500,
                    cursor: 'pointer',
                    transition: 'background 120ms ease, border-color 120ms ease, color 120ms ease',
                  }}
                >
                  <Icon name="tune" size="sm" />
                  {t('monitor.onlyProblems')}
                  <span className="num" aria-hidden="true">{problemCount}</span>
                </button>
              )}
            </div>
          </div>
          {hasSummary && (
            <p
              ref={summaryRef}
              style={{
                margin: '10px 0 0 0',
                display: 'flex',
                alignItems: 'center',
                flexWrap: 'wrap',
                gap: '8px 12px',
                fontSize: 13,
                lineHeight: 1.3,
                color: 'var(--text-muted)',
              }}
            >
              {summaryCounts}
            </p>
          )}
        </div>
      </div>

      <p className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {q
          ? visibleItems.length === 0
            ? t('monitor.noMatch', { q: summarizedQuery })
            : t('monitor.searchResults', { count: visibleItems.length })
          : filteringProblems
            ? t('monitor.onlyProblemsShown', { count: visibleItems.length })
            : ''}
      </p>

      {/* Upstream health — the page's main content */}
      <div
        data-monitor-upstreams
        className="fade-up fade-up-d1"
        aria-busy={statsQuery.isPending || undefined}
        style={{ display: 'flex', flexDirection: 'column', gap: 12 }}
      >
        {statsQuery.isPending ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <span className="sr-only">{t('monitor.loading')}</span>
            <div aria-hidden="true" className="contents">
              {[...Array(4)].map((_, index) => (
                <div
                  key={index}
                  className="h-32 animate-pulse rounded-lg"
                  style={{ background: 'var(--bg-soft)' }}
                />
              ))}
            </div>
          </div>
        ) : statsQuery.isError && !statsQuery.data ? (
          <QueryErrorState
            message={t('monitor.loadError')}
            onRetry={() => { void statsQuery.refetch() }}
          />
        ) : (
          <>
            {statsQuery.isRefetchError && statsQuery.data && (
              <InlineNotice tone="warning">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <span>{t('monitor.refreshUnavailable')}</span>
                  <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void statsQuery.refetch() }}>
                    {t('common.retry')}
                  </ButtonV2>
                </div>
              </InlineNotice>
            )}
            {upstreamItems.length > 0 && latencyQuery.isError && (
              <InlineNotice tone="warning">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <span>{t('monitor.historyUnavailable')}</span>
                  <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void latencyQuery.refetch() }}>
                    {t('common.retry')}
                  </ButtonV2>
                </div>
              </InlineNotice>
            )}
            {upstreamItems.length === 0 ? (
              <EmptyState
                icon="dns"
                title={t('monitor.noUpstreams')}
                hint={t('monitor.noUpstreamsHint')}
                action={(
                  <Link
                    to="/admin/upstreams"
                    className="app-button stripe-focus-ring inline-flex min-h-10 items-center justify-center rounded-sm px-3 text-[13px] font-[500] no-underline"
                    style={{
                      border: '1px solid var(--border-strong)',
                      color: 'var(--text)',
                      background: 'var(--bg-card)',
                    }}
                  >
                    {t('monitor.configureUpstreams')}
                  </Link>
                )}
              />
            ) : q !== '' && visibleItems.length === 0 ? (
              <EmptyState
                icon="search"
                title={t('monitor.noMatch', { q: summarizedQuery })}
                hint={t('monitor.noMatchHint')}
              />
            ) : (
              <UpstreamGroupedPanel upstreams={visibleItems} variant="cards" minColumnWidth={380} />
            )}
          </>
        )}
      </div>
    </div>
  )
}
