import { describe, expect, it, vi } from 'vitest'
import type { ReplayChunkDetail } from '../api'
import { ScreenChunkSource } from './screenChunkSource'

async function gzJson(obj: unknown): Promise<Uint8Array> {
  const cs = new CompressionStream('gzip')
  const w = cs.writable.getWriter()
  void w.write(new TextEncoder().encode(JSON.stringify(obj)))
  void w.close()
  return new Uint8Array(await new Response(cs.readable).arrayBuffer())
}

const detail = (seq: number, url: string): ReplayChunkDetail => ({
  chunk_seq: seq,
  object_key: `k${seq}`,
  size_bytes: 1,
  event_count: 1,
  created_at: '',
  url,
})

const payload = { format: 'artracker-screen-v1', frames: [] }

describe('ScreenChunkSource', () => {
  it('returns the parsed payload on 200', async () => {
    const fetchImpl = vi.fn(async () => new Response(await gzJson(payload)))
    const src = new ScreenChunkSource([detail(0, 'https://x/a?sig=1')], vi.fn(), fetchImpl as unknown as typeof fetch)
    expect(await src.fetchChunk(0)).toEqual(payload)
  })

  it('refreshes the url once on 403 and retries (Review Focus 3)', async () => {
    const fetchImpl = vi.fn(async (url: string) =>
      url.includes('sig=old') ? new Response('expired', { status: 403 }) : new Response(await gzJson(payload)),
    )
    const refresh = vi.fn(async () => [detail(4, 'https://x/a?sig=new')])
    const src = new ScreenChunkSource([detail(4, 'https://x/a?sig=old')], refresh, fetchImpl as unknown as typeof fetch)

    expect(await src.fetchChunk(4)).toEqual(payload)
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(refresh).toHaveBeenCalledWith(4, 4)
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(fetchImpl.mock.calls[1][0]).toContain('sig=new')

    // the refreshed url is remembered: no further refresh
    await src.fetchChunk(4)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('returns null when the refreshed url is rejected again', async () => {
    const fetchImpl = vi.fn(async () => new Response('no', { status: 401 }))
    const refresh = vi.fn(async () => [detail(1, 'https://x/a?sig=new')])
    const src = new ScreenChunkSource([detail(1, 'https://x/a?sig=old')], refresh, fetchImpl as unknown as typeof fetch)
    expect(await src.fetchChunk(1)).toBeNull()
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('returns null when the refresh itself fails', async () => {
    const fetchImpl = vi.fn(async () => new Response('no', { status: 403 }))
    const refresh = vi.fn(async () => {
      throw new Error('network down')
    })
    const src = new ScreenChunkSource([detail(1, 'https://x/a')], refresh, fetchImpl as unknown as typeof fetch)
    expect(await src.fetchChunk(1)).toBeNull()
  })

  it('returns null for other HTTP errors without refreshing, and for unknown seqs without fetching', async () => {
    const fetchImpl = vi.fn(async () => new Response('boom', { status: 500 }))
    const refresh = vi.fn()
    const src = new ScreenChunkSource([detail(1, 'https://x/a')], refresh, fetchImpl as unknown as typeof fetch)
    expect(await src.fetchChunk(1)).toBeNull()
    expect(refresh).not.toHaveBeenCalled()
    fetchImpl.mockClear()
    expect(await src.fetchChunk(99)).toBeNull()
    expect(fetchImpl).not.toHaveBeenCalled()
  })

  it('rejects with AbortError when the signal is aborted', async () => {
    const ctrl = new AbortController()
    const fetchImpl = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.signal?.aborted) throw new DOMException('Aborted', 'AbortError')
      return new Response(await gzJson(payload))
    })
    const src = new ScreenChunkSource([detail(1, 'https://x/a')], vi.fn(), fetchImpl as unknown as typeof fetch)
    ctrl.abort()
    await expect(src.fetchChunk(1, ctrl.signal)).rejects.toMatchObject({ name: 'AbortError' })
  })
})
