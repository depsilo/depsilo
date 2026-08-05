import type { Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

function requestParams(request: Request) {
  return Object.fromEntries(new URL(request.url()).searchParams.entries())
}

test('Access Logs keeps applied filters and pagination in the URL and restores them with Back', async ({ page }) => {
  const requests: Array<Record<string, string>> = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': (request: Request) => {
      requests.push(requestParams(request))
      return { items: [], total: 120, page: Number(new URL(request.url()).searchParams.get('page') ?? 1), page_size: 50 }
    },
  })

  await page.goto('/admin/logs?package=requests&ecosystem=pypi&result=hit&page=2')
  await expect.poll(() => requests.at(-1)).toMatchObject({
    search: 'requests',
    adapter_type: 'pypi',
    hit: 'true',
    page: '2',
  })
  await expect(page.getByLabel(/按包名搜索访问日志|Search access logs by package/)).toHaveValue('requests')

  await page.getByLabel(/生态|Ecosystem/).selectOption('npm')
  await expect(page).toHaveURL(/package=requests/)
  await expect(page).toHaveURL(/ecosystem=npm/)
  await expect(page).not.toHaveURL(/page=2/)
  await expect.poll(() => requests.at(-1)).toMatchObject({
    search: 'requests',
    adapter_type: 'npm',
    hit: 'true',
    page: '1',
  })

  await page.goBack()
  await expect(page).toHaveURL(/ecosystem=pypi/)
  await expect(page).toHaveURL(/page=2/)
  await expect.poll(() => requests.at(-1)).toMatchObject({
    adapter_type: 'pypi',
    page: '2',
  })

  await page.reload()
  await expect(page.getByLabel(/按包名搜索访问日志|Search access logs by package/)).toHaveValue('requests')
  await expect(page.getByRole('button', { name: /清除筛选|Clear filters/ })).toBeVisible()
})

test('Audit Logs deep-links every applied filter and returns to prior filter state', async ({ page }) => {
  const requests: Array<Record<string, string>> = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit-logs': (request: Request) => {
      requests.push(requestParams(request))
      return { items: [], total: 0, page: Number(new URL(request.url()).searchParams.get('page') ?? 1) }
    },
  })

  await page.goto('/admin/audit?package=urllib3&ecosystem=pypi&result=error&range=7d')
  await expect.poll(() => requests.at(-1)).toMatchObject({
    package: 'urllib3',
    ecosystem: 'pypi',
    result: 'error',
    page: '1',
  })

  await page.getByRole('button', { name: /30 天|30 Days/ }).click()
  await expect(page).toHaveURL(/range=30d/)
  await page.goBack()
  await expect(page).toHaveURL(/range=7d/)
  await expect(page.getByRole('button', { name: /7 天|7 Days/ })).toHaveAttribute('aria-pressed', 'true')

  await page.getByRole('button', { name: /清除筛选|Clear filters/ }).click()
  await expect(page).toHaveURL(/\/admin\/audit$/)
  await expect(page.getByLabel(/按包名搜索审计日志|Search audit logs by package/)).toHaveValue('')
})

for (const malformedCase of [
  {
    name: 'Access Logs',
    path: '/admin/logs?package=%20requests%20&ecosystem=invalid-ecosystem&result=error&page=2tail',
    canonicalPath: '/admin/logs?package=requests',
    endpoint: 'GET /api/v1/admin/logs',
  },
  {
    name: 'Audit Logs',
    path: '/admin/audit?package=%20urllib3%20&ecosystem=invalid-ecosystem&result=unknown&range=forever&page=1.5',
    canonicalPath: '/admin/audit?package=urllib3',
    endpoint: 'GET /api/v1/admin/audit-logs',
  },
] as const) {
  test(`${malformedCase.name} replaces malformed filter and page URLs with canonical state`, async ({ page }) => {
    const requests: Array<Record<string, string>> = []
    await mockAdminApi(page, {
      [malformedCase.endpoint]: (request: Request) => {
        requests.push(requestParams(request))
        return { items: [], total: 0, page: 1, page_size: 50 }
      },
    })

    await page.goto('/admin')
    await page.goto(malformedCase.path)

    await expect(page).toHaveURL(new RegExp(`${malformedCase.canonicalPath.replace('?', '\\?')}$`))
    await expect.poll(() => requests.at(-1)).toMatchObject({
      page: '1',
    })
    expect(requests.at(-1)).toMatchObject(
      malformedCase.name === 'Access Logs' ? { search: 'requests' } : { package: 'urllib3' },
    )
    expect(requests.at(-1)).not.toHaveProperty('adapter_type')
    expect(requests.at(-1)).not.toHaveProperty('ecosystem')
    expect(requests.at(-1)).not.toHaveProperty('hit')
    expect(requests.at(-1)).not.toHaveProperty('result')

    await page.goBack()
    await expect(page).toHaveURL(/\/admin$/)
  })
}

