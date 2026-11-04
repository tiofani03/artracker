export type Artwork = {
  id: number
  title: string
  artist: string
  year: number | null
  medium: string
  notes: string
  created_at: string
  updated_at: string
}

export type ArtworkInput = {
  title: string
  artist?: string
  year?: number | null
  medium?: string
  notes?: string
}

export type Role = 'owner' | 'admin' | 'member' | 'viewer'

export type OrgMembership = { id: number; name: string; slug: string; role: Role }

export type User = {
  id: number
  email: string
  name: string
  avatar_url: string
  orgs: OrgMembership[]
}

export const BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1'

export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized')
  }
}

// Access token lives in memory only; the refresh cookie (httpOnly) survives reloads.
let accessToken: string | null = null
export function setAccessToken(token: string | null) {
  accessToken = token
}

// Refresh tokens rotate and reuse revokes the session, so concurrent refreshes
// (StrictMode double effects, parallel 401s) must share one request.
let refreshing: Promise<void> | null = null
function refreshAccess(): Promise<void> {
  refreshing ??= (async () => {
    try {
      const res = await fetch(`${BASE}/auth/refresh`, { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new UnauthorizedError()
      const body = (await res.json()) as { access_token: string }
      accessToken = body.access_token
    } catch (err) {
      accessToken = null
      throw err instanceof UnauthorizedError ? err : new UnauthorizedError()
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
      ...(init.headers ?? {}),
    },
  })

  if (res.status === 401 && retry && !path.startsWith('/auth/')) {
    await refreshAccess() // throws UnauthorizedError when the session is gone
    return request<T>(path, init, false)
  }

  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // non-JSON error body, keep the status text
    }
    throw new Error(message)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  list: () => request<{ items: Artwork[] }>('/artworks?limit=100'),
  create: (input: ArtworkInput) =>
    request<Artwork>('/artworks', { method: 'POST', body: JSON.stringify(input) }),
  remove: (id: number) => request<void>(`/artworks/${id}`, { method: 'DELETE' }),
}

export const authApi = {
  refresh: () => refreshAccess(),
  me: () => request<User>('/auth/me'),
  logout: async () => {
    await request<void>('/auth/logout', { method: 'POST' }, false)
    accessToken = null
  },
}

export type Organization = OrgMembership & { created_at: string }

export type Platform = 'web' | 'android' | 'ios'

export type Project = {
  id: number
  org_id: number
  name: string
  slug: string
  platform: Platform
  created_at: string
}

export type KeyType = 'public_client' | 'secret_admin'

export type ApiKey = {
  id: number
  project_id: number
  key_type: KeyType
  prefix: string
  is_active: boolean
  created_at: string
  revoked_at: string | null
}

// secret is the plaintext key; the server returns it exactly once.
export type CreatedKey = { key: ApiKey; secret: string }

const post = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })

export const orgApi = {
  create: (name: string) => post<Organization>('/orgs', { name }),
  listProjects: (orgId: number) => request<{ items: Project[] }>(`/orgs/${orgId}/projects`),
  createProject: (orgId: number, input: { name: string; platform: Platform }) =>
    post<Project>(`/orgs/${orgId}/projects`, input),
}

export const keyApi = {
  list: (projectId: number) => request<{ items: ApiKey[] }>(`/projects/${projectId}/keys`),
  create: (projectId: number, keyType: KeyType) => post<CreatedKey>(`/projects/${projectId}/keys`, { key_type: keyType }),
  revoke: (projectId: number, keyId: number) => post<ApiKey>(`/projects/${projectId}/keys/${keyId}/revoke`),
  rotate: (projectId: number, keyId: number) => post<CreatedKey>(`/projects/${projectId}/keys/${keyId}/rotate`),
}
