import type { Locator, Page, Request } from '@playwright/test'
import type {
  ComplianceProfile,
  ProjectDetail,
  ProjectListResponse,
  ProjectPackagesResponse,
  ProjectSummary,
} from '../src/lib/adminApi.types'
import { expect, mockAdminApi, setUiPreferences, test, freeProjectCapabilities, proProjectCapabilities } from './fixtures/admin-api'

const projectName = 'critical-platform'

const project: ProjectSummary = {
  id: 7,
  name: projectName,
  slug: projectName,
  description: 'Critical platform dependencies',
  package_count: 41,
  last_activity_at: '2026-07-17T12:00:00Z',
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-17T12:00:00Z',
}

const projectDetail: ProjectDetail = {
  ...project,
  proxy_url: `https://depsilo.example/p/${project.slug}`,
  ecosystem_breakdown: { pypi: 20, docker: 12, huggingface: 9 },
}

const projectPackages: ProjectPackagesResponse = {
  items: [{
    ecosystem: 'pypi',
    package_name: 'requests',
    version: '2.32.0',
    first_seen_at: '2026-07-16T12:00:00Z',
    last_seen_at: '2026-07-17T12:00:00Z',
    download_count: 3,
  }],
  total: 41,
  page: 1,
}

const operatorEcosystemIds = [
  'pypi',
  'apt',
  'npm',
  'go',
  'cargo',
  'maven',
  'rubygems',
  'composer',
  'nuget',
  'conda',
  'cran',
  'helm',
  'alpine',
  'docker',
  'huggingface',
] as const

async function mockPopulatedProject(page: Page, summary: ProjectSummary = project) {
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { items: [summary], total: 1, capabilities: proProjectCapabilities } satisfies ProjectListResponse,
    [`GET /api/v1/admin/projects/${summary.id}`]: {
      ...projectDetail,
      ...summary,
      proxy_url: `https://depsilo.example/p/${summary.slug}`,
    } satisfies ProjectDetail,
    [`GET /api/v1/admin/projects/${summary.id}/packages`]: projectPackages,
    'GET /api/v1/admin/compliance/profile': {
      organization: 'Acme GmbH',
      contact: 'security@acme.example',
      signing_configured: true,
      component_annotation_count: 2,
    } satisfies ComplianceProfile,
  })
}

async function expectStableProjectsPage(page: Page) {
  const adminPage = page.locator('[data-admin-page]')
  await expect(adminPage).toHaveCount(1)
  await expect(adminPage).toBeVisible()
  await expect(adminPage).toHaveAttribute('data-admin-page-width', 'fluid')
  await expect(adminPage.locator('[data-admin-page-description]')).toContainText('按项目归集依赖活动')
  await expect(page.locator('h1:visible')).toHaveCount(1)
  await expect(adminPage.locator('[data-admin-page-title]')).toHaveText('项目管理')
  await expect(page.locator('[data-admin-topbar]')).toHaveCount(0)
}

async function expectInViewport(page: Page, locator: Locator) {
  await expect(locator).toBeVisible()
  // A refetch can replace the element between resolving and scrolling it, so
  // the scroll and measurement retry together against the current element.
  await expect(async () => {
    await locator.scrollIntoViewIfNeeded({ timeout: 2_000 })
    const box = await locator.evaluate(element => {
      const rect = element.getBoundingClientRect()
      return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }
    })
    const viewport = page.viewportSize()
    expect(viewport).not.toBeNull()
    expect(box.left).toBeGreaterThanOrEqual(-1)
    expect(box.right).toBeLessThanOrEqual(viewport!.width + 1)
    expect(box.top).toBeGreaterThanOrEqual(-1)
    expect(box.bottom).toBeLessThanOrEqual(viewport!.height + 1)
  }).toPass()
}

async function expectNoRootOverflow(page: Page, width: number) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
}

const initialStates = [
  {
    name: 'failed',
    response: { status: 500, body: { code: 'PROJECTS_UNAVAILABLE', message: 'fixture projects failure' } },
    content: /fixture projects failure/,
  },
  {
    name: 'empty',
    response: { items: [], total: 0, capabilities: freeProjectCapabilities } satisfies ProjectListResponse,
    content: /^暂无项目$/,
  },
] as const

for (const state of initialStates) {
  test(`Projects keeps stable page chrome in the ${state.name} state`, async ({ page }) => {
    await mockAdminApi(page, { 'GET /api/v1/admin/projects': state.response })
    await page.goto('/admin/projects')

    await expectStableProjectsPage(page)
    await expect(page.getByText(state.content)).toBeVisible()
  })
}

