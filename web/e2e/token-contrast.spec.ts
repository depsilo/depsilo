import type { Page } from '@playwright/test'
import { expect, setUiPreferences, test } from './fixtures/admin-api'

/**
 * The token layer's contrast gate.
 *
 * Colours are resolved by painting them onto a canvas and reading the sRGB
 * bytes back, so this works for any colour space or `color-mix` expression the
 * token layer uses and never pins a literal value. A token edit that breaks a
 * pair fails here rather than in production, and the Stage B design documents
 * point at this spec as the enforcement of their contrast section.
 */

const TEXT_FLOOR = 4.5
const GRAPHIC_FLOOR = 3

/** Text on its surface. The tinted pair is the stricter of the two. */
const TEXT_PAIRS: ReadonlyArray<readonly [string, string]> = [
  ['--foreground', '--background'],
  ['--foreground', '--card'],
  ['--foreground', '--popover'],
  ['--foreground', '--muted'],
  ['--foreground', '--accent'],
  ['--foreground', '--secondary'],
  ['--foreground', '--sidebar'],
  ['--foreground', '--sidebar-accent'],
  ['--card-foreground', '--card'],
  ['--popover-foreground', '--popover'],
  ['--muted-foreground', '--background'],
  ['--muted-foreground', '--card'],
  ['--muted-foreground', '--muted'],
  ['--neutral-status', '--background'],
  ['--neutral-status', '--muted'],
  ['--primary-foreground', '--primary'],
  ['--secondary-foreground', '--secondary'],
  ['--accent-foreground', '--accent'],
  ['--sidebar-foreground', '--sidebar'],
  ['--sidebar-accent-foreground', '--sidebar-accent'],
  ['--success', '--background'],
  ['--success', '--success-surface'],
  ['--warning', '--background'],
  ['--warning', '--warning-surface'],
  ['--destructive', '--background'],
  ['--destructive', '--destructive-surface'],
  ['--info', '--background'],
  ['--info', '--card'],
]

/** Graphics that carry meaning on their own. */
const GRAPHIC_PAIRS: ReadonlyArray<readonly [string, string]> = [
  ['--ring', '--background'],
  ['--ring', '--card'],
  ['--chart-1', '--background'],
  ['--chart-2', '--background'],
  ['--chart-3', '--background'],
  ['--chart-4', '--background'],
  ['--chart-5', '--background'],
  ['--chart-1', '--card'],
  ['--chart-2', '--card'],
  ['--chart-3', '--card'],
  ['--chart-4', '--card'],
  ['--chart-5', '--card'],
]

const ALL_TOKENS = [
  ...new Set([...TEXT_PAIRS, ...GRAPHIC_PAIRS].flat()),
]

async function luminances(page: Page): Promise<Record<string, number>> {
  return page.evaluate((tokens) => {
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const context = canvas.getContext('2d')
    if (!context) throw new Error('2d context unavailable')

    const probe = document.createElement('span')
    document.body.appendChild(probe)

    const result: Record<string, number> = {}
    for (const token of tokens) {
      probe.style.color = `var(${token})`
      const resolved = getComputedStyle(probe).color
      context.fillStyle = '#000'
      context.fillStyle = resolved
      context.fillRect(0, 0, 1, 1)
      const [r, g, b] = context.getImageData(0, 0, 1, 1).data
      const [lr, lg, lb] = [r, g, b].map((value) => {
        const channel = value / 255
        return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
      })
      result[token] = 0.2126 * lr + 0.7152 * lg + 0.0722 * lb
    }
    probe.remove()
    return result
  }, ALL_TOKENS)
}

function ratio(luminance: Record<string, number>, fg: string, bg: string) {
  const a = luminance[fg]
  const b = luminance[bg]
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

async function measure(page: Page, theme: 'light' | 'dark') {
  await setUiPreferences(page, theme, 'en')
  await page.goto('/')
  return luminances(page)
}

for (const theme of ['light', 'dark'] as const) {
  test(`token layer meets its contrast floors in ${theme} mode`, { tag: '@smoke' }, async ({ page }) => {
    const luminance = await measure(page, theme)

    const failures: string[] = []
    for (const [fg, bg] of TEXT_PAIRS) {
      const value = ratio(luminance, fg, bg)
      if (value < TEXT_FLOOR) {
        failures.push(`${fg} on ${bg}: ${value.toFixed(2)} < ${TEXT_FLOOR}`)
      }
    }
    for (const [fg, bg] of GRAPHIC_PAIRS) {
      const value = ratio(luminance, fg, bg)
      if (value < GRAPHIC_FLOOR) {
        failures.push(`${fg} on ${bg}: ${value.toFixed(2)} < ${GRAPHIC_FLOOR}`)
      }
    }
    expect(failures).toEqual([])
  })
}
