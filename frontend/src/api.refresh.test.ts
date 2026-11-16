import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// App build only: the refresh token lives in the native store and rotates on every refresh.
let stored: string | null
const getRefreshToken = vi.fn(async () => stored)
const setRefreshToken = vi.fn(async (t: string | null) => {
  stored = t
})

vi.mock('./auth/appStore', () => ({ getRefreshToken, setRefreshToken }))

async function loadApi() {
  vi.resetModules()
  vi.stubEnv('VITE_TARGET', 'app')
  vi.stubEnv('VITE_API_BASE', 'https://api.example/api/v1')
  return import('./api')
}

const json = (status: number, body: unknown = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('app refresh token handling', () => {
  beforeEach(() => {
    stored = 'A'
    getRefreshToken.mockClear()
    setRefreshToken.mockClear()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('sends the stored token in the body and stores the rotated one', async () => {
    const fetchMock = vi.fn(async () => json(200, { access_token: 'acc', refresh_token: 'B' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, getAccessToken } = await loadApi()

    await authApi.refresh()

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('https://api.example/api/v1/auth/refresh')
    expect(JSON.parse(init.body as string)).toEqual({ refresh_token: 'A' })
    expect(init.credentials).toBe('omit')
    expect(stored).toBe('B')
    expect(getAccessToken()).toBe('acc')
  })

  it('does not wipe a token another webview load already rotated (stale 401)', async () => {
    // First call (token A) loses the race: the other load rotated A -> B and stored B meanwhile.
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(async () => {
        stored = 'B'
        return json(401, { error: 'unauthorized' })
      })
      .mockImplementationOnce(async () => json(200, { access_token: 'acc2', refresh_token: 'C' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, getAccessToken } = await loadApi()

    const p = authApi.refresh()
    await vi.advanceTimersByTimeAsync(500)
    await p

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string)).toEqual({ refresh_token: 'B' })
    expect(stored).toBe('C')
    expect(getAccessToken()).toBe('acc2')
  })

  it('clears the token when the sent one is really dead', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(401, { error: 'unauthorized' })))
    const { authApi, UnauthorizedError } = await loadApi()

    const p = authApi.refresh()
    const assertion = expect(p).rejects.toBeInstanceOf(UnauthorizedError)
    await vi.advanceTimersByTimeAsync(500)
    await assertion

    expect(stored).toBeNull()
  })

  it('keeps the token on non-401 failures (offline, 5xx)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(503)))
    const { authApi } = await loadApi()

    await expect(authApi.refresh()).rejects.toThrow()
    expect(stored).toBe('A')
  })
})
