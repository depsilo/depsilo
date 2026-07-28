import AxeBuilder from '@axe-core/playwright'
import type { Page, Request } from '@playwright/test'

import type {
  AdminUpstreamUpdateEvent,
  AdminUpstreamUpdateListResponse,
} from '../src/lib/adminApi.types'
import {
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

function updateEvent(
  id: number,
  overrides: Partial<AdminUpstreamUpdateEvent> = {},
): AdminUpstreamUpdateEvent {
  return {
    id,
    cache_entry_id: 40 + id,
    ecosystem: 'pypi',
    upstream: 'pypi.org',
    package: `package-${id}`,
    result: 'unchanged',
    detail: 'upstream metadata not modified',
    latency_ms: 45,
    occurrence_count: 1,
    first_seen_at: `2026-07-17T0${id}:00:00Z`,
    last_seen_at: `2026-07-17T0${id}:00:00Z`,
    created_at: `2026-07-17T0${id}:00:00Z`,
    ...overrides,
  }
}

function updatePage(
  items: AdminUpstreamUpdateEvent[],
  nextCursor: string | null = null,
  total = items.length,
): AdminUpstreamUpdateListResponse {
  return { items, total, next_cursor: nextCursor }
}

interface CapturedQuery {
  cursor: string | null
  ecosystem: string | null
  hasOffset: boolean
  packageName: string | null
  result: string | null
}

function captureQuery(request: Request): CapturedQuery {
  const params = new URL(request.url()).searchParams
  return {
    cursor: params.get('cursor'),
    ecosystem: params.get('ecosystem'),
    hasOffset: params.has('offset'),
    packageName: params.get('package'),
    result: params.get('result'),
  }
}

async function navigateClient(page: Page, path: string) {
  await page.evaluate((nextPath) => {
    window.history.pushState({}, '', nextPath)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, path)
}

async function warmRouteThenPauseClock(page: Page) {
  await page.goto('/admin/upstream-updates')
  await expect(page.locator('[data-admin-page]')).toBeVisible()
  await navigateClient(page, '/')
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)
  await page.clock.install({ time: new Date('2099-01-01T00:00:00Z') })
  await page.clock.pauseAt(new Date('2099-01-01T00:00:01Z'))
}

async function settleClockedRoute(page: Page, requestCount: () => number) {
  for (let phase = 0; phase < 3; phase += 1) {
    await page.waitForLoadState('networkidle')
    await page.clock.runFor(100)
  }
  await page.clock.runFor(1_700)
  await expect.poll(requestCount).toBe(1)
  await page.waitForLoadState('networkidle')
  await page.clock.runFor(100)
}

test('trims package search, sends server filters, resets cursors, and clears filters', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  const queries: CapturedQuery[] = []

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': (request: Request) => {
      const query = captureQuery(request)
      queries.push(query)

      if (query.cursor === 'page-2') {
        return updatePage([updateEvent(1, { package: 'older-package' })], null, 2)
      }
      if (query.packageName || query.ecosystem || query.result) {
        return updatePage([updateEvent(3, {
          ecosystem: 'pypi',
          package: 'requests',
          result: 'updated',
          detail: 'cached metadata refreshed',
        })])
      }
      return updatePage([updateEvent(2, { package: 'newer-package' })], 'page-2', 2)
    },
  })

  await page.goto('/admin/upstream-updates')
  const table = page.locator('[data-upstream-update-table]')
  await page.getByRole('button', { name: /Load more/i }).click()
  await expect(table.getByText('older-package', { exact: true })).toBeVisible()
  expect(queries.some(query => query.cursor === 'page-2')).toBe(true)

  const filteredRequestStart = queries.length
  const toolbar = page.locator('[data-upstream-updates-toolbar]')
  const search = toolbar.getByLabel('Search packages')
  await search.fill('  requests  ')
  await toolbar.getByLabel('Ecosystem').selectOption('pypi')
  await toolbar.getByLabel('Result').selectOption('updated')

  await expect.poll(() => queries.slice(filteredRequestStart).some(query => (
    query.packageName === 'requests'
    && query.ecosystem === 'pypi'
    && query.result === 'updated'
  ))).toBe(true)
  expect(queries.slice(filteredRequestStart).every(query => (
    query.cursor === null && !query.hasOffset
  ))).toBe(true)
  await expect(table.getByText('requests', { exact: true })).toBeVisible()

  const clearRequestStart = queries.length
  await toolbar.getByRole('button', { name: 'Clear filters' }).click()
  await expect(search).toHaveValue('')
  await expect.poll(() => queries.slice(clearRequestStart).some(query => (
    query.packageName === null
    && query.ecosystem === null
    && query.result === null
    && query.cursor === null
    && !query.hasOffset
  ))).toBe(true)
  await expect(table.getByText('newer-package', { exact: true })).toBeVisible()
})

test('distinguishes an absolute empty history from an empty filtered result', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': updatePage([]),
  })

  await page.goto('/admin/upstream-updates')
  const absoluteEmpty = page.getByText(/No (?:upstream )?update (?:probe )?records yet/i)
  await expect(absoluteEmpty).toBeVisible()

  await page.locator('[data-upstream-updates-toolbar]')
    .getByLabel('Result')
    .selectOption('error')

  const filteredEmpty = page.getByText(/No .*match.*(?:filter|criteria)/i)
  await expect(filteredEmpty).toBeVisible()
  await expect(absoluteEmpty).toBeHidden()
  const toolbarClear = page.locator('[data-upstream-updates-toolbar]')
    .getByRole('button', { name: 'Clear filters' })
  await expect(toolbarClear).toBeVisible()

  await toolbarClear.click()
  await expect(absoluteEmpty).toBeVisible()
  await expect(filteredEmpty).toBeHidden()
})

