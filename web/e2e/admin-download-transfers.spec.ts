import AxeBuilder from '@axe-core/playwright'
import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'
import type { CacheTransfer } from '../src/lib/adminApi.types'

const transfer: CacheTransfer = {
  id: '1', name: 'torch-2.8.0-cp311-manylinux_x86_64.whl', ecosystem: 'extra:pytorch', upstream: 'pytorch',
  status: 'downloading', downloaded_bytes: 512 * 1024 * 1024, total_bytes: 1024 * 1024 * 1024,
  resume_attempts: 0, resume_offset: 0, bytes_per_second: 1024 * 1024,
  started_at: '2026-10-16T10:00:00Z', updated_at: '2026-10-16T10:00:01Z',
}

test('cache fills show progress and resume history without treating saved bytes as committed cache', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'light', 'en')
  let item = { ...transfer }
  let cacheReads = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/cache/transfers': () => ({ items: [item], limit: 64 }),
    'GET /api/v1/admin/cache': () => { cacheReads += 1; return { items: [], total: 0 } },
    'GET /api/v1/admin/cache/distribution': () => ({ total_size: item.status === 'cached' ? item.total_bytes : 0, max_size: 10 * 1024 ** 3, by_type: [], top_packages: [] }),
  })
  await page.goto('/admin/cache')
  const section = page.getByRole('region', { name: 'Download tasks' })
  await expect(section.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '50')
  await expect(section.getByText('Downloading', { exact: true })).toBeVisible()
  const initialCacheReads = cacheReads
  item = { ...item, status: 'resuming', resume_attempts: 1, resume_offset: transfer.downloaded_bytes }
  await page.clock.runFor(2100)
  await expect(section.getByText('Attempting resume', { exact: true })).toBeVisible()
  await expect(section.getByText(/Resume attempts: 1/)).toBeVisible()
  item = { ...item, status: 'saving', downloaded_bytes: item.total_bytes }
  await page.clock.runFor(2100)
  await expect(section.getByText('Writing cache', { exact: true })).toBeVisible()
  await expect(section.getByText('Complete cache entry', { exact: true })).toHaveCount(0)
  item = { ...item, status: 'cached', finished_at: '2026-10-16T10:00:03Z' }
  await page.clock.runFor(2100)
  await expect(section.getByText('Complete cache entry', { exact: true })).toBeVisible()
  await expect(section.getByText(/Resume attempts: 1/)).toBeVisible()
  await expect.poll(() => cacheReads).toBeGreaterThan(initialCacheReads)
  await page.screenshot({ path: test.info().outputPath('download-tasks.png'), fullPage: true })
})

test('unknown sizes remain indeterminate and long artifact names fit a mobile viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 })
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, { 'GET /api/v1/admin/cache/transfers': { items: [{ ...transfer, name: 'very-long-artifact-name-'.repeat(9) + '.whl', total_bytes: -1 }], limit: 64 } })
  await page.goto('/admin/cache')
  const section = page.getByRole('region', { name: '下载任务' })
  await expect(section.getByRole('progressbar')).not.toHaveAttribute('aria-valuenow')
  await expect(section.getByText(/总大小未知/)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  const result = await new AxeBuilder({ page }).include('section[aria-label="下载任务"]').analyze()
  expect(result.violations).toEqual([])
})

test('poll failures label stale progress and permission loss hides the previous snapshot', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'light', 'en')
  let status = 200
  await mockAdminApi(page, { 'GET /api/v1/admin/cache/transfers': () => status === 200 ? { items: [transfer], limit: 64 } : { status, body: { message: 'unavailable' } } })
  await page.goto('/admin/cache')
  const section = page.getByRole('region', { name: 'Download tasks' })
  await expect(section.getByText(transfer.name)).toBeVisible()
  status = 503
  await page.clock.runFor(2100)
  await expect(section.getByRole('button', { name: 'Refresh' })).toBeVisible()
  await expect(section.getByText(transfer.name)).toBeVisible()
  status = 403
  await page.clock.runFor(2100)
  await expect(section.getByText('You do not have permission to view download tasks.')).toBeVisible()
  await expect(section.getByText(transfer.name)).toHaveCount(0)
})


test('download status failures can be retried and failed fills never claim a complete cache', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'light', 'en')
  let failed = true
  let items: CacheTransfer[] = []
  await mockAdminApi(page, { 'GET /api/v1/admin/cache/transfers': () => failed ? { status: 503, body: { message: 'origin internal details' } } : { items, limit: 64 } })
  await page.goto('/admin/cache')
  const section = page.getByRole('region', { name: 'Download tasks' })
  await expect(section.getByText('Download status is unavailable.')).toBeVisible()
  failed = false
  await section.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(section.getByText('No recent download tasks. New upstream downloads appear here.')).toBeVisible()
  items = [{ ...transfer, status: 'failed', finished_at: '2026-10-16T10:01:00Z' }]
  await page.clock.runFor(2100)
  await expect(section.getByText('Not cached', { exact: true })).toBeVisible()
  await expect(section.getByText('Complete cache entry', { exact: true })).toHaveCount(0)
  await expect(section.getByText('The cache fill did not complete. Check access logs for details.')).toBeVisible()
})
