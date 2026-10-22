import type { Request } from '@playwright/test'
import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

const project = { id: 7, name: 'homelab', slug: 'homelab', description: '', package_count: 21, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', last_activity_at: '2026-10-21T00:00:00Z' }
const projectMocks = {
  'GET /api/v1/admin/projects': { items: [project], total: 1 },
  'GET /api/v1/admin/projects/7': { ...project, proxy_url: 'https://wrong-origin.example/p/homelab', ecosystem_breakdown: { pypi: 21 } },
  'GET /api/v1/admin/projects/7/packages': { items: [], total: 21, page: 2 },
}

test('project details and package filters survive reload, and editing preserves its address', async ({ page }) => {
  let edited: unknown
  await mockAdminApi(page, { ...projectMocks, 'PUT /api/v1/admin/projects/7': (request: Request) => { edited = request.postDataJSON(); return { ...project, ...request.postDataJSON() } } })
  await page.goto('/admin/projects?project=7&ecosystem=pypi&page=2')
  await expect(page.getByRole('heading', { name: 'homelab', exact: true })).toBeVisible()
  await expect(page.getByLabel('筛选生态', { exact: true })).toHaveValue('pypi')
  await expect(page.locator('[data-project-proxy-value]')).toHaveText(`${new URL(page.url()).origin}/p/homelab`)
  await page.reload()
  await expect(page.getByRole('heading', { name: 'homelab', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: '分页' })).toContainText('第 2 / 2 页')
  await page.getByRole('button', { name: '编辑项目', exact: true }).click()
  await page.getByRole('dialog').getByLabel('项目名称', { exact: true }).fill('homelab-new')
  await page.getByRole('dialog').getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => edited).toEqual({ name: 'homelab-new', description: '' })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page).toHaveURL(/project=7/)
})

test('project token rotation explains attribution impact and reveals the replacement only once', async ({ page }) => {
  let rotations = 0
  await mockAdminApi(page, { ...projectMocks, 'POST /api/v1/admin/projects/7/token': () => { rotations++; return { token: 'depsilo_proj_fixture_new', proxy_url: 'https://wrong-origin.example/p/homelab' } } })
  await page.goto('/admin/projects?project=7')
  await page.getByRole('button', { name: '重新生成 Token', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('旧 Token 将立即停止归集到此项目')
  await page.getByRole('dialog').getByRole('button', { name: '重新生成 Token', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('depsilo_proj_fixture_new')
  expect(rotations).toBe(1)
  await page.getByRole('dialog').getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('depsilo_proj_fixture_new')).toHaveCount(0)
})

test('failed compliance data cannot masquerade as unconfigured signing', async ({ page }) => {
  let available = false
  await mockAdminApi(page, { ...projectMocks, 'GET /api/v1/admin/compliance/profile': () => available ? { organization: 'Homelab', signing_configured: true, component_annotation_count: 0 } : { status: 503, body: { message: 'fixture unavailable' } } })
  await page.goto('/admin/projects?project=7')
  await expect(page.getByRole('button', { name: '导出 SBOM', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '导出 SBOM', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/合规配置暂不可用/)).toBeVisible()
  await expect(dialog.getByText(/未配置签名密钥/)).toHaveCount(0)
  await expect(dialog.getByRole('checkbox', { name: /签名/ })).toBeDisabled()
  available = true
  await dialog.getByRole('button', { name: '重试', exact: true }).click()
  await expect(dialog.getByRole('checkbox', { name: /签名/ })).toBeEnabled()
})

test('failed access details offer retry and preserve request identity', async ({ page }) => {
  let available = false
  const log = { id: 17, adapter_type: 'pypi', package_name: 'six', method: 'GET', cache_key: 'pypi/six.whl', hit: true, cache_result: 'hit', upstream: 'official', latency_ms: 1400, status_code: 503, client_ip: '127.0.0.1', bytes_sent: 0, created_at: '2026-10-21T00:00:00Z' }
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': { items: [log], total: 1, page: 1, page_size: 50 },
    'GET /api/v1/admin/logs/17': () => available ? { ...log, request_id: 'fixture-17', cache_reason: 'fresh', policy_decision: 'allow', policy_reason: '', delivery_result: 'failed', delivery_reason: 'fixture', audit_events: [] } : { status: 503, body: { message: 'fixture request unavailable' } },
  })
  await page.goto('/admin/logs?detail=17')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('fixture request unavailable')).toBeVisible()
  await expect(dialog.getByRole('heading', { name: /正在加载/ })).toHaveCount(0)
  available = true
  await dialog.getByRole('button', { name: '重试', exact: true }).click()
  await expect(dialog.getByText('fixture-17')).toBeVisible()
  await expect(page).toHaveURL(/detail=17/)
  await dialog.getByRole('button', { name: '关闭', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: 'six' })
  await expect(row).toContainText('HIT')
  await expect(row).toContainText('503')
})

for (const width of [1440, 320]) test(`standard form controls retain readable font and touch targets at ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  await setUiPreferences(page, width === 320 ? 'light' : 'dark', 'zh')
  await mockAdminApi(page, projectMocks)
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: '创建项目', exact: true }).click()
  const name = page.getByRole('dialog').getByLabel('项目名称', { exact: true })
  await expect(name).toHaveCSS('font-size', width === 320 ? '16px' : '14px')
  await expect.poll(async () => (await name.boundingBox())!.height).toBeGreaterThanOrEqual(40)
  await name.fill('keyboard-project')
  await page.keyboard.press('Tab')
  await expect(page.getByRole('dialog').getByLabel('描述', { exact: true })).toBeFocused()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
})

test('a lost signing capability still lets the operator choose an unsigned export', async ({ page }) => {
  const now = Date.now()
  await page.clock.install({ time: now })
  let signingConfigured = true
  await mockAdminApi(page, { ...projectMocks, 'GET /api/v1/admin/compliance/profile': () => ({ organization: 'Homelab', signing_configured: signingConfigured, component_annotation_count: 0 }) })
  await page.goto('/admin/projects?project=7')
  const openExport = page.getByRole('button', { name: '导出 SBOM', exact: true })
  await openExport.click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('checkbox', { name: /签名/ }).check()
  await dialog.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  signingConfigured = false
  await page.clock.setSystemTime(now + 6 * 60 * 1000)
  await openExport.click()
  await expect(dialog.getByText(/未配置签名密钥/)).toBeVisible()
  await expect(dialog.getByRole('button', { name: '下载 SBOM', exact: true })).toBeDisabled()
  await dialog.getByRole('checkbox', { name: /签名/ }).uncheck()
  await expect(dialog.getByRole('button', { name: '下载 SBOM', exact: true })).toBeEnabled()
})

test('Docker request details do not link into the ordinary upstream CRUD', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/logs/17': { id: 17, adapter_type: 'docker', package_name: 'library/alpine', request_id: 'fixture-docker', cache_result: 'hit', cache_key: 'docker/alpine', policy_decision: 'allow', delivery_result: 'completed', created_at: '2026-10-22T00:00:00Z', audit_events: [] } })
  await page.goto('/admin/logs?detail=17')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('fixture-docker')).toBeVisible()
  await expect(dialog.getByRole('link', { name: '查看对应生态上游' })).toHaveCount(0)
  await expect(dialog.getByRole('link', { name: '查看相关审计' })).toBeVisible()
})
