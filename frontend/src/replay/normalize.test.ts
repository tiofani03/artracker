import { describe, expect, it } from 'vitest'
import type { eventWithTime } from 'rrweb'
import type { TelemetryEvent } from '../api'
import { buildReplayModel, extractMobileFrames, normalizeWebEvents } from './normalize'

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

const tree = (label: string) => ({ cls: 'UIView', frame: [0, 0, 402, 874], text: label })

const tl = (
  name: string,
  ts: number,
  attributes: Record<string, unknown>,
): TelemetryEvent => ({
  id: `${name}-${ts}`,
  kind: 'event',
  name,
  level: '',
  message: '',
  session_id: 's',
  user_id: 'u',
  environment: 'production',
  release_version: '',
  event_timestamp: new Date(ts).toISOString(),
  attributes,
})

const mobileChunk = (extra: Record<string, unknown> = {}) => ({
  format: 'artracker-mobile-v1',
  platform: 'android',
  frames: [{ ts: 1000, screen: 'Home', view_tree: tree('home') }],
  touches: [{ ts: 1500, x: 10, y: 20 }],
  ...extra,
})

describe('extractMobileFrames', () => {
  it('reads frames, touches and platform from an artracker-mobile-v1 chunk', () => {
    const out = extractMobileFrames([mobileChunk()], [])
    expect(out.platform).toBe('android')
    expect(out.frames).toEqual([{ ts: 1000, screen: 'Home', viewTree: tree('home') }])
    expect(out.touches).toEqual([{ ts: 1500, x: 10, y: 20 }])
  })

  it('maps presentation and masked_count from chunk frames', () => {
    const out = extractMobileFrames(
      [mobileChunk({ frames: [{ ts: 1, screen: 'A', view_tree: tree('a'), presentation: 'modal', masked_count: 3 }] })],
      [],
    )
    expect(out.frames[0]).toMatchObject({ presentation: 'modal', maskedCount: 3 })
  })

  it('reads frames from telemetry view_tree attributes', () => {
    const out = extractMobileFrames([], [tl('screen.view', 2000, { screen: 'Detail', view_tree: tree('d'), platform: 'ios' })])
    expect(out.frames).toEqual([{ ts: 2000, screen: 'Detail', viewTree: tree('d') }])
    expect(out.platform).toBe('ios')
  })

  it('reads touches from telemetry tap events with x/y', () => {
    const out = extractMobileFrames([], [tl('touch.tap', 3000, { x: 5, y: 6 })])
    expect(out.touches).toEqual([{ ts: 3000, x: 5, y: 6, name: 'touch.tap' }])
  })

  it('ignores telemetry with x/y whose name is not a touch', () => {
    const out = extractMobileFrames([], [tl('scroll.end', 3000, { x: 5, y: 6 })])
    expect(out.touches).toEqual([])
  })

  it('de-duplicates a chunk frame and telemetry frame with the same ts and screen, chunk wins (Review Focus #2)', () => {
    const out = extractMobileFrames(
      [mobileChunk()],
      [tl('screen.view', 1000, { screen: 'Home', view_tree: tree('from-telemetry') })],
    )
    expect(out.frames).toHaveLength(1)
    expect(out.frames[0].viewTree).toEqual(tree('home'))
  })

  it('sorts frames by ts and ignores frames without a view tree', () => {
    const out = extractMobileFrames(
      [
        mobileChunk({
          frames: [
            { ts: 3000, screen: 'C', view_tree: tree('c') },
            { ts: 1000, screen: 'A', view_tree: tree('a') },
            { ts: 2000, screen: 'B' },
          ],
        }),
      ],
      [],
    )
    expect(out.frames.map((f) => f.screen)).toEqual(['A', 'C'])
  })

  it('ignores payloads with an unknown format', () => {
    const out = extractMobileFrames([mobileChunk({ format: 'something-else' })], [])
    expect(out.frames).toEqual([])
    expect(out.platform).toBeNull()
  })

  it('prefers chunk platform over telemetry platform', () => {
    const out = extractMobileFrames(
      [mobileChunk({ platform: 'android' })],
      [tl('screen.view', 5000, { screen: 'X', view_tree: tree('x'), platform: 'ios' })],
    )
    expect(out.platform).toBe('android')
  })
})

describe('buildReplayModel (mobile)', () => {
  it('returns a mobile model when only mobile frames exist', () => {
    const m = buildReplayModel({ payloads: [mobileChunk()], timeline: [] })
    expect(m).toMatchObject({ kind: 'mobile', platform: 'android', startMs: 1000, endMs: 1500 })
  })

  it('prefers web replay when both web events and mobile frames exist', () => {
    const m = buildReplayModel({ payloads: [[ev(4, 1), ev(2, 2)], mobileChunk()], timeline: [] })
    expect(m.kind).toBe('web')
  })

  it('falls back to projectPlatform, then ios, when no frame states a platform', () => {
    const t = [tl('screen.view', 1000, { screen: 'A', view_tree: tree('a') })]
    expect(buildReplayModel({ payloads: [], timeline: t, projectPlatform: 'android' })).toMatchObject({
      kind: 'mobile',
      platform: 'android',
    })
    expect(buildReplayModel({ payloads: [], timeline: t })).toMatchObject({ kind: 'mobile', platform: 'ios' })
  })

  it('builds a mobile model from telemetry alone even with no chunks', () => {
    const m = buildReplayModel({
      payloads: [],
      timeline: [tl('screen.view', 1000, { screen: 'A', view_tree: tree('a') }), tl('touch.tap', 4000, { x: 1, y: 1 })],
    })
    expect(m).toMatchObject({ kind: 'mobile', startMs: 1000, endMs: 4000 })
  })
})
