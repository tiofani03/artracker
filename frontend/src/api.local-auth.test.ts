import { afterEach, describe, expect, it, vi } from 'vitest'

async function loadWebApi() {
  vi.resetModules()
  vi.stubEnv('VITE_TARGET', 'web')
  vi.stubEnv('VITE_API_BASE', 'https://api.example/api/v1')
  return import('./api')
}

const json = (status: number, body: unknown = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('local auth API (web)', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('config() parses google and local booleans', async () => {
    const fetchMock = vi.fn(async () => json(200, { google: false, local: true }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi } = await loadWebApi()

    await expect(authApi.config()).resolves.toEqual({ google: false, local: true })
    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example/api/v1/auth/config',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('localLogin() posts credentials and refreshes the access token', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(json(200, { access_token: 'acc-local' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, getAccessToken } = await loadWebApi()

    await authApi.localLogin('ada@example.com', 'correct-horse-battery')

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [loginUrl, loginInit] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(loginUrl).toBe('https://api.example/api/v1/auth/local/login')
    expect(loginInit.method).toBe('POST')
    expect(loginInit.credentials).toBe('include')
    expect(JSON.parse(loginInit.body as string)).toEqual({
      email: 'ada@example.com',
      password: 'correct-horse-battery',
    })

    const [refreshUrl, refreshInit] = fetchMock.mock.calls[1] as unknown as [string, RequestInit]
    expect(refreshUrl).toBe('https://api.example/api/v1/auth/refresh')
    expect(refreshInit.method).toBe('POST')
    expect(refreshInit.credentials).toBe('include')
    expect(getAccessToken()).toBe('acc-local')
  })

  it('localLogin() rejects with ApiError on 401 without calling refresh', async () => {
    const fetchMock = vi.fn(async () => json(401, { error: 'invalid email or password' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, ApiError } = await loadWebApi()

    const err = await authApi.localLogin('ada@example.com', 'wrong').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as InstanceType<typeof ApiError>).status).toBe(401)
    expect((err as Error).message).toBe('invalid email or password')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('localRegister() posts invite token, name, password and refreshes', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json(201, { user_id: 7, org_id: 2, role: 'member' }))
      .mockResolvedValueOnce(json(200, { access_token: 'acc-reg' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, getAccessToken } = await loadWebApi()

    await authApi.localRegister('tok-123', 'Ada', 'correct-horse-battery')

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [regUrl, regInit] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(regUrl).toBe('https://api.example/api/v1/auth/local/register')
    expect(regInit.method).toBe('POST')
    expect(regInit.credentials).toBe('include')
    expect(JSON.parse(regInit.body as string)).toEqual({
      invite_token: 'tok-123',
      name: 'Ada',
      password: 'correct-horse-battery',
    })
    expect(getAccessToken()).toBe('acc-reg')
  })

  it('localChangePassword() posts with bearer token and succeeds on 204', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(json(200, { access_token: 'acc-pw' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi } = await loadWebApi()

    await authApi.localLogin('ada@example.com', 'correct-horse-battery')
    await authApi.localChangePassword('correct-horse-battery', 'new-correct-horse-battery')

    expect(fetchMock).toHaveBeenCalledTimes(3)
    const [pwUrl, pwInit] = fetchMock.mock.calls[2] as unknown as [string, RequestInit]
    expect(pwUrl).toBe('https://api.example/api/v1/auth/local/password')
    expect(pwInit.method).toBe('POST')
    expect(pwInit.credentials).toBe('include')
    expect((pwInit.headers as Record<string, string>).Authorization).toBe('Bearer acc-pw')
    expect(JSON.parse(pwInit.body as string)).toEqual({
      current_password: 'correct-horse-battery',
      new_password: 'new-correct-horse-battery',
    })
  })

  it('localChangePassword() refreshes access token and retries once on 401', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(json(200, { access_token: 'acc-refreshed' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi } = await loadWebApi()

    await authApi.localChangePassword('old-password-1', 'new-password-1')

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect((fetchMock.mock.calls[1] as unknown as [string])[0]).toBe('https://api.example/api/v1/auth/refresh')
    const [retryUrl, retryInit] = fetchMock.mock.calls[2] as unknown as [string, RequestInit]
    expect(retryUrl).toBe('https://api.example/api/v1/auth/local/password')
    expect((retryInit.headers as Record<string, string>).Authorization).toBe('Bearer acc-refreshed')
  })

  it('localChangePassword() rejects with ApiError(403) without calling refresh', async () => {
    const fetchMock = vi.fn(async () => json(403, { error: 'current password is incorrect' }))
    vi.stubGlobal('fetch', fetchMock)
    const { authApi, ApiError } = await loadWebApi()

    const err = await authApi.localChangePassword('wrong-password', 'new-password-1').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as InstanceType<typeof ApiError>).status).toBe(403)
    expect((err as Error).message).toBe('current password is incorrect')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
