import AxeBuilder from '@axe-core/playwright'
import type { Request } from '@playwright/test'

import { adminApiDefaults, test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

async function expectNoDialogAxeViolations(page: import('@playwright/test').Page) {
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  // Axe blends translucent text with every layer behind the dialog. Wait for
  // the 160ms entry transition to finish so contrast is measured at the state
  // an Operator actually reads, not at an arbitrary animation frame.
  await expect(dialog).toHaveCSS('opacity', '1')
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
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [{
      id: 1,
      ecosystem: 'pypi',
      auto_block_enabled: true,
      min_cvss_score: 8.5,
      created_by: 'admin',
      created_at: '2026-07-10T00:00:00Z',
      updated_at: '2026-07-10T00:00:00Z',
    }],
  })
  await page.goto('/admin/security')
  // TabsV2 receives tab semantics in Plan 04 Task 5; use its current button contract here.
  await page.getByRole('combobox', { name: /情报视图|Intelligence view/ }).selectOption('policies')

  const pypiSwitch = page.getByRole('switch', { name: 'PYPI 自动拦截' })
  const npmPolicy = page.locator('[data-policy-ecosystem="npm"]')
  const npmSwitch = npmPolicy.getByRole('switch')
  const goPolicy = page.locator('[data-policy-ecosystem="go"]')
  const goSwitch = goPolicy.getByRole('switch')
  const aptPolicy = page.locator('[data-policy-ecosystem="apt"]')
  await expect(pypiSwitch).toBeVisible()
  await expect(pypiSwitch).toBeChecked()
  await expect(pypiSwitch).toBeEnabled()
  await expect(page.locator('[data-policy-ecosystem="pypi"]')).toContainText(/完整 OSV 受影响集合|complete OSV affected sets/)
  await expect(npmSwitch).toBeDisabled()
  await expect(npmPolicy).toContainText(/完整 OSV 受影响集合|complete OSV affected sets/)
  await expect(goSwitch).toBeDisabled()
  await expect(goPolicy).toContainText(/自动拦截依赖版本范围|automatic blocking requires version ranges/)
  await expect(aptPolicy.getByRole('switch')).toBeDisabled()
  await expect(aptPolicy).toContainText(/\.deb 路径不含 Debian epoch|\.deb paths omit the Debian epoch/)
  await expect(page.getByRole('spinbutton', { name: 'PYPI CVSS 阈值' })).toBeVisible()
  await expect(page.getByRole('spinbutton', { name: 'NPM CVSS 阈值' })).toBeVisible()

  await pypiSwitch.focus()
  const before = await pypiSwitch.getAttribute('aria-checked')
  await page.keyboard.press('Space')
  expect(await pypiSwitch.getAttribute('aria-checked')).not.toBe(before)
  await expect(pypiSwitch).toBeDisabled()
})

test('dynamic rule and cache forms expose named controls and pass axe', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'POST /api/v1/admin/cache/warmup': { packages: 1 },
  })
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

  const ecosystem = ruleDialog.getByLabel(/生态|Ecosystem/)
  const packageName = ruleDialog.getByLabel(/包名|Package Name/)
  const version = ruleDialog.getByLabel(/版本|Version/)
  await expect(ecosystem.locator('option[value="*"]')).toHaveText('全部包规则生态 (*)')
  for (const unsupported of ['rubygems', 'helm', 'docker', 'huggingface']) {
    await expect(ecosystem.locator(`option[value="${unsupported}"]`)).toHaveCount(0)
  }
  await ecosystem.selectOption('go')
  await expect(ruleDialog.getByText(/仅支持包级规则和精确版本规则|package-wide and exact-version rules only/)).toBeVisible()
  await expect(version).toHaveAttribute('placeholder', /精确版本|Exact version/)

  await ecosystem.selectOption('maven')
  await expect(packageName).toHaveAttribute('placeholder', /org\.apache\.logging\.log4j:log4j-core$/)

  await ecosystem.selectOption('*')
  await expect(packageName).toBeDisabled()
  await expect(packageName).toHaveValue('*')
  await expect(version).toBeDisabled()
  await expect(version).toHaveValue('*')
  await expect(ruleDialog.getByText(/包名和版本都必须使用 \*|Package and version must both be \*/)).toBeVisible()

  await ecosystem.selectOption('apt')
  await expect(packageName).toBeEnabled()
  await expect(version).toBeDisabled()
  await expect(version).toHaveValue('*')
  await expect(ruleDialog.getByText(/\.deb 路径不含 Debian epoch|\.deb paths omit the Debian epoch/)).toBeVisible()

  await ecosystem.selectOption('npm')
  await expect(packageName).toBeEnabled()
  await expect(version).toBeEnabled()
  await expect(version).toHaveAttribute('placeholder', /< 2\.17\.0/)
  await expect(ruleDialog.getByText(/signed dist\.tarball URL|已签名(?:的)? dist\.tarball URL/)).toBeVisible()

  await ecosystem.selectOption('composer')
  await expect(version).toBeDisabled()
  await expect(ruleDialog.getByText(/规范化版本或 reference 回退|normalized version or a reference fallback/)).toBeVisible()
  await expectNoDialogAxeViolations(page)

  await page.keyboard.press('Escape')
  await page.goto('/admin/cache')
  await page.getByRole('button', { name: /预热|Warmup/ }).click()

  const warmupDialog = page.getByRole('dialog', { name: /缓存预热|Cache Warmup/ })
  const warmupPackages = warmupDialog.getByRole('textbox', { name: /包名列表|Package list/ })
  const warmupStart = warmupDialog.getByRole('button', { name: /开始预热|Start Warmup/ })
  await expect(warmupPackages).toBeVisible()
  await warmupPackages.fill('# only a comment')
  await expect(warmupStart).toBeDisabled()
  await warmupPackages.fill('requests')
  await warmupStart.click()
  await expect(warmupDialog).toContainText(/已接受 1 个包的预热请求|Warmup accepted for 1 packages/)
  await expectNoDialogAxeViolations(page)
})

test('Dashboard range control drives overview and trends and persists the choice', async ({ page }) => {
  const calls: Record<string, number> = {}
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range') ?? ''
      calls[`overview-${range}`] = (calls[`overview-${range}`] ?? 0) + 1
      return adminApiDefaults['GET /api/v1/admin/dashboard']
    },
    'GET /api/v1/admin/dashboard/trends': (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range') ?? ''
      calls[`trends-${range}`] = (calls[`trends-${range}`] ?? 0) + 1
      return { points: [] }
    },
  })
  await page.goto('/admin')
  await expect.poll(() => calls['overview-30d'] ?? 0).toBe(1)
  await expect.poll(() => calls['trends-30d'] ?? 0).toBe(1)

  await page.getByRole('group', { name: '统计周期' }).getByRole('button', { name: '7 天' }).click()
  await expect.poll(() => calls['overview-7d'] ?? 0).toBe(1)
  await expect.poll(() => calls['trends-7d'] ?? 0).toBe(1)
  expect(await page.evaluate(() => localStorage.getItem('depsilo-dashboard-range'))).toBe('7d')
})
