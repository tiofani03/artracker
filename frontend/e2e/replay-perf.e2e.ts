import { gzipSync } from 'node:zlib'
import { expect, test, type Page } from '@playwright/test'

type PerfHarness = {
  ready: boolean
  chunkPayloads(): unknown[]
  mountPlayer(opts?: { fillViewport?: boolean }): void
}

async function mount(page: Page, query: string) {
  await page.goto(`/harness/screen.html?mode=none${query}`)
  await page.waitForFunction(() => (window as unknown as { __harness?: PerfHarness }).__harness?.ready === true)
  const payloads = await page.evaluate(() => (window as unknown as { __harness: PerfHarness }).__harness.chunkPayloads())
  await page.route('**/__chunks/*.gz', async (route) => {
    const seq = Number(/\/(\d+)\.gz/.exec(route.request().url())?.[1])
    await route.fulfill({ body: gzipSync(JSON.stringify(payloads[seq])), contentType: 'application/gzip' })
  })
  await page.evaluate(() =>
    (window as unknown as { __harness: PerfHarness }).__harness.mountPlayer({ fillViewport: true }),
  )
  await expect(page.getByTestId('screen-replay-canvas')).toBeVisible({ timeout: 15_000 })
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1366, height: 768 })
})

test('perf tracks, Performance tab and breach seeking', async ({ page }) => {
  await mount(page, '&perfFixture=1')

  for (const id of ['cpu', 'mem', 'fps']) {
    await expect(page.locator(`.or-track[data-track="${id}"]`)).toBeVisible()
  }

  await page.getByRole('tab', { name: /Performance/ }).click()
  await expect(page.locator('.or-perf-breach')).toHaveCount(2)

  await page.locator('.or-perf-breach', { hasText: 'High CPU' }).click()
  await expect(page.locator('.or-transport-time')).toContainText('00:05')

  // the CPU track label shows the value at the playhead (samples 5-8 are 95 %)
  await expect(page.locator('.or-track-label', { hasText: 'CPU' })).toContainText('95%')
})

test('breaches also appear as markers behind the Performance filter chip', async ({ page }) => {
  await mount(page, '&perfFixture=1')
  await page.getByRole('tab', { name: 'Events' }).click()
  await page.getByRole('button', { name: /Performance \(2\)/ }).click()
  await expect(page.locator('.or-activity-row')).toHaveCount(2)
})

test('hiding a track is remembered across reloads', async ({ page }) => {
  await mount(page, '&perfFixture=1')
  await page.getByRole('button', { name: 'Tracks ▾' }).click()
  await page.getByLabel('CPU', { exact: true }).uncheck()
  await expect(page.locator('.or-track[data-track="cpu"]')).toHaveCount(0)

  await mount(page, '&perfFixture=1')
  await expect(page.locator('.or-track[data-track="mem"]')).toBeVisible()
  await expect(page.locator('.or-track[data-track="cpu"]')).toHaveCount(0)
})

test('a session without profiling data has no perf tracks and shows the empty state', async ({ page }) => {
  await mount(page, '')
  await expect(page.locator('.or-track[data-track="cpu"]')).toHaveCount(0)
  await page.getByRole('tab', { name: /Performance/ }).click()
  await expect(page.getByText('No profiling data for this session.')).toBeVisible()
})