test('project creation belongs only to list page actions and disappears in detail', async ({ page }) => {
  await mockPopulatedProject(page)
  await page.goto('/admin/projects')

  await expectStableProjectsPage(page)
  const createProject = page.getByRole('button', { name: '创建项目', exact: true })
  await expect(createProject).toHaveCount(1)
  await expect(page.locator('[data-admin-page-actions]').getByRole('button', { name: '创建项目', exact: true })).toHaveCount(1)
  await expect(page.locator('[data-admin-page-content]').getByRole('button', { name: '创建项目', exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: `查看 ${projectName}`, exact: true }).click()

  await expectStableProjectsPage(page)
  await expect(page.getByRole('heading', { name: projectName, exact: true })).toBeVisible()
  await expect(page.locator('[data-admin-page-actions]')).toHaveCount(0)
  await expect(createProject).toHaveCount(0)
})

test('free users create their first project and deletion restores the allowance', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 })
  let items: ProjectSummary[] = []
  let creations = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': () => ({ items, total: items.length, capabilities: { ...freeProjectCapabilities, can_create: items.length === 0 } }),
    'POST /api/v1/admin/projects': (request: Request) => {
      creations++
      expect(request.postDataJSON()).toEqual({ name: projectName, description: '' })
      items = [project]
      return { ...project, token: 'fixture-first-project-token', proxy_url: `/p/${project.slug}` }
    },
    'DELETE /api/v1/admin/projects/7': () => { items = []; return { status: 'deleted' } },
  })
  await page.goto('/admin/projects')
  await expect(page.locator('[data-project-capacity]')).toHaveText('免费版包含 1 个项目 · 已使用 0 个')
  await page.getByRole('button', { name: '创建项目', exact: true }).click()
  await page.getByRole('dialog').getByLabel('项目名称', { exact: true }).fill(projectName)
  await page.getByRole('dialog').getByRole('button', { name: '保存', exact: true }).click()
  const tokenDialog = page.getByRole('dialog', { name: '项目 Token' })
  await expect(tokenDialog).toContainText('fixture-first-project-token')
  await tokenDialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByRole('button', { name: '创建项目', exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '管理 Pro 授权', exact: true })).toHaveAttribute('href', '/admin/license')
  await expect(page.locator('[data-project-capacity]')).toContainText('已使用 1 个')
  await expectNoRootOverflow(page, 320)
  await page.getByRole('button', { name: `删除 ${projectName}`, exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('button', { name: '创建项目', exact: true })).toBeEnabled()
  await expect(page.locator('[data-project-capacity]')).toHaveText('免费版包含 1 个项目 · 已使用 0 个')
  expect(creations).toBe(1)
})

for (const count of [1, 2]) test(`free instances retain ${count} existing project(s), with SBOM clearly Pro`, async ({ page }) => {
  await mockPopulatedProject(page)
  let exports = 0
  let complianceRequests = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': {
      items: Array.from({ length: count }, (_, index) => ({ ...project, id: 7 + index, name: index ? 'retained-project' : projectName })),
      total: count,
      capabilities: { ...freeProjectCapabilities, can_create: false },
    },
    'GET /api/v1/admin/projects/7/sbom': () => { exports++; return {} },
    'GET /api/v1/admin/compliance/profile': () => { complianceRequests++; return {} },
  })
  await page.goto('/admin/projects')
  await expect(page.getByRole('button', { name: /^查看 / })).toHaveCount(count)
  await expect(page.getByRole('button', { name: /^删除 / })).toHaveCount(count)
  await expect(page.getByRole('button', { name: '创建项目', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: `查看 ${projectName}`, exact: true }).click()
  await expect(page.getByRole('button', { name: '编辑项目', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: '重新生成 Token', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '导出 SBOM Pro', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '导出 SBOM', exact: true })
  await expect(dialog).toContainText('项目级 SBOM 导出（含 CRA 模式与技术文件）需要 Pro')
  await expect(dialog.getByRole('link', { name: '管理 Pro 授权', exact: true })).toHaveAttribute('href', '/admin/license')
  await expect(dialog.getByRole('button', { name: '下载 SBOM' })).toHaveCount(0)
  expect(exports).toBe(0)
  expect(complianceRequests).toBe(0)
})

test('Pro instances can add projects beyond the free allowance', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { items: [project, { ...project, id: 8, name: 'second-project' }], total: 2, capabilities: proProjectCapabilities },
  })
  await page.goto('/admin/projects')
  await expect(page.locator('[data-project-capacity]')).toHaveCount(0)
  await page.getByRole('button', { name: 'Create Project', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
  await expect(page.getByRole('dialog').getByRole('link', { name: 'Manage Pro access', exact: true })).toHaveCount(0)
})

