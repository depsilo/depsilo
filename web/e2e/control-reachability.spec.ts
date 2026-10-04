import { expect, test, type Page } from '@playwright/test'
import { mockAdminApi, setUiPreferences } from './fixtures/admin-api'

// Every route that lays out real content, Portal and Admin.
const ROUTES = [
  '/',
  '/monitor',
  '',
  'upstreams',
  'cache',
  'indexes',
  'compile-cache',
  'logs',
  'upstream-updates',
  'audit',
  'security',
  'quarantine',
  'rules',
  'projects',
  'users',
  'settings',
  'license',
].map(function (route) {
  if (route === '/') return '/'
  if (route === '/monitor') return '/monitor'
  return route ? '/admin/' + route : '/admin'
})

// A control whose box sits outside the viewport with no scrollable ancestor is
// unreachable by touch. That is a silent failure mode: the document simply
// clips it, nothing errors, and only a programmatic scroll could reach it —
// which is how the settings directory rail hid its last tabs on a phone.
// A deliberately off-canvas control (a closed drawer, a carousel slide) should
// be excluded explicitly rather than by weakening this check.
async function unreachableControls(page: Page) {
  return page.evaluate(function () {
    const selector = 'a[href], button, input, select, textarea, [role="tab"], [role="button"]'
    const results: Array<{ name: string; left: number; right: number }> = []

    for (const element of Array.from(document.querySelectorAll<HTMLElement>(selector))) {
      const box = element.getBoundingClientRect()
      if (box.width === 0 || box.height === 0) continue
      if (box.right <= window.innerWidth + 0.5 && box.left >= -0.5) continue

      let scrollable = false
      for (let node = element.parentElement; node; node = node.parentElement) {
        const overflowX = getComputedStyle(node).overflowX
        if ((overflowX === 'auto' || overflowX === 'scroll') && node.scrollWidth > node.clientWidth) {
          scrollable = true
          break
        }
      }
      if (scrollable) continue

      results.push({
        name: (element.getAttribute('aria-label') || element.textContent || element.tagName).trim().slice(0, 40),
        left: Math.round(box.left),
        right: Math.round(box.right),
      })
    }
    return results
  })
}

test('no route hides an interactive control outside the 320px viewport', async ({ page }) => {
  test.setTimeout(300_000)
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {})

  const failures: string[] = []
  for (const route of ROUTES) {
    await page.setViewportSize({ width: 320, height: 844 })
    await page.goto(route)
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(200)

    const overflow = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }))
    if (overflow.scrollWidth > overflow.clientWidth) {
      failures.push(`${route}: document scrollWidth ${overflow.scrollWidth} > ${overflow.clientWidth}`)
    }

    const unreachable = await unreachableControls(page)
    if (unreachable.length > 0) {
      failures.push(`${route}: unreachable ${JSON.stringify(unreachable.slice(0, 3))}`)
    }
  }

  expect(failures).toEqual([])
})
