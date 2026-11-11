import { gzipSync } from 'node:zlib'
import { expect, test, type Page } from '@playwright/test'

// Local view of window.__harness (harness/screen-main.tsx); the global declaration lives in screen-replay.e2e.ts.
type WorkspaceHarness = {
  ready: boolean
  chunkPayloads(): unknown[]
  mountPlayer(opts?: { fillViewport?: boolean }): void
}

async function mount(page: Page) {
  await page.goto('/harness/screen.html?mode=none')
  await page.waitForFunction(() => (window as unknown as { __harness?: WorkspaceHarness }).__harness?.ready === true)
  const payloads = await page.evaluate(() => (window as unknown as { __harness: WorkspaceHarness }).__harness.chunkPayloads())
  await page.route('**/__chunks/*.gz', async (route) => {
    const seq = Number(/\/(\d+)\.gz/.exec(route.request().url())?.[1])
    await route.fulfill({ body: gzipSync(JSON.stringify(payloads[seq])), contentType: 'application/gzip' })
  })
  await page.evaluate(() =>
    (window as unknown as { __harness: WorkspaceHarness }).__harness.mountPlayer({ fillViewport: true }),
  )
  await expect(page.getByTestId('screen-replay-canvas')).toBeVisible({ timeout: 15_000 })
}

const bottom = async (page: Page, selector: string) => {
  const box = await page.locator(selector).first().boundingBox()
  expect(box, `${selector} must be rendered`).not.toBeNull()
  return box!.y + box!.height
}

for (const vp of [
  { width: 1366, height: 768 },
  { width: 1024, height: 768 },
]) {
  test(`workspace fits ${vp.width}x${vp.height} and never scrolls the page while playing`, async ({ page }) => {
    await page.setViewportSize(vp)
    await mount(page)

    // Not collapsed: the stage keeps a usable size.
    const stage = await page.locator('.or-stage').boundingBox()
    expect(stage!.height).toBeGreaterThan(200)
    expect(stage!.width).toBeGreaterThan(200)

    expect(await bottom(page, '.or-stage')).toBeLessThanOrEqual(vp.height)
    expect(await bottom(page, '.or-transport-bar')).toBeLessThanOrEqual(vp.height)

    if (vp.width > 1100) {
      const panel = await page.locator('.or-side-panel').boundingBox()
      expect(panel!.width).toBeGreaterThan(300)
      expect(panel!.x + panel!.width).toBeLessThanOrEqual(vp.width)
      expect(panel!.y + panel!.height).toBeLessThanOrEqual(vp.height)
    } else {
      // bottom sheet: at least its header must be reachable inside the viewport
      expect(await bottom(page, '.or-side-panel__head')).toBeLessThanOrEqual(vp.height)
    }

    // Space plays; the page must not move while the follow-playhead list updates.
    await page.keyboard.press('Space')
    await page.waitForTimeout(3000)
    expect(await page.evaluate(() => window.scrollY)).toBe(0)
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= document.documentElement.clientHeight)).toBe(true)
  })
}