test('a concurrent creation limit preserves the draft and offers Pro without hiding projects', async ({ page }) => {
  let occupied = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': () => ({ items: occupied ? [project] : [], total: occupied ? 1 : 0, capabilities: { ...freeProjectCapabilities, can_create: !occupied } }),
    'POST /api/v1/admin/projects': () => {
      occupied = true
      return { status: 402, body: { code: 'PRO_REQUIRED', message: 'raw backend entitlement detail', trial_available: true } }
    },
  })
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: '创建项目', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('项目名称', { exact: true }).fill('my-project-draft')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('创建更多项目需要 Pro')
  await expect(dialog.getByLabel('项目名称', { exact: true })).toHaveValue('my-project-draft')
  await expect(dialog.getByRole('link', { name: '管理 Pro 授权', exact: true })).toHaveAttribute('href', '/admin/license')
  await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  await expect(dialog).not.toContainText('raw backend entitlement detail')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('button', { name: `查看 ${projectName}`, exact: true })).toBeVisible()
})

test('a readonly free user sees project details without write or system-management actions', async ({ page }) => {
  await mockPopulatedProject(page)
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { id: 2, username: 'viewer', role: 'readonly', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: false },
    'GET /api/v1/admin/projects': { items: [project], total: 1, capabilities: { ...freeProjectCapabilities, can_create: false } },
  })
  await page.goto('/admin/projects')
  await expect(page.getByRole('button', { name: '创建项目', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^删除 / })).toHaveCount(0)
  await page.getByRole('button', { name: `查看 ${projectName}`, exact: true }).click()
  await expect(page.getByRole('button', { name: '编辑项目', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新生成 Token', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '导出 SBOM Pro', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('请联系管理员启用 Pro')
  await expect(page.getByRole('link', { name: '管理 Pro 授权', exact: true })).toHaveCount(0)
})

test('loading and missing capabilities do not invent a free or Pro allowance', async ({ page }) => {
  let release: () => void = () => {}
  const pending = new Promise<void>(resolve => { release = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': async () => { await pending; return { items: [project], total: 1 } },
  })
  await page.goto('/admin/projects')
  try {
    await expect(page.locator('[data-admin-page-content] [aria-busy="true"]')).toBeVisible()
    await expect(page.getByRole('button', { name: '创建项目', exact: true })).toHaveCount(0)
    await expect(page.locator('[data-project-capacity]')).toHaveCount(0)
  } finally { release() }
  await expect(page.getByRole('alert')).toContainText('项目额度暂不可用')
  await expect(page.getByRole('button', { name: '创建项目', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: `查看 ${projectName}`, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '管理 Pro 授权', exact: true })).toHaveCount(0)
  await expect(page.locator('[data-project-capacity]')).toHaveCount(0)
})

test('an expired entitlement during SBOM export shows an actionable Pro explanation', async ({ page }) => {
  await mockPopulatedProject(page)
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects/7/sbom': { status: 402, body: { code: 'PRO_REQUIRED', message: 'fixture expired' } },
  })
  await page.goto('/admin/projects?project=7')
  await page.getByRole('button', { name: '导出 SBOM', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '下载 SBOM', exact: true }).click()
  await expect(dialog).toContainText('项目级 SBOM 导出（含 CRA 模式与技术文件）需要 Pro')
  await expect(dialog.getByRole('link', { name: '管理 Pro 授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '下载 SBOM', exact: true })).toHaveCount(0)
})

test('project ecosystem filters expose all Operator surfaces without internal aliases', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockPopulatedProject(page)
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: `View ${projectName}`, exact: true }).click()

  await page.getByRole('button', { name: 'Export SBOM', exact: true }).click()
  const ecosystemFilters = page.getByLabel('Filter Ecosystem', { exact: true })
  await expect(ecosystemFilters).toHaveCount(2)

  for (const filter of await ecosystemFilters.all()) {
    const options = await filter.locator('option').evaluateAll(elements => elements.map(element => ({
      value: (element as HTMLOptionElement).value,
      label: element.textContent?.trim() ?? '',
    })))
    expect(options.map(option => option.value).filter(Boolean)).toEqual(operatorEcosystemIds)
    expect(options).toContainEqual({ value: 'docker', label: 'Docker' })
    expect(options).toContainEqual({ value: 'huggingface', label: 'Hugging Face' })
    for (const internalAlias of ['pip', 'goproxy', 'crates']) {
      expect(options.map(option => option.value)).not.toContain(internalAlias)
      expect(options.map(option => option.label.toLowerCase())).not.toContain(internalAlias)
    }
  }
})

