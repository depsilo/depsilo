import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.PLAYWRIGHT_PORT ?? '4173')
if (!Number.isInteger(port) || port < 1 || port > 65_535) {
  throw new Error('PLAYWRIGHT_PORT must be an integer between 1 and 65535')
}
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './e2e',
  testIgnore: '**/production-binary.spec.ts',
  fullyParallel: false,
  // Keep cases within a spec serial, but use half of the four cores available
  // on GitHub's public Linux runners so Chromium and Vite retain headroom.
  workers: process.env.CI ? 2 : undefined,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  // Every page is a lazy route and the Dashboard pulls Recharts in its own
  // chunk, so under parallel load a first assertion can legitimately arrive
  // after the default 5s without anything being wrong with the product. The
  // assertions themselves are unchanged; they simply get a budget that matches
  // a suite that fetches chunks from a dev server while four browsers run.
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [['html', { open: 'never' }], ['line']] : 'line',
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    locale: 'zh-CN',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port}`,
    url: baseURL,
    // Reusing any HTTP server on the port can silently run the suite against
    // another application. Keep reuse an explicit local-development opt-in.
    reuseExistingServer: process.env.PLAYWRIGHT_REUSE_SERVER === '1',
  },
})
