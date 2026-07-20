import { expect, mockAdminApi, test } from './fixtures/admin-api'

const originalAlpha = {
  id: 1,
  adapter_type: 'pypi',
  name: 'alpha',
  url: 'https://alpha.example/simple',
  proxy: '',
  priority: 1,
  probe_mode: 'active' as const,
  probe_interval: '30m',
  healthy: true,
  avg_latency_ms: 20,
  success_rate: 1,
  last_checked_at: null,
  worker_running: true,
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
}

const originalBeta = {
  ...originalAlpha,
  id: 2,
  name: 'beta',
  url: 'https://beta.example/simple',
}

test('late batch checks cannot roll back an edit or resurrect a deletion', async ({ page }) => {
  let startedChecks = 0
  let markChecksStarted: (() => void) | undefined
  let releaseChecks: (() => void) | undefined
  const checksStarted = new Promise<void>(resolve => { markChecksStarted = resolve })
  const checksReleased = new Promise<void>(resolve => { releaseChecks = resolve })

  const delayedCheck = (upstream: typeof originalAlpha) => async () => {
    startedChecks += 1
    if (startedChecks === 2) markChecksStarted?.()
    await checksReleased
    return {
      upstream: { ...upstream, healthy: false, avg_latency_ms: 900, last_checked_at: '2026-07-10T00:01:00Z' },
      check: { healthy: false, latency_ms: 900, checked_at: '2026-07-10T00:01:00Z', error: 'timeout' },
    }
  }

  const editedAlpha = {
    ...originalAlpha,
    name: 'alpha-edited',
    url: 'https://edited.example/simple',
    updated_at: '2026-07-10T00:02:00Z',
  }

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': { items: [originalAlpha, originalBeta], total: 2 },
    'POST /api/v1/admin/upstreams/1/check': delayedCheck(originalAlpha),
    'POST /api/v1/admin/upstreams/2/check': delayedCheck(originalBeta),
    'PUT /api/v1/admin/upstreams/1': editedAlpha,
    'DELETE /api/v1/admin/upstreams/2': { deleted_id: 2, adapter_type: 'pypi' },
  })

  await page.goto('/admin/upstreams')
  await expect(page.getByText('alpha', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '全部检测' }).click()
  await checksStarted

  const alphaActions = page.getByText('alpha', { exact: true }).locator('..').locator('button')
  await alphaActions.nth(1).click()
  await page.getByLabel('名称').fill('alpha-edited')
  await page.getByLabel('URL').fill('https://edited.example/simple')
  await page.getByRole('button', { name: '保存' }).click()
  await expect(page.getByText('alpha-edited', { exact: true })).toBeVisible()

  const betaActions = page.getByText('beta', { exact: true }).locator('..').locator('button')
  await betaActions.nth(2).click()
  await page.getByRole('button', { name: '删除' }).click()
  await expect(page.getByText('beta', { exact: true })).toHaveCount(0)

  releaseChecks?.()
  await expect(page.getByRole('button', { name: '全部检测' })).toBeEnabled()
  await expect(page.getByText('alpha-edited', { exact: true })).toBeVisible()
  await expect(page.getByText('alpha', { exact: true })).toHaveCount(0)
  await expect(page.getByText('beta', { exact: true })).toHaveCount(0)
})

test('a late single check cannot overwrite an edit', async ({ page }) => {
  let markCheckStarted: (() => void) | undefined
  let releaseCheck: (() => void) | undefined
  const checkStarted = new Promise<void>(resolve => { markCheckStarted = resolve })
  const checkReleased = new Promise<void>(resolve => { releaseCheck = resolve })
  const editedAlpha = {
    ...originalAlpha,
    name: 'alpha-edited',
    url: 'https://edited.example/simple',
    updated_at: '2026-07-10T00:02:00Z',
  }

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': { items: [originalAlpha], total: 1 },
    'POST /api/v1/admin/upstreams/1/check': async () => {
      markCheckStarted?.()
      await checkReleased
      return {
        upstream: { ...originalAlpha, healthy: false, avg_latency_ms: 900 },
        check: { healthy: false, latency_ms: 900, checked_at: '2026-07-10T00:01:00Z', error: 'timeout' },
      }
    },
    'PUT /api/v1/admin/upstreams/1': editedAlpha,
  })

  await page.goto('/admin/upstreams')
  const alphaActions = page.getByText('alpha', { exact: true }).locator('..').locator('button')
  await alphaActions.nth(0).click()
  await checkStarted

  await alphaActions.nth(1).click()
  await page.getByLabel('名称').fill('alpha-edited')
  await page.getByLabel('URL').fill('https://edited.example/simple')
  await page.getByRole('button', { name: '保存' }).click()
  await expect(page.getByText('alpha-edited', { exact: true })).toBeVisible()

  const checkResponse = page.waitForResponse(response => response.url().endsWith('/api/v1/admin/upstreams/1/check'))
  releaseCheck?.()
  await checkResponse
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
  await expect(page.getByText('alpha-edited', { exact: true })).toBeVisible()
  await expect(page.getByText('alpha', { exact: true })).toHaveCount(0)
})
