import { describe, expect, it } from 'vitest'
import type { eventWithTime } from 'rrweb'
import { buildReplayModel, normalizeWebEvents } from './normalize'

// rrweb EventType: 2 = FullSnapshot, 3 = IncrementalSnapshot, 4 = Meta
const ev = (type: number, timestamp: number, data: unknown = {}) =>
  ({ type, timestamp, data }) as unknown as eventWithTime

describe('normalizeWebEvents', () => {
  it('drops events before the first Meta that is followed by a FullSnapshot', () => {
    const out = normalizeWebEvents([ev(3, 100), ev(4, 200), ev(2, 201), ev(3, 300)])
    expect(out?.map((e) => [e.type, e.timestamp])).toEqual([
      [4, 200],
      [2, 201],
      [3, 300],
    ])
  })

  it('sorts events by timestamp', () => {
    const out = normalizeWebEvents([ev(3, 300), ev(2, 201), ev(4, 200)])
    expect(out?.map((e) => e.timestamp)).toEqual([200, 201, 300])
  })

  it('removes exact duplicates', () => {
    const out = normalizeWebEvents([ev(4, 200), ev(2, 201), ev(3, 300, { a: 1 }), ev(3, 300, { a: 1 })])
    expect(out).toHaveLength(3)
  })

  it('keeps events with same timestamp but different data', () => {
    const out = normalizeWebEvents([ev(4, 200), ev(2, 201), ev(3, 300, { a: 1 }), ev(3, 300, { a: 2 })])
    expect(out).toHaveLength(4)
  })

  it('starts at a later snapshot when the first chunk is missing (Review Focus #1)', () => {
    const out = normalizeWebEvents([ev(3, 50), ev(3, 60), ev(4, 1000), ev(2, 1001), ev(3, 1500)])
    expect(out?.[0].timestamp).toBe(1000)
    expect(out).toHaveLength(3)
  })

  it('returns null when there is no FullSnapshot', () => {
    expect(normalizeWebEvents([ev(4, 1), ev(3, 2)])).toBeNull()
  })

  it('returns null when fewer than 2 events remain', () => {
    expect(normalizeWebEvents([ev(2, 1)])).toBeNull()
  })
})

describe('buildReplayModel (web)', () => {
  it('builds a web model from a bare rrweb array payload', () => {
    const m = buildReplayModel({ payloads: [[ev(4, 1000), ev(2, 1001), ev(3, 5000)]], timeline: [] })
    expect(m.kind).toBe('web')
    if (m.kind !== 'web') return
    expect(m.startMs).toBe(1000)
    expect(m.endMs).toBe(5000)
    expect(m.events).toHaveLength(3)
  })

  it('accepts {events:[...]} payloads and merges chunks', () => {
    const m = buildReplayModel({
      payloads: [{ events: [ev(4, 1000), ev(2, 1001)] }, [ev(3, 2000)]],
      timeline: [],
    })
    expect(m.kind).toBe('web')
    if (m.kind !== 'web') return
    expect(m.events).toHaveLength(3)
    expect(m.endMs).toBe(2000)
  })

  it('returns none/no-chunks when there are no payloads', () => {
    expect(buildReplayModel({ payloads: [], timeline: [] })).toEqual({ kind: 'none', reason: 'no-chunks' })
  })

  it('returns none/no-visual-data when payloads hold no snapshot', () => {
    expect(buildReplayModel({ payloads: [[ev(3, 1)]], timeline: [] })).toEqual({
      kind: 'none',
      reason: 'no-visual-data',
    })
  })

  it('ignores null payloads (unreadable chunks)', () => {
    const m = buildReplayModel({ payloads: [null, [ev(4, 1), ev(2, 2)]], timeline: [] })
    expect(m.kind).toBe('web')
  })
})
