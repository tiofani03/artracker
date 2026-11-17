import { gzipSync } from 'node:zlib'
import { expect, test, type Page } from '@playwright/test'

// Local view of window.__harness (harness/screen-main.tsx); the global declaration lives in screen-replay.e2e.ts.
type WorkspaceHarness = {
  ready: boolean
  chunkPayloads(): unknown[]
  mountPlayer(opts?: { fillViewport?: boolean; shell?: boolean }): void
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

// The harness test above has no page chrome; this one wraps the player in the dashboard's real topbar + padded
// content (same CSS classes, same useFillHeight hook) and checks the PAGE cannot scroll at all.
for (const vp of [
  { width: 1366, height: 768 },
  { width: 1024, height: 768 },
  { width: 1920, height: 1080 },
]) {
  test(`inside the page chrome the document never overflows at ${vp.width}x${vp.height}`, async ({ page }) => {
    await page.setViewportSize(vp)
    await page.goto('/harness/screen.html?mode=none')
    await page.waitForFunction(() => (window as unknown as { __harness?: WorkspaceHarness }).__harness?.ready === true)
    const payloads = await page.evaluate(() => (window as unknown as { __harness: WorkspaceHarness }).__harness.chunkPayloads())
    await page.route('**/__chunks/*.gz', async (route) => {
      const seq = Number(/\/(\d+)\.gz/.exec(route.request().url())?.[1])
      await route.fulfill({ body: gzipSync(JSON.stringify(payloads[seq])), contentType: 'application/gzip' })
    })
    await page.evaluate(() => (window as unknown as { __harness: WorkspaceHarness }).__harness.mountPlayer({ shell: true }))
    await expect(page.getByTestId('screen-replay-canvas')).toBeVisible({ timeout: 15_000 })

    const overflow = () =>
      page.evaluate(() => document.documentElement.scrollHeight - document.documentElement.clientHeight)
    expect(await overflow()).toBeLessThanOrEqual(0)

    // A banner appearing above the workspace later (e.g. an error) must be absorbed, not scroll the page.
    await page.evaluate(() => {
      const banner = document.createElement('div')
      banner.style.height = '40px'
      document.querySelector('.console-content')?.prepend(banner)
    })
    await page.waitForTimeout(400)
    expect(await overflow()).toBeLessThanOrEqual(0)
  })
}

// --- Replay page against a mocked API (demo mode: no login, the project is supplied by the shell) ---

type MockSession = {
  id: string
  user_id: string
  has_replay: boolean
  event_count?: number
}

function sessionItem(s: MockSession) {
  return {
    id: s.id,
    project_id: 1,
    user_id: s.user_id,
    environment: 'production',
    start_time: '2026-11-17T10:00:00Z',
    end_time: '2026-11-17T10:00:05Z',
    duration_ms: 5000,
    chunk_count: s.has_replay ? 1 : 0,
    event_count: s.event_count ?? 3,
    total_size_bytes: 0,
    has_error: !s.has_replay,
    has_rage_click: false,
    has_dead_click: false,
    has_replay: s.has_replay,
    storage_prefix: '',
    created_at: '2026-11-17T10:00:00Z',
    updated_at: '2026-11-17T10:00:05Z',
  }
}

const crashTimeline = [
  {
    id: 'ev_1',
    kind: 'event',
    name: 'app_start',
    level: 'info',
    message: 'Launched',
    session_id: 'ses_noreplay_01',
    user_id: 'carol',
    environment: 'production',
    release_version: '1.0.0',
    event_timestamp: '2026-11-17T10:00:00Z',
    attributes: {},
  },
  {
    id: 'ie_9',
    kind: 'crash',
    name: 'NullPointerException',
    level: 'fatal',
    message: 'null deref in CheckoutActivity',
    session_id: 'ses_noreplay_01',
    user_id: 'carol',
    environment: 'production',
    release_version: '1.0.0',
    event_timestamp: '2026-11-17T10:00:03Z',
    attributes: {},
  },
]

/** Answers the replay API from `list` and `details` (session id to detail body); everything else is empty or 401. */
async function mockReplayApi(
  page: Page,
  list: MockSession[],
  details: Record<string, { session: MockSession; timeline: unknown[] }>,
  /** Session ids whose detail answers 500. */
  failing: string[] = [],
  /** Session id to the body of GET …/sessions/:id/context (the replay User tab). */
  contexts: Record<string, unknown> = {},
) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path.startsWith('/auth/')) return json({ error: 'unauthorized' }, 401)
    if (/^\/projects\/\d+\/sessions$/.test(path)) {
      return json({ items: list.map(sessionItem), total: list.length, limit: 25, offset: 0 })
    }
    const detail = /^\/projects\/\d+\/sessions\/([^/]+)$/.exec(path)
    if (detail) {
      const id = decodeURIComponent(detail[1])
      if (failing.includes(id)) return json({ error: 'internal error' }, 500)
      const d = details[id]
      if (!d) return json({ error: 'not found' }, 404)
      return json({ session: sessionItem(d.session), chunks: [], timeline_events: d.timeline })
    }
    if (/\/sessions\/[^/]+\/perf$/.test(path)) return json({ metrics: [], samples: [], downsampled: false })
    const ctx = /\/sessions\/([^/]+)\/context$/.exec(path)
    if (ctx) {
      const id = decodeURIComponent(ctx[1])
      return json(contexts[id] ?? { session_id: id, user_id: '', context: {}, person: null, other_sessions: [] })
    }
    return json({ items: [] })
  })
}