test('makes 403 explicit and lets an initial 500 retry into the successful empty state', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': {
      status: 403,
      body: { code: 'FORBIDDEN', message: 'fixture forbidden' },
    },
  })

  await page.goto('/admin/upstream-updates')
  await expect(page.getByRole('alert')).toContainText(/permission/i)
  await expect(page.getByText(/No (?:upstream )?update (?:probe )?records yet/i)).toHaveCount(0)

  let retryCalls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': () => {
      retryCalls += 1
      return retryCalls === 1
        ? { status: 500, body: { code: 'FAILED', message: 'fixture initial failure' } }
        : updatePage([])
    },
  })
  await page.reload()

  const failure = page.getByRole('alert')
  await expect(failure).toContainText(/Unable to load upstream update records/i)
  await expect.poll(() => retryCalls).toBe(1)
  await failure.getByRole('button', { name: 'Retry' }).click()
  await expect.poll(() => retryCalls).toBe(2)
  await expect(failure).toHaveCount(0)
  await expect(page.getByText(/No (?:upstream )?update (?:probe )?records yet/i)).toBeVisible()
})

test('renders a populated mobile record list without overflow and passes axe in light English', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  const longPackage = '@depsilo/a-very-long-package-name-that-must-wrap-without-expanding-the-document'
  const longUpstream = 'https://packages.example.test/a/very/long/upstream/path/that/must/remain/inside/the/mobile/record'
  const longDetail = 'A deliberately long safe diagnostic that must wrap inside the mobile record without hiding the failed result.'

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': updatePage([
      updateEvent(3, {
        ecosystem: 'npm',
        upstream: longUpstream,
        package: longPackage,
        result: 'updated',
        detail: 'cached metadata refreshed',
        occurrence_count: 4,
        first_seen_at: '2026-07-17T07:00:00Z',
        last_seen_at: '2026-07-17T09:00:00Z',
      }),
      updateEvent(2, {
        package: 'requests',
        result: 'unchanged',
        detail: 'upstream metadata not modified',
      }),
      updateEvent(1, {
        ecosystem: 'cargo',
        upstream: 'crates.io',
        package: 'serde',
        result: 'error',
        detail: longDetail,
      }),
    ]),
  })

  await page.goto('/admin/upstream-updates')

  const mobileList = page.locator('[data-upstream-update-mobile-list]')
  await expect(mobileList).toBeVisible()
  await expect(page.locator('[data-upstream-update-table]')).toBeHidden()
  await expect(mobileList).toContainText(longPackage)
  await expect(mobileList).toContainText(longUpstream)
  await expect(mobileList).toContainText(longDetail)
  for (const result of ['Updated', 'No change', 'Failed']) {
    await expect(mobileList.getByText(result, { exact: true })).toBeVisible()
  }
  await expect(mobileList.getByLabel(/First seen .*last seen .*4 checks/i)).toBeVisible()
  await expect(mobileList.getByText('×4', { exact: true })).toHaveAttribute('aria-label', 'Checks: 4')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)

  const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(axe.violations).toEqual([])
})

test('pauses automatic refresh and preserves older pages until manual recovery succeeds', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await warmRouteThenPauseClock(page)

  let headCalls = 0
  let cursorCalls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': (request: Request) => {
      const query = captureQuery(request)
      if (query.cursor === 'page-2') {
        cursorCalls += 1
        return updatePage([updateEvent(1, { package: 'older-retained' })], null, 2)
      }

      headCalls += 1
      if (headCalls === 2) {
        return { status: 500, body: { code: 'FAILED', message: 'fixture manual refresh failure' } }
      }
      return updatePage([
        updateEvent(2, {
          package: headCalls > 2 ? 'newer-after-recovery' : 'newer-retained',
        }),
      ], 'page-2', 2)
    },
  })

  await navigateClient(page, '/admin/upstream-updates')
  await settleClockedRoute(page, () => headCalls)
  const table = page.locator('[data-upstream-update-table]')
  await expect(table.getByText('newer-retained', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: /Load more/i }).click()
  await page.clock.runFor(100)
  await expect.poll(() => cursorCalls).toBe(1)
  await expect(table.getByText('older-retained', { exact: true })).toBeVisible()
  await expect(page.locator('[data-upstream-updates-summary]')).toContainText(/auto.*refresh.*paused/i)

  const requestsBeforeInterval = headCalls + cursorCalls
  await page.clock.runFor(30_000)
  await page.clock.runFor(100)
  expect(headCalls + cursorCalls).toBe(requestsBeforeInterval)

  const backToLatest = page.locator('[data-admin-page-actions]')
    .getByRole('button', { name: 'Back to latest', exact: true })
  await backToLatest.click()
  await page.clock.runFor(100)
  await expect.poll(() => headCalls).toBe(2)
  await expect(page.getByRole('alert')).toContainText(/refresh failed|last successful/i)
  await expect(table.getByText('newer-retained', { exact: true })).toBeVisible()
  await expect(table.getByText('older-retained', { exact: true })).toBeVisible()

  await expect(backToLatest).toBeEnabled()
  await backToLatest.click()
  await page.clock.runFor(100)
  await expect.poll(() => headCalls).toBe(3)
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(table.getByText('newer-after-recovery', { exact: true })).toBeVisible()
  await expect(table.getByText('older-retained', { exact: true })).toHaveCount(0)
  await expect(page.locator('[data-admin-page-actions]')
    .getByRole('button', { name: 'Refresh', exact: true })).toBeVisible()
  await expect(page.locator('[data-upstream-updates-summary]')).toContainText(/auto.*refresh.*30 seconds/i)
})
