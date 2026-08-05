import type { Page, Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const paidStatus = {
  is_pro: true,
  source: 'paid',
  days_left: 0,
  trial_used: false,
  trial_available: false,
  last_checked: '2026-07-10T00:00:00Z',
  license_key_masked: 'depsilo-old-***',
} as const

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

async function openReplacementEditor(page: Page) {
  await page.getByRole('button', { name: /许可证密钥|License key/ }).click()
  await page.getByRole('button', { name: /更换密钥|Change key/ }).click()
  return page.getByLabel(/许可证密钥|License key/)
}

test('paid license can enter replacement mode and submit a new key', async ({ page }) => {
  const response = deferred<void>()
  let submittedKey = ''
  await mockAdminApi(page, {
    'GET /api/v1/admin/license/status': paidStatus,
    'PUT /api/v1/admin/license/key': async (request: Request) => {
      submittedKey = (request.postDataJSON() as { key: string }).key
      await response.promise
      return {
        ...paidStatus,
        license_key_masked: 'depsilo-new-***',
        last_checked: '2026-07-11T00:00:00Z',
      }
    },
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/license')
  const input = await openReplacementEditor(page)
  await expect(input).toBeVisible()
  await input.fill('  depsilo-replacement-key  ')

  const save = page.getByRole('button', { name: /^保存$|^Save$/ })
  await save.click()
  await expect(save).toHaveAttribute('aria-busy', 'true')
  await expect(save).toBeDisabled()
  await expect(page.getByRole('button', { name: /^取消$|^Cancel$/ })).toBeDisabled()
  await expect.poll(() => submittedKey).toBe('depsilo-replacement-key')

  response.resolve()
  await expect(page.getByText('depsilo-new-***')).toBeVisible()
  await expect(input).toHaveCount(0)
  await expect(page.getByRole('button', { name: /更换密钥|Change key/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
})

test('failed paid license replacement preserves the editor and entered key', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/license/status': paidStatus,
    'PUT /api/v1/admin/license/key': {
      status: 422,
      body: { code: 'INVALID_LICENSE', message: 'fixture rejected replacement' },
    },
  })

  await page.goto('/admin/license')
  const input = await openReplacementEditor(page)
  await input.fill('depsilo-invalid-replacement')
  await page.getByRole('button', { name: /^保存$|^Save$/ }).click()

  await expect(page.getByRole('alert')).toContainText('fixture rejected replacement')
  await expect(input).toHaveValue('depsilo-invalid-replacement')
  await expect(page.getByRole('button', { name: /^保存$|^Save$/ })).toBeEnabled()
  await expect(page.getByRole('button', { name: /^取消$|^Cancel$/ })).toBeEnabled()
})
