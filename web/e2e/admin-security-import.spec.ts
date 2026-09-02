import { expect, mockAdminApi, test } from './fixtures/admin-api'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

async function openSecurityImport(page: import('@playwright/test').Page) {
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /策略|Policies/ }).click()
  return page.locator('[data-security-import-dropzone]')
}

test('security import reports exact results and rejects duplicate interaction while pending', async ({ page }) => {
  const releaseImport = deferred<void>()
  let importCalls = 0
  let policyCalls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': () => {
      policyCalls += 1
      return []
    },
    'POST /api/v1/admin/security/import': async () => {
      importCalls += 1
      await releaseImport.promise
      return { imported: 3, received: 5, packages: 2, duplicates: 1, skipped: 1, rules_created: 0 }
    },
  })

  const dropzone = await openSecurityImport(page)
  const input = dropzone.locator('input[type="file"]')
  await expect(input).toHaveAttribute('accept', '.json,application/json')
  await input.setInputFiles({
    name: 'advisories.json',
    mimeType: 'application/json',
    buffer: Buffer.from('[{"id":"OSV-FIXTURE"}]'),
  })

  await expect.poll(() => importCalls).toBe(1)
  const uploadButton = dropzone.locator('button')
  await expect(dropzone).toHaveAttribute('aria-busy', 'true')
  await expect(uploadButton).toBeDisabled()

  await page.evaluate(() => {
    const target = document.querySelector<HTMLElement>('[data-security-import-dropzone]')
    if (!target) throw new Error('security import dropzone not found')
    const transfer = new DataTransfer()
    transfer.items.add(new File(['[]'], 'duplicate.json', { type: 'application/json' }))
    target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }))
    target.querySelector<HTMLButtonElement>('button')?.click()
  })
  await expect.poll(() => importCalls).toBe(1)

  releaseImport.resolve()
  await expect(page.getByText(/已导入 3 条漏洞|3 vulnerabilities imported/)).toBeVisible()
  await expect(page.getByText(/^接收 5 · 涉及包 2 · 重复 1 · 跳过 1$|^Received 5 · Packages 2 · Duplicates 1 · Skipped 1$/)).toBeVisible()
  await expect(page.getByText(/新建规则|Rules created/)).toHaveCount(0)
  await expect.poll(() => policyCalls).toBeGreaterThanOrEqual(2)
})

test('security import shows the server-safe failure message', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [],
    'POST /api/v1/admin/security/import': {
      status: 400,
      body: { code: 'INVALID_IMPORT', message: 'only valid OSV JSON can be imported' },
    },
  })

  const dropzone = await openSecurityImport(page)
  await dropzone.locator('input[type="file"]').setInputFiles({
    name: 'invalid.json',
    mimeType: 'application/json',
    buffer: Buffer.from('{invalid'),
  })

  await expect(page.getByRole('alert')).toContainText('only valid OSV JSON can be imported')
  await expect(page.getByText(/已导入 \d+ 条漏洞|\d+ vulnerabilities imported/)).toHaveCount(0)
})
