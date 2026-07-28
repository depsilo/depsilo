import type { Request } from '@playwright/test'

import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('upstream update history renders episode counts and loads the next cursor page', async ({ page }) => {
  const requestedCursors: Array<string | null> = []
  let releaseSecondPage: () => void = () => {}
  const secondPageGate = new Promise<void>(resolve => {
    releaseSecondPage = resolve
  })

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': async (request: Request) => {
      const searchParams = new URL(request.url()).searchParams
      const cursor = searchParams.get('cursor')
      expect(searchParams.get('limit')).toBe('100')
      expect(searchParams.has('offset')).toBe(false)
      requestedCursors.push(cursor)

      if (cursor === null) {
        return {
          items: [{
            id: 2,
            cache_entry_id: 42,
            ecosystem: 'pypi',
            upstream: 'pypi.org',
            package: 'requests',
            result: 'unchanged',
            detail: 'upstream metadata not modified',
            latency_ms: 45,
            occurrence_count: 3,
            first_seen_at: '2026-07-17T08:00:00Z',
            last_seen_at: '2026-07-17T08:10:00Z',
            created_at: '2026-07-17T08:00:00Z',
          }],
          total: 2,
          next_cursor: 'page-2',
        }
      }

      expect(cursor).toBe('page-2')
      await secondPageGate
      return {
        items: [{
          id: 1,
          cache_entry_id: 43,
          ecosystem: 'pypi',
          upstream: 'pypi.org',
          package: 'urllib3',
          result: 'error',
          detail: 'metadata refresh failed; inspect server logs',
          latency_ms: 110,
          occurrence_count: 1,
          first_seen_at: '2026-07-17T07:00:00Z',
          last_seen_at: '2026-07-17T07:00:00Z',
          created_at: '2026-07-17T07:00:00Z',
        }],
        total: 2,
        next_cursor: null,
      }
    },
  })

  await page.goto('/admin/upstream-updates')

  const table = page.locator('[data-upstream-update-table]')
  await expect(table.getByRole('cell', { name: 'requests', exact: true })).toBeVisible()
  await expect(table.getByText('×3', { exact: true })).toHaveAttribute('aria-label', '检查次数：3')

  const loadMore = page.getByRole('button', { name: /加载/ })
  await loadMore.click()
  await expect(loadMore).toBeDisabled()
  await expect(loadMore).toHaveText('正在加载...')

  releaseSecondPage()
  await expect(page.getByRole('cell', { name: 'urllib3', exact: true })).toBeVisible()
  await expect(loadMore).toBeHidden()
  expect(requestedCursors).toEqual([null, 'page-2'])
})

test('keeps loaded history visible when the next cursor page fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': async (request: Request) => {
      const cursor = new URL(request.url()).searchParams.get('cursor')
      if (cursor !== null) {
        return { status: 500, body: { code: 'FAILED', message: 'fixture next-page failure' } }
      }
      return {
        items: [{
          id: 1,
          ecosystem: 'pypi',
          upstream: 'pypi.org',
          package: 'requests',
          result: 'unchanged',
          detail: 'upstream metadata not modified',
          latency_ms: 45,
          occurrence_count: 1,
          first_seen_at: '2026-07-17T08:00:00Z',
          last_seen_at: '2026-07-17T08:00:00Z',
          created_at: '2026-07-17T08:00:00Z',
        }],
        total: 2,
        next_cursor: 'page-2',
      }
    },
  })

  await page.goto('/admin/upstream-updates')
  await page.getByRole('button', { name: '加载更多' }).click()

  await expect(page.getByRole('alert')).toHaveText('加载后续记录失败，请重试“加载更多”。')
  await expect(page.getByRole('cell', { name: 'requests', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '加载更多' })).toBeEnabled()
})
