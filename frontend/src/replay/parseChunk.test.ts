import { describe, expect, it, vi } from 'vitest'
import { fetchChunkPayloads, parseChunkBytes } from './parseChunk'

async function gzip(text: string): Promise<ArrayBuffer> {
  const stream = new Blob([text]).stream().pipeThrough(new CompressionStream('gzip'))
  return new Response(stream).arrayBuffer()
}

const bytes = (text: string) => new TextEncoder().encode(text).buffer as ArrayBuffer

describe('parseChunkBytes', () => {
  it('gunzips and parses gzipped JSON', async () => {
    expect(await parseChunkBytes(await gzip('[{"type":4}]'))).toEqual([{ type: 4 }])
  })

  it('parses plain (already decompressed) JSON', async () => {
    expect(await parseChunkBytes(bytes('{"events":[]}'))).toEqual({ events: [] })
  })

  it('returns null for gzipped non-JSON (Review Focus #3)', async () => {
    expect(await parseChunkBytes(await gzip('not json'))).toBeNull()
  })

  it('returns null for an empty buffer', async () => {
    expect(await parseChunkBytes(new ArrayBuffer(0))).toBeNull()
  })

  it('returns null for truncated gzip data', async () => {
    const full = new Uint8Array(await gzip('[{"type":4,"timestamp":1}]'))
    expect(await parseChunkBytes(full.slice(0, 12).buffer)).toBeNull()
  })
})

describe('fetchChunkPayloads', () => {
  const okJson = (body: string) => new Response(body, { status: 200 })

  it('skips one unreadable chunk and still returns the others (Review Focus #3)', async () => {
    const responses: Record<string, Response> = {
      '/a': okJson('[{"type":4,"timestamp":1}]'),
      '/b': new Response('nope', { status: 404 }),
      '/c': okJson('not json'),
    }
    vi.stubGlobal('fetch', async (url: string) => responses[url])
    const out = await fetchChunkPayloads(['/a', '/b', '/c'])
    expect(out.failed).toBe(2)
    expect(out.payloads).toEqual([[{ type: 4, timestamp: 1 }], null, null])
    vi.unstubAllGlobals()
  })

  it('rejects when every chunk fails', async () => {
    vi.stubGlobal('fetch', async () => new Response('x', { status: 500 }))
    await expect(fetchChunkPayloads(['/a', '/b'])).rejects.toThrow(/All 2 replay chunk/)
    vi.unstubAllGlobals()
  })

  it('rewrites absolute download-chunk proxy URLs to same-origin paths', async () => {
    const seen: string[] = []
    vi.stubGlobal('fetch', async (url: string) => {
      seen.push(url)
      return okJson('[]')
    })
    await fetchChunkPayloads(['http://api.example:8080/api/v1/replay/download-chunk?key=k&exp=1&sig=s'])
    expect(seen).toEqual(['/api/v1/replay/download-chunk?key=k&exp=1&sig=s'])
    vi.unstubAllGlobals()
  })
})
