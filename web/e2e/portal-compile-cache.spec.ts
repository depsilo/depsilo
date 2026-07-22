import { expect, setUiPreferences, test } from './fixtures/admin-api'

test('Portal explains compiler cache boundaries and links to its Admin workspace', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  const intro = page.getByRole('region', { name: '为 ccache 和 sccache 提供远程缓存' })
  await expect(intro).toBeVisible()
  await expect(intro).toContainText('配置文件中启用并重启服务')
  await expect(intro).toContainText('ccache HTTP remote storage · sccache 窄 WebDAV · 两者不交叉命中')
  await expect(intro).toContainText('Depsilo 内部本地 / S3 存储 · 不暴露 S3 API')
  await expect(intro).toContainText('不是 sccache-dist')
  await expect(intro).toContainText('当前仅支持单实例')

  const adminLink = intro.getByRole('link', { name: '打开编译缓存管理' })
  await expect(adminLink).toHaveAttribute('href', '/admin/compile-cache')
  await adminLink.click()
  await expect(page).toHaveURL(/\/admin\/compile-cache$/)
  await expect(page.getByRole('heading', { level: 1, name: '编译缓存' })).toBeVisible()
})

test('Portal compiler cache explanation is localized in English', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 })
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const intro = page.getByRole('region', { name: 'Remote build cache for ccache and sccache' })
  await expect(intro).toBeVisible()
  await expect(intro).toContainText('dedicated build credential for each namespace')
  await expect(intro).toContainText('narrow sccache WebDAV · no cross-hits')
  await expect(intro).toContainText('no S3 API')
  await expect(intro).toContainText('not sccache-dist')
  await expect(intro).toContainText('Single-instance only')
  await expect(intro.getByRole('link', { name: 'Open cache workspace' })).toHaveAttribute('href', '/admin/compile-cache')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})
