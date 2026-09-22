import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

// Isolated preview data. The fixed clock makes before/after screenshots comparable.
const at = '2026-09-22T06:37:00Z'
const points = Array.from({ length: 24 }, (_, i) => ({
  bucket: Date.parse('2026-09-21T06:00:00Z') / 1000 + i * 3600,
  date: '2026-09-21', requests: i === 18 ? 26 : 0, hits: i === 18 ? 4 : 0,
  misses: i === 18 ? 22 : 0, hit_rate: i === 18 ? 4 / 26 : 0,
  bytes_served: i === 18 ? 9542042 : 0, bytes_hit: i === 18 ? 12902 : 0,
  bytes_miss: i === 18 ? 9529140 : 0, sum_latency_ms: i === 18 ? 4202 : 0,
  hit_latency_ms: 0, miss_latency_ms: i === 18 ? 4202 : 0,
  avg_latency_ms: i === 18 ? 4202 / 26 : 0, errors: 0,
}))
const request = { id: 8, request_id: 'req-preview-8', adapter_type: 'pypi', method: 'GET', cache_key: 'pypi/idna', package_name: 'idna', hit: true, cache_result: 'hit', upstream: 'pypi.org', latency_ms: 0, status_code: 200, client_ip: '127.0.0.1', bytes_sent: 12902, created_at: '2026-09-22T03:37:00Z', policy_decision: 'allow', delivery_result: 'completed', audit_events: [] }
export const lowTraffic = {
  'GET /api/v1/admin/dashboard': {
    last_24h: { total_requests: 26, hit_count: 4, hit_rate: 4 / 26, bytes_served: 9542042, avg_latency_ms: 161.6 },
    prev_24h: { total_requests: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 },
    upstreams: Array.from({ length: 25 }, (_, i) => ({ id: i + 1, name: i % 2 ? 'official' : 'tuna', adapter: 'pypi', healthy: true, avg_latency_ms: i < 23 ? 191 : 42, success_rate: 1 })),
    daily_stats: [], top_packages: {}, cache_usage_percent: 0.01,
    runtime: { heap_alloc_bytes: 11219763, heap_sys_bytes: 16777216, goroutines: 42, sampled_at: at },
  },
  'GET /api/v1/admin/policy/status': { status: 'unknown', using_stale_snapshot: false, refresh_failures: 0 },
  'GET /api/v1/now': { status: 'healthy', uptime_seconds: 35, now_unix: Date.parse(at) / 1000, rate: { requests_per_min: 0, upstream_requests_per_min: 0, egress_bps: 0, ingress_bps: 0, has_data: false }, upstreams: { healthy: 25, total: 25 }, sparkline: [], last_activity: { seconds_ago: 10800, adapter_type: 'pypi', package_name: 'pip', hit: false } },
  'GET /api/v1/admin/dashboard/trends': { points },
  'GET /api/v1/admin/bandwidth': { range: { start: '2026-08-23', end: '2026-09-22' }, summary: { total_bytes: 9542042, hit_bytes: 12902, miss_bytes: 9529140, savings_rate: .00135, total_requests: 26, hit_requests: 4, miss_requests: 22, time_saved_ms: 764, avg_hit_latency: 0, avg_miss_latency: 191 }, daily: [], by_ecosystem: [], top_packages: [], by_upstream: [] },
  'GET /api/v1/admin/logs': { items: [request], total: 1, page: 1, page_size: 5 },
  'GET /api/v1/admin/logs/8': request,
}

for (const width of [1440, 1920]) {
  test(`comparison screenshot ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1440 ? 900 : 1080 })
    await page.clock.setFixedTime(new Date(at))
    await setUiPreferences(page, 'light', 'zh')
    await mockAdminApi(page, lowTraffic)
    await page.goto('/admin')
    await expect(page.getByText('9.1')).toBeVisible()
    await expect(page.getByText('~100%')).toHaveCount(0)
    await expect(page.getByText('缓存响应：—')).toBeVisible()
    await expect(page.getByRole('row').nth(1)).toBeAttached()
    const phase = process.env.DASHBOARD_CAPTURE_PHASE ?? 'after'
    await page.screenshot({ path: `/tmp/depsilo-layout-refinement/${phase}-${width}.png` })
    const metrics = await page.evaluate(() => {
      const bounds = (query: string) => { const el = document.querySelector(query); if (!el) return null; const r = el.getBoundingClientRect(); const s = getComputedStyle(el); return { top: r.top, height: r.height, fontSize: s.fontSize, lineHeight: s.lineHeight, fontWeight: s.fontWeight } }
      const metricPanels = document.querySelector('[data-dashboard-metric-panels]')
      const metricSections = metricPanels ? Array.from(metricPanels.children) : []
      const firstTop = metricSections[0]?.getBoundingClientRect().top
      const secondTop = metricSections[1]?.getBoundingClientRect().top
      const cards = Array.from(document.querySelectorAll('.dashboard-metric-card'))
      const cardGeometry = cards.map((card) => {
        const label = card.querySelector('[data-dashboard-label]')?.getBoundingClientRect()
        const content = card.querySelector('.dashboard-metric-content')?.getBoundingClientRect()
        return { label: label && {left: label.left, right: label.right, top: label.top, bottom: label.bottom}, content: content && {left: content.left, right: content.right, top: content.top, bottom: content.bottom} }
      })
      const horizontalCards = cardGeometry.every(({label, content}) => Boolean(label && content && label.right <= content.left + 1 && Math.abs(label.top - content.top) < 40))
      const benefitHorizontal = Array.from(document.querySelectorAll('.dashboard-benefit-card')).every((card) => {
        const heading = card.querySelector('.dashboard-benefit-heading')?.getBoundingClientRect()
        const content = card.querySelector('.dashboard-benefit-content')?.getBoundingClientRect()
        return Boolean(heading && content && heading.right <= content.left + 1 && Math.abs(heading.top - content.top) < 48)
      })
      return { title: bounds('h1'), label: bounds('[data-dashboard-label]'), value: bounds('[data-dashboard-value]'), benefits: bounds('.dashboard-benefits-panel'), attention: bounds('[aria-labelledby="dashboard-attention-title"]'), trend: bounds('[data-query-key="dashboard-trends"]'), metricPanelsSideBySide: firstTop === secondTop, horizontalCards, benefitHorizontal, cardGeometry, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    console.log(`${phase}-${width}`, JSON.stringify(metrics))
    expect(metrics.metricPanelsSideBySide).toBe(true)
    expect(metrics.horizontalCards).toBe(true)
    expect(metrics.benefitHorizontal).toBe(true)
    await page.getByRole('row').nth(1).click()
    await expect(page.getByRole('dialog')).toContainText('idna')
    await page.screenshot({ path: `/tmp/depsilo-layout-refinement/${phase}-dialog-${width}.png` })
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
}