test.describe('replay page without a replay', () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1366, height: 768 })
  })

  test('a session without replay is badged and shows its events and crash instead of a player', async ({ page }) => {
    const noReplay: MockSession = { id: 'ses_noreplay_01', user_id: 'carol', has_replay: false, event_count: 2 }
    await mockReplayApi(page, [noReplay], { [noReplay.id]: { session: noReplay, timeline: crashTimeline } })

    await page.goto('/dashboard/replays?demo=1')
    // below 1600px the queue starts collapsed once a session is selected
    await page.getByRole('button', { name: 'Toggle session list' }).click()
    const item = page.locator('.dash-session-item', { hasText: 'carol' })
    await expect(item.getByText('no replay')).toBeVisible()
    await expect(item).toContainText('2 events')
    await expect(item).not.toContainText('chunk')

    await item.click()
    await expect(page.getByText('Replay not recorded for this session')).toBeVisible()
    await expect(
      page.getByText('Replay is disabled in the SDK or this session was sampled out. Showing its events instead.'),
    ).toBeVisible()
    const crash = page.locator('.or-timeline-panel__row', { hasText: 'crash' })
    await expect(crash).toContainText('null deref in CheckoutActivity')
    await expect(crash).toContainText('+3.0s')
    await expect(page.locator('.or-transport-bar')).toHaveCount(0)
  })

  test('a deep link to an unknown session says so and shows no player', async ({ page }) => {
    const other: MockSession = { id: 'ses_other_01', user_id: 'alice', has_replay: true }
    await mockReplayApi(page, [other], {})

    await page.goto('/dashboard/replays?demo=1&session=ses_missing_01')
    await expect(page.getByText('Session not found')).toBeVisible()
    await expect(page.locator('.or-transport-bar')).toHaveCount(0)
    await expect(page.locator('.or-unified-header__user')).toHaveCount(0)
  })

  test('a deep link to a session outside the first page opens that session, not the first one', async ({ page }) => {
    const first: MockSession = { id: 'ses_first_01', user_id: 'alice', has_replay: true }
    const far: MockSession = { id: 'ses_far_99', user_id: 'zoe_far_away', has_replay: false }
    await mockReplayApi(page, [first], { [far.id]: { session: far, timeline: crashTimeline } })

    await page.goto('/dashboard/replays?demo=1&session=ses_far_99')
    await expect(page.locator('.or-unified-header__user')).toHaveText('zoe_far_away')
    await expect(page.locator('.or-unified-header__user', { hasText: 'alice' })).toHaveCount(0)
  })

  test('a failing detail request for a deep-linked session offers a retry, not "not found" or another session', async ({ page }) => {
    const first: MockSession = { id: 'ses_first_01', user_id: 'alice', has_replay: true }
    await mockReplayApi(page, [first], {}, ['ses_far_99'])

    await page.goto('/dashboard/replays?demo=1&session=ses_far_99')
    await expect(page.getByText("Couldn't load this session")).toBeVisible()
    await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible()
    await expect(page.getByText('Session not found')).toHaveCount(0)
    await expect(page.locator('.or-transport-bar')).toHaveCount(0)
    await expect(page.locator('.or-unified-header__user')).toHaveCount(0)
  })
})

