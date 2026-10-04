import { expect, mockAdminApi, test } from './fixtures/admin-api'
import type { AccessLog, AccessLogDetail } from '../src/lib/adminApi.types'

function log(overrides: Partial<AccessLog> & Pick<AccessLog, 'id' | 'package_name'>): AccessLog {
  return {
    adapter_type: 'pypi',
    method: 'GET',
    cache_key: 'pypi/simple/requests/requests-2.32.4.whl',
    version: '2.32.4',
    hit: true,
    cache_result: 'hit',
    upstream: '',
    latency_ms: 18,
    status_code: 200,
    client_ip: '192.168.1.100',
    bytes_sent: 2_097_152,
    created_at: new Date(Date.now() - 120_000).toISOString(),
    ...overrides,
    id: overrides.id,
    package_name: overrides.package_name,
  }
}

const detail: AccessLogDetail = {
  ...log({ id: 1, package_name: 'requests' }),
  request_id: 'req-fixture-1',
  cache_result: 'miss',
  cache_reason: 'cached_response_not_used',
  policy_decision: 'allow',
  policy_reason: 'no_matching_rule',
  delivery_result: 'complete',
  delivery_reason: 'stream_completed',
  upstream_requests: 1,
  upstream_bytes: 2_000_000,
  audit_events: [
    {
      id: 11,
      ecosystem: 'pypi',
      package_name: 'requests',
      version: '2.32.4',
      action: 'download',
      cache_result: 'miss',
      status_code: 200,
      created_at: '2026-10-03T00:00:00Z',
    },
  ],
}

test('Overview lists the latest client requests independent of the statistics range', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': {
      items: [
        log({ id: 2, package_name: 'numpy', version: '2.1.0' }),
        log({ id: 1, package_name: 'requests', version: '2.32.4', cache_result: 'miss', hit: false, upstream: 'tuna', latency_ms: 240, bytes_sent: 4_194_304 }),
      ],
      total: 2,
      page: 1,
      page_size: 5,
    },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const table = page.locator('[data-dashboard-recent-requests]')
  await expect(table.getByRole('heading', { name: '最近请求' })).toBeVisible()
  await expect(table).toContainText('不受上方统计周期限制')
  await expect(table.locator('[data-recent-request-id]')).toHaveCount(2)
  await expect(table.locator('[data-recent-request-id="1"]')).toContainText('requests')
  await expect(table.locator('[data-recent-request-id="1"]')).toContainText('回源完成')
  await expect(table.getByRole('link', { name: /查看全部/ })).toHaveAttribute('href', '/admin/logs')
})

test('request rows open a centered detail dialog that closes with Escape', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': {
      items: [log({ id: 1, package_name: 'requests' })],
      total: 1,
      page: 1,
      page_size: 5,
    },
    'GET /api/v1/admin/logs/1': detail,
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const row = page.locator('[data-recent-request-id="1"]')
  await row.getByRole('button').click()

  const dialog = page.locator('[data-slot="dialog-content"]')
  await expect(dialog.getByText('请求详情')).toBeVisible()
  await expect(dialog).toContainText('req-fixture-1')
  await expect(page.locator('[data-slot="sheet-content"]')).toHaveCount(0)

  const geometry = await dialog.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return { center: rect.left + rect.width / 2, viewport: window.innerWidth / 2 }
  })
  expect(Math.abs(geometry.center - geometry.viewport)).toBeLessThan(2)

  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(row.getByRole('button').first()).toBeFocused()
})

test('Overview recent requests render an empty state when nothing has been served', async ({ page }) => {
  await page.goto('/admin')
  const table = page.locator('[data-dashboard-recent-requests]')
  await expect(table).toContainText('等待首次下载')
  await expect(table.locator('[data-recent-request-id]')).toHaveCount(0)
})
