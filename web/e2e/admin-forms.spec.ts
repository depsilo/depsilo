import AxeBuilder from '@axe-core/playwright'

import { test, expect, mockAdminApi } from './fixtures/admin-api'

async function expectNoDialogAxeViolations(page: import('@playwright/test').Page) {
  const results = await new AxeBuilder({ page })
    .include('[role="dialog"]')
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze()
  expect(results.violations).toEqual([])
}

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

test('select keeps a visible keyboard focus ring and native theme scheme', async ({ page }) => {
  await page.goto('/e2e/fixtures/admin-forms.html')

  const root = page.locator('html')
  const select = page.getByLabel('select plain')

  await root.evaluate(element => {
    element.classList.remove('dark')
    element.classList.add('light')
    element.setAttribute('data-theme', 'light')
  })
  await expect.poll(() => root.evaluate(element => getComputedStyle(element).colorScheme)).toBe('light')
  await expect.poll(() => select.evaluate(element => getComputedStyle(element).colorScheme)).toBe('light')

  await page.getByLabel('input error').focus()
  await page.keyboard.press('Tab')
  await expect(select).toBeFocused()
  expect(await select.evaluate(element => {
    const styles = getComputedStyle(element)
    return {
      matchesFocusVisible: element.matches(':focus-visible'),
      outlineStyle: styles.outlineStyle,
      outlineWidth: styles.outlineWidth,
    }
  })).toEqual({
    matchesFocusVisible: true,
    outlineStyle: 'solid',
    outlineWidth: '2px',
  })

  await root.evaluate(element => {
    element.classList.remove('light')
    element.classList.add('dark')
    element.setAttribute('data-theme', 'dark')
  })
  await expect.poll(() => root.evaluate(element => getComputedStyle(element).colorScheme)).toBe('dark')
  await expect.poll(() => select.evaluate(element => getComputedStyle(element).colorScheme)).toBe('dark')
})

test('security policy controls have distinct ecosystem names and toggle with Space', async ({ page }) => {
  await page.goto('/admin/security')
  // TabsV2 receives tab semantics in Plan 04 Task 5; use its current button contract here.
  await page.getByRole('tab', { name: /策略/ }).click()

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

test('dynamic rule and cache forms expose named controls and pass axe', async ({ page }) => {
  await page.goto('/admin/rules')
  await page.getByRole('button', { name: /添加规则|Add Rule/ }).click()

  const ruleDialog = page.getByRole('dialog', { name: /添加规则|Add Rule/ })
  const allow = ruleDialog.getByRole('button', { name: /允许|Allow/ })
  const deny = ruleDialog.getByRole('button', { name: /禁止|Deny/ })
  await expect(allow).toHaveAttribute('aria-pressed', 'false')
  await expect(deny).toHaveAttribute('aria-pressed', 'true')
  await allow.click()
  await expect(allow).toHaveAttribute('aria-pressed', 'true')
  await expect(deny).toHaveAttribute('aria-pressed', 'false')
  await expect(ruleDialog.getByRole('textbox', { name: /原因|Reason/ })).toBeVisible()
  await expectNoDialogAxeViolations(page)

  await page.keyboard.press('Escape')
  await page.goto('/admin/cache')
  await page.getByRole('button', { name: /预热|Warmup/ }).click()

  const warmupDialog = page.getByRole('dialog', { name: /缓存预热|Cache Warmup/ })
  await expect(warmupDialog.getByRole('textbox', { name: /包名列表|Package list/ })).toBeVisible()
  await expectNoDialogAxeViolations(page)
})

test('custom bandwidth dates are labelled and invalid ranges do not load', async ({ page }) => {
  let reportRequests = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/bandwidth': () => {
      reportRequests += 1
      return { summary: {}, daily: [], by_ecosystem: [], top_packages: [], by_upstream: [] }
    },
  })
  await page.goto('/admin/bandwidth')
  await expect.poll(() => reportRequests).toBe(1)

  await page.getByRole('button', { name: /自定义|Custom/ }).click()
  const start = page.getByLabel(/开始日期|Start date/)
  const end = page.getByLabel(/结束日期|End date/)
  await start.fill('2026-08-05')
  await end.fill('2026-08-01')

  await expect(end).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByRole('alert')).toContainText(/结束日期不能早于开始日期|End date cannot be before start date/)
  expect((await new AxeBuilder({ page })
    .include('[data-bandwidth-custom-range]')
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze()).violations).toEqual([])
  expect(reportRequests).toBe(1)

  await end.fill('2026-08-07')
  await expect(end).not.toHaveAttribute('aria-invalid', 'true')
  await expect.poll(() => reportRequests).toBe(2)
})