test.describe('replay User tab', () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1366, height: 768 })
  })

  const current: MockSession = { id: 'ses_user_now', user_id: 'u_42', has_replay: true }
  const older: MockSession = { id: 'ses_user_old', user_id: 'u_42', has_replay: false }
  const userContext = {
    session_id: current.id,
    user_id: 'u_42',
    context: { platform: 'android', model: 'Pixel 8', manufacturer: 'Google', os_version: '14', app_version: '2.3.1' },
    person: {
      user_id: 'u_42',
      traits: { plan: 'pro', country: 'ID' },
      first_seen: '2026-11-01T08:00:00Z',
      last_seen: '2026-11-17T10:00:05Z',
      session_count: 2,
      error_session_count: 1,
      last_context: {},
    },
    other_sessions: [
      {
        session_id: older.id,
        first_seen: '2026-11-16T09:00:00Z',
        last_seen: '2026-11-16T09:02:00Z',
        has_error: true,
        has_replay: false,
        context: {},
      },
    ],
  }

  test('shows traits, device context and other sessions, and links onward', async ({ page }) => {
    await mockReplayApi(
      page,
      [current],
      {
        [current.id]: { session: current, timeline: [] },
        [older.id]: { session: older, timeline: crashTimeline },
      },
      [],
      { [current.id]: userContext },
    )
    await page.goto(`/dashboard/replays?demo=1&session=${current.id}`)
    await page.getByRole('tab', { name: 'User' }).click()

    const panel = page.locator('.ppl-panel')
    await expect(panel.locator('.ppl-identity__id')).toHaveText('u_42')
    await expect(panel.locator('.ppl-kv__row', { hasText: 'plan' })).toContainText('pro')
    await expect(panel.locator('.ppl-kv__row', { hasText: 'OS' })).toContainText('Android 14')
    await expect(panel.locator('.ppl-kv__row', { hasText: 'Device' })).toContainText('Google Pixel 8')
    await expect(panel).toContainText('2 sessions · 1 with errors')
    const row = panel.locator('.ppl-session')
    await expect(row).toHaveCount(1)
    await expect(row.locator('.badge--exception')).toHaveText('Error')

    await expect(panel.getByRole('link', { name: 'View profile →' })).toHaveAttribute('href', '/dashboard/people?user=u_42')
    await row.click()
    await expect(page).toHaveURL(/session=ses_user_old/)
    await expect(page.locator('.or-unified-header__user')).toHaveText('u_42')
    await expect(page.getByText('Replay not recorded for this session')).toBeVisible()

    // The header user id links to the People profile (outside demo mode, so only the target is checked here).
    await expect(page.locator('.or-unified-header__user a')).toHaveAttribute('href', '/dashboard/people?user=u_42')
  })

  test('an anonymous session shows its device and how to identify users', async ({ page }) => {
    const anon: MockSession = { id: 'ses_anon_01', user_id: '', has_replay: true }
    await mockReplayApi(page, [anon], { [anon.id]: { session: anon, timeline: [] } }, [], {
      [anon.id]: { session_id: anon.id, user_id: '', context: { platform: 'ios', os_version: '17.4.1' }, person: null, other_sessions: [] },
    })
    await page.goto(`/dashboard/replays?demo=1&session=${anon.id}`)
    await page.getByRole('tab', { name: 'User' }).click()
    const panel = page.locator('.ppl-panel')
    await expect(panel).toContainText('This session is anonymous')
    await expect(panel.locator('.ppl-kv__row', { hasText: 'OS' })).toContainText('iOS 17.4.1')
    await expect(panel).toContainText('ARTracker.identify(userId: "u_123"')
    await expect(page.locator('.or-unified-header__user a')).toHaveCount(0)
  })

  test('a failing context request offers a retry', async ({ page }) => {
    // Fails until Retry is clicked (React StrictMode may fetch twice on mount, so no call counting).
    let failing = true
    await mockReplayApi(page, [current], { [current.id]: { session: current, timeline: [] } })
    await page.route('**/sessions/*/context', async (route) => {
      if (failing) return route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' })
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(userContext) })
    })
    await page.goto(`/dashboard/replays?demo=1&session=${current.id}`)
    await page.getByRole('tab', { name: 'User' }).click()
    await expect(page.locator('.ppl-panel')).toContainText("Couldn't load user details")
    failing = false
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page.locator('.ppl-identity__id')).toHaveText('u_42')
  })
})
