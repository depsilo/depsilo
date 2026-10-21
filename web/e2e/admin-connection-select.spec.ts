import AxeBuilder from '@axe-core/playwright'
import { mkdir } from 'node:fs/promises'
import { expect, setUiPreferences, test } from './fixtures/admin-api'

for (const { theme, locale, width } of [
  { theme: 'dark', locale: 'zh', width: 1440 },
  { theme: 'light', locale: 'en', width: 1440 },
  { theme: 'dark', locale: 'zh', width: 320 },
] as const) {
  test(`connection menus follow the ${theme} theme at ${width}px in ${locale}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.emulateMedia({ colorScheme: theme === 'dark' ? 'light' : 'dark', reducedMotion: 'reduce' })
    await setUiPreferences(page, theme, locale)
    await page.goto('/admin/connect')
    const ecosystem = page.getByRole('combobox', { name: locale === 'zh' ? '生态' : 'Ecosystem', exact: true })
    await expect(ecosystem).toHaveText('Python')
    await ecosystem.click()
    const menu = page.getByRole('listbox')
    await expect(menu).toBeVisible()
    await expect(page.getByRole('option', { name: 'Python', exact: true })).toHaveAttribute('aria-selected', 'true')
    const triggerBox = (await ecosystem.boundingBox())!
    const menuBox = (await menu.boundingBox())!
    expect(menuBox.y).toBeGreaterThanOrEqual(triggerBox.y + triggerBox.height)
    expect(menuBox.height).toBeLessThanOrEqual(322)
    expect(menuBox.x).toBeGreaterThanOrEqual(0)
    expect(menuBox.x + menuBox.width).toBeLessThanOrEqual(width)
    expect((await page.getByRole('option', { name: 'Python', exact: true }).boundingBox())!.height).toBeGreaterThanOrEqual(40)
    expect(await page.locator('[data-slot="select-content"]').evaluate(element => getComputedStyle(element).borderRadius)).not.toBe('0px')
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
    await mkdir('/tmp/depsilo-themed-select-shots', { recursive: true })
    await page.screenshot({ path: `/tmp/depsilo-themed-select-shots/connect-menu-${theme}-${locale}-${width}-mock.png`, fullPage: true })

    await page.keyboard.press('End')
    await expect(page.getByRole('option', { name: 'Hugging Face', exact: true })).toBeInViewport()
    await page.keyboard.press('Escape')
    await expect(menu).toBeHidden()
    await expect(ecosystem).toBeFocused()
    await expect(ecosystem).toHaveText('Python')
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)

    // The other three controls share the same popup and selection affordances.
    for (const field of locale === 'zh' ? ['包管理器', '配置范围', '终端'] : ['Package manager', 'Configuration scope', 'Shell']) {
      const trigger = page.getByRole('combobox', { name: field, exact: true })
      await trigger.click()
      await expect(trigger).toHaveAttribute('aria-expanded', 'true')
      await expect(menu).toBeVisible()
      await expect(page.getByRole('option', { selected: true })).toHaveCount(1)
      await page.keyboard.press('Escape')
      await expect(menu).toBeHidden()
      await expect(trigger).toBeFocused()
    }
  })
}

test('connection selection supports typeahead, keyboard commit and continuous focus', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/admin/connect')
  const copyConfiguration = page.getByRole('button', { name: 'Copy configuration', exact: true })
  await expect(copyConfiguration).toBeVisible()
  await copyConfiguration.click()
  await expect(copyConfiguration).toContainText('Copied')
  const ecosystem = page.getByRole('combobox', { name: 'Ecosystem', exact: true })
  await ecosystem.focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('listbox')).toBeVisible()
  await expect(page.getByRole('option', { name: 'Python', exact: true })).toBeFocused()
  await page.keyboard.press('n')
  await expect(page.getByRole('option', { name: 'Node.js', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(ecosystem).toHaveText('Node.js')
  await expect(ecosystem).toBeFocused()
  await expect(page.getByRole('heading', { name: 'Configure npm', exact: true })).toBeVisible()
  await expect(copyConfiguration).toHaveText('Copy configuration')
  await page.keyboard.press('Tab')
  const manager = page.getByRole('combobox', { name: 'Package manager', exact: true })
  await expect(manager).toBeFocused()
  await page.keyboard.press('Space')
  await page.getByRole('option', { name: 'Yarn', exact: true }).click()
  await expect(manager).toHaveText('Yarn')
  await expect(manager).toBeFocused()
  await expect(page.getByRole('heading', { name: 'Configure Yarn', exact: true })).toBeVisible()
})
