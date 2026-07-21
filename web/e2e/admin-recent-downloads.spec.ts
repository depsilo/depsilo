import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'
import type { RecentDownload } from '../src/lib/adminApi.types'

function download(overrides: Partial<RecentDownload> & Pick<RecentDownload, 'id' | 'package_name'>): RecentDownload {
  return {
    ecosystem: 'pypi',
    version: '1.0.0',
    cache_result: 'hit',
    latency_ms: 12,
    bytes_sent: 2048,
    status_code: 200,
    created_at: new Date(Date.now() - 2_000).toISOString(),
    ...overrides,
    id: overrides.id,
    package_name: overrides.package_name,
  }
}

test('dashboard shows a compact live download snapshot and polls for new items', async ({ page }) => {
  let calls = 0
  const first = [
    download({ id: 2, package_name: 'requests', version: '2.32.4' }),
    download({ id: 1, package_name: 'numpy', cache_result: 'miss', bytes_sent: 4_194_304 }),
  ]
  const next = [
    download({ id: 3, ecosystem: 'npm', package_name: '@depsilo/live-package', version: '3.1.0', status_code: 502, cache_result: 'error' }),
    ...first,
  ]
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/recent-downloads': () => {
      calls += 1
      if (calls === 1) return { items: first }
      if (calls === 2) return { status: 500, body: { code: 'FAILED', message: 'live refresh failed' } }
      return { items: next }
    },
  })
  await page.goto('/admin')

  const feed = page.locator('[data-recent-downloads]')
  await expect(feed.getByRole('heading', { name: '实时下载' })).toBeVisible()
  await expect(feed).toContainText('实时 · 每 5 秒更新')
  await expect(feed.locator('[data-download-id]')).toHaveCount(2)
  await expect(feed.locator('[data-download-id="2"]')).toContainText('requests@2.32.4')
  await expect(feed.locator('[data-download-id="1"]')).toContainText('上游拉取')
  await expect(feed.getByRole('link', { name: /查看审计日志/ })).toHaveAttribute('href', '/admin/audit')

  await expect(feed).toContainText('自动重试', { timeout: 7_000 })
  await expect(feed.locator('[data-download-id]')).toHaveCount(2)
  expect(await feed.locator('.live-download-flow').evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('none')
  await expect(feed.locator('[data-download-id="3"]')).toBeVisible({ timeout: 7_000 })
  await expect(feed.locator('[data-download-id]')).toHaveCount(3)
  await expect(feed.locator('[data-download-id="3"]')).toContainText('HTTP 502')
  expect(await feed.locator('[data-download-id="3"]').evaluate(element => getComputedStyle(element).animationName)).toBe('liveDownloadEnter')
  expect(await feed.locator('.live-download-flow').evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('liveDownloadSweep')
})

for (const status of [403, 500] as const) {
  test(`live downloads recover from an initial ${status} and render the empty state`, async ({ page }) => {
    let calls = 0
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard/recent-downloads': () => {
        calls += 1
        return calls === 1
          ? { status, body: { code: status === 403 ? 'FORBIDDEN' : 'FAILED', message: 'initial download feed failure' } }
          : { items: [] }
      },
    })
    await page.goto('/admin')

    const feed = page.locator('[data-recent-downloads]')
    const alert = feed.getByRole('alert')
    await expect(alert).toContainText(status === 403 ? '权限不足' : '暂时无法读取下载状态')
    await expect(feed).toContainText('自动重试中')
    expect(await feed.locator('.live-download-flow').evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('none')
    await alert.getByRole('button', { name: '重试' }).click()
    await expect(feed).toContainText('等待首次下载')
    await expect(alert).toHaveCount(0)
  })
}

test('live download motion is explicit and respects reduced-motion on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/recent-downloads': {
      items: [
        download({ id: 3, ecosystem: 'cargo', package_name: 'a-very-long-package-name-that-must-not-overflow' }),
        download({ id: 2, ecosystem: 'npm', package_name: '@scope/package' }),
        download({ id: 1, package_name: 'requests' }),
      ],
    },
  })
  await page.goto('/admin')

  const feed = page.locator('[data-recent-downloads]')
  await expect(feed.getByRole('heading', { name: 'Live downloads' })).toBeVisible()
  await expect(feed.locator('[data-download-id]')).toHaveCount(3)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  expect(await feed.locator('.live-download-row').first().evaluate(element => getComputedStyle(element).animationName)).toBe('none')
  expect(await feed.locator('.live-download-pulse').evaluate(element => getComputedStyle(element).animationName)).toBe('none')
  expect(await feed.locator('.live-download-flow').evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('none')
})
