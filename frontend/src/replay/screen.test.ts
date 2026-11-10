import { describe, expect, it } from 'vitest'
import {
  activeScreenFrameAt,
  isScreenPayload,
  isScreenSession,
  parseScreenChunk,
  screenFrameData,
  touchMarkerAt,
  type ScreenFrameMeta,
  type ScreenTouch,
} from './screen'

const frame = (ts: number, over: Record<string, unknown> = {}) => ({
  ts,
  screen: 'home',
  w: 720,
  h: 1600,
  vw: 360,
  vh: 800,
  mime: 'image/jpeg',
  data: 'QUJD',
  masked: 0,
  ...over,
})
const chunk = (frames: unknown[], touches: unknown[] = [], over: Record<string, unknown> = {}) => ({
  format: 'artracker-screen-v1',
  platform: 'android',
  seq: 0,
  frames,
  touches,
  ...over,
})

const meta = (ts: number, chunkSeq = 0, index = 0): ScreenFrameMeta => ({
  ts,
  screen: 'home',
  w: 720,
  h: 1600,
  vw: 360,
  vh: 800,
  mime: 'image/jpeg',
  chunkSeq,
  index,
  masked: 0,
})

describe('parseScreenChunk', () => {
  it('maps valid frames and keeps the original index', () => {
    const parsed = parseScreenChunk(chunk([frame(100), frame(NaN), frame(300, { screen: 'detail', masked: 2 })]), 7)
    expect(parsed?.seq).toBe(7)
    expect(parsed?.platform).toBe('android')
    expect(parsed?.frames.map((f) => [f.ts, f.index, f.chunkSeq, f.screen, f.masked])).toEqual([
      [100, 0, 7, 'home', 0],
      [300, 2, 7, 'detail', 2],
    ])
  })

  it('skips frames with wrong mime, zero size, NaN ts or empty data (Review Focus 4)', () => {
    const parsed = parseScreenChunk(
      chunk([
        frame(1, { mime: 'image/png' }),
        frame(2, { w: 0 }),
        frame(3, { vh: -1 }),
        frame(NaN),
        frame(5, { data: '' }),
        frame(6, { data: 42 }),
        frame(7),
      ]),
      0,
    )
    expect(parsed?.frames.map((f) => f.ts)).toEqual([7])
  })

  it('returns null for non-screen payloads and for chunks with nothing usable', () => {
    expect(parseScreenChunk({ format: 'artracker-mobile-v1', frames: [frame(1)] }, 0)).toBeNull()
    expect(parseScreenChunk([{ type: 4 }], 0)).toBeNull()
    expect(parseScreenChunk(null, 0)).toBeNull()
    expect(parseScreenChunk(chunk([frame(1, { mime: 'image/png' })]), 0)).toBeNull()
  })

  it('keeps valid touches sorted and drops invalid ones; a touch-only chunk is kept', () => {
    const parsed = parseScreenChunk(
      chunk([], [
        { ts: 20, x: 1, y: 2, kind: 'up' },
        { ts: 10, x: 1, y: 2, kind: 'down' },
        { ts: 15, x: 1, y: 2, kind: 'swipe' },
        { ts: NaN, x: 1, y: 2, kind: 'move' },
        { ts: 18, x: 'a', y: 2, kind: 'move' },
      ]),
      3,
    )
    expect(parsed?.frames).toEqual([])
    expect(parsed?.touches.map((t) => [t.ts, t.kind])).toEqual([
      [10, 'down'],
      [20, 'up'],
    ])
  })

  it('reports null platform when the chunk names none', () => {
    expect(parseScreenChunk(chunk([frame(1)], [], { platform: undefined }), 0)?.platform).toBeNull()
    expect(parseScreenChunk(chunk([frame(1)], [], { platform: 'ios' }), 0)?.platform).toBe('ios')
  })
})

describe('screenFrameData', () => {
  it('returns the base64 string for a valid index and null otherwise', () => {
    const p = chunk([frame(1, { data: 'AAAA' }), frame(2, { mime: 'image/png' })])
    expect(screenFrameData(p, 0)).toBe('AAAA')
    expect(screenFrameData(p, 1)).toBeNull()
    expect(screenFrameData(p, 9)).toBeNull()
    expect(screenFrameData({ format: 'x' }, 0)).toBeNull()
  })
})

describe('isScreenSession', () => {
  const rrweb = [{ type: 4, timestamp: 1, data: {} }]
  const screen = chunk([frame(1)])
  const mobile = { format: 'artracker-mobile-v1', platform: 'ios', frames: [] }

  it('is true when any probe is a screen payload (Review Focus 1)', () => {
    expect(isScreenSession([rrweb, screen])).toBe(true)
    expect(isScreenSession([screen, rrweb])).toBe(true)
  })

  it('is false for rrweb / wireframe sessions', () => {
    expect(isScreenSession([rrweb, mobile])).toBe(false)
    expect(isScreenSession([])).toBe(false)
    expect(isScreenPayload(null)).toBe(false)
  })
})

describe('activeScreenFrameAt', () => {
  const frames = [meta(100), meta(200), meta(300)]
  it('returns the greatest ts <= T', () => {
    expect(activeScreenFrameAt(frames, 100)?.ts).toBe(100)
    expect(activeScreenFrameAt(frames, 199)?.ts).toBe(100)
    expect(activeScreenFrameAt(frames, 200)?.ts).toBe(200)
  })
  it('is null before the first frame and the last frame after the end', () => {
    expect(activeScreenFrameAt(frames, 99)).toBeNull()
    expect(activeScreenFrameAt(frames, 99999)?.ts).toBe(300)
    expect(activeScreenFrameAt([], 5)).toBeNull()
  })
})

describe('touchMarkerAt', () => {
  const view = { vw: 360, vh: 800 }
  const canvas = { width: 180, height: 400 }
  const t = (ts: number, x: number, y: number, kind: ScreenTouch['kind']): ScreenTouch => ({ ts, x, y, kind })

  it('scales vw/vh coordinates to the canvas', () => {
    expect(touchMarkerAt([t(1000, 180, 400, 'down')], 1000, view, canvas)).toEqual({ x: 90, y: 200, kind: 'down' })
  })

  it('interpolates a move between two samples of one gesture', () => {
    const touches = [t(1000, 0, 0, 'down'), t(1200, 100, 200, 'move'), t(1400, 200, 400, 'up')]
    const m = touchMarkerAt(touches, 1100, view, canvas)
    expect(m).toEqual({ x: 25, y: 50, kind: 'move' })
  })

  it('returns null outside TOUCH_WINDOW_MS (600 ms) and for no touches', () => {
    const touches = [t(1000, 10, 10, 'down')]
    expect(touchMarkerAt(touches, 1601, view, canvas)).toBeNull()
    expect(touchMarkerAt(touches, 399, view, canvas)).toBeNull()
    expect(touchMarkerAt([], 1000, view, canvas)).toBeNull()
  })
})
