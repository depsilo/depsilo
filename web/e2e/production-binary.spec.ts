import { expect, test } from '@playwright/test'

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
