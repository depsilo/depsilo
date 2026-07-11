import { test, expect } from './fixtures/admin-api'

test('login fields are addressable by their visible labels', async ({ page }) => {
  await page.goto('/admin/login')
  await expect(page.getByLabel('用户名')).toBeVisible()
  await expect(page.getByLabel('密码')).toHaveAttribute('type', 'password')
})

test('field primitives compose native and local accessibility feedback', async ({ page }) => {
  await page.goto('/e2e/fixtures/admin-forms.html')

  for (const kind of ['input', 'select', 'textarea']) {
    const plain = page.getByLabel(`${kind} plain`)
    await expect(plain).toHaveAttribute('aria-describedby', `${kind}-plain-external`)
    await expect(plain).toHaveAttribute('aria-invalid', 'true')

    const hinted = page.getByLabel(`${kind} hint`)
    await expect(hinted).toHaveAttribute(
      'aria-describedby',
      `${kind}-hint-external ${kind}-hint-description`,
    )
    await expect(hinted).toHaveAttribute('aria-invalid', 'true')

    const errored = page.getByLabel(`${kind} error`)
    await expect(errored).toHaveAttribute(
      'aria-describedby',
      `${kind}-error-external ${kind}-error-description`,
    )
    await expect(errored).toHaveAttribute('aria-invalid', 'true')
  }
})

test('security policy controls have distinct ecosystem names and toggle with Space', async ({ page }) => {
  await page.goto('/admin/security')
  // TabsV2 receives tab semantics in Plan 04 Task 5; use its current button contract here.
  await page.getByRole('button', { name: /策略/ }).click()

  const pypiSwitch = page.getByRole('switch', { name: 'PYPI 自动拦截' })
  const npmSwitch = page.getByRole('switch', { name: 'NPM 自动拦截' })
  await expect(pypiSwitch).toBeVisible()
  await expect(npmSwitch).toBeVisible()
  await expect(page.getByRole('spinbutton', { name: 'PYPI CVSS 阈值' })).toBeVisible()
  await expect(page.getByRole('spinbutton', { name: 'NPM CVSS 阈值' })).toBeVisible()

  await pypiSwitch.focus()
  const before = await pypiSwitch.getAttribute('aria-checked')
  await page.keyboard.press('Space')
  expect(await pypiSwitch.getAttribute('aria-checked')).not.toBe(before)
})
