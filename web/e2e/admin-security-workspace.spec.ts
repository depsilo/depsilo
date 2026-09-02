import type { Request } from '@playwright/test'

import { securityEcosystems, supportsVulnerabilityAutoBlock } from '../src/admin/operatorEcosystems'
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
  await expect(adminPage.locator('[data-admin-page-title]')).toHaveText(/包安全|Package Security/)
  await expect(page.locator('[data-admin-topbar]').getByRole('heading')).toHaveCount(0)

  for (const tabName of [/漏洞列表|Vulnerabilities/, /建议规则|Suggested Rules/, /策略配置|Policies/]) {
    await page.getByRole('tab', { name: tabName }).click()
    await expect(adminPage).toHaveCount(1)
    await expect(page.locator('h1:visible')).toHaveCount(1)
    await expect(adminPage.locator('[data-admin-page-title]')).toHaveText(/包安全|Package Security/)
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

test('Security tabs are deep-linkable and preserve browser back navigation', async ({ page }) => {
  await page.goto('/admin/security?tab=suggestions')
  await expect(page.getByRole('tab', { name: /建议规则|Suggested Rules/ })).toHaveAttribute('aria-selected', 'true')

  await page.getByRole('tab', { name: /策略配置|Policies/ }).click()
  await expect(page).toHaveURL(/\/admin\/security\?tab=policies$/)
  await expect(page.getByRole('tab', { name: /策略配置|Policies/ })).toHaveAttribute('aria-selected', 'true')

  await page.goBack()
  await expect(page).toHaveURL(/\/admin\/security\?tab=suggestions$/)
  await expect(page.getByRole('tab', { name: /建议规则|Suggested Rules/ })).toHaveAttribute('aria-selected', 'true')
})

test('Security replaces an invalid tab with the canonical overview URL', async ({ page }) => {
  await page.goto('/admin')
  await page.goto('/admin/security?tab=not-a-security-tab')

  await expect(page).toHaveURL(/\/admin\/security$/)
  await expect(page.getByRole('tab', { name: /总览|Overview/ })).toHaveAttribute('aria-selected', 'true')

  await page.goBack()
  await expect(page).toHaveURL(/\/admin$/)
})

test('Security suggestions direct blocking to manual rules and preserve a failed dismissal for retry', async ({ page }) => {
  let requestCount = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': {
      items: [suggestion(1, 'unsafe-package')],
      total: 1,
      page: 1,
    },
    'POST /api/v1/admin/security/suggestions/1/dismiss': () => {
      requestCount += 1
      if (requestCount === 1) {
        return {
          status: 503,
          body: { code: 'DISMISS_UNAVAILABLE', message: 'Suggestion service is temporarily unavailable' },
        }
      }
      return { status: 'dismissed' }
    },
  })
  await page.goto('/admin/security?tab=suggestions')

  await expect(page.getByText(/完整 OSV 受影响集合目前无法无损投影|complete affected sets cannot be projected losslessly/)).toBeVisible()
  await expect(page.getByRole('link', { name: /打开包规则|Open Package Rules/ })).toHaveAttribute('href', '/admin/rules')
  await expect(page.getByRole('button', { name: /^(阻止|Block)$/ })).toHaveCount(0)

  await page.getByRole('button', { name: /^(忽略|Dismiss)$/ }).click()
  const dialog = page.getByRole('dialog', { name: /要忽略这条安全建议吗|Dismiss this security suggestion/ })
  const confirm = dialog.getByRole('button', { name: /忽略建议|Dismiss suggestion/ })
  await expect(dialog).toContainText('unsafe-package')
  expect(requestCount).toBe(0)

  await confirm.click()
  await expect(dialog.getByRole('alert')).toContainText('Suggestion service is temporarily unavailable')
  await expect(confirm).toBeEnabled()

  await confirm.click()
  await expect(dialog).not.toBeVisible()
  expect(requestCount).toBe(2)
})

