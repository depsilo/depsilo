import { expect, test } from '@playwright/test'
import type { ProjectListResponse } from '../src/lib/adminApi.types'

test('Go binary serves and executes its embedded production frontend', async ({ page, request }) => {
  const document = await page.goto('/', { waitUntil: 'domcontentloaded' })
  expect(document?.status()).toBe(200)

  const html = await document?.text() ?? ''
  expect(html).not.toContain('/@vite/client')
  expect(html).toMatch(/\/assets\/[^"']+\.js/)

  await expect(page).toHaveTitle(/Depsilo/i)
  await expect(page.locator('#root')).not.toBeEmpty()

  const health = await request.get('/health')
  expect(health.status()).toBe(200)
  expect((await health.json()).status).toBe('healthy')

  const browserRoute = await page.goto('/monitor', { waitUntil: 'domcontentloaded' })
  expect(browserRoute?.status()).toBe(200)
  expect(await browserRoute?.text()).not.toContain('/@vite/client')

  const missingMachineRoute = await request.get('/api/v1/production-smoke-missing', {
    headers: { Accept: 'text/html' },
  })
  expect(missingMachineRoute.status()).toBe(404)
  expect(missingMachineRoute.headers()['content-type']).toContain('text/plain')
})

test('free project creation works through the embedded UI and enforces capacity on the server', async ({ page, request }, testInfo) => {
  // These credentials belong only to run-production-ui-smoke.sh's disposable instance.
  await page.addInitScript(() => {
    localStorage.setItem('lang', 'en')
    localStorage.setItem('depsilo-theme', 'dark')
  })
  await page.goto('/admin/projects')
  await page.getByPlaceholder('Enter username').fill('production-smoke')
  await page.getByPlaceholder('Enter password').fill('Correct-Horse&Battery-47')
  await page.getByRole('button', { name: 'Sign In', exact: true }).click()
  await page.waitForURL(url => url.pathname !== '/admin/login')
  await page.goto('/admin/projects')
  const create = page.getByRole('button', { name: 'Create Project', exact: true })
  await expect(create).toBeEnabled()

  const headers = { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('token'))}` }
  const before = await request.get('/api/v1/admin/projects', { headers })
  expect(before.status()).toBe(200)
  expect((await before.json()).capabilities).toEqual({ project_limit: 1, can_create: true, can_export_sbom: false })

  await create.click()
  const form = page.getByRole('dialog', { name: 'Create Project', exact: true })
  await form.getByPlaceholder('e.g. ai-platform').fill('production-first-project')
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'OK', exact: true }).click()
  await expect(page.getByRole('button', { name: 'View production-first-project', exact: true })).toBeVisible()
  await expect(create).toHaveCount(0)

  const list: ProjectListResponse = await (await request.get('/api/v1/admin/projects', { headers })).json()
  expect(list.total).toBe(1)
  expect(list.capabilities).toEqual({ project_limit: 1, can_create: false, can_export_sbom: false })
  const project = list.items[0]
  try {
    const second = await request.post('/api/v1/admin/projects', { headers, data: { name: 'production-second-project' } })
    expect(second.status()).toBe(402)
    expect((await second.json()).code).toBe('PRO_REQUIRED')
    expect((await request.get(`/api/v1/admin/projects/${project.id}/sbom`, { headers })).status()).toBe(402)
    for (const width of [1440, 320]) {
      await page.setViewportSize({ width, height: 900 })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`free-project-${width}.png`), fullPage: true, animations: 'disabled' })
    }
    await page.getByRole('button', { name: 'View production-first-project', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'production-first-project', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Regenerate Token', exact: true })).toBeEnabled()
    expect((await request.get(`/api/v1/admin/projects/${project.id}/packages`, { headers })).status()).toBe(200)
  } finally {
    expect((await request.delete(`/api/v1/admin/projects/${project.id}`, { headers })).status()).toBe(200)
  }
  await page.goto('/admin/projects')
  await expect(create).toBeEnabled()
})
