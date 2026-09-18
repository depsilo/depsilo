import { adminApiDefaults, expect, mockAdminApi, test, type JsonValue } from './fixtures/admin-api'
import {
  RESPONSIVE_VIEWPORTS,
  assertAccessibleDocument,
  assertAccessibleRoute,
} from './fixtures/a11y'

/**
 * Step 14's exit criterion: the anonymous surfaces meet the standard the Admin
 * routes already met. Nothing here is Portal-specific — that is the point.
 */

const populatedStats: JsonValue = {
  service: { status: 'healthy' },
  week: { total_requests: 12_345, hit_count: 10_000, hit_rate: 0.81, bytes_saved: 4_194_304 },
  upstreams: [
    {
      id: 101,
      name: 'public mirror',
      adapter: 'npm',
      url: 'https://registry.example',
      healthy: true,
      avg_latency_ms: 42,
      success_rate: 1,
    },
    {
      id: 102,
      name: 'slow mirror',
      adapter: 'pypi',
      url: 'https://pypi.example',
      healthy: false,
      avg_latency_ms: 900,
      success_rate: 0.4,
    },
  ],
}

const latencySeries: JsonValue = {
  series: [
    { upstream_id: 101, points: [{ time: '2026-08-06T00:00:00Z', latency_ms: 40, healthy: true, requests: 3 }] },
    { upstream_id: 102, points: [{ time: '2026-08-06T00:00:00Z', latency_ms: 900, healthy: false, requests: 1 }] },
  ],
}

test.describe('anonymous Portal', () => {
  test.use({ initialToken: null })

  for (const viewport of RESPONSIVE_VIEWPORTS) {
    for (const route of ['/', '/monitor'] as const) {
      test(`${route} ${viewport.width} ${viewport.theme} ${viewport.locale} passes the accessibility contract`, async ({ page }) => {
        await mockAdminApi(page, {
          'GET /api/v1/stats': populatedStats,
          'GET /api/v1/latency-series': latencySeries,
        })
        await assertAccessibleRoute(page, { route, ...viewport })
      })
    }
  }

  test('Quick Start keeps the document inside a 320px viewport', async ({ page }) => {
    await mockAdminApi(page)
    await page.setViewportSize({ width: 320, height: 844 })
    await page.goto('/')
    await expect(page.locator('h1')).toBeVisible()
    await assertAccessibleDocument(page, 320)
  })

  test('Monitor keeps the document inside a 320px viewport with an upstream present', async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/stats': populatedStats,
      'GET /api/v1/latency-series': latencySeries,
    })
    await page.setViewportSize({ width: 320, height: 844 })
    await page.goto('/monitor')
    await expect(page.locator('[data-monitor-upstreams]')).toContainText('public mirror')
    await assertAccessibleDocument(page, 320)
  })
})

test.describe('first-run Setup', () => {
  test.use({ initialToken: null })

  for (const viewport of RESPONSIVE_VIEWPORTS) {
    test(`Setup ${viewport.width} ${viewport.theme} ${viewport.locale} passes the accessibility contract`, async ({ page }) => {
      await mockAdminApi(page, {
        'GET /api/v1/setup/status': { ...(adminApiDefaults['GET /api/v1/setup/status'] as object), needs_setup: true, token_required: true },
      })
      await assertAccessibleRoute(page, { route: '/', ...viewport })
    })
  }

  test('Setup with advanced settings exposed keeps the document inside 390px', async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/setup/status': { ...(adminApiDefaults['GET /api/v1/setup/status'] as object), needs_setup: true, token_required: true },
    })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /Depsilo/ }).first()).toBeVisible()
    await page.locator('summary').filter({ hasText: /高级设置|Advanced/ }).click()
    await assertAccessibleDocument(page, 390)
  })
})
