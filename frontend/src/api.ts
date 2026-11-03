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

const BASE = import.meta.env.VITE_API_BASE ?? '/api/v1'

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
  })

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
