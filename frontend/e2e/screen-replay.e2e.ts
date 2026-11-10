import { gzipSync } from 'node:zlib'
import { expect, test, type Page } from '@playwright/test'

type RGB = [number, number, number]

/** The harness (harness/screen-main.tsx) puts this on window. Colours encode the frame index. */
type Harness = {
  ready: boolean
  base: number
  frameTs: number[]
  expectedColor(i: number): Promise<RGB>
  setTs(ts: number): void
  pixel(): RGB
  buffering(): boolean
  chunkPayloads(): unknown[]
  mountPlayer(): void
  memoryMb(): number
  settle(): Promise<void>
}
declare global {
  interface Window {
    __harness: Harness
  }
}

const near = (a: RGB, b: RGB, tol = 2) => a.every((v, i) => Math.abs(v - b[i]) <= tol)

async function open(page: Page, query: string) {
  await page.goto(`/harness/screen.html?${query}`)
  await page.waitForFunction(() => window.__harness?.ready === true)
}

const pixel = (page: Page) => page.evaluate(() => window.__harness.pixel())
const colorsOf = (page: Page) =>
  page.evaluate(() => Promise.all(window.__harness.frameTs.map((_, i) => window.__harness.expectedColor(i))))

test('canvas shows the frame with greatest ts <= T and never a later one', async ({ page }) => {
  await open(page, 'mode=scan')
  const base = await page.evaluate(() => window.__harness.base)
  const colors = await colorsOf(page)
  const last = colors.length - 1

  // 40 frames, 500 ms apart: exact frame times, 1 ms before a frame, mid-gap, backwards seeks, past the end
  const offsets = [0, 1, 499, 500, 2250, 7000, 7499, 10000, 12501, 19500, 19999, 5000, 250, 20_000, 0]
  for (const offset of offsets) {
    await page.evaluate((ts) => window.__harness.setTs(ts), base + offset)
    const due = Math.min(last, Math.floor(offset / 500))

    // right after the seek: whatever is drawn must not be a later frame
    const immediate = await pixel(page)
    for (let j = due + 1; j <= last; j++) {
      expect(near(immediate, colors[j]), `offset ${offset}: drew later frame ${j}`).toBe(false)
    }

    await expect.poll(async () => near(await pixel(page), colors[due]), { timeout: 5000, message: `offset ${offset}` }).toBe(true)
    for (let k = 0; k < 4; k++) {
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => r(null))))
      const px = await pixel(page)
      for (let j = due + 1; j <= last; j++) expect(near(px, colors[j])).toBe(false)
    }
  }
})

test('seeking far ahead keeps the previous frame and shows buffering until the chunk arrives', async ({ page }) => {
  await open(page, 'mode=lazy&delay=3:1500') // chunk 3 (frames 30-39) is held back for 1.5 s, nothing is pre-scanned
  const base = await page.evaluate(() => window.__harness.base)
  const colors = await colorsOf(page)

  await page.evaluate((ts) => window.__harness.setTs(ts), base)
  await expect.poll(async () => near(await pixel(page), colors[0]), { timeout: 5000 }).toBe(true)

  await page.evaluate((ts) => window.__harness.setTs(ts), base + 17_000) // frame 34, inside chunk 3
  await expect.poll(() => page.evaluate(() => window.__harness.buffering()), { timeout: 1000 }).toBe(true)
  const during = await pixel(page)
  // only chunk 0 (frames 0-9) is known, so the last frame at or before the playhead is frame 9: it stays up while buffering
  expect(colors.slice(0, 10).some((c) => near(during, c)), 'an earlier frame stays on screen').toBe(true)
  expect(near(during, colors[34])).toBe(false)

  await expect.poll(async () => near(await pixel(page), colors[34]), { timeout: 8000 }).toBe(true)
  await expect.poll(() => page.evaluate(() => window.__harness.buffering()), { timeout: 2000 }).toBe(false)
})

test('ReplayPlayer plays a screenshot session end to end', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))

  await open(page, 'mode=none')
  const payloads = await page.evaluate(() => window.__harness.chunkPayloads())
  const colors = await colorsOf(page)
  await page.route('**/__chunks/*.gz', async (route) => {
    const seq = Number(/\/(\d+)\.gz/.exec(route.request().url())?.[1])
    await route.fulfill({ body: gzipSync(JSON.stringify(payloads[seq])), contentType: 'application/gzip' })
  })
  await page.evaluate(() => window.__harness.mountPlayer())

  await expect(page.getByTestId('screen-replay-canvas')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.or-res-chip', { hasText: 'Screenshot' })).toBeVisible()
  await expect.poll(async () => near(await pixel(page), colors[0]), { timeout: 8000 }).toBe(true)

  await page.getByRole('button', { name: 'Play replay' }).click()
  await expect
    .poll(
      async () => {
        const px = await pixel(page)
        return colors.slice(3, 12).some((c) => near(px, c))
      },
      { timeout: 10_000 },
    )
    .toBe(true)
  expect(errors).toEqual([])
})

test('30-minute 2 fps session stays under 150 MB of replay data', async ({ page }) => {
  test.skip(!process.env.E2E_PERF, 'set E2E_PERF=1 to run the memory check')
  test.setTimeout(300_000)
  await open(page, 'mode=perf')
  const base = await page.evaluate(() => window.__harness.base)
  for (const minute of [0, 12, 29, 3, 20, 0]) {
    await page.evaluate((ts) => window.__harness.setTs(ts), base + minute * 60_000)
    await page.evaluate(() => window.__harness.settle())
  }
  const mb = await page.evaluate(() => window.__harness.memoryMb())
  console.log(`replay data heap after scan and seeks: ${mb.toFixed(1)} MB`)
  expect(mb).toBeLessThan(150)
})
