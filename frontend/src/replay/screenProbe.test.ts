import { describe, expect, it, vi } from 'vitest'
import type { ReplayChunkDetail } from '../api'
import { probeScreenSession } from './screenProbe'

const detail = (seq: number, url: string | undefined = `https://x/${seq}`): ReplayChunkDetail => ({
  chunk_seq: seq,
  object_key: `k${seq}`,
  size_bytes: 1,
  event_count: 1,
  created_at: '2026-11-10T02:00:00Z',
  url,
})

const screen = (seq: number) => ({
  format: 'artracker-screen-v1',
  platform: 'android',
  seq,
  frames: [{ ts: 1000 + seq, screen: 's', w: 10, h: 10, vw: 5, vh: 5, mime: 'image/jpeg', data: 'QUJD', masked: 0 }],
  touches: [],
})
const rrweb = [{ type: 4, timestamp: 1, data: {} }]

function probe(chunks: ReplayChunkDetail[], payloads: Record<number, unknown>) {
  const fetchChunk = vi.fn(async (seq: number) => (seq in payloads ? payloads[seq] : null))
  return {
    fetchChunk,
    run: () => probeScreenSession({ chunks, sessionStartMs: 1000, fetchChunk }),
  }
}

describe('probeScreenSession', () => {
  it('returns a session when the first chunk is a screen chunk', async () => {
    const { run, fetchChunk } = probe([detail(0), detail(1), detail(2)], { 0: screen(0), 2: screen(2) })
    expect(await run()).not.toBeNull()
    expect(fetchChunk.mock.calls.map((c) => c[0]).sort()).toEqual([0, 2]) // only first and last are probed
  })

  it('returns a session for a mixed session: wireframe first, screen last (Review Focus 1)', async () => {
    const { run } = probe([detail(0), detail(1), detail(2)], { 0: rrweb, 2: screen(2) })
    expect(await run()).not.toBeNull()
  })

  it('returns a session for the reverse mix: screen first, wireframe last', async () => {
    const { run } = probe([detail(0), detail(1)], { 0: screen(0), 1: rrweb })
    expect(await run()).not.toBeNull()
  })

  it('returns null for a pure rrweb / wireframe session', async () => {
    const { run } = probe([detail(0), detail(1)], { 0: rrweb, 1: rrweb })
    expect(await run()).toBeNull()
  })

  it('returns null when the probed chunks are unreadable', async () => {
    const { run } = probe([detail(0), detail(1)], {})
    expect(await run()).toBeNull()
  })

  it('fetches a single-chunk session once', async () => {
    const { run, fetchChunk } = probe([detail(0)], { 0: screen(0) })
    expect(await run()).not.toBeNull()
    expect(fetchChunk).toHaveBeenCalledTimes(1)
  })

  it('ignores chunks without a download url and returns null when none is fetchable', async () => {
    const { run, fetchChunk } = probe([detail(0, ''), detail(1, '   ')], { 0: screen(0) })
    expect(await run()).toBeNull()
    expect(fetchChunk).not.toHaveBeenCalled()
  })
})
