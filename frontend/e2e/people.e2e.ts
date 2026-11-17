import { expect, test, type Page } from '@playwright/test'

// People page against a mocked, signed-in API: list, search, paging, detail, 404 and empty project.

const person = (id: string, extra: Record<string, unknown> = {}) => ({
  user_id: id,
  traits: { plan: 'pro', country: 'ID' },
  first_seen: '2026-11-01T08:00:00Z',
  last_seen: '2026-11-17T10:00:00Z',
  session_count: 3,
  error_session_count: 1,
  last_context: { platform: 'android', model: 'Pixel 8', os_version: '14', app_version: '2.3.1' },
  ...extra,
})

type Mock = {
  /** Answers GET /projects/1/people; receives the query string. */
  list?: (qs: URLSearchParams) => { items: unknown[]; total: number }
  /** user id to detail body; missing ids answer 404. */
  details?: Record<string, unknown>
}

async function signedInWith(page: Page, mock: Mock, seen: URLSearchParams[] = []) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/auth/refresh') return json({ access_token: 'test-token' })
    if (path === '/auth/me') {
      return json({ id: 7, email: 'ops@example.test', name: 'Ops', avatar_url: '', orgs: [{ id: 1, name: 'Acme', slug: 'acme', role: 'owner' }] })
    }
    if (path === '/orgs/1/projects') {
      return json({
        items: [{ id: 1, org_id: 1, name: 'Shop App', slug: 'shop', platform: 'android', created_at: '2026-01-01T00:00:00Z', replay_screenshot_enabled: true }],
      })
    }
    if (path === '/projects/1/nav-counts') return json({ frustrated_replays_24h: 0, open_issues: 0, firing_alerts: 0 })
    if (path === '/projects/1/people') {
      seen.push(url.searchParams)
      const res = mock.list?.(url.searchParams) ?? { items: [], total: 0 }
      return json({ ...res, limit: Number(url.searchParams.get('limit') ?? 25), offset: Number(url.searchParams.get('offset') ?? 0) })
    }
    const detail = /^\/projects\/1\/people\/(.+)$/.exec(path)
    if (detail) {
      const body = mock.details?.[decodeURIComponent(detail[1])]
      return body ? json(body) : json({ error: 'person not found' }, 404)
    }
    return json({ items: [] })
  })
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1366, height: 768 })
})

test('people list searches and paginates', async ({ page }) => {
  const seen: URLSearchParams[] = []
  await signedInWith(
    page,
    {
      list: (qs) => {
        const offset = Number(qs.get('offset') ?? 0)
        if (qs.get('search') === 'pro') return { items: [person('u_pro')], total: 1 }
        return { items: Array.from({ length: 25 }, (_, i) => person(`u_${offset + i}`)), total: 30 }
      },
    },
    seen,
  )
  await page.goto('/dashboard/people')

  const table = page.getByRole('table', { name: 'People' })
  for (const col of ['User', 'Traits', 'Last seen', 'Sessions', 'Errors', 'Last device']) {
    await expect(table.getByRole('columnheader', { name: col })).toBeVisible()
  }
  await expect(table.getByRole('row')).toHaveCount(26) // header + 25
  await expect(table.getByRole('row').nth(1)).toContainText('Pixel 8 · Android 14 · v2.3.1')
  await expect(page.getByText('1–25 of 30')).toBeVisible()

  await page.getByRole('button', { name: 'Next →' }).click()
  await expect(page).toHaveURL(/offset=25/)
  await expect(page.getByText('26–30 of 30')).toBeVisible()
  expect(seen.some((q) => q.get('offset') === '25')).toBe(true)

  await page.getByRole('searchbox', { name: 'Search people' }).fill('pro')
  await expect(page).toHaveURL(/q=pro/)
  await expect(page).not.toHaveURL(/offset=/)
  await expect(table.getByRole('row')).toHaveCount(2)
  expect(seen.some((q) => q.get('search') === 'pro')).toBe(true)
})

test('people detail shows sessions and links to replay', async ({ page }) => {
  await signedInWith(page, {
    list: () => ({ items: [person('u_42')], total: 1 }),
    details: {
      u_42: {
        person: person('u_42'),
        sessions: [
          { session_id: 'ses_a', first_seen: '2026-11-17T09:00:00Z', last_seen: '2026-11-17T09:05:00Z', has_error: true, has_replay: true, context: {} },
          { session_id: 'ses_b', first_seen: '2026-11-16T09:00:00Z', last_seen: '2026-11-16T09:01:00Z', has_error: false, has_replay: false, context: {} },
        ],
      },
    },
  })
  await page.goto('/dashboard/people')
  await page.getByRole('row', { name: /u_42/ }).click()
  await expect(page).toHaveURL(/\/dashboard\/people\?user=u_42$/)

  await expect(page.locator('.ppl-detail__id')).toHaveText('u_42')
  await expect(page.locator('.ppl-kv__row', { hasText: 'plan' })).toContainText('pro')
  await expect(page.locator('.ppl-kv__row', { hasText: 'OS' })).toContainText('Android 14')
  const rows = page.locator('.ppl-session')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('Error')
  await expect(rows.nth(0)).toContainText('Replay')
  await expect(rows.nth(1)).toContainText('No replay')
  await expect(rows.nth(0)).toHaveAttribute('href', '/dashboard/replays?session=ses_a')

  await page.getByRole('link', { name: '← Back to People' }).click()
  await expect(page).toHaveURL(/\/dashboard\/people$/)
  await expect(page.getByRole('table', { name: 'People' })).toBeVisible()
})

test('people detail for an id with reserved characters round-trips', async ({ page }) => {
  const id = 'a/b c@x.id'
  await signedInWith(page, { details: { [id]: { person: person(id), sessions: [] } } })
  await page.goto(`/dashboard/people?user=${encodeURIComponent(id)}`)
  await expect(page.locator('.ppl-detail__id')).toHaveText(id)
  await expect(page.getByText('No sessions recorded.')).toBeVisible()
})

test('people detail 404', async ({ page }) => {
  await signedInWith(page, {})
  await page.goto('/dashboard/people?user=ghost')
  await expect(page.getByText('No profile for this user yet')).toBeVisible()
  await expect(page.getByRole('link', { name: '← Back to People' })).toBeVisible()
})

test('people empty project explains identify', async ({ page }) => {
  await signedInWith(page, {})
  await page.goto('/dashboard/people')
  await expect(page.getByText('No identified users yet')).toBeVisible()
  await expect(page.getByText('ARTracker.identify("u_123", mapOf("plan" to "pro"))')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Identify users docs →' })).toHaveAttribute('href', '/docs#identify-users')
})

test('People is in the sidebar', async ({ page }) => {
  await signedInWith(page, {})
  await page.goto('/dashboard/people')
  await expect(page.getByRole('link', { name: 'People' }).first()).toHaveAttribute('href', '/dashboard/people')
})

test('the docs link opens the Identify Users section', async ({ page }) => {
  await signedInWith(page, {})
  await page.goto('/docs#identify-users')
  await expect(page.getByRole('heading', { name: 'Identify Users (People)' }).first()).toBeVisible()
  await expect(page.getByText('ARTracker.identify(userId: "u_123", traits: ["plan": "pro", "country": "ID"])')).toBeVisible()
})
