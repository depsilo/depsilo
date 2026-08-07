import { expect, test } from '@playwright/test'

test('Portal mounts and language switching works when Web Storage is blocked', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', error => pageErrors.push(error.message))

  await page.addInitScript(() => {
    const blocked = () => { throw new DOMException('blocked storage', 'SecurityError') }
    Object.defineProperties(Storage.prototype, {
      getItem: { configurable: true, value: blocked },
      setItem: { configurable: true, value: blocked },
      removeItem: { configurable: true, value: blocked },
    })
  })
  await page.route('**/api/v1/stats', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      service: { status: 'healthy' },
      week: { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_saved: 0 },
      upstreams: [],
    }),
  }))

  await page.goto('/')

  await expect(page.locator('#root')).not.toBeEmpty()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')

  await page.locator('[data-language-toggle="portal"]').click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')

  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect(page.locator('#root')).not.toBeEmpty()
  expect(pageErrors).toEqual([])
})
