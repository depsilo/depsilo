import type { Locator, Page } from '@playwright/test'
import type {
  ProjectDetail,
  ProjectListResponse,
  ProjectPackagesResponse,
  ProjectSummary,
} from '../src/lib/adminApi.types'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

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
    'GET /api/v1/admin/projects': { items: [summary], total: 1 } satisfies ProjectListResponse,
    [`GET /api/v1/admin/projects/${summary.id}`]: {
      ...projectDetail,
      ...summary,
      proxy_url: `https://depsilo.example/p/${summary.slug}`,
    } satisfies ProjectDetail,
    [`GET /api/v1/admin/projects/${summary.id}/packages`]: projectPackages,
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
  await expect(page.locator('[data-admin-topbar]').getByRole('heading')).toHaveCount(0)
}

async function expectInViewport(page: Page, locator: Locator) {
  await expect(locator).toBeVisible()
  await locator.scrollIntoViewIfNeeded()
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
}

async function expectNoRootOverflow(page: Page, width: number) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
}

const initialStates = [
  {
    name: 'Pro-required',
    response: { status: 402, body: { code: 'PRO_REQUIRED', message: 'Pro required' } },
    content: /多项目工作区是 Depsilo Pro 功能/,
  },
  {
    name: 'failed',
    response: { status: 500, body: { code: 'PROJECTS_UNAVAILABLE', message: 'fixture projects failure' } },
    content: /fixture projects failure/,
  },
  {
    name: 'empty',
    response: { items: [], total: 0 } satisfies ProjectListResponse,
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

test('project ecosystem filters expose all Operator surfaces without internal aliases', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockPopulatedProject(page)
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: `View ${projectName}`, exact: true }).click()

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

  const sbomControls = page.locator('[data-project-sbom-controls]')
  await expectInViewport(page, sbomControls.getByLabel('格式'))
  await expectInViewport(page, sbomControls.getByLabel('筛选生态'))
  await expectInViewport(page, sbomControls.getByRole('button', { name: '下载 SBOM' }))
  await expectNoRootOverflow(page, 320)

  const packageFilter = page.getByLabel('筛选生态', { exact: true }).nth(1)
  await expectInViewport(page, packageFilter)
  await packageFilter.selectOption('docker')

  const pagination = page.getByRole('navigation', { name: '分页' })
  await expectInViewport(page, pagination)
  await pagination.getByRole('button', { name: '下一页' }).click()
  await expect(pagination.getByText('第 2 / 3 页', { exact: true })).toBeVisible()
  await expectNoRootOverflow(page, 320)
})
