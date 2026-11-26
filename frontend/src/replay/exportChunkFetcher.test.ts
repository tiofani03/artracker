import { describe, expect, it, vi } from 'vitest'
import { fetchExportChunks, parseExportScreenChunks } from './exportChunkFetcher'
import type { ReplayChunkDetail } from '../api'

describe('fetchExportChunks & parseExportScreenChunks', () => {
  it('fetches targeted chunks in sequence and tracks progress', async () => {
    const chunks: ReplayChunkDetail[] = [
      { chunk_seq: 0, object_key: 'c0', size_bytes: 10, event_count: 1, created_at: '', url: 'http://localhost/c0' },
      { chunk_seq: 1, object_key: 'c1', size_bytes: 10, event_count: 1, created_at: '', url: 'http://localhost/c1' },
      { chunk_seq: 2, object_key: 'c2', size_bytes: 10, event_count: 1, created_at: '', url: 'http://localhost/c2' },
    ]

    const screenPayload0 = {
      format: 'artracker-screen-v1',
      platform: 'android',
      seq: 0,
      frames: [{ ts: 1000, screen: 'Home', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', data: 'abc' }],
      touches: [],
    }

    const mockFetch = vi.fn().mockImplementation((url: string) => {
      const isC0 = url.includes('c0')
      const payload = isC0 ? screenPayload0 : { format: 'artracker-screen-v1', platform: 'android', seq: 1, frames: [], touches: [] }
      return Promise.resolve(new Response(JSON.stringify(payload), { status: 200 }))
    })

    const origFetch = globalThis.fetch
    globalThis.fetch = mockFetch

    try {
      const progressList: Array<{ loaded: number; total: number }> = []
      const results = await fetchExportChunks({
        chunks,
        targetSeqs: [0, 1],
        refreshUrls: vi.fn(),
        onProgress: (loaded, total) => progressList.push({ loaded, total }),
      })

      expect(results.length).toBe(2)
      expect(results[0].seq).toBe(0)
      expect(results[1].seq).toBe(1)
      expect(progressList).toEqual([
        { loaded: 1, total: 2 },
        { loaded: 2, total: 2 },
      ])

      const parsed = parseExportScreenChunks(results)
      expect(parsed.length).toBe(1) // chunk 1 has 0 frames & 0 touches, so parseScreenChunk returns null
      expect(parsed[0].seq).toBe(0)
      expect(parsed[0].frames.length).toBe(1)
    } finally {
      globalThis.fetch = origFetch
    }
  })

  it('aborts when signal is cancelled', async () => {
    const controller = new AbortController()
    controller.abort()

    await expect(
      fetchExportChunks({
        chunks: [{ chunk_seq: 0, object_key: 'c0', size_bytes: 10, event_count: 1, created_at: '', url: 'http://localhost/c0' }],
        targetSeqs: [0],
        refreshUrls: vi.fn(),
        signal: controller.signal,
      }),
    ).rejects.toThrow('Aborted')
  })
})