test('Security policies support a shared draft, changed-only review, and one batch save', async ({ page }) => {
  const policyRequests: Array<{ ecosystem: string; autoBlock: boolean; threshold: number }> = []
  let activeRequests = 0
  let maxConcurrentRequests = 0
  let releaseRequests!: () => void
  const requestsReleased = new Promise<void>((resolve) => { releaseRequests = resolve })
  const legacyPolicies = securityEcosystems.map((ecosystem, index) => ({
    id: index + 1,
    ecosystem: ecosystem.id,
    auto_block_enabled: true,
    min_cvss_score: 8.5,
    created_by: 'admin',
    created_at: '2026-07-10T00:00:00Z',
    updated_at: '2026-07-10T00:00:00Z',
  }))
  const policyOverrides = Object.fromEntries(securityEcosystems.map(ecosystem => [
    `PUT /api/v1/admin/security/policies/${ecosystem.id}`,
    async (request: Request) => {
      const body = request.postDataJSON() as { auto_block_enabled: boolean; min_cvss_score: number }
      policyRequests.push({
        ecosystem: ecosystem.id,
        autoBlock: body.auto_block_enabled,
        threshold: body.min_cvss_score,
      })
      const requestId = policyRequests.length
      activeRequests += 1
      maxConcurrentRequests = Math.max(maxConcurrentRequests, activeRequests)
      try {
        await requestsReleased
        return {
          id: requestId,
          ecosystem: ecosystem.id,
          auto_block_enabled: body.auto_block_enabled,
          min_cvss_score: body.min_cvss_score,
          created_by: 'admin',
          created_at: '2026-07-10T00:00:00Z',
          updated_at: '2026-07-10T00:00:00Z',
        }
      } finally {
        activeRequests -= 1
      }
    },
  ]))
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': legacyPolicies,
    ...policyOverrides,
  })
  await page.goto('/admin/security?tab=policies')

  const bulk = page.locator('[data-security-policy-bulk]')
  await expect(bulk.getByRole('switch', { name: /共享自动拦截基线|Shared auto-block baseline/ })).toBeDisabled()
  await expect(bulk.getByLabel(/CVSS 阈值|CVSS threshold/)).toBeDisabled()
  await expect(bulk).toContainText(/所有生态的自动拦截都已安全停用|safety-disabled for every ecosystem/)
  await bulk.getByRole('button', { name: /应用草稿到全部|Apply draft to all/ }).click()

  await expect(page.getByText(/10 项未保存变更|10 unsaved changes/)).toBeVisible()
  await expect(page.locator('[data-policy-ecosystem]')).toHaveCount(10)
  await page.getByRole('button', { name: /保存全部变更|Save all changes/ }).click()
  const dialog = page.getByRole('dialog', { name: /要保存安全策略变更吗|Save security policy changes/ })
  await expect(dialog).toContainText(/10 个生态策略|10 ecosystem policies/)
  await dialog.getByRole('button', { name: /保存策略变更|Save policy changes/ }).click()

  await expect.poll(() => policyRequests.length).toBeGreaterThan(0)
  await page.waitForTimeout(100)
  expect(policyRequests.length).toBeLessThanOrEqual(4)
  expect(maxConcurrentRequests).toBeLessThanOrEqual(4)
  releaseRequests()

  await expect.poll(() => policyRequests).toHaveLength(10)
  expect(maxConcurrentRequests).toBeLessThanOrEqual(4)
  expect(policyRequests.every(request =>
    request.autoBlock === supportsVulnerabilityAutoBlock(request.ecosystem) && request.threshold === 8.5,
  )).toBe(true)
  await expect(page.getByText(/没有策略变更|No policy changes/)).toBeVisible()
})

test('Security permits disabling but not re-enabling a legacy exact-only auto-block policy', async ({ page }) => {
  let savedAutoBlock: boolean | undefined
  const legacyGoPolicy = {
    id: 1,
    ecosystem: 'go',
    auto_block_enabled: true,
    min_cvss_score: 8.5,
    created_by: 'admin',
    created_at: '2026-07-10T00:00:00Z',
    updated_at: '2026-07-10T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [legacyGoPolicy],
    'PUT /api/v1/admin/security/policies/go': (request: Request) => {
      const body = request.postDataJSON() as { auto_block_enabled: boolean; min_cvss_score: number }
      savedAutoBlock = body.auto_block_enabled
      return { ...legacyGoPolicy, auto_block_enabled: body.auto_block_enabled }
    },
  })
  await page.goto('/admin/security?tab=policies')

  const goPolicy = page.locator('[data-policy-ecosystem="go"]')
  const autoBlock = goPolicy.getByRole('switch')
  await expect(autoBlock).toBeChecked()
  await expect(autoBlock).toBeEnabled()
  await autoBlock.click()
  await expect(autoBlock).not.toBeChecked()
  await expect(autoBlock).toBeDisabled()

  await goPolicy.getByRole('button', { name: /GO.*保存|GO.*Save/ }).click()
  await expect.poll(() => savedAutoBlock).toBe(false)
  await expect(goPolicy.getByRole('button', { name: /GO.*保存|GO.*Save/ })).toBeDisabled()
})

test('Security disables conflicting bulk policy actions while a row save is pending', async ({ page }) => {
  let markSaveStarted!: () => void
  let releaseSave!: () => void
  const saveStarted = new Promise<void>((resolve) => { markSaveStarted = resolve })
  const saveReleased = new Promise<void>((resolve) => { releaseSave = resolve })
  const legacyPypiPolicy = {
    id: 1,
    ecosystem: 'pypi',
    auto_block_enabled: true,
    min_cvss_score: 8.5,
    created_by: 'admin',
    created_at: '2026-07-10T00:00:00Z',
    updated_at: '2026-07-10T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [legacyPypiPolicy],
    'PUT /api/v1/admin/security/policies/pypi': async (request: Request) => {
      const body = request.postDataJSON() as { auto_block_enabled: boolean; min_cvss_score: number }
      markSaveStarted()
      await saveReleased
      return {
        id: 1,
        ecosystem: 'pypi',
        auto_block_enabled: body.auto_block_enabled,
        min_cvss_score: body.min_cvss_score,
        created_by: 'admin',
        created_at: '2026-07-10T00:00:00Z',
        updated_at: '2026-07-10T00:00:00Z',
      }
    },
  })
  await page.goto('/admin/security?tab=policies')

  const pypiPolicy = page.locator('[data-policy-ecosystem="pypi"]')
  await pypiPolicy.getByRole('switch', { name: /PYPI.*自动拦截|PYPI.*Auto-block/ }).click()
  await pypiPolicy.getByRole('button', { name: /PYPI.*保存|PYPI.*Save/ }).click()
  await saveStarted

  const bulk = page.locator('[data-security-policy-bulk]')
  await expect(bulk.getByRole('switch', { name: /共享自动拦截基线|Shared auto-block baseline/ })).toBeDisabled()
  await expect(bulk.getByRole('button', { name: /应用草稿到全部|Apply draft to all/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /重置变更|Reset changes/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /保存全部变更|Save all changes/ })).toBeDisabled()

  releaseSave()
  await expect(pypiPolicy.getByRole('button', { name: /PYPI.*保存|PYPI.*Save/ })).toBeDisabled()
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
  await expect(page.getByRole('link', { name: /打开包规则|Open Package Rules/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /忽略|Dismiss/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})
