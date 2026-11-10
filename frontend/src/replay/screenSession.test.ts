import { describe, expect, it } from 'vitest'
import { ScreenReplaySession } from './screenSession'

const BASE = 1_700_000_000_000
const SEQS = [0, 1, 2, 3, 4]

const chunkPayload = (seq: number) => ({
  format: 'artracker-screen-v1',
  platform: 'android',
  seq,
  frames: Array.from({ length: 20 }, (_, k) => ({
    ts: BASE + seq * 10_000 + k * 500,
    screen: 'home',
    w: 10,
    h: 10,
    vw: 5,
    vh: 5,
    mime: 'image/jpeg',
    data: 'QUJD',
    masked: 0,
  })),
  touches: [],
})

function makeSession(opts: { failFirst?: number[] } = {}) {
  const calls: number[] = []
  const failed = new Set(opts.failFirst ?? [])
  let clock = 0
  const session = new ScreenReplaySession({
    seqs: SEQS,
    sessionStartMs: BASE,
    chunkCreatedAtMs: new Map(SEQS.map((s) => [s, BASE + (s + 1) * 10_000 + 500])),
    fetchChunk: async (seq: number) => {
      calls.push(seq)
      if (failed.has(seq)) {
        failed.delete(seq)
        return null
      }
      return chunkPayload(seq)
    },
    now: () => clock,
  })
  return { session, calls, advance: (ms: number) => (clock += ms) }
}

describe('ScreenReplaySession', () => {
  it('start() indexes every chunk and notifies subscribers', async () => {
    const { session } = makeSession()
    let notified = 0
    session.subscribe(() => notified++)
    await session.start()
    expect(session.index.frames).toHaveLength(100)
    expect(session.index.ranges()).toHaveLength(5)
    expect(notified).toBeGreaterThanOrEqual(5)
    expect(session.index.platform).toBe('android')
  })

  it('frameImage returns data only for resident chunks and requests the others', async () => {
    const { session, calls } = makeSession()
    await session.start()
    const inWindow = session.index.frames[0] // chunk 0, window around the start keeps chunks 0..3
    const outside = session.index.frames.find((f) => f.chunkSeq === 4)!
    expect(session.frameImage(inWindow)).toBe('QUJD')
    expect(session.frameImage(outside)).toBeNull()
    await session.whenIdle()
    expect(calls.filter((s) => s === 4).length).toBeGreaterThanOrEqual(2) // scan + the on-demand request
    expect(session.frameImage(outside)).toBe('QUJD')
  })

  it('setPlayhead keeps only the window resident, never the whole session', async () => {
    const { session } = makeSession()
    await session.start()
    session.setPlayhead(BASE + 25_000)
    await session.whenIdle()
    expect(session.residentSeqs()).toEqual([1, 2, 3, 4])
    session.setPlayhead(BASE)
    await session.whenIdle()
    expect(session.residentSeqs()).toEqual([0, 1, 2, 3])
    expect(session.residentSeqs().length).toBeLessThan(SEQS.length)
  })

  it('setPlayhead into an unscanned region fetches the estimated chunk and resolves it (Review Focus 2)', async () => {
    const { session, calls } = makeSession()
    expect(session.index.isResolved(BASE + 45_000)).toBe(false)
    session.setPlayhead(BASE + 45_000)
    await session.whenIdle()
    expect(calls).toContain(4)
    expect(session.index.isResolved(BASE + 45_000)).toBe(true)
  })

  it('skips a chunk whose fetch fails and retries it on a later setPlayhead', async () => {
    const { session, calls, advance } = makeSession({ failFirst: [2] })
    await session.start()
    expect(session.index.ranges().map((r) => r.seq)).toEqual([0, 1, 3, 4])

    session.setPlayhead(BASE + 22_000)
    await session.whenIdle()
    expect(calls.filter((s) => s === 2)).toHaveLength(1) // inside the retry backoff: no new request
    expect(session.index.isResolved(BASE + 22_000)).toBe(false)

    advance(3_000)
    session.setPlayhead(BASE + 22_000)
    await session.whenIdle()
    expect(calls.filter((s) => s === 2)).toHaveLength(2)
    expect(session.index.isResolved(BASE + 22_000)).toBe(true)
  })

  it('treats a non-screen chunk as skipped so it does not leave a permanent gap', async () => {
    const session = new ScreenReplaySession({
      seqs: [0, 1, 2],
      sessionStartMs: BASE,
      chunkCreatedAtMs: new Map([[0, BASE + 10_500], [1, BASE + 20_500], [2, BASE + 30_500]]),
      fetchChunk: async (seq: number) => (seq === 1 ? [{ type: 4, timestamp: BASE, data: {} }] : chunkPayload(seq)),
    })
    await session.start()
    expect(session.index.ranges().map((r) => r.seq)).toEqual([0, 2])
    expect(session.index.isResolved(BASE + 15_000)).toBe(true)
  })
})