test('Audit refetch and export compute their time range when each request executes', async ({ page }) => {
  const queryRequests: Array<Record<string, string>> = []
  const exportRequests: Array<Record<string, string>> = []
  let queryCalls = 0
  await page.clock.install({ time: new Date('2026-07-29T12:00:00.000Z') })
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit-logs': (request: Request) => {
      queryCalls += 1
      queryRequests.push(requestParams(request))
      if (queryCalls === 1) {
        return {
          status: 503,
          body: { code: 'AUDIT_UNAVAILABLE', message: 'Audit query is temporarily unavailable' },
        }
      }
      return { items: [], total: 0, page: 1 }
    },
    'GET /api/v1/admin/audit-logs/export': (request: Request) => {
      exportRequests.push(requestParams(request))
      return {
        status: 200,
        body: 'header,value\nfixture,1\n',
        contentType: 'text/csv',
        serialize: 'text' as const,
      }
    },
  })

  await page.goto('/admin/audit?range=7d')
  await expect(page.getByRole('alert')).toContainText('Audit query is temporarily unavailable')
  const firstStart = Date.parse(queryRequests[0].start)
  const firstEnd = Date.parse(queryRequests[0].end)
  expect(firstEnd - firstStart).toBe(7 * 24 * 60 * 60 * 1_000)

  await page.clock.runFor(60_000)
  await page.getByRole('button', { name: /重试|Retry/ }).click()
  await expect.poll(() => queryRequests).toHaveLength(2)
  const refetchStart = Date.parse(queryRequests[1].start)
  const refetchEnd = Date.parse(queryRequests[1].end)
  expect(refetchStart - firstStart).toBeGreaterThanOrEqual(60_000)
  expect(refetchEnd - firstEnd).toBeGreaterThanOrEqual(60_000)

  await page.clock.runFor(60_000)
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: /导出 CSV|Export CSV/ }).click()
  await downloadPromise
  expect(exportRequests).toHaveLength(1)
  expect(Date.parse(exportRequests[0].start) - refetchStart).toBeGreaterThanOrEqual(60_000)
  expect(Date.parse(exportRequests[0].end) - refetchEnd).toBeGreaterThanOrEqual(60_000)
})

for (const exportCase of [
  {
    path: '/admin/logs',
    endpoint: 'GET /api/v1/admin/logs/export',
    button: /导出 CSV|Export CSV/,
    failure: /访问日志导出失败|Could not export access logs/,
    success: /已导出 depsilo-access-logs|Exported depsilo-access-logs/,
    filename: /^depsilo-access-logs-\d{4}-\d{2}-\d{2}\.csv$/,
  },
  {
    path: '/admin/audit',
    endpoint: 'GET /api/v1/admin/audit-logs/export',
    button: /导出 CSV|Export CSV/,
    failure: /审计日志导出失败|Could not export audit logs/,
    success: /已导出 depsilo-audit|Exported depsilo-audit/,
    filename: /^depsilo-audit-\d{4}-\d{2}-\d{2}\.csv$/,
  },
] as const) {
  test(`${exportCase.path} export prevents silent failure and reports the downloaded file`, async ({ page }) => {
    let calls = 0
    await mockAdminApi(page, {
      [exportCase.endpoint]: () => {
        calls += 1
        if (calls === 1) {
          return {
            status: 503,
            body: { code: 'EXPORT_UNAVAILABLE', message: 'Export service is temporarily unavailable' },
          }
        }
        return {
          status: 200,
          body: 'header,value\nfixture,1\n',
          contentType: 'text/csv',
          serialize: 'text' as const,
        }
      },
    })
    await page.goto(exportCase.path)

    const exportButton = page.getByRole('button', { name: exportCase.button })
    await exportButton.click()
    await expect(page.locator('[data-toast-tone="danger"]')).toContainText(exportCase.failure)
    await expect(exportButton).toBeEnabled()

    const downloadPromise = page.waitForEvent('download')
    await exportButton.click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toMatch(exportCase.filename)
    await expect(page.locator('[data-toast-tone="success"]')).toContainText(exportCase.success)
    expect(calls).toBe(2)
  })
}
