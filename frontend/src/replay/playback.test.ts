import { describe, expect, it } from 'vitest'
import type { MobileFrame, ReplayModel } from './model'
import {
  activeFrameAt,
  activeTouchAt,
  deviceClock,
  fallbackNotice,
  offsetFromPointer,
  routeStrip,
  TOUCH_WINDOW_MS,
} from './playback'

const f = (ts: number, screen: string): MobileFrame => ({ ts, screen, viewTree: { cls: 'UIView' } })

describe('activeFrameAt', () => {
  const frames = [f(1000, 'A'), f(2000, 'B'), f(3000, 'C')]
  it('returns the last frame at or before ts', () => {
    expect(activeFrameAt(frames, 2500)?.screen).toBe('B')
    expect(activeFrameAt(frames, 2000)?.screen).toBe('B')
  })
  it('returns the first frame before the first ts', () => {
    expect(activeFrameAt(frames, 0)?.screen).toBe('A')
  })
  it('returns null for no frames', () => {
    expect(activeFrameAt([], 0)).toBeNull()
  })
})

describe('activeTouchAt', () => {
  const touches = [{ ts: 1000, x: 1, y: 1 }, { ts: 5000, x: 2, y: 2 }]
  it('returns a touch within the window', () => {
    expect(activeTouchAt(touches, 1000 + TOUCH_WINDOW_MS)?.x).toBe(1)
    expect(activeTouchAt(touches, 5000 - TOUCH_WINDOW_MS)?.x).toBe(2)
  })
  it('returns null outside the window', () => {
    expect(TOUCH_WINDOW_MS).toBe(600)
    expect(activeTouchAt(touches, 1000 + TOUCH_WINDOW_MS + 1)).toBeNull()
  })
})

describe('fallbackNotice', () => {
  it('explains each none reason', () => {
    expect(fallbackNotice({ kind: 'none', reason: 'no-chunks' })).toBe(
      'No replay chunks were uploaded for this session.',
    )
    expect(fallbackNotice({ kind: 'none', reason: 'no-visual-data' })).toBe(
      'Chunks contain no DOM snapshot or view hierarchy.',
    )
    expect(fallbackNotice({ kind: 'none', reason: 'load-error', detail: 'boom' })).toBe('boom')
  })
  it('returns null for renderable models', () => {
    const web: ReplayModel = { kind: 'web', events: [], startMs: 0, endMs: 1 }
    expect(fallbackNotice(web)).toBeNull()
  })
})

describe('offsetFromPointer (scrubber seek)', () => {
  it('maps a pointer x inside the track to a playback offset', () => {
    expect(offsetFromPointer(150, { left: 100, width: 200 }, 10_000)).toBe(2_500)
  })
  it('clamps outside the track', () => {
    expect(offsetFromPointer(50, { left: 100, width: 200 }, 10_000)).toBe(0)
    expect(offsetFromPointer(999, { left: 100, width: 200 }, 10_000)).toBe(10_000)
  })
  it('returns 0 for a zero-width track', () => {
    expect(offsetFromPointer(150, { left: 100, width: 0 }, 10_000)).toBe(0)
  })
})

describe('deviceClock', () => {
  it('formats the recorded wall-clock time as HH:MM', () => {
    const ts = new Date(2026, 10, 9, 7, 5, 30).getTime()
    expect(deviceClock(ts)).toBe('07:05')
  })
  it('returns null for an invalid timestamp', () => {
    expect(deviceClock(Number.NaN)).toBeNull()
  })
})

describe('routeStrip', () => {
  it('shows the active mobile screen, with a readable name', () => {
    expect(routeStrip({ platform: 'ios', screen: 'UIHostingController<ModifiedContent<AnyView, RootModifier>>', recordedUrl: null })).toEqual({
      label: 'SCREEN',
      value: 'SwiftUI view',
    })
    expect(routeStrip({ platform: 'android', screen: 'CartActivity', recordedUrl: null })).toEqual({
      label: 'ACTIVITY',
      value: 'CartActivity',
    })
  })
  it('shows the recorded web URL', () => {
    expect(routeStrip({ platform: 'web', screen: null, recordedUrl: 'https://a.test/x' })).toEqual({
      label: 'URL',
      value: 'https://a.test/x',
    })
  })
  it('never invents a placeholder when nothing was recorded', () => {
    expect(routeStrip({ platform: 'web', screen: null, recordedUrl: null }).value).toBe('Not recorded')
    expect(routeStrip({ platform: 'ios', screen: null, recordedUrl: null }).value).toBe('Not recorded')
  })
  it('falls back to a recorded screen attribute for mobile without frames', () => {
    expect(routeStrip({ platform: 'ios', screen: null, recordedUrl: 'CheckoutScreen' }).value).toBe('CheckoutScreen')
  })
})
