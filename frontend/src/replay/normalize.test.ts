import { describe, expect, it } from 'vitest'
import type { eventWithTime } from 'rrweb'
import type { TelemetryEvent } from '../api'
import { buildReplayModel, extractMobileFrames, flattenNestedAdds, normalizeWebEvents } from './normalize'

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

describe('extractMobileFrames chunk/telemetry overlap (final review #1)', () => {
  it('drops telemetry frames inside the chunk-covered range even when ts and screen differ', () => {
    // ingest clock correction shifts telemetry ts away from raw device ts
    const chunk = mobileChunk({
      frames: [
        { ts: 10_000, screen: 'Home', view_tree: tree('home') },
        { ts: 20_000, view_tree: tree('cart') }, // no screen name in chunk
      ],
    })
    const out = extractMobileFrames(
      [chunk],
      [
        tl('screen.view', 10_350, { screen: 'Home', view_tree: tree('home-tel') }),
        tl('screen.view', 19_800, { screen: 'Cart', view_tree: tree('cart-tel') }),
      ],
    )
    expect(out.frames.map((f) => f.ts)).toEqual([10_000, 20_000])
    expect(out.frames.map((f) => f.viewTree)).toEqual([tree('home'), tree('cart')])
  })

  it('keeps telemetry frames outside the chunk-covered range', () => {
    const chunk = mobileChunk({ frames: [{ ts: 10_000, screen: 'Home', view_tree: tree('home') }] })
    const out = extractMobileFrames([chunk], [tl('screen.view', 30_000, { screen: 'Later', view_tree: tree('later') })])
    expect(out.frames.map((f) => f.screen)).toEqual(['Home', 'Later'])
  })

  it('uses all telemetry frames when no chunk frames exist', () => {
    const out = extractMobileFrames(
      [],
      [
        tl('screen.view', 1000, { screen: 'A', view_tree: tree('a') }),
        tl('screen.view', 1200, { screen: 'B', view_tree: tree('b') }),
      ],
    )
    expect(out.frames).toHaveLength(2)
  })

  it('accepts legacy mobile chunks without format property', () => {
    const legacy = {
      platform: 'ios',
      frames: [{ ts: 5000, screen: 'Profile', view_tree: tree('profile') }],
    }
    const out = extractMobileFrames([legacy], [])
    expect(out.platform).toBe('ios')
    expect(out.frames).toHaveLength(1)
    expect(out.frames[0].screen).toBe('Profile')
  })
})