for (const locale of [
  { id: 'en', expected: '2d ago', forbidden: '2 天前' },
  { id: 'zh', expected: '2 天前', forbidden: '2d ago' },
] as const) {
  test(`project activity time stays in the ${locale.id} UI language`, async ({ page }) => {
    await page.clock.install({ time: new Date('2026-07-19T12:00:00Z') })
    await setUiPreferences(page, 'light', locale.id)
    await mockPopulatedProject(page)
    await page.goto('/admin/projects')

    const projectsTable = page.getByRole('region', { name: locale.id === 'en' ? 'Projects table' : '项目表格' })
    await expect(projectsTable.getByText(locale.expected, { exact: true })).toBeVisible()
    await expect(projectsTable.getByText(locale.forbidden, { exact: true })).toHaveCount(0)
  })
}

test('long project detail, SBOM controls, package filter, and pagination remain operable at 320px', async ({ page }) => {
  const longName = 'criticalplatformdependencyobservabilityandsbomworkspacewithoutanynaturalbreakpoints'
  const longProject: ProjectSummary = {
    ...project,
    name: longName,
    slug: longName,
  }
  await page.setViewportSize({ width: 320, height: 844 })
  await mockPopulatedProject(page, longProject)
  await page.goto('/admin/projects')
  await expectNoRootOverflow(page, 320)

  await page.getByRole('button', { name: `查看 ${longName}`, exact: true }).click()

  await expectInViewport(page, page.getByRole('heading', { name: longName, exact: true }))
  await expectNoRootOverflow(page, 320)

  await page.getByRole('button', { name: '导出 SBOM', exact: true }).click()
  const sbomControls = page.locator('[data-project-sbom-controls]')
  await expectInViewport(page, sbomControls.getByLabel('格式'))
  await expectInViewport(page, sbomControls.getByLabel('筛选生态'))
  await expectInViewport(page, sbomControls.getByRole('button', { name: '下载 SBOM' }))
  await expectNoRootOverflow(page, 320)

  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const packageFilter = page.getByLabel('筛选生态', { exact: true })
  await expectInViewport(page, packageFilter)
  // Filtering refetches the package page; the pagination remounts once it lands.
  const filteredPackages = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === `/api/v1/admin/projects/${longProject.id}/packages` && url.searchParams.get('ecosystem') === 'docker'
  })
  await packageFilter.selectOption('docker')
  await filteredPackages

  const pagination = page.getByRole('navigation', { name: '分页' })
  await expectInViewport(page, pagination)
  await pagination.getByRole('button', { name: '下一页' }).click()
  await expect(pagination.getByText('第 2 / 3 页', { exact: true })).toBeVisible()
  await expectNoRootOverflow(page, 320)
})

test('SBOM export requests the CRA preset and saves the detached signature', async ({ page }) => {
  await mockPopulatedProject(page)
  const sbomQueries: string[] = []
  await page.route('**/api/v1/admin/projects/7/sbom*', async route => {
    const url = new URL(route.request().url())
    sbomQueries.push(url.search)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: {
        'x-depsilo-sbom-signature-algorithm': 'ed25519',
        'x-depsilo-sbom-signature': 'c2lnbmF0dXJl',
        'x-depsilo-sbom-signature-publickey': 'cHVibGljLWtleQ==',
        'x-depsilo-sbom-signature-payloadsha256': 'a'.repeat(64),
      },
      body: JSON.stringify({
        sbom: { spdxVersion: 'SPDX-2.3', packages: [] },
        technical_file: { format: 'depsilo/cra-technical-file/v1' },
      }),
    })
  })

  await page.goto('/admin/projects')
  await page.getByRole('button', { name: `查看 ${projectName}`, exact: true }).click()
  await page.getByRole('button', { name: '导出 SBOM', exact: true }).click()
  await page.getByRole('checkbox', { name: /CRA 技术文件/ }).check()
  await page.getByRole('checkbox', { name: /签名（Ed25519）/ }).check()

  const downloads: string[] = []
  page.on('download', download => {
    downloads.push(download.suggestedFilename())
  })
  await page.getByRole('button', { name: '下载 SBOM' }).click()

  await expect.poll(() => sbomQueries.length).toBe(1)
  expect(sbomQueries[0]).toContain('preset=technical-file')
  expect(sbomQueries[0]).toContain('sign=true')
  await expect.poll(() => downloads.length).toBe(2)
  expect(downloads).toContain(`${projectName}-cra-technical-file.json`)
  expect(downloads).toContain(`${projectName}-cra-technical-file.json.sig.json`)
})
