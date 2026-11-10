import { describe, expect, it } from 'vitest'
import { ScreenIndex } from './screenIndex'
import type { ParsedScreenChunk } from './screen'

const parsed = (seq: number, frameTs: number[], touchTs: number[] = [], platform: 'android' | 'ios' | null = null): ParsedScreenChunk => ({
  seq,
  platform,
  frames: frameTs.map((ts, index) => ({
    ts,
    screen: 's',
    w: 10,
    h: 10,
    vw: 5,
    vh: 5,
    mime: 'image/jpeg' as const,
    chunkSeq: seq,
    index,
    masked: 0,
  })),
  touches: touchTs.map((ts) => ({ ts, x: 0, y: 0, kind: 'down' as const })),
})

describe('ScreenIndex', () => {
  it('merges out-of-order chunks into frames sorted by ts', () => {
    const idx = new ScreenIndex([0, 1, 2])
    idx.addChunk(parsed(2, [300, 400]))
    idx.addChunk(parsed(0, [100, 200]))
    idx.addChunk(parsed(1, [250]))
    expect(idx.frames.map((f) => f.ts)).toEqual([100, 200, 250, 300, 400])
  })

  it('keeps the frame of the higher chunk seq when two frames share a ts (Review Focus 4)', () => {
    const idx = new ScreenIndex([0, 1])
    idx.addChunk(parsed(0, [100, 200]))
    idx.addChunk(parsed(1, [200, 300]))
    expect(idx.frames.map((f) => [f.ts, f.chunkSeq])).toEqual([
      [100, 0],
      [200, 1],
      [300, 1],
    ])
  })

  it('addChunk is idempotent per seq', () => {
    const idx = new ScreenIndex([0])
    idx.addChunk(parsed(0, [100, 200]))
    idx.addChunk(parsed(0, [999]))
    expect(idx.frames.map((f) => f.ts)).toEqual([100, 200])
    expect(idx.ranges()).toEqual([{ seq: 0, startMs: 100, endMs: 200 }])
  })

  it('reports start, end, touches and platform; an empty index is zeroed', () => {
    const empty = new ScreenIndex([0])
    expect([empty.startMs, empty.endMs, empty.platform, empty.frames.length]).toEqual([0, 0, null, 0])
    expect(empty.isResolved(5)).toBe(false)

    const idx = new ScreenIndex([0, 1])
    idx.addChunk(parsed(1, [300], [350], 'ios'))
    idx.addChunk(parsed(0, [100], [50]))
    expect(idx.startMs).toBe(100)
    expect(idx.endMs).toBe(350)
    expect(idx.touches.map((t) => t.ts)).toEqual([50, 350])
    expect(idx.platform).toBe('ios')
  })

  it('isResolved is false in a gap between non-adjacent chunks and true between adjacent ones', () => {
    const idx = new ScreenIndex([0, 1, 2, 3, 4])
    idx.addChunk(parsed(0, [0, 100]))
    idx.addChunk(parsed(3, [300, 400]))
    expect(idx.isResolved(50)).toBe(true) // inside chunk 0
    expect(idx.isResolved(150)).toBe(false) // gap, chunks 1-2 unknown
    idx.addChunk(parsed(1, [110, 200]))
    expect(idx.isResolved(105)).toBe(true) // between chunk 0 and its adjacent chunk 1
    expect(idx.isResolved(250)).toBe(false) // between chunk 1 and chunk 3
  })

  it('isResolved after the last known chunk only when it is the session\'s last seq', () => {
    const idx = new ScreenIndex([0, 1, 2])
    idx.addChunk(parsed(0, [0, 100]))
    expect(idx.isResolved(500)).toBe(false)
    idx.addChunk(parsed(2, [200, 300]))
    expect(idx.isResolved(500)).toBe(true)
  })

  it('isResolved before the first known chunk only when it is the session\'s first seq', () => {
    const idx = new ScreenIndex([0, 1])
    idx.addChunk(parsed(1, [100, 200]))
    expect(idx.isResolved(50)).toBe(false)
    idx.addChunk(parsed(0, [10, 90]))
    expect(idx.isResolved(5)).toBe(true)
  })

  it('a skipped (non-screen) chunk does not break adjacency', () => {
    const idx = new ScreenIndex([0, 1, 2])
    idx.addChunk(parsed(0, [0, 100]))
    idx.addChunk(parsed(2, [200, 300]))
    expect(idx.isResolved(150)).toBe(false)
    idx.markSkipped(1)
    expect(idx.isResolved(150)).toBe(true)
  })
})
