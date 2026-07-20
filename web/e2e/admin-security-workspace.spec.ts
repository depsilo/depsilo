import type { Request } from '@playwright/test'

import { securityEcosystems } from '../src/admin/operatorEcosystems'
import type { SecurityVulnerability } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const suggestion = (id: number, packageName = `fixture-package-${id}`): SecurityVulnerability => ({
  id,
  osv_id: `OSV-2026-${id}`,
  ecosystem: 'pypi',
  package_name: packageName,
  affected_ranges: '>=1.0.0 <1.0.1',
  severity: 'high',
  cvss_score: 8.1,
  summary: 'Fixture vulnerability',
  details: 'Fixture vulnerability details',
  aliases: '',
  references: '',
  published_at: '2026-07-10T00:00:00Z',
  modified_at: '2026-07-10T00:00:00Z',
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
})

test('Security uses stable Operator page chrome with one page heading across tabs', async ({ page }) => {
  await page.goto('/admin/security')

  const adminPage = page.locator('[data-admin-page]')
  await expect(adminPage).toHaveCount(1)
  await expect(adminPage).toHaveAttribute('data-admin-page-width', 'fluid')
  await expect(adminPage.locator('[data-admin-page-description]')).toContainText(
    /查看漏洞情报与拦截建议|Review vulnerability intelligence/,
  )
  await expect(page.locator('h1:visible')).toHaveCount(1)
  await expect(page.locator('[data-admin-topbar] h1')).toHaveText(/包安全|Package Security/)

  for (const tabName of [/漏洞列表|Vulnerabilities/, /建议规则|Suggested Rules/, /策略配置|Policies/]) {
    await page.getByRole('tab', { name: tabName }).click()
    await expect(adminPage).toHaveCount(1)
    await expect(page.locator('h1:visible')).toHaveCount(1)
    await expect(page.locator('[data-admin-topbar] h1')).toHaveText(/包安全|Package Security/)
  }
})

test('Security vulnerability search keeps draft input local and submits a trimmed page-one query', async ({ page }) => {
  const requests: Array<{ page: string | null; perPage: string | null; packageName: string | null }> = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/vulnerabilities': (request: Request) => {
      const params = new URL(request.url()).searchParams
      requests.push({
        page: params.get('page'),
        perPage: params.get('per_page'),
        packageName: params.get('package'),
      })
      return { items: [], total: 41, page: Number(params.get('page') ?? 1) }
    },
  })
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /漏洞列表|Vulnerabilities/ }).click()

  await expect.poll(() => requests.length).toBe(1)
  const pagination = page.locator('[data-admin-pagination]')
  await pagination.getByRole('button', { name: /下一页|Next/ }).click()
  await expect.poll(() => requests.at(-1)).toEqual({ page: '2', perPage: '20', packageName: null })

  const search = page.getByLabel(/包名搜索|Package search/)
  await search.fill('  fixture-package  ')
  await expect(search).toHaveValue('  fixture-package  ')
  await page.waitForTimeout(400)
  expect(requests).toHaveLength(2)

  await page.getByRole('button', { name: /^(搜索|Search)$/ }).click()
  await expect.poll(() => requests.at(-1)).toEqual({
    page: '1',
    perPage: '20',
    packageName: 'fixture-package',
  })
  expect(requests).toHaveLength(3)
})

test('Security ecosystem catalog matches the scanner capability surface exactly', () => {
  expect(securityEcosystems.map(ecosystem => ecosystem.id)).toEqual([
    'pypi',
    'apt',
    'npm',
    'go',
    'cargo',
    'maven',
    'rubygems',
    'composer',
    'nuget',
    'cran',
  ])
  expect(securityEcosystems).toHaveLength(10)
})

test('Security suggestions use total-count pagination and disable Next on the last page', async ({ page }) => {
  const requestedPages: number[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': (request: Request) => {
      const currentPage = Number(new URL(request.url()).searchParams.get('page') ?? 1)
      requestedPages.push(currentPage)
      return {
        items: [suggestion(currentPage === 1 ? 1 : 21)],
        total: 21,
        page: currentPage,
      }
    },
  })
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /建议规则|Suggested Rules/ }).click()

  const pagination = page.locator('[data-admin-pagination]')
  await expect(pagination).toBeVisible()
  await expect(pagination.getByRole('button', { name: /上一页|Previous/ })).toBeDisabled()
  const next = pagination.getByRole('button', { name: /下一页|Next/ })
  await expect(next).toBeEnabled()
  await next.click()

  await expect.poll(() => requestedPages).toEqual([1, 2])
  await expect(pagination).toContainText(/2\s*\/\s*2|2\s+of\s+2/i)
  await expect(pagination.getByRole('button', { name: /上一页|Previous/ })).toBeEnabled()
  await expect(pagination.getByRole('button', { name: /下一页|Next/ })).toBeDisabled()
})

test('Security reports an API scan already in progress as a busy disabled action', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/dashboard': {
      total_vulnerabilities: 1,
      affected_packages: 1,
      by_severity: { critical: 1, high: 0, medium: 0, low: 0 },
      auto_blocked_count: 0,
      last_scan_at: '2026-07-10T00:00:00Z',
      scan_in_progress: true,
    },
  })
  await page.goto('/admin/security')

  const scan = page.getByRole('button', { name: /扫描中|Scanning/ })
  await expect(scan).toBeVisible()
  await expect(scan).toHaveAttribute('aria-busy', 'true')
  await expect(scan).toBeDisabled()
})

test('Security suggestion rows do not create root overflow at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  const longPackageName = 'a-deliberately-long-security-package-name-that-must-wrap-within-the-operator-workspace'
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': {
      items: [suggestion(1, longPackageName)],
      total: 1,
      page: 1,
    },
  })
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /建议规则|Suggested Rules/ }).click()

  await expect(page.getByText(longPackageName)).toBeVisible()
  await expect(page.getByRole('button', { name: /阻止|Block/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /忽略|Dismiss/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})