describe('buildReplayModel with synthetic mobile rrweb events', () => {
  it('infers android platform and viewport from Meta event and injects wireframe styles into head', () => {
    const meta = ev(4, 1000, { href: 'android://MainActivity', width: 320, height: 716 })
    const fullSnapshot = ev(2, 1001, {
      node: {
        type: 0,
        id: 1,
        childNodes: [
          {
            type: 2,
            id: 2,
            tagName: 'html',
            childNodes: [
              { type: 2, id: 3, tagName: 'head', childNodes: [] },
              {
                type: 2,
                id: 4,
                tagName: 'body',
                childNodes: [
                  {
                    type: 2,
                    id: 5,
                    tagName: 'div',
                    attributes: {
                      'data-artracker-type': 'BUTTON',
                      style: 'position:absolute;left:10px;top:20px;width:100px;height:40px;',
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    })

    const m = buildReplayModel({ payloads: [[meta, fullSnapshot]], timeline: [] })
    expect(m.kind).toBe('web')
    if (m.kind !== 'web') return

    expect(m.inferredPlatform).toBe('android')
    expect(m.viewport).toEqual({ width: 320, height: 716 })

    const snapshot = m.events.find((e) => e.type === 2) as any
    const htmlNode = snapshot.data.node.childNodes[0]
    const headNode = htmlNode.childNodes.find((c: any) => c.tagName === 'head')
    expect(headNode.childNodes.length).toBeGreaterThan(0)
    const styleChild = headNode.childNodes[0]
    expect(styleChild.tagName).toBe('style')
    expect(styleChild.attributes['data-artracker-injected']).toBe('true')
  })

  it('eliminates ghosting when two large container screens overlap in full snapshot', () => {
    const meta = ev(4, 1000, { href: 'android://lab', width: 320, height: 716 })
    const fullSnapshot = ev(2, 1001, {
      node: {
        type: 0,
        id: 1,
        childNodes: [
          {
            type: 2,
            id: 2,
            tagName: 'html',
            childNodes: [
              { type: 2, id: 3, tagName: 'head', childNodes: [] },
              {
                type: 2,
                id: 4,
                tagName: 'body',
                childNodes: [
                  {
                    type: 2,
                    id: 16,
                    tagName: 'div',
                    childNodes: [
                      {
                        type: 2,
                        id: 21,
                        tagName: 'div',
                        attributes: {
                          'data-artracker-type': 'CONTAINER',
                          style: 'position:absolute;left:0px;top:160px;width:320px;height:432px;overflow:hidden;',
                        },
                      },
                      {
                        type: 2,
                        id: 54,
                        tagName: 'div',
                        attributes: {
                          'data-artracker-type': 'CONTAINER',
                          style: 'position:absolute;left:0px;top:160px;width:320px;height:432px;overflow:hidden;',
                        },
                      },
                    ],
                  },
                ],
              },
            ],
          },
        ],
      },
    })

    const m = buildReplayModel({ payloads: [[meta, fullSnapshot]], timeline: [] })
    if (m.kind !== 'web') throw new Error('expected web model')

    const snapshot = m.events.find((e) => e.type === 2) as any
    const body = snapshot.data.node.childNodes[0].childNodes.find((c: any) => c.tagName === 'body')
    const parent = body.childNodes[0]
    const exiting = parent.childNodes[0]
    const entering = parent.childNodes[1]

    expect(exiting.attributes.style).toContain('display:none')
    expect(entering.attributes.style).toContain('background-color:#fafafa')
  })
})

// --- nested rrweb adds (Android SDK serialises whole subtrees inside one `adds` entry) ---

type TestNode = { type: number; id: number; tagName?: string; textContent?: string; attributes?: object; childNodes?: TestNode[] }
type TestAdd = { parentId: number; nextId: number | null; node: TestNode }

const el = (id: number, childNodes: TestNode[] = []): TestNode => ({ type: 2, id, tagName: 'div', attributes: {}, childNodes })
const txt = (id: number, textContent: string): TestNode => ({ type: 3, id, textContent })
const mutation = (ts: number, adds: TestAdd[]) => ev(3, ts, { source: 0, texts: [], attributes: [], removes: [], adds })
const addsOf = (e: eventWithTime) => (e.data as { adds: TestAdd[] }).adds

describe('flattenNestedAdds', () => {
  it('splits a nested add into pre-order flat adds', () => {
    const nested = el(87, [txt(88, 'x'), el(89, [txt(90, 'y')])])
    const out = flattenNestedAdds([mutation(10, [{ parentId: 16, nextId: 48, node: nested }])])
    const adds = addsOf(out[0])

    expect(adds.map((a) => a.node.id)).toEqual([87, 88, 89, 90])
    expect(adds.map((a) => [a.parentId, a.nextId])).toEqual([
      [16, 48],
      [87, null],
      [87, null],
      [89, null],
    ])
    for (const a of adds) {
      if (a.node.type === 2) expect(a.node.childNodes).toEqual([])
    }
    expect(adds[1].node.textContent).toBe('x')
  })

  it('returns the same event object when nothing is nested, and leaves other events alone', () => {
    const flat = mutation(10, [{ parentId: 16, nextId: null, node: el(87) }])
    const full = ev(2, 5, { node: { type: 0, id: 1, childNodes: [el(2, [el(3)])] } })
    const touch = ev(3, 20, { source: 2, type: 7, id: 4, x: 1, y: 2 })
    const out = flattenNestedAdds([full, flat, touch])
    expect(out[0]).toBe(full)
    expect(out[1]).toBe(flat)
    expect(out[2]).toBe(touch)
  })

  it('keeps the text of a node added together with its text child (session 01a1c339 shape)', () => {
    const out = flattenNestedAdds([
      mutation(14637, [{ parentId: 105, nextId: null, node: el(120, [txt(121, 'Item #4')]) }]),
    ])
    const adds = addsOf(out[0])
    expect(adds.map((a) => [a.node.id, a.parentId])).toEqual([
      [120, 105],
      [121, 120],
    ])
    expect(adds[1].node.textContent).toBe('Item #4')
  })

  it('does not mutate its input', () => {
    const nested = el(87, [txt(88, 'x')])
    flattenNestedAdds([mutation(10, [{ parentId: 16, nextId: null, node: nested }])])
    expect(nested.childNodes).toHaveLength(1)
  })
})

describe('normalizeWebEvents with nested adds', () => {
  it('flattens nested adds in its result', () => {
    const out = normalizeWebEvents([
      ev(4, 200),
      ev(2, 201),
      mutation(300, [{ parentId: 16, nextId: null, node: el(87, [txt(88, 'x')]) }]),
    ])
    const mut = out?.find((e) => e.type === 3)
    expect(addsOf(mut!).map((a) => a.node.id)).toEqual([87, 88])
    expect(addsOf(mut!).every((a) => a.node.type !== 2 || a.node.childNodes?.length === 0)).toBe(true)
  })
})

describe('buildReplayModel with artracker-screen-v1 payloads', () => {
  const screenChunk = {
    format: 'artracker-screen-v1',
    platform: 'android',
    frames: [{ ts: 1, screen: 's', w: 1, h: 1, vw: 1, vh: 1, mime: 'image/jpeg', data: 'QUJD', masked: 0 }],
    touches: [],
  }
  it('never misreads a screen chunk as wireframe frames (screen sessions are detected by the player)', () => {
    expect(buildReplayModel({ payloads: [screenChunk], timeline: [] })).toEqual({ kind: 'none', reason: 'no-visual-data' })
  })
  it('still builds the web model when an rrweb chunk sits next to a screen chunk', () => {
    const m = buildReplayModel({ payloads: [screenChunk, [ev(4, 1000), ev(2, 1001)]], timeline: [] })
    expect(m.kind).toBe('web')
  })
})
